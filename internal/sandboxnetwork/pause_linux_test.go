//go:build linux

package sandboxnetwork

import (
	"net"
	"strings"
	"testing"

	"github.com/vishvananda/netlink"
)

func TestPauseNetworkRequiresTheOriginalOwnedPair(t *testing.T) {
	config, _ := derive(strings.Repeat("a", 32))
	config.MTU = 1500
	pair := func() (*netlink.Veth, *netlink.Veth) {
		pMAC, _ := net.ParseMAC(config.PeerMAC)
		gMAC, _ := net.ParseMAC(config.MAC)
		return &netlink.Veth{LinkAttrs: netlink.LinkAttrs{Name: config.Peer, Alias: "factorybox:" + config.Owner,
				Index: 11, ParentIndex: 12, HardwareAddr: pMAC, MTU: config.MTU, Flags: net.FlagUp}},
			&netlink.Veth{LinkAttrs: netlink.LinkAttrs{Name: config.Guest, Alias: "factorybox:" + config.Owner,
				Index: 12, ParentIndex: 11, HardwareAddr: gMAC, MTU: config.MTU, Flags: net.FlagUp}}
	}
	for _, active := range []bool{false, true} {
		peer, guest := pair()
		if !active {
			peer.Flags = 0
		}
		if err := ownedPausePair(config, peer, guest); err != nil {
			t.Fatal("original peer must permit an exact isolated/up transition", err)
		}
	}
	for _, change := range []func(*netlink.Veth, *netlink.Veth){
		func(p, g *netlink.Veth) { p.Alias = "factorybox:foreign" },
		func(p, g *netlink.Veth) { g.Alias = "factorybox:foreign" },
		func(p, g *netlink.Veth) { p.Name = "foreign" },
		func(p, g *netlink.Veth) { g.Name = "foreign" },
		func(p, g *netlink.Veth) { p.ParentIndex++ },
		func(p, g *netlink.Veth) { g.ParentIndex++ },
		func(p, g *netlink.Veth) { p.MTU++ },
		func(p, g *netlink.Veth) { g.Flags = 0 },
		func(p, g *netlink.Veth) { p.HardwareAddr, _ = net.ParseMAC("02:00:00:00:00:01") },
	} {
		peer, guest := pair()
		change(peer, guest)
		if err := ownedPausePair(config, peer, guest); err == nil {
			t.Fatal("admitted a changed or foreign pause pair")
		}
	}
	peer, guest := pair()
	if err := ownedPausePair(config, &netlink.Dummy{LinkAttrs: peer.LinkAttrs}, guest); err == nil {
		t.Fatal("admitted another link type")
	}
}
