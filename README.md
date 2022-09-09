# VPN Gateway Monitor

Tired of your VPN hijacking your default route?

Is your VPN pushing annoying routes that overlap with your local subnets?

This tool will fix that. Simply tell it what interface to monitor for routes,
and whether to delete any default gateways set on that interface, any additional routes
you'd like deleted, or both.

Uses netlink to get instant updates when routes are added to the system.

## Using it

**Build it**
```shell
go build .
```

**Configure it**
Just edit the command in gateway-mon.service. No need to get fancy.
```
Usage of ./gateway-mon:
	-del value
		Extra destination net to delete
	-delDefaultGw
		Delete Default Gateway
	-linkName string
		Name of interface to monitor routes for (default "gpd0")
	-logLevel string
		Default Log Level (default "info")
																				```

**Install it**
```shell
sudo install -vm544 gateway-mon /usr/local/bin/
sudo install gateway-mon.service /etc/systemd/system/
```

**Run it**
```shell
sudo systemctl daemon-reload
sudo systemctl enable gateway-mon
sudo systemctl start gateway-mon
```

**Watch it**
```shell
sudo journalctl -fu gateway-mon
INFO[0000] Deleting Extra Networks: 192.168.0.0/16
INFO[0000] Deleting default gateways
INFO[0000] Receiving Route Updates from Netlink...
INFO[0099] Route added to gpd0: {Ifindex: 5 Dst: <nil> Src: <nil> Gw: 172.18.248.3 Flags: [] Table: 254}
INFO[0099] Default route detected on gpd0: {Ifindex: 5 Dst: <nil> Src: <nil> Gw: 172.18.248.3 Flags: [] Table: 254}
WARN[0099] Deleted default route via 172.18.248.3 on gpd0
INFO[0099] Route added to gpd0: {Ifindex: 5 Dst: 10.0.0.0/8 Src: <nil> Gw: 172.18.248.3 Flags: [] Table: 254}
INFO[0099] Route added to gpd0: {Ifindex: 5 Dst: 10.104.1.3/32 Src: <nil> Gw: 172.18.248.3 Flags: [] Table: 254}
INFO[0099] Route added to gpd0: {Ifindex: 5 Dst: 10.104.1.4/32 Src: <nil> Gw: 172.18.248.3 Flags: [] Table: 254}
INFO[0099] Route added to gpd0: {Ifindex: 5 Dst: 172.20.0.0/16 Src: <nil> Gw: 172.18.248.3 Flags: [] Table: 254}
INFO[0099] Route added to gpd0: {Ifindex: 5 Dst: 172.21.1.0/24 Src: <nil> Gw: 172.18.248.3 Flags: [] Table: 254}
INFO[0099] Route added to gpd0: {Ifindex: 5 Dst: 172.31.37.0/24 Src: <nil> Gw: 172.18.248.3 Flags: [] Table: 254}
INFO[0099] Route added to gpd0: {Ifindex: 5 Dst: 192.168.0.0/16 Src: <nil> Gw: 172.18.248.3 Flags: [] Table: 254}
INFO[0099] Found extra route to delete: {Ifindex: 5 Dst: 192.168.0.0/16 Src: <nil> Gw: 172.18.248.3 Flags: [] Table: 254}
WARN[0099] Deleted route to 192.168.0.0/16 via 172.18.248.3 on gpd0
```
