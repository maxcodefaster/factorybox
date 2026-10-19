//go:build linux

package lifecycle

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

type timeActorState struct {
	Payload   []byte `json:"payload"`
	Counter   int    `json:"counter"`
	Namespace string `json:"namespace"`
	MonoSec   int64  `json:"mono_sec"`
	MonoNSec  int64  `json:"mono_nsec"`
	BootSec   int64  `json:"boot_sec"`
	BootNSec  int64  `json:"boot_nsec"`
}

func timeActorOffsets() (monoSec, monoNSec, bootSec, bootNSec int64, err error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/timens_offsets", unix.Gettid()))
	if err != nil {
		return 0, 0, 0, 0, err
	}
	values := map[string][2]int64{}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 3 {
			return 0, 0, 0, 0, fmt.Errorf("invalid actor clock offset")
		}
		sec, e1 := strconv.ParseInt(fields[1], 10, 64)
		nsec, e2 := strconv.ParseInt(fields[2], 10, 64)
		if e1 != nil || e2 != nil {
			return 0, 0, 0, 0, fmt.Errorf("invalid actor clock offset numbers")
		}
		values[fields[0]] = [2]int64{sec, nsec}
	}
	m, mOK := values["monotonic"]
	b, bOK := values["boottime"]
	if !mOK || !bOK {
		return 0, 0, 0, 0, fmt.Errorf("actor clock offsets missing")
	}
	return m[0], m[1], b[0], b[1], nil
}

