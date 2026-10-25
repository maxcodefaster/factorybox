//go:build linux

// Package sandboxnetwork owns one IPv4 veth pair and exact NAT rules inside
// the worker's private network namespace. It never joins a host namespace.
package sandboxnetwork

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"time"

	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
	"golang.org/x/sys/unix"
)

const Annotation = "io.factorybox.outbound-ipv4"

var ErrIndependentCopy = errors.New("outbound network supports same-worker pause/resume; independent copies need a separate network identity")

type Config struct {
	Owner           string `json:"owner"`
	Peer            string `json:"peer"`
	Guest           string `json:"guest"`
	CIDR            string `json:"cidr"`
	Gateway         string `json:"gateway"`
	Address         string `json:"address"`
	MAC             string `json:"mac"`
	PeerMAC         string `json:"peer_mac"`
	MTU             int    `json:"mtu"`
	NamespaceDevice uint64 `json:"namespace_device"`
	NamespaceInode  uint64 `json:"namespace_inode"`
}

func Requested(annotations map[string]string) (bool, error) {
	value, present := annotations[Annotation]
	if !present {
		return false, nil
	}
	if value != "true" {
		return false, fmt.Errorf("%s must be true when present", Annotation)
	}
	return true, nil
}

func derive(owner string) (Config, error) {
	data, err := hex.DecodeString(owner)
	if err != nil || len(data) != 16 || strings.ToLower(owner) != owner {
		return Config{}, fmt.Errorf("network requires the exact sandbox owner")
	}
	third, fourth := int(data[0]), int(data[1]&63)*4
	return Config{Owner: owner, Peer: "fbw" + owner[:12], Guest: "fbs" + owner[:12], CIDR: fmt.Sprintf("100.127.%d.%d/30", third, fourth),
		Gateway: fmt.Sprintf("100.127.%d.%d/30", third, fourth+1), Address: fmt.Sprintf("100.127.%d.%d/30", third, fourth+2),
		MAC:     fmt.Sprintf("02:fb:%02x:%02x:%02x:%02x", data[0], data[1], data[2], data[3]),
		PeerMAC: fmt.Sprintf("02:fc:%02x:%02x:%02x:%02x", data[0], data[1], data[2], data[3])}, nil
}

func defaultRoute(route netlink.Route) bool {
	if route.LinkIndex <= 0 {
		return false
	}
	if route.Dst == nil {
		return true
	}
	ones, bits := route.Dst.Mask.Size()
	return ones == 0 && bits == 32
}

func uplinkMTU() (int, error) {
	routes, err := netlink.RouteList(nil, netlink.FAMILY_V4)
	if err != nil {
		return 0, err
	}
	mtu := 0
	for _, route := range routes {
		if defaultRoute(route) {
			link, err := netlink.LinkByIndex(route.LinkIndex)
			if err != nil {
				return 0, err
			}
			if mtu == 0 || link.Attrs().MTU < mtu {
				mtu = link.Attrs().MTU
			}
		}
	}
	if mtu < 576 || mtu > 65535 {
		return 0, fmt.Errorf("owned outbound networking requires a finite IPv4 uplink MTU")
	}
	return mtu, nil
}

func workerNamespace() (unix.Stat_t, error) {
	var current, init unix.Stat_t
	if err := unix.Stat("/proc/self/ns/net", &current); err != nil {
		return current, err
	}
	if err := unix.Stat("/proc/1/ns/net", &init); err != nil {
		return current, err
	}
	if current.Dev != init.Dev || current.Ino != init.Ino {
		return current, fmt.Errorf("network caller left its worker PID 1 namespace")
	}
	return current, nil
}

func Plan(owner string) (*Config, error) {
	config, err := derive(owner)
	if err != nil {
		return nil, err
	}
	current, err := workerNamespace()
	if err != nil {
		return nil, err
	}
	config.NamespaceDevice, config.NamespaceInode = uint64(current.Dev), current.Ino
	config.MTU, err = uplinkMTU()
	if err != nil {
		return nil, err
	}
	return &config, nil
}

func (config Config) Validate() error {
	expected, err := derive(config.Owner)
	if err != nil {
		return err
	}
	current, err := workerNamespace()
	if err != nil {
		return err
	}
	expected.NamespaceDevice, expected.NamespaceInode = uint64(current.Dev), current.Ino
	expected.MTU, err = uplinkMTU()
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(config, expected) {
		return fmt.Errorf("owned network configuration or worker namespace changed")
	}
	return nil
}

