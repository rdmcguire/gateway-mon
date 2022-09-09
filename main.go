package main

import (
	"flag"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/sirupsen/logrus"
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

// List of destination networks to delete
type netList []string

var (
	logLevel     string = "info"
	matchRoute   *netlink.Route
	deleteRoutes []*netlink.Route
	log          *logrus.Logger
	extraNets    netList // Routed networks to delete (in addition to default)
	linkName     string  = "gpd0"
	delDefaultGw bool
)

func init() {
	// Flags
	flag.StringVar(&logLevel, "logLevel", logLevel, "Default Log Level")
	flag.StringVar(&linkName, "linkName", linkName, "Name of interface to monitor routes for")
	flag.BoolVar(&delDefaultGw, "delDefaultGw", delDefaultGw, "Delete Default Gateway")
	flag.Var(&extraNets, "del", "Extra destination net to delete")
	flag.Parse()

	// Logging
	log = logrus.New()
	level, err := logrus.ParseLevel(logLevel)
	if err != nil {
		level = logrus.InfoLevel
	}
	log.SetLevel(level)

	if len(extraNets) > 0 {
		log.Info(extraNets.String())
	}

	if delDefaultGw {
		log.Info("Deleting default gateways")
	}

	if len(extraNets) == 0 && !delDefaultGw {
		log.Fatalf("I'm useless, nothing to delete, set one or more -del subnets or -delDefaultGw")
		os.Exit(1)
	}

	log.Infof("Receiving Route Updates from Netlink...")

}

// Slice of networks
func (l *netList) String() string {
	str := "Deleting Extra Networks:"
	for _, n := range *l {
		str += " " + n
	}
	return str
}
func (l *netList) Set(val string) error {
	_, net, err := net.ParseCIDR(val)
	if err != nil {
		return err
	}
	// Add value to list
	*l = append(*l, val)
	// Create Route
	deleteRoutes = append(deleteRoutes, &netlink.Route{
		Dst: net,
	})
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

	// Handle signals
	signal.Notify(killed, os.Interrupt, os.Kill, syscall.SIGTERM, syscall.SIGABRT)

	for {
		select {
		case update := <-routeUpdates:
			route := update.Route
			// If it's a new route, check it
			if update.Type == unix.RTM_NEWROUTE {
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
					delIfExtraRoute(&route) // Deletet this route if it's unwanted
				}
			}

		// Handle signal
		case <-killed:
			log.Printf("Cleaning up and dying")
			return
		}
	}

}

// If this route is listed in our extraNets slice, delete it
func delIfExtraRoute(r *netlink.Route) {
	for _, route := range deleteRoutes {
		if r.Dst != nil && r.Dst.IP.Equal(route.Dst.IP) {
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
	if r.Src == nil && r.Dst == nil && r.Gw.IsPrivate() {
		isDefault = true
	}
	return isDefault
}

// Deletes a route
func delRoute(r *netlink.Route) {
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
