//go:build linux

package sandboxnetwork

import (
	"net"
	"testing"

	"github.com/vishvananda/netlink"
)

func TestKernelDefaultRouteWithExplicitZeroDestination(t *testing.T) {
	// netlink v1.3.0 normalizes a missing RTA_DST to 0.0.0.0/0. The
	// first native network proof therefore failed to find its real uplink.
	_, zero, _ := net.ParseCIDR("0.0.0.0/0")
	_, subnet, _ := net.ParseCIDR("192.0.2.0/30")
	for _, route := range []netlink.Route{{LinkIndex: 4}, {LinkIndex: 4, Dst: zero}} {
		if !defaultRoute(route) {
			t.Fatal("lost real default route", route)
		}
	}
	if defaultRoute(netlink.Route{LinkIndex: 4, Dst: subnet}) || defaultRoute(netlink.Route{Dst: zero}) {
		t.Fatal("accepted a subnet or missing uplink")
	}
}

func TestNetworkRejectsCallerSuppliedCommandsAndMalformedOwners(t *testing.T) {
	for _, value := range []string{"false", "true\nveth-pair eth0=other", "{}"} {
		if _, err := Requested(map[string]string{Annotation: value}); err == nil {
			t.Fatal("admitted network option injection", value)
		}
	}
	for _, owner := range []string{"", "../../foreign", "0123456789abcdef0123456789abcdef\n", "ABCDEF0123456789abcdef0123456789"} {
		if _, err := derive(owner); err == nil {
			t.Fatal("admitted malformed network ownership", owner)
		}
	}
}