func (config Config) CRIUConfiguration() (string, error) {
	if err := config.Validate(); err != nil {
		return "", err
	}
	return "veth-pair " + config.Guest + "=" + config.Peer + "\nexternal netdev[" + config.Guest + "]:" + config.staged() + "\n", nil
}

func (config Config) staged() string { return "fbg" + config.Owner[:12] }

func absent(err error) bool { var missing netlink.LinkNotFoundError; return errors.As(err, &missing) }

func (config Config) RequirePeerAbsent() error {
	if err := config.Validate(); err != nil {
		return err
	}
	for _, name := range []string{config.Peer, config.staged()} {
		_, err := netlink.LinkByName(name)
		if absent(err) {
			continue
		}
		if err != nil {
			return err
		}
		return fmt.Errorf("restore requires its previous network peer to be absent")
	}
	return nil
}

// PrepareRestore creates both ends in the original worker namespace. CRIU's
// userns helper then moves the staged end into the fresh sandbox namespace.
// Creating an external worker peer from inside the restored userns is denied.
func (config Config) PrepareRestore() error {
	if err := config.Validate(); err != nil {
		return err
	}
	alias := "factorybox:" + config.Owner
	peer, err := netlink.LinkByName(config.Peer)
	created := false
	if absent(err) {
		if _, err := netlink.LinkByName(config.staged()); !absent(err) {
			return fmt.Errorf("restore staged interface already exists: %v", err)
		}
		peerMAC, _ := net.ParseMAC(config.PeerMAC)
		guestMAC, _ := net.ParseMAC(config.MAC)
		pair := &netlink.Veth{LinkAttrs: netlink.LinkAttrs{Name: config.Peer, MTU: config.MTU, HardwareAddr: peerMAC}, PeerName: config.staged(), PeerHardwareAddr: guestMAC}
		if err := netlink.LinkAdd(pair); err != nil {
			return err
		}
		created = true
		peer, err = netlink.LinkByName(config.Peer)
	}
	if err != nil {
		return err
	}
	staged, err := netlink.LinkByName(config.staged())
	if err != nil {
		return err
	}
	if peer.Type() != "veth" || staged.Type() != "veth" || peer.Attrs().ParentIndex != staged.Attrs().Index || staged.Attrs().ParentIndex != peer.Attrs().Index {
		return fmt.Errorf("restore interfaces are not the owned staging pair")
	}
	for _, link := range []netlink.Link{peer, staged} {
		if link.Attrs().Alias != alias && !(created && link.Attrs().Alias == "") {
			return fmt.Errorf("restore staging pair has another owner or an uncertain create")
		}
		if err := netlink.LinkSetAlias(link, alias); err != nil {
			return err
		}
		if err := netlink.LinkSetMTU(link, config.MTU); err != nil {
			return err
		}
	}
	return nil
}

func overlaps(a, b *net.IPNet) bool { return a.Contains(b.IP) || b.Contains(a.IP) }

// Admit checks the worker's connected networks before any external create.
// Default routes do not reserve the entire IPv4 address space.
func (config Config) Admit() error {
	if err := config.RequirePeerAbsent(); err != nil {
		return err
	}
	_, subnet, _ := net.ParseCIDR(config.CIDR)
	routes, err := netlink.RouteList(nil, netlink.FAMILY_V4)
	if err != nil {
		return err
	}
	for _, route := range routes {
		if route.Dst != nil {
			ones, _ := route.Dst.Mask.Size()
			if ones > 0 && overlaps(subnet, route.Dst) {
				return fmt.Errorf("sandbox subnet overlaps an existing worker route")
			}
		}
	}
	return nil
}

func (config Config) rules() [][]string {
	comment := "factorybox:" + config.Owner
	return [][]string{
		{"nat", "POSTROUTING", "-s", config.CIDR, "-m", "comment", "--comment", comment, "-j", "MASQUERADE"},
		{"filter", "FORWARD", "-i", config.Peer, "-m", "comment", "--comment", comment, "-j", "ACCEPT"},
		{"filter", "FORWARD", "-o", config.Peer, "-m", "conntrack", "--ctstate", "ESTABLISHED,RELATED", "-m", "comment", "--comment", comment, "-j", "ACCEPT"},
	}
}

func iptables(ctx context.Context, operation string, rule []string) error {
	bounded, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	command := exec.CommandContext(bounded, "iptables", append([]string{"-w", "2", "-t", rule[0], operation, rule[1]}, rule[2:]...)...)
	command.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LC_ALL=C"}
	output, err := command.CombinedOutput()
	if len(output) > 8192 {
		return fmt.Errorf("owned network rule output exceeded its bound")
	}
	if err != nil {
		return fmt.Errorf("owned network rule %s failed: %w: %s", operation, err, strings.TrimSpace(string(output)))
	}
	return nil
}

