//go:build linux && (amd64 || arm64)

package lifecycle

import (
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"

	"factorybox/internal/sysboxcapture"
)

// A shared worker must preserve the original after a peer was restored and
// deleted. Select individual consumer inputs without exporting a full image.
func TestNativeStandbyPeers(t *testing.T) {
	if os.Getenv("FACTORYBOX_NATIVE_STANDBY_PEERS") != "1" {
		t.Skip("owned shared-worker peer retirement control only")
	}
	result := map[string]any{"passed": false, "production_qualified": false}
	defer func() { boundedLossSave(t, "standby-peers.json", result) }()
	status := boundedLossStatusNow(t)
	if os.Getenv("FACTORYBOX_STANDBY_PEERS_NETWORK") == "1" {
		standbyPeersUplink(t, status.StoreID)
	}
	nested := os.Getenv("FACTORYBOX_STANDBY_PEERS_NESTED") == "1"
	docker := os.Getenv("FACTORYBOX_STANDBY_PEERS_DOCKER") == "1"
	result["nested"] = nested
	result["docker"] = docker
	result["shared_source"] = os.Getenv("FACTORYBOX_STANDBY_PEERS_SOURCE") == "1"
	result["network"] = os.Getenv("FACTORYBOX_STANDBY_PEERS_NETWORK") == "1"
	common := os.Getenv("FACTORYBOX_STANDBY_PEERS_COMMON_BASE") == "1"
	releasePeer := os.Getenv("FACTORYBOX_STANDBY_PEERS_RELEASE") == "1"
	result["common_base"], result["release_peer_inputs"] = common, releasePeer
	var base *Base
	if common {
		input := status.Store + "/peer-base-input"
		standbyRepeatLower(t, input, nested, result["shared_source"].(bool))
		var prepared Base
		if err := json.Unmarshal(boundedLossAction(t, "prepare-base", "--source", input, "peer-base"), &prepared); err != nil ||
			prepared.ID != "peer-base" || prepared.PreparedAt == "" || prepared.Tree.Immutable == nil {
			t.Fatal("prepare exact common peer base", prepared, err)
		}
		base = &prepared
		result["base"] = prepared
		t.Cleanup(func() {
			if err := sysboxcapture.ReleaseFilesystemBaseMount(prepared.Tree); err != nil {
				t.Error("release exact common peer base after both Sandboxes", err)
			}
		})
	}
	first, _ := standbyRepeatFixtureWithBase(t, status, "standby-original", false, true, nested, base)
	initial := standbyRepeatRequest(t, status, first, "ready", false)
	result["original"] = first
	if docker {
		proof := map[string]any{}
		result["docker_before"] = proof
		standbyPeersDocker(t, status, first, true, proof)
	} else if nested {
		standbyPeersNested(t, status, first, true)
	}
	second, release := standbyRepeatFixtureWithBase(t, status, "standby-peer", false, true, false, base)
	result["peer"] = second
	standbyRepeatRequest(t, status, second, "ready", false)
	result["peer_pause"] = boundedLossView(t, "pause", "--snapshot-id", strings.Repeat("b", 32), second.Record.ID)
	result["peer_resume"] = boundedLossView(t, "resume", "--snapshot-id", strings.Repeat("b", 32), second.Record.ID)
	result["peer_delete"] = boundedLossView(t, "delete", second.Record.ID)
	if releasePeer {
		release()
		if t.Failed() {
			t.Fatal("peer private input release failed")
		}
		if base != nil {
			if err := sysboxcapture.VerifyImmutableTree(first.Record.InputBundle+"/lower", base.Tree); err != nil {
				t.Fatal("peer release changed original base", err)
			}
		}
		standbyRepeatRequest(t, status, first, "after-release", false)
		result["peer_inputs_released"] = true
	}
	result["original_pause"] = boundedLossView(t, "pause", "--snapshot-id", strings.Repeat("c", 32), first.Record.ID)
	resumed := boundedLossView(t, "resume", "--snapshot-id", strings.Repeat("c", 32), first.Record.ID)
	result["original_resume"] = resumed
	after := standbyRepeatRequest(t, status, resumed, "after-peer", false)
	expected := float64(2)
	if releasePeer {
		expected++
	}
	if after["nonce"] != initial["nonce"] || after["count"] != expected {
		t.Fatal("peer retirement replaced the original's process memory", initial, after)
	}
	result["original_actor_after"] = after
	if docker {
		proof := map[string]any{}
		result["docker_after"] = proof
		standbyPeersDocker(t, status, resumed, false, proof)
	} else if nested {
		standbyPeersNested(t, status, resumed, false)
	}
	result["passed"] = true
}