func TestTimeActorLauncher(t *testing.T) {
	group := os.Getenv("FACTORYBOX_TIME_LAUNCHER")
	if group != "ab" && group != "c" {
		t.Skip("owned time namespace actor launcher only")
	}
	runtime.LockOSThread()
	m, mn, b, bn, err := timeActorOffsets()
	if err != nil {
		t.Fatal(err)
	}
	delta := int64(120)
	roles := []string{"a", "b"}
	if group == "c" {
		delta, roles = 360, []string{"c"}
	}
	if err := unix.Unshare(unix.CLONE_NEWTIME); err != nil {
		t.Fatal(err)
	}
	offsets := fmt.Sprintf("monotonic %d %d\nboottime %d %d\n", m+delta, mn, b+2*delta, bn)
	if err := os.WriteFile(fmt.Sprintf("/proc/%d/timens_offsets", unix.Gettid()), []byte(offsets), 0600); err != nil {
		t.Fatal(err)
	}
	for _, role := range roles {
		child := exec.Command(os.Args[0], "-test.run=^TestTimeActor$", "-test.timeout=0")
		child.Env = append(os.Environ(), "FACTORYBOX_TIME_ACTOR="+role)
		if err := child.Start(); err != nil {
			t.Fatal(err)
		}
		if err := child.Process.Release(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestTimeActor(t *testing.T) {
	role := os.Getenv("FACTORYBOX_TIME_ACTOR")
	if role != "root" && role != "a" && role != "b" && role != "c" {
		t.Skip("owned time namespace actor only")
	}
	if role == "root" {
		if err := os.Mkdir("/state", 0700); err != nil {
			t.Fatal(err)
		}
		for _, group := range []string{"ab", "c"} {
			launcher := exec.Command(os.Args[0], "-test.run=^TestTimeActorLauncher$", "-test.timeout=0")
			launcher.Env = append(os.Environ(), "FACTORYBOX_TIME_LAUNCHER="+group)
			if output, err := launcher.CombinedOutput(); err != nil {
				t.Fatalf("clock actor launcher: %v: %s", err, output)
			}
		}
	}
	state := timeActorState{Payload: make([]byte, 8192)}
	if _, err := rand.Read(state.Payload); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("unix", "/state/time-"+role+".sock")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	for {
		client, err := listener.Accept()
		if err != nil {
			t.Fatal(err)
		}
		func() {
			defer client.Close()
			var command string
			if err := json.NewDecoder(io.LimitReader(client, 1024)).Decode(&command); err != nil {
				t.Error(err)
				return
			}
			switch command {
			case "get":
			case "advance":
				state.Counter++
				state.Payload[0] ^= 0xff
			default:
				t.Error("invalid clock actor command")
				return
			}
			state.Namespace, err = os.Readlink("/proc/thread-self/ns/time")
			if err == nil {
				state.MonoSec, state.MonoNSec, state.BootSec, state.BootNSec, err = timeActorOffsets()
			}
			if err != nil {
				t.Error(err)
				return
			}
			if err := json.NewEncoder(client).Encode(state); err != nil {
				t.Error(err)
			}
		}()
	}
}

func readTimeActor(pid int, role, command string) (timeActorState, error) {
	var result timeActorState
	client, err := net.DialTimeout("unix",
		fmt.Sprintf("/proc/%d/root/state/time-%s.sock", pid, role), time.Second)
	if err != nil {
		return result, err
	}
	defer client.Close()
	client.SetDeadline(time.Now().Add(time.Second))
	if err := json.NewEncoder(client).Encode(command); err != nil {
		return result, err
	}
	data, err := io.ReadAll(io.LimitReader(client, 32<<10))
	if err != nil {
		return result, err
	}
	return result, json.Unmarshal(data, &result)
}

func timeTree(t *testing.T, pid int) map[string]timeActorState {
	t.Helper()
	tree := map[string]timeActorState{}
	for _, role := range []string{"root", "a", "b", "c"} {
		deadline := time.Now().Add(5 * time.Second)
		for {
			state, err := readTimeActor(pid, role, "get")
			if err == nil {
				tree[role] = state
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("time actor not ready", role, err)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	r, a, b, c := tree["root"], tree["a"], tree["b"], tree["c"]
	if a.Namespace != b.Namespace || a.Namespace == r.Namespace ||
		c.Namespace == r.Namespace || c.Namespace == a.Namespace {
		t.Fatal("time namespace sharing or separation differs", tree)
	}
	for role, delta := range map[string]int64{"a": 120, "b": 120, "c": 360} {
		state := tree[role]
		if state.MonoSec-r.MonoSec != delta || state.MonoNSec != r.MonoNSec ||
			state.BootSec-r.BootSec != 2*delta || state.BootNSec != r.BootNSec {
			t.Fatal("namespace relative clock offsets differ", role, state, r)
		}
	}
	return tree
}

func sameTimeTree(t *testing.T, expected, actual map[string]timeActorState) {
	t.Helper()
	for role, before := range expected {
		after := actual[role]
		if before.Namespace == after.Namespace {
			t.Fatal("restoration reused the original time namespace", role)
		}
		if before.Counter != after.Counter || !reflect.DeepEqual(before.Payload, after.Payload) {
			t.Fatal("restoration lost original actor RAM", role)
		}
	}
}

func TestTimeNamespaceForkNative(t *testing.T) {
	if os.Getenv("FACTORYBOX_NATIVE_TIME") != "1" {
		t.Skip("requires the bounded positive nested-time restore fixture")
	}
	engine, bundle := nativeEngine(t,
		"/bin/env FACTORYBOX_TIME_ACTOR=root /bin/time-actor -test.run=^TestTimeActor$ -test.timeout=0")
	binary, err := os.ReadFile(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bundle+"/lower/bin/time-actor", binary, 0755); err != nil {
		t.Fatal(err)
	}
	config, err := os.ReadFile(bundle + "/config.json")
	if err != nil {
		t.Fatal(err)
	}
	var spec map[string]any
	if err := json.Unmarshal(config, &spec); err != nil {
		t.Fatal(err)
	}
	args := spec["process"].(map[string]any)["args"].([]any)
	args[2] = strings.ReplaceAll(args[2].(string), ">/dev/null", ">/time-actor.log")
	config, err = json.Marshal(spec)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bundle+"/config.json", config, 0600); err != nil {
		t.Fatal(err)
	}
	engine.config.Criu, engine.config.CriuSHA256 = "/out/criu", os.Getenv("FACTORYBOX_CRIU_SHA256")
	engine.config.ManagerRoot = "/var/lib/sysbox/manager"
	ctx := context.Background()
	source, err := engine.Start(ctx, "time-source", bundle)
	if err != nil {
		log, _ := os.ReadFile(bundle + "/rootfs/time-actor.log")
		t.Fatalf("%v: %s", err, log)
	}
	t.Cleanup(func() {
		current, stateErr := engine.Inspect(ctx, "time-source")
		if stateErr == nil && current.Record.Phase == "deleted" {
			return
		}
		group, groupErr := captureGuardOpen(cgroupRoot + "/" + source.Record.RuntimeID)
		if groupErr != nil {
			t.Error(groupErr)
			return
		}
		events, groupErr := captureGuardEvents(group)
		group.Close()
		if groupErr != nil || events["frozen"] != 0 {
			t.Log("retain unresolved time source for exact outer archive", groupErr)
			return
		}
		if _, err := engine.Delete(ctx, "time-source"); err != nil {
			t.Error(err)
		}
	})
	before := timeTree(t, source.Runtime.PID)
	snapshot := strings.Repeat("b", 32)
	if _, err := engine.Checkpoint(ctx, "time-source", snapshot); err != nil {
		t.Fatal("positive nested time namespace checkpoint", err)
	}
	branch, err := forkNativeCLI(engine, "time-source", snapshot, "time-fork")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := engine.Delete(ctx, "time-fork"); err != nil {
			t.Error(err)
		}
	})
	cloned := timeTree(t, branch.Runtime.PID)
	sameTimeTree(t, before, cloned)
	advanced, err := readTimeActor(branch.Runtime.PID, "a", "advance")
	if err != nil || advanced.Counter != 1 {
		t.Fatal("fork did not continue its original actor", err)
	}
	retained := timeTree(t, source.Runtime.PID)
	for role, state := range before {
		if !reflect.DeepEqual(state, retained[role]) {
			t.Fatal("fork mutation changed source state or clocks", role)
		}
	}
	if _, err := engine.Delete(ctx, "time-source"); err != nil {
		t.Fatal(err)
	}
	pauseID := strings.Repeat("c", 32)
	if _, err := engine.Pause(ctx, "time-fork", pauseID); err != nil {
		t.Fatal(err)
	}
	resumed, err := engine.Resume(ctx, "time-fork", pauseID)
	if err != nil {
		t.Fatal(err)
	}
	cloned["a"] = advanced
	restored := timeTree(t, resumed.Runtime.PID)
	sameTimeTree(t, cloned, restored)
}