func missingRule(err error) bool {
	var exit *exec.ExitError
	return errors.As(err, &exit) && exit.ExitCode() == 1
}

// Configure runs before first useful work or while the restored cgroup is
// still held. CRIU alone creates the peer during a restore. The caller must
// have verified its absence before the restore attempt was persisted.
func (config Config) Configure(ctx context.Context, pid int, restored bool) error {
	if err := config.Validate(); err != nil {
		return err
	}
	if pid <= 1 {
		return fmt.Errorf("network requires the owned sandbox init")
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
		return fmt.Errorf("sandbox network is not separate from the worker")
	}
	guest, err := netlink.NewHandleAt(netns.NsHandle(namespace.Fd()))
	if err != nil {
		return err
	}
	defer guest.Close()
	peer, err := netlink.LinkByName(config.Peer)
	created := false
	if absent(err) && !restored {
		if _, err := guest.LinkByName(config.Guest); !absent(err) {
			return fmt.Errorf("sandbox network interface already exists: %v", err)
		}
		pair := &netlink.Veth{LinkAttrs: netlink.LinkAttrs{Name: config.Peer, Alias: "factorybox:" + config.Owner, MTU: config.MTU}, PeerName: config.Guest, PeerNamespace: netlink.NsFd(namespace.Fd())}
		if err := netlink.LinkAdd(pair); err != nil {
			return err
		}
		created = true
		peer, err = netlink.LinkByName(config.Peer)
	}
	if err != nil {
		return err
	}
	inside, err := guest.LinkByName(config.Guest)
	if err != nil {
		return err
	}
	if peer.Type() != "veth" || inside.Type() != "veth" || peer.Attrs().ParentIndex != inside.Attrs().Index || inside.Attrs().ParentIndex != peer.Attrs().Index {
		return fmt.Errorf("network names do not identify the original veth pair")
	}
	alias := "factorybox:" + config.Owner
	// The veth constructor can omit IFLA_IFALIAS. Only the just-created
	// pair or the pair from the held CRIU restore may acquire an empty alias.
	// A pre-existing unlabelled peer is an uncertain create, never adopted.
	if peer.Attrs().Alias != alias && !((restored || created) && peer.Attrs().Alias == "") {
		return fmt.Errorf("worker peer belongs to another owner: expected %q, found %q", alias, peer.Attrs().Alias)
	}
	if err := netlink.LinkSetAlias(peer, alias); err != nil {
		return err
	}
	if inside.Attrs().Alias != "" && inside.Attrs().Alias != alias {
		return fmt.Errorf("guest interface belongs to another owner")
	}
	if err := guest.LinkSetAlias(inside, alias); err != nil {
		return err
	}
	mac, _ := net.ParseMAC(config.MAC)
	if !restored {
		if err := guest.LinkSetHardwareAddr(inside, mac); err != nil {
			return err
		}
	}
	peerMAC, _ := net.ParseMAC(config.PeerMAC)
	if err := netlink.LinkSetHardwareAddr(peer, peerMAC); err != nil {
		return err
	}
	if err := netlink.LinkSetMTU(peer, config.MTU); err != nil {
		return err
	}
	if err := guest.LinkSetMTU(inside, config.MTU); err != nil {
		return err
	}
	address, _ := netlink.ParseAddr(config.Gateway)
	if err := netlink.AddrReplace(peer, address); err != nil {
		return err
	}
	address, _ = netlink.ParseAddr(config.Address)
	if err := guest.AddrReplace(inside, address); err != nil {
		return err
	}
	if err := netlink.LinkSetUp(peer); err != nil {
		return err
	}
	if err := guest.LinkSetUp(inside); err != nil {
		return err
	}
	loopback, err := guest.LinkByName("lo")
	if err != nil {
		return err
	}
	if err := guest.LinkSetUp(loopback); err != nil {
		return err
	}
	if err := guest.RouteReplace(&netlink.Route{LinkIndex: inside.Attrs().Index, Gw: net.ParseIP(strings.Split(config.Gateway, "/")[0])}); err != nil {
		return err
	}
	// Deployment must supply a private Pod/container network namespace. Only
	// that epoch's namespace is changed; the host is never entered.
	forward, err := os.ReadFile("/proc/sys/net/ipv4/ip_forward")
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(forward)) != "1" {
		if err := os.WriteFile("/proc/sys/net/ipv4/ip_forward", []byte("1\n"), 0600); err != nil {
			return err
		}
	}
	for _, rule := range config.rules() {
		if err := iptables(ctx, "-C", rule); err != nil {
			if !missingRule(err) {
				return err
			}
			if err := iptables(ctx, "-A", rule); err != nil {
				return err
			}
		}
	}
	return nil
}

