//go:build linux

package sandboxnetwork

import (
	"errors"
	"net"
	"os"
	"strings"
	"testing"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

func TestNetworkCopyRetiresAddressesBeforeLinkDown(t *testing.T) {
	if os.Getenv("FACTORYBOX_NATIVE_NETWORK_COPY_UNITS") != "1" {
		t.Skip("owned private-network Linux syscall fixture only")
	}
	const name, peerName, alias = "fb-copy-unit", "fb-copy-peer", "factorybox:copy-unit"
	for _, name := range []string{name, peerName} {
		if _, err := netlink.LinkByName(name); !absent(err) {
			t.Fatal("unit interface already exists", name, err)
		}
	}
	pair := &netlink.Veth{LinkAttrs: netlink.LinkAttrs{Name: name}, PeerName: peerName}
	if err := netlink.LinkAdd(pair); err != nil {
		t.Fatal(err)
	}
	inside, err := netlink.LinkByName(name)
	if err != nil {
		t.Fatal(err)
	}
	if err := netlink.LinkSetAlias(inside, alias); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		current, err := netlink.LinkByName(name)
		if err != nil || current.Attrs().Index != inside.Attrs().Index || current.Attrs().Alias != alias {
			t.Error("refuse changed unit network cleanup", err)
			return
		}
		if err := netlink.LinkDel(current); err != nil {
			t.Error(err)
		}
	})
	for _, address := range []string{"192.0.2.2/30", "fe80::fb/64"} {
		parsed, err := netlink.ParseAddr(address)
		if err != nil {
			t.Fatal(err)
		}
		if err := netlink.AddrAdd(inside, parsed); err != nil {
			t.Fatal(err)
		}
	}
	if err := netlink.LinkSetUp(inside); err != nil {
		t.Fatal(err)
	}
	policy, err := os.ReadFile("/proc/sys/net/ipv6/conf/" + name + "/keep_addr_on_down")
	if err != nil || strings.TrimSpace(string(policy)) != "0" {
		t.Fatal("unit does not exercise the actual IPv6 link-down deletion policy", err)
	}
	if err := netlink.LinkSetDown(inside); err != nil {
		t.Fatal(err)
	}
	ipv6, _ := netlink.ParseAddr("fe80::fb/64")
	if err := netlink.AddrDel(inside, ipv6); !errors.Is(err, unix.EADDRNOTAVAIL) {
		t.Fatal("old link-down-first order did not reproduce its actual address loss", err)
	}
	for _, address := range []string{"192.0.2.2/30", "fe80::fb/64"} {
		parsed, _ := netlink.ParseAddr(address)
		if err := netlink.AddrReplace(inside, parsed); err != nil {
			t.Fatal(err)
		}
	}
	if err := netlink.LinkSetUp(inside); err != nil {
		t.Fatal(err)
	}
	addresses, err := netlink.AddrList(inside, netlink.FAMILY_ALL)
	if err != nil || len(addresses) < 2 {
		t.Fatal("unit omitted either IPv4 or IPv6", addresses, err)
	}
	handle, err := netlink.NewHandle()
	if err != nil {
		t.Fatal(err)
	}
	defer handle.Close()
	if err := retireCopyInterface(handle, inside, addresses); err != nil {
		t.Fatal("copy address retirement ran after implicit IPv6 deletion", err)
	}
	after, err := netlink.AddrList(inside, netlink.FAMILY_ALL)
	current, linkErr := netlink.LinkByName(name)
	if err != nil || linkErr != nil || len(after) != 0 || current.Attrs().Flags&net.FlagUp != 0 {
		t.Fatal("owned copy interface still has old addresses or is up", after, err, linkErr)
	}
}

func TestNetworkCopyChangesOnlyTheExactOuterMapping(t *testing.T) {
	source, _ := derive(strings.Repeat("a", 32))
	target, _ := derive(strings.Repeat("b", 32))
	block := "veth-pair " + source.Guest + "=" + source.Peer +
		"\nexternal netdev[" + source.Guest + "]:" + source.staged() + "\n"
	prefix, suffix := "nested-ns\nsysbox-oom-floor\n", "irmap-scan-path /workspace/project\n"
	saved := []byte(prefix + block + suffix)
	before := string(saved)
	actual, err := copyCRIUConfiguration(source, target, saved)
	expected := prefix + "veth-pair " + source.Guest + "=" + target.Peer +
		"\nexternal netdev[" + source.Guest + "]:" + target.staged() + "\n" + suffix
	if err != nil || string(actual) != expected || string(saved) != before {
		t.Fatal("copy changed the original metadata or unrelated CRIU configuration", string(actual), err)
	}
	for _, changed := range []string{prefix + suffix, before + block, strings.Replace(before, source.staged(), "foreign", 1)} {
		if _, err := copyCRIUConfiguration(source, target, []byte(changed)); err == nil {
			t.Fatal("admitted missing, duplicate or changed original network mapping", changed)
		}
	}
}
