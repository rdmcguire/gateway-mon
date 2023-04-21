package main

import (
	"flag"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/sirupsen/logrus"
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

// List of networks to be deleted or
// added to the interface
type netList struct {
	items []string
	nets  []*net.IPNet
}

var (
	logLevel     string = "info"
	log          *logrus.Logger
	matchRoute   *netlink.Route
	delNets      netList // Routed networks to delete (in addition to default)
	addNets      netList // Routes to add to interface if missing (or deleted by a -del)
	linkName     string  = "gpd0"
	delDefaultGw bool
)

func init() {
	// Flags
	flag.StringVar(&logLevel, "logLevel", logLevel, "Default Log Level")
	flag.StringVar(&linkName, "linkName", linkName, "Name of interface to monitor routes for")
	flag.BoolVar(&delDefaultGw, "delDefaultGw", delDefaultGw, "Delete Default Gateway")
	flag.Var(&delNets, "del", "Extra destination net to delete (can be used more than once)")
	flag.Var(&addNets, "add", "Routed networks to create (can be used more than once)")
	flag.Parse()

	// Logging
	log = logrus.New()
	level, err := logrus.ParseLevel(logLevel)
	if err != nil {
		level = logrus.InfoLevel
	}
	log.SetLevel(level)

	if len(delNets.items) > 0 {
		log.Infof("Deleting Net Routes: %s", delNets.String())
	}
	if len(addNets.items) > 0 {
		log.Infof("Adding Net Routes: %s", addNets.String())
	}

	if delDefaultGw {
		log.Info("Deleting default gateways")
	}

	if len(delNets.items) == 0 && !delDefaultGw {
		log.Fatalf("I'm useless, nothing to delete, set one or more -del subnets or -delDefaultGw")
		os.Exit(1)
	}
}

// Slice of networks
// Stringer returns list of parsed CIDRs
func (l *netList) String() string {
	return strings.Join(l.items, ", ")
}

// Addes item to the list and also parses
// the subnet, returning err if invalid
func (l *netList) Set(val string) error {
	_, net, err := net.ParseCIDR(val)
	if err != nil {
		return err
	}
	// Add value and network
	l.items = append(l.items, val)
	l.nets = append(l.nets, net)
	return nil
}

func main() {
	routeUpdates := make(chan netlink.RouteUpdate)
	routeSubscrDone := make(chan struct{})
	killed := make(chan os.Signal)

	err := netlink.RouteSubscribe(routeUpdates, routeSubscrDone)
	defer close(routeSubscrDone)

	if err != nil {
		log.Fatalf("Failed to subscribe to netlink route monitor")
	}

	log.Infof("Receiving Route Updates from Netlink...")

	// Handle signals
	signal.Notify(killed, os.Interrupt, os.Kill, syscall.SIGTERM, syscall.SIGABRT)

	// First-time run
	addRoutesIfMissing()
	delUnwantedRoutes()

	for {
		select {
		case update := <-routeUpdates:
			route := update.Route
			if update.Type == unix.RTM_NEWROUTE {
				// If it's a new route, check it
				log.Debugf("Route Added: %+v", route)

				// Check interface
				attrs, err := getLinkAttrs(route.LinkIndex)
				if err != nil {
					log.Errorf("Route added to unknown interface: %+v", err)
					break
				}
				if attrs.Name == linkName {
					log.Infof("Route added to %s: %s", attrs.Name, route.String())
					log.Debugf("Route: %+v", route)

					delIfDefault(&route)    // Delete this route if it's a default route
					delIfExtraRoute(&route) // Delete this route if it's unwanted
				}
			} else if update.Type == unix.RTM_DELROUTE {
				// If a route was removed, ensure desired routes
				addRoutesIfMissing()
			}

		// Handle signal
		case <-killed:
			log.Printf("Cleaning up and dying")
			return
		}
	}

}

// Scans through the link's routes, deleting any route that
// is set to delete in -del or -delDefaultGw. Typically only
// run on startup
func delUnwantedRoutes() {
	// Retrieve the link
	link, err := netlink.LinkByName(linkName)
	if err != nil {
		log.Errorf("Can't delete routes, link %s not found yet: %+v", linkName, err)
		return
	}

	// Retrieve its routes
	linkRoutes, err := netlink.RouteList(link, 4)
	if err != nil || len(linkRoutes) == 0 {
		log.Errorf("Can't delete routes, no routes found on %s: %+v", linkName, err)
		return
	}

	// Delete unwanted routes
	for _, r := range linkRoutes {
		delIfExtraRoute(&r)
		delIfDefault(&r)
	}
}

// Scans through the link's routes, and adds in any requested
// routes that are missing. Chooses the most common next-hop, and aborts
// if there are no routes or if the link doesn't exist
func addRoutesIfMissing() {
	if len(addNets.nets) == 0 {
		log.Debugf("No routes to add, skipping...")
		return
	}

	// Retrieve the link
	link, err := netlink.LinkByName(linkName)
	if err != nil {
		log.Errorf("Can't add routes, link %s not found yet: %+v", linkName, err)
		return
	}

	// Retrieve its routes
	linkRoutes, err := netlink.RouteList(link, 4)
	if err != nil || len(linkRoutes) == 0 {
		log.Errorf("Can't add routes, no routes found on %s: %+v", linkName, err)
		return
	}

	// Find next-hop
	nextHop := getLinkNextHop(linkRoutes)

	// Add routes
	if nextHop != nil {
		for _, net := range addNets.nets {
			newRoute := &netlink.Route{
				LinkIndex: link.Attrs().Index,
				Dst:       net,
				Gw:        *nextHop,
			}
			if err := netlink.RouteAdd(newRoute); err != nil {
				log.WithFields(logrus.Fields{
					"link":  linkName,
					"route": newRoute,
					"error": err,
				})
			} else {
				log.WithFields(logrus.Fields{
					"link": linkName,
					"to":   newRoute.Dst.String(),
					"via":  nextHop.String(),
				}).Infof("Added route to link")
			}
		}
	}
}

// Given a list of routes, returns the most commonly used Gw address
func getLinkNextHop(routes []netlink.Route) *net.IP {
	var nextHop *net.IP
	// Record gateway occurrences
	gateways := make(map[*net.IP]int)
	for _, r := range routes {
		gateways[&r.Gw] += 1
	}
	// Return most common
	var max int
	for gw, i := range gateways {
		if i > max {
			nextHop = gw
		}
	}
	log.Debugf("Found most common gateway (%s) in %d routes", nextHop.String(), len(routes))
	return nextHop
}

// If this route is listed in our extraNets slice, delete it
func delIfExtraRoute(r *netlink.Route) {
	for _, route := range delNets.nets {
		if r.Dst != nil && r.Dst.IP.Equal(route.IP) {
			log.Infof("Found extra route to delete: %+v", r)
			delRoute(r)
		}
	}
}

// If this is a default route, delete it
func delIfDefault(r *netlink.Route) {
	if isDefault(r) {
		log.Infof("Default route detected on %s: %s", linkName, r.String())
		if delDefaultGw {
			delRoute(r)
		} else {
			log.Infof("Ignoring default gw on %s (consider setting -delDefaultGw)", linkName)
		}
	}
}

// Checks if route is default
func isDefault(r *netlink.Route) bool {
	var isDefault bool
	if r.Src == nil && r.Dst == nil && (r.Gw.IsPrivate() || r.Gw == nil) {
		isDefault = true
	} else {
		log.Debugf("%+v is not default route", r)
	}
	return isDefault
}

// Deletes a route
func delRoute(r *netlink.Route) {
	// Can't delete a route with no dest net, so explicitly set to all if nil
	if isDefault(r) && r.Dst == nil {
		_, defaultDest, _ := net.ParseCIDR("0.0.0.0/0")
		r.Dst = defaultDest
	}
	err := netlink.RouteDel(r)
	if err != nil {
		log.Errorf("Failed to delete route on %s: %+v", linkName, err)
	} else {
		if isDefault(r) {
			log.Warnf("Deleted default route via %s on %s", r.Gw.String(), linkName)
		} else {
			log.Warnf("Deleted route to %s via %s on %s", r.Dst, r.Gw.String(), linkName)
		}
	}
}

// Finds link by ifIndex  and returns attributes
func getLinkAttrs(idx int) (*netlink.LinkAttrs, error) {
	link, err := netlink.LinkByIndex(idx)
	if err != nil {
		log.Errorf("Failed to retrieve link by index %d: %+v", idx, err)
		return nil, err
	}
	return link.Attrs(), err
}