// RetirePeer is called only after the original source is durably captured,
// stopped and its owned cgroup is empty. Other namespace references may keep
// that old veth alive; CRIU needs its name free for the fresh namespace.
func (config Config) RetirePeer() error {
	if err := config.Validate(); err != nil {
		return err
	}
	peer, err := netlink.LinkByName(config.Peer)
	if err == nil {
		if peer.Type() != "veth" || peer.Attrs().Alias != "factorybox:"+config.Owner {
			return fmt.Errorf("refusing cleanup of a foreign worker peer")
		}
		if err := netlink.LinkDel(peer); err != nil {
			return err
		}
	} else if !absent(err) {
		return err
	}
	return nil
}

func (config Config) Delete(ctx context.Context) error {
	if err := config.RetirePeer(); err != nil {
		return err
	}
	for _, rule := range config.rules() {
		if err := iptables(ctx, "-C", rule); err == nil {
			if err := iptables(ctx, "-D", rule); err != nil {
				return err
			}
		} else if !missingRule(err) {
			return err
		}
	}
	return nil
}

// VerifySource refuses changed outer wiring and a duplicate outer interface
// name in a nested namespace before CRIU receives its global netdev mapping.
func (config Config) VerifySource(pid int, threads map[int]string) error {
	if err := config.Validate(); err != nil {
		return err
	}
	peer, err := netlink.LinkByName(config.Peer)
	if err != nil {
		return err
	}
	alias := "factorybox:" + config.Owner
	if peer.Type() != "veth" || peer.Attrs().Alias != alias || peer.Attrs().HardwareAddr.String() != config.PeerMAC || peer.Attrs().MTU != config.MTU || peer.Attrs().Flags&net.FlagUp == 0 {
		return fmt.Errorf("owned worker network peer changed before capture")
	}
	root, err := os.Stat(fmt.Sprintf("/proc/%d/ns/net", pid))
	if err != nil {
		return err
	}
	seen := map[[2]uint64]bool{}
	rootSeen := false
	for tid := range threads {
		namespace, err := os.Open(fmt.Sprintf("/proc/%d/ns/net", tid))
		if err != nil {
			return err
		}
		stat, err := namespace.Stat()
		if err != nil {
			namespace.Close()
			return err
		}
		var identity unix.Stat_t
		if err := unix.Fstat(int(namespace.Fd()), &identity); err != nil {
			namespace.Close()
			return err
		}
		key := [2]uint64{uint64(identity.Dev), identity.Ino}
		if seen[key] {
			namespace.Close()
			continue
		}
		seen[key] = true
		handle, err := netlink.NewHandleAt(netns.NsHandle(namespace.Fd()))
		namespace.Close()
		if err != nil {
			return err
		}
		inside, lookupErr := handle.LinkByName(config.Guest)
		if !os.SameFile(root, stat) {
			handle.Close()
			if !absent(lookupErr) {
				return fmt.Errorf("nested namespace collides with the owned outer network mapping: %v", lookupErr)
			}
			continue
		}
		if lookupErr != nil {
			handle.Close()
			return lookupErr
		}
		rootSeen = true
		attrs := inside.Attrs()
		if inside.Type() != "veth" || attrs.Alias != alias || attrs.HardwareAddr.String() != config.MAC || attrs.MTU != config.MTU || attrs.Flags&net.FlagUp == 0 ||
			attrs.ParentIndex != peer.Attrs().Index || peer.Attrs().ParentIndex != attrs.Index {
			handle.Close()
			return fmt.Errorf("owned sandbox network changed before capture")
		}
		addresses, err := handle.AddrList(inside, netlink.FAMILY_V4)
		handle.Close()
		if err != nil {
			return err
		}
		found := false
		for _, address := range addresses {
			if address.IPNet.String() == config.Address {
				found = true
			}
		}
		if !found {
			return fmt.Errorf("owned sandbox IPv4 address changed before capture")
		}
	}
	if !rootSeen {
		return fmt.Errorf("capture omitted its original sandbox network namespace")
	}
	return nil
}
