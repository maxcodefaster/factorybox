//go:build linux

package sandboxnetwork

import (
	"context"
	"fmt"
	"net"
	"os"
	"strings"

	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
)

func (config Config) validateCopy(source Config) error {
	if err := source.Validate(); err != nil {
		return err
	}
	if err := config.Validate(); err != nil {
		return err
	}
	if source.Owner == config.Owner || source.CIDR == config.CIDR ||
		source.NamespaceDevice != config.NamespaceDevice || source.NamespaceInode != config.NamespaceInode ||
		source.MTU != config.MTU {
		return fmt.Errorf("independent network copy requires another owner, subnet and peer in the original worker")
	}
	return nil
}

// CopyConfiguration changes only the complete known outer-netdev mapping.
// The saved source bytes remain immutable and all other CRIU options survive.
func (config Config) CopyConfiguration(source Config, saved []byte) ([]byte, error) {
	if err := config.validateCopy(source); err != nil {
		return nil, err
	}
	return copyCRIUConfiguration(source, config, saved)
}

func copyCRIUConfiguration(source, config Config, saved []byte) ([]byte, error) {
	original := "veth-pair " + source.Guest + "=" + source.Peer +
		"\nexternal netdev[" + source.Guest + "]:" + source.staged() + "\n"
	if strings.Count(string(saved), original) != 1 {
		return nil, fmt.Errorf("checkpoint lacks its exact original network mapping")
	}
	replacement := "veth-pair " + source.Guest + "=" + config.Peer +
		"\nexternal netdev[" + source.Guest + "]:" + config.staged() + "\n"
	return []byte(strings.Replace(string(saved), original, replacement, 1)), nil
}

// ConfigureCopy runs on the independent restored namespace while its complete
// cgroup is held. Socket admission must already exclude the replaced outer IP
// and external connections. The original source namespace is never changed.
func (config Config) ConfigureCopy(ctx context.Context, pid int, source Config) error {
	if err := config.validateCopy(source); err != nil {
		return err
	}
	if pid <= 1 {
		return fmt.Errorf("network copy requires its held Sandbox init")
	}
	namespace, err := os.Open(fmt.Sprintf("/proc/%d/ns/net", pid))
	if err != nil {
		return err
	}
	defer namespace.Close()
	handle, err := netlink.NewHandleAt(netns.NsHandle(namespace.Fd()))
	if err != nil {
		return err
	}
	defer handle.Close()
	inside, err := handle.LinkByName(source.Guest)
	if err != nil {
		return err
	}
	peer, err := netlink.LinkByName(config.Peer)
	if err != nil {
		return err
	}
	attrs := inside.Attrs()
	if inside.Type() != "veth" || peer.Type() != "veth" || peer.Attrs().Alias != "factorybox:"+config.Owner ||
		peer.Attrs().ParentIndex != attrs.Index || attrs.ParentIndex != peer.Attrs().Index ||
		attrs.MTU != source.MTU || attrs.HardwareAddr.String() != source.MAC ||
		attrs.Alias != "factorybox:"+config.Owner {
		return fmt.Errorf("restored copy does not join the original-shaped guest to its new owned peer")
	}
	if _, err := handle.LinkByName(config.Guest); !absent(err) {
		return fmt.Errorf("copy target guest interface already exists: %v", err)
	}
	addresses, err := handle.AddrList(inside, netlink.FAMILY_ALL)
	if err != nil {
		return err
	}
	found := false
	for _, address := range addresses {
		if address.IPNet.String() == source.Address {
			found = true
		} else if address.IP.To4() != nil || !address.IP.IsLinkLocalUnicast() {
			return fmt.Errorf("copy outer interface has an unrecognized address")
		}
	}
	if !found {
		return fmt.Errorf("copy outer interface lost the saved IPv4 address")
	}
	if err := retireCopyInterface(handle, inside, addresses); err != nil {
		return err
	}
	if err := handle.LinkSetName(inside, config.Guest); err != nil {
		return fmt.Errorf("rename copied outer interface: %w", err)
	}
	mac, _ := net.ParseMAC(config.MAC)
	if err := handle.LinkSetHardwareAddr(inside, mac); err != nil {
		return fmt.Errorf("replace copied outer hardware address: %w", err)
	}
	if err := config.Configure(ctx, pid, true); err != nil {
		return fmt.Errorf("activate independently owned copy network: %w", err)
	}
	return nil
}

func retireCopyInterface(handle *netlink.Handle, inside netlink.Link, addresses []netlink.Addr) error {
	// Bringing a link down can itself remove automatic IPv6 addresses. Retire
	// the verified original set first, while every address still exists.
	for _, address := range addresses {
		if err := handle.AddrDel(inside, &address); err != nil {
			return fmt.Errorf("remove copied outer address %s: %w", address.String(), err)
		}
	}
	if err := handle.LinkSetDown(inside); err != nil {
		return fmt.Errorf("hold copied outer interface for renaming: %w", err)
	}
	return nil
}