func standbyPeersDocker(t *testing.T, status boundedLossStatus, source View, create bool, proof map[string]any) {
	t.Helper()
	guest := func(script string) string {
		t.Helper()
		out, err := exec.Command(status.Lifecycle.Runtime, "--root", status.Lifecycle.Root+"/runtime",
			"exec", source.Record.RuntimeID, "/bin/sh", "-ec", script).CombinedOutput()
		if err != nil {
			t.Fatal("Docker peer control", string(out), err)
		}
		return string(out)
	}
	if create {
		guest(`mkdir -p /sys/fs/cgroup/init.scope
printf '1\n' > /sys/fs/cgroup/init.scope/cgroup.procs
printf '%s\n' "$$" > /sys/fs/cgroup/init.scope/cgroup.procs
for controller in cpuset cpu io memory pids; do
  printf '+%s\n' "$controller" > /sys/fs/cgroup/cgroup.subtree_control
done
dockerd --storage-driver=overlay2 --exec-opt=native.cgroupdriver=cgroupfs --iptables=false --ip6tables=false --bridge=none --ip-forward=false --ip-masq=false --userland-proxy=false >/dockerd.log 2>&1 &
for iteration in $(seq 1 500); do
  if docker info >/docker-info 2>/dev/null; then break; fi
  sleep 0.02
done
docker info >/docker-info || { cat /dockerd.log >&2; exit 1; }
tar -C /inner-rootfs -c . | docker import - peer:owned >/docker-import
docker run --name peer-child --detach --network=none peer:owned /bin/sleep 300 >/docker-child`)
	}
	if err := json.Unmarshal([]byte(guest(`docker inspect --format '{"state":{{json .State}},"restarts":{{.RestartCount}}}' peer-child`)), &proof); err != nil {
		t.Fatal("Docker child state", err)
	}
	if out := guest("docker exec peer-child /bin/echo docker-peer-ready"); out != "docker-peer-ready\n" {
		t.Fatal("Docker child did not retain useful work", out)
	}
	if !create {
		if out := guest("docker inspect --format '{{.RestartCount}}' peer-child"); out != "0\n" {
			t.Fatal("Docker child restarted instead of preserving its process", out)
		}
		guest("docker rm --force peer-child >/dev/null")
	}
}

func standbyPeersUplink(t *testing.T, owner string) {
	t.Helper()
	self, err := os.Readlink("/proc/self/ns/net")
	init, initErr := os.Readlink("/proc/1/ns/net")
	if err != nil || initErr != nil || self != init {
		t.Fatal("peer uplink must remain in its private worker namespace", err, initErr)
	}
	alias := "factorybox-peer:" + owner
	ip := func(arguments ...string) {
		t.Helper()
		if out, err := exec.Command("ip", arguments...).CombinedOutput(); err != nil {
			t.Fatal("private peer uplink", arguments, string(out), err)
		}
	}
	ip("link", "add", "peer-uplink", "type", "veth", "peer", "name", "peer-test")
	ip("link", "set", "peer-uplink", "alias", alias)
	ip("link", "set", "peer-test", "alias", alias)
	t.Cleanup(func() {
		current, err := os.Readlink("/proc/self/ns/net")
		label, readErr := os.ReadFile("/sys/class/net/peer-uplink/ifalias")
		if err != nil || current != self || readErr != nil || strings.TrimSpace(string(label)) != alias {
			t.Error("refuse foreign peer uplink cleanup", err, readErr)
			return
		}
		if out, err := exec.Command("ip", "link", "del", "peer-uplink").CombinedOutput(); err != nil {
			t.Error("release private peer uplink", string(out), err)
		}
	})
	ip("link", "set", "peer-uplink", "up")
	ip("link", "set", "peer-test", "up")
	ip("addr", "add", "192.0.2.1/32", "dev", "peer-uplink")
	ip("route", "add", "default", "dev", "peer-uplink")
}

func standbyPeersNested(t *testing.T, status boundedLossStatus, source View, create bool) {
	t.Helper()
	guest := func(script string) string {
		t.Helper()
		out, err := exec.Command(status.Lifecycle.Runtime, "--root", status.Lifecycle.Root+"/runtime",
			"exec", source.Record.RuntimeID, "/bin/sh", "-ec", script).CombinedOutput()
		if err != nil {
			t.Fatal("nested peer control", string(out), err)
		}
		return string(out)
	}
	if create {
		guest(`mkdir -p /sys/fs/cgroup/init.scope /inner-bundle
printf '1\n' > /sys/fs/cgroup/init.scope/cgroup.procs
printf '%s\n' "$$" > /sys/fs/cgroup/init.scope/cgroup.procs
for controller in cpuset cpu io memory pids; do
  printf '+%s\n' "$controller" > /sys/fs/cgroup/cgroup.subtree_control
done
mkdir /sys/fs/cgroup/nested
for controller in cpuset cpu io memory pids; do
  printf '+%s\n' "$controller" > /sys/fs/cgroup/nested/cgroup.subtree_control
done
/bin/nested-runc spec --bundle /inner-bundle`)
		var spec map[string]any
		if err := json.Unmarshal([]byte(guest("cat /inner-bundle/config.json")), &spec); err != nil {
			t.Fatal(err)
		}
		spec["root"].(map[string]any)["path"] = "/inner-rootfs"
		process := spec["process"].(map[string]any)
		process["terminal"] = false
		process["args"] = []string{"/bin/sleep", "300"}
		spec["linux"].(map[string]any)["cgroupsPath"] = "/nested/child"
		data, err := json.Marshal(spec)
		if err != nil {
			t.Fatal(err)
		}
		guest("cat > /inner-bundle/config.json <<'FACTORYBOX_PEER_OCI'\n" + string(data) + "\nFACTORYBOX_PEER_OCI")
		guest("/bin/nested-runc --root /run/nested-runc run --detach --bundle /inner-bundle child </dev/null >/tmp/nested-create.stdout 2>/tmp/nested-create.stderr")
	}
	if out := guest("/bin/nested-runc --root /run/nested-runc exec child /bin/echo nested-peer-ready"); out != "nested-peer-ready\n" {
		t.Fatal("nested peer did not retain useful work", out)
	}
	if !create {
		guest("/bin/nested-runc --root /run/nested-runc delete --force child")
	}
}
