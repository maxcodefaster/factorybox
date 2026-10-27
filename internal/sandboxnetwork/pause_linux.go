//go:build linux

package sandboxnetwork

import (
	"fmt"
	"net"
	"os"

	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
	"golang.org/x/sys/unix"
)

func ownedPausePair(config Config, peer, guest netlink.Link) error {
	p, g := peer.Attrs(), guest.Attrs()
	alias := "factorybox:" + config.Owner
	if peer.Type() != "veth" || guest.Type() != "veth" ||
		p.Name != config.Peer || g.Name != config.Guest ||
		p.Alias != alias || g.Alias != alias ||
		p.HardwareAddr.String() != config.PeerMAC || g.HardwareAddr.String() != config.MAC ||
		p.MTU != config.MTU || g.MTU != config.MTU ||
		p.Index <= 0 || g.Index <= 0 || p.ParentIndex != g.Index || g.ParentIndex != p.Index ||
		g.Flags&net.FlagUp == 0 {
		return fmt.Errorf("pause network no longer identifies its original owned pair")
	}
	return nil
}

// SetPeerActive changes only the original worker end of the verified pair.
// The lifecycle guard must hold the original source frozen and untraced.
// Isolate before dump and keep it isolated through source destruction: closing
// the original TCP sockets must not send FIN/RST to their uncaptured peers.
func (config Config) SetPeerActive(pid int, active bool) error {
	if err := config.Validate(); err != nil {
		return err
	}
	if pid <= 1 {
		return fmt.Errorf("pause network requires its guarded Sandbox init")
	}
	namespace, err := os.Open(fmt.Sprintf("/proc/%d/ns/net", pid))
	if err != nil {
		return err
	}
	defer namespace.Close()
	var identity unix.Stat_t
	if err := unix.Fstat(int(namespace.Fd()), &identity); err != nil {
		return err
	}
	if uint64(identity.Dev) == config.NamespaceDevice && identity.Ino == config.NamespaceInode {
		return fmt.Errorf("pause network must not change its worker namespace")
	}
	guest, err := netlink.NewHandleAt(netns.NsHandle(namespace.Fd()))
	if err != nil {
		return err
	}
	defer guest.Close()
	peer, err := netlink.LinkByName(config.Peer)
	if err != nil {
		return err
	}
	inside, err := guest.LinkByName(config.Guest)
	if err != nil {
		return err
	}
	if err := ownedPausePair(config, peer, inside); err != nil {
		return err
	}
	for _, entry := range []struct {
		link    netlink.Link
		address string
		handle  *netlink.Handle
	}{{peer, config.Gateway, nil}, {inside, config.Address, guest}} {
		var addresses []netlink.Addr
		if entry.handle == nil {
			addresses, err = netlink.AddrList(entry.link, netlink.FAMILY_V4)
		} else {
			addresses, err = entry.handle.AddrList(entry.link, netlink.FAMILY_V4)
		}
		if err != nil {
			return err
		}
		found := false
		for _, address := range addresses {
			found = found || address.IPNet.String() == entry.address
		}
		if !found {
			return fmt.Errorf("pause network lost its original IPv4 address")
		}
	}
	if active {
		return netlink.LinkSetUp(peer)
	}
	return netlink.LinkSetDown(peer)
}
