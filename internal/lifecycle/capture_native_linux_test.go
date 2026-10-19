//go:build linux

package lifecycle

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"factorybox/internal/sysboxcapture"
	"golang.org/x/sys/unix"
)

type captureActorState struct {
	Payload      []byte `json:"payload"`
	Counter      int    `json:"counter"`
	BallastSHA   string `json:"ballast_sha,omitempty"`
	BallastBytes int    `json:"ballast_bytes,omitempty"`
	NestedPID    int    `json:"nested_pid,omitempty"`
	TimeMode     string `json:"time_mode,omitempty"`
	CgroupLimit  string `json:"cgroup_limit,omitempty"`
}

func TestCaptureTimeNamespaceLauncher(t *testing.T) {
	if os.Getenv("FACTORYBOX_CAPTURE_TIME_LAUNCHER") != "1" {
		t.Skip("owned nested-time-namespace launcher only")
	}
	runtime.LockOSThread()
	// Keep the creator out of the final captured tree. Its child enters the
	// new time namespace; PID 1 retains its original time/time_for_children.
	if err := unix.Unshare(unix.CLONE_NEWTIME); err != nil {
		t.Fatal(err)
	}
	child := exec.Command("/bin/sleep", "300")
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("/state/nested-time.pid", []byte(strconv.Itoa(child.Process.Pid)), 0600); err != nil {
		t.Fatal(err)
	}
	if err := child.Process.Release(); err != nil {
		t.Fatal(err)
	}
}

func TestCaptureActor(t *testing.T) {
	if os.Getenv("FACTORYBOX_CAPTURE_ACTOR") != "1" {
		t.Skip("native capture workload only")
	}
	state := captureActorState{Payload: make([]byte, 8192)}
	var futureStop, futureDone chan struct{}
	futureTID := 0
	if _, err := rand.Read(state.Payload); err != nil {
		t.Fatal(err)
	}
	var ballast []byte
	var cgroupLimit *os.File
	if os.Getenv("FACTORYBOX_CAPTURE_BALLAST") == "1" {
		var err error
		ballast, err = unix.Mmap(-1, 0, 32<<20, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_PRIVATE|unix.MAP_ANON)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := rand.Read(ballast); err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(ballast)
		state.BallastSHA, state.BallastBytes = fmt.Sprintf("%x", digest), len(ballast)
	}
	if err := os.Mkdir("/state", 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("/state/marker", []byte("retained-source-file\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Link("/state/marker", "/state/marker-hardlink"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("marker", "/state/marker-link"); err != nil {
		t.Fatal(err)
	}
	if err := unix.Setxattr("/state/marker", "user.factorybox", []byte("captured-xattr"), 0); err != nil {
		t.Fatal(err)
	}
	if err := unix.Mkfifo("/state/fifo", 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("/var/lib/docker/capture-marker", []byte("private-daemon-before\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if os.Getenv("FACTORYBOX_CAPTURE_NESTED_TIME") == "1" {
		state.TimeMode = "child"
		launcher := exec.Command(os.Args[0], "-test.run=^TestCaptureTimeNamespaceLauncher$", "-test.timeout=0")
		launcher.Env = append(os.Environ(), "FACTORYBOX_CAPTURE_TIME_LAUNCHER=1")
		if output, err := launcher.CombinedOutput(); err != nil {
			t.Fatalf("nested time namespace launcher: %v: %s", err, output)
		}
		data, err := os.ReadFile("/state/nested-time.pid")
		if err != nil {
			t.Fatal(err)
		}
		state.NestedPID, err = strconv.Atoi(string(data))
		if err != nil || state.NestedPID <= 1 {
			t.Fatal("nested time namespace child lacks its exact PID", err)
		}
	}
	if os.Getenv("FACTORYBOX_CAPTURE_NESTED_TIME") == "children-only" {
		runtime.LockOSThread()
		if err := unix.Unshare(unix.CLONE_NEWTIME); err != nil {
			t.Fatal(err)
		}
		state.TimeMode = "children-only"
	}
	if os.Getenv("FACTORYBOX_CAPTURE_NESTED_TIME") == "future-thread" {
		type ready struct {
			tid int
			err error
		}
		started := make(chan ready)
		futureStop, futureDone = make(chan struct{}), make(chan struct{})
		go func() {
			runtime.LockOSThread()
			defer close(futureDone)
			err := unix.Unshare(unix.CLONE_NEWTIME)
			started <- ready{tid: unix.Gettid(), err: err}
			if err == nil {
				<-futureStop
			}
			// Returning with the thread locked retires this OS thread and
			// its private future-children namespace, without setns().
		}()
		result := <-started
		if result.err != nil {
			t.Fatal(result.err)
		}
		futureTID = result.tid
		child := exec.Command("/bin/sleep", "300")
		if err := child.Start(); err != nil {
			t.Fatal(err)
		}
		state.NestedPID, state.TimeMode = child.Process.Pid, "future-thread"
		if err := child.Process.Release(); err != nil {
			t.Fatal(err)
		}
	}
	server, err := net.Listen("unix", "/state/control.sock")
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	var mutex sync.Mutex
	for {
		client, err := server.Accept()
		if err != nil {
			t.Fatal(err)
		}
		func() {
			defer client.Close()
			var request string
			if err := json.NewDecoder(io.LimitReader(client, 1024)).Decode(&request); err != nil {
				t.Error(err)
				return
			}
			mutex.Lock()
			defer mutex.Unlock()
			switch request {
			case "open-cgroup-limit":
				if cgroupLimit != nil {
					t.Error("cgroup limit descriptor already open")
					return
				}
				cgroupLimit, err = os.Open("/sys/fs/cgroup/docker/first/memory.max")
				if err != nil {
					t.Error(err)
					return
				}
			case "make-shared-child":
				if state.NestedPID != 0 {
					t.Error("owned child already exists")
					return
				}
				child := exec.Command("/bin/sleep", "300")
				if err := child.Start(); err != nil {
					t.Error(err)
					return
				}
				state.NestedPID, state.TimeMode = child.Process.Pid, "shared"
				if err := child.Process.Release(); err != nil {
					t.Error(err)
					return
				}
			case "drop-time-child":
				if state.NestedPID <= 1 {
					t.Error("no owned time namespace child")
					return
				}
				if err := unix.Kill(state.NestedPID, unix.SIGTERM); err != nil {
					t.Error(err)
					return
				}
				var status unix.WaitStatus
				pid, err := unix.Wait4(state.NestedPID, &status, 0, nil)
				if err != nil || pid != state.NestedPID || !status.Signaled() || status.Signal() != unix.SIGTERM {
					t.Error("owned time namespace child did not terminate", pid, status, err)
					return
				}
				if futureStop != nil {
					close(futureStop)
					<-futureDone
					deadline := time.Now().Add(time.Second)
					for {
						_, err := os.Stat(fmt.Sprintf("/proc/self/task/%d", futureTID))
						if os.IsNotExist(err) {
							break
						}
						if err != nil || time.Now().After(deadline) {
							t.Error("future-children thread did not retire", err)
							return
						}
						time.Sleep(time.Millisecond)
					}
					futureStop = nil
				}
				state.NestedPID, state.TimeMode = 0, ""
			case "advance":
				state.Payload[0] ^= 0xff
				state.Counter++
				if len(ballast) != 0 {
					ballast[0] ^= 0xff
					digest := sha256.Sum256(ballast)
					state.BallastSHA = fmt.Sprintf("%x", digest)
				}
				if err := os.WriteFile("/state/marker", []byte("source-after\n"), 0600); err != nil {
					t.Error(err)
				}
				if err := os.WriteFile("/var/lib/docker/capture-marker", []byte("private-daemon-after\n"), 0600); err != nil {
					t.Error(err)
				}
			case "get":
				if len(ballast) != 0 {
					digest := sha256.Sum256(ballast)
					state.BallastSHA = fmt.Sprintf("%x", digest)
				}
			case "flip-ballast-without-refresh":
				if len(ballast) < 2 {
					t.Error("live-hash control requires anonymous ballast")
					return
				}
				ballast[1] ^= 0xff
			default:
				t.Error("unknown RAM actor request")
				return
			}
			if cgroupLimit != nil {
				var value [64]byte
				n, err := cgroupLimit.ReadAt(value[:], 0)
				if err != nil && err != io.EOF {
					t.Error("read retained cgroup limit descriptor", err)
					return
				}
				state.CgroupLimit = strings.TrimSpace(string(value[:n]))
			}
			if err := json.NewEncoder(client).Encode(state); err != nil {
				t.Error(err)
			}
		}()
	}
}

func readCaptureActor(pid int, request string) (captureActorState, error) {
	var state captureActorState
	client, err := net.DialTimeout("unix", fmt.Sprintf("/proc/%d/root/state/control.sock", pid), time.Second)
	if err != nil {
		return state, err
	}
	defer client.Close()
	if err := client.SetDeadline(time.Now().Add(time.Second)); err != nil {
		return state, err
	}
	if err := json.NewEncoder(client).Encode(request); err != nil {
		return state, err
	}
	// Wait for the workload to close its connection. No external control
	// socket may remain open when CRIU captures the source.
	data, err := io.ReadAll(io.LimitReader(client, 64<<10))
	if err != nil {
		return state, err
	}
	return state, json.Unmarshal(data, &state)
}

func TestRetainedCaptureNative(t *testing.T) {
	if os.Getenv("FACTORYBOX_NATIVE_RETAINED_CAPTURE") != "1" {
		t.Skip("requires the bounded capture fixture with rebuilt CRIU")
	}
	engine, bundle := nativeEngine(t, "/bin/env FACTORYBOX_CAPTURE_ACTOR=1 /bin/capture-actor -test.run=^TestCaptureActor$ -test.timeout=0")
	binary, err := os.ReadFile(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bundle+"/lower/bin/capture-actor", binary, 0755); err != nil {
		t.Fatal(err)
	}
	engine.config.Criu, engine.config.CriuSHA256 = "/out/criu", os.Getenv("FACTORYBOX_CRIU_SHA256")
	engine.config.ManagerRoot = "/var/lib/sysbox/manager"
	prepareNativeBase(t, engine, bundle)
	ctx := context.Background()
	view, err := engine.Start(ctx, "captured", bundle)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := engine.Delete(ctx, "captured"); err != nil {
			t.Errorf("delete owned capture source: %v", err)
		}
	})
	hostPanic, err := os.ReadFile("/proc/sys/kernel/panic")
	if err != nil {
		t.Fatal(err)
	}
	writeVirtual := func(value string) {
		t.Helper()
		control, err := engine.lock()
		if err != nil {
			t.Fatal(err)
		}
		defer control.Close()
		if _, err := engine.command(ctx, control, "exec", view.Record.RuntimeID,
			"/bin/sh", "-c", "printf '"+value+"\\n' > /proc/sys/kernel/panic"); err != nil {
			t.Fatal("write private Sysbox virtual setting", err)
		}
	}
	writeVirtual("17")
	deadline := time.Now().Add(5 * time.Second)
	var before captureActorState
	for {
		before, err = readCaptureActor(view.Runtime.PID, "get")
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("RAM actor not ready", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(before.Payload) != 8192 || before.Counter != 0 {
		t.Fatal("RAM actor did not initialize its private payload")
	}
	snapshotID := strings.Repeat("d", 32)
	generation := engine.config.Root + "/objects/captured/captures/" + snapshotID
	checkpoint := func() (Checkpoint, error) {
		command := exec.Command("/factorybox", "--root", engine.config.Root,
			"--runtime", engine.config.Runtime, "--runtime-sha256", engine.config.RuntimeSHA256,
			"--criu", engine.config.Criu, "--criu-sha256", engine.config.CriuSHA256,
			"--manager-root", engine.config.ManagerRoot, "checkpoint", "--snapshot-id", snapshotID, "captured")
		output, err := command.CombinedOutput()
		var result Checkpoint
		if err != nil {
			return result, fmt.Errorf("%w: %s", err, output)
		}
		if err := json.Unmarshal(output, &result); err != nil {
			return result, fmt.Errorf("checkpoint output: %w: %s", err, output)
		}
		return result, nil
	}
	started := time.Now()
	saved, err := checkpoint()
	captureElapsed := time.Since(started)
	if err != nil {
		t.Fatal(err)
	}
	if saved.ID != snapshotID || saved.SourcePID != view.Runtime.PID || saved.ReleasedAt == "" || len(saved.Files) == 0 {
		t.Fatal("public checkpoint did not publish its exact generation and source release")
	}
	started = time.Now()
	replayed, err := checkpoint()
	replayElapsed := time.Since(started)
	if err != nil || !replayedCheckpoint(saved, replayed) {
		t.Fatal("checkpoint replay created another generation or changed outcome", err)
	}
	fsStatePath := generation + "/sysbox-state.json"
	fsState, err := os.ReadFile(fsStatePath)
	if err != nil {
		t.Fatal(err)
	}
	var virtual struct {
		Version int               `json:"version"`
		Source  string            `json:"source"`
		Data    map[string][]byte `json:"data"`
	}
	if err := json.Unmarshal(fsState, &virtual); err != nil || virtual.Version != 1 ||
		virtual.Source != view.Record.RuntimeID ||
		strings.TrimSpace(string(virtual.Data["/proc/sys/kernel/panic"])) != "17" {
		t.Fatalf("FUSE snapshot did not capture its source's virtual value: %v", err)
	}
	images := generation + "/images"
	files, err := filepath.Glob(images + "/pages-*.img")
	if err != nil || len(files) == 0 {
		t.Fatalf("actual captured memory pages absent: %v", err)
	}
	hashes := map[string]string{}
	payloadFound := false
	for _, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		hashes[path] = sum(data)
		payloadFound = payloadFound || bytes.Contains(data, before.Payload)
	}
	if !payloadFound {
		t.Fatal("complete ordinary page writer did not contain the source's private RAM payload")
	}
	for path, expected := range map[string]string{
		"/filesystem/upper/state/marker":            "retained-source-file\n",
		"/filesystem/volumes/docker/capture-marker": "private-daemon-before\n",
	} {
		data, err := os.ReadFile(generation + path)
		if err != nil || string(data) != expected {
			t.Fatalf("filesystem checkpoint content differs: %s: %v", path, err)
		}
	}
	one, err := os.Stat(generation + "/filesystem/upper/state/marker")
	if err != nil {
		t.Fatal(err)
	}
	two, err := os.Stat(generation + "/filesystem/upper/state/marker-hardlink")
	if err != nil || !os.SameFile(one, two) {
		t.Fatal("captured hardlink relationship lost", err)
	}
	if target, err := os.Readlink(generation + "/filesystem/upper/state/marker-link"); err != nil || target != "marker" {
		t.Fatal("captured symlink target differs", err)
	}
	writeVirtual("29")
	preserved, err := os.ReadFile(fsStatePath)
	if err != nil || !bytes.Equal(preserved, fsState) {
		t.Fatal("source virtual-setting mutation changed the saved checkpoint", err)
	}
	if err := os.WriteFile(fsStatePath, append(append([]byte(nil), fsState...), ' '), 0600); err != nil {
		t.Fatal(err)
	}
	_, corruptErr := checkpoint()
	if err := os.WriteFile(fsStatePath, fsState, 0600); err != nil {
		t.Fatal(err)
	}
	if corruptErr == nil {
		t.Fatal("checkpoint replay accepted corrupted metadata")
	}
	readyPath := generation + "/checkpoint.json"
	readyBytes, err := os.ReadFile(readyPath)
	if err != nil {
		t.Fatal(err)
	}
	checkCorruptManifest := func(name string, change func(Checkpoint) Checkpoint) {
		t.Helper()
		t.Run(name, func(t *testing.T) {
			original, err := engine.readCheckpoint("objects/captured/captures/"+snapshotID, "checkpoint.json")
			if err != nil {
				t.Fatal(err)
			}
			data, err := json.Marshal(change(original))
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(readyPath, data, 0600); err != nil {
				t.Fatal(err)
			}
			_, rejected := checkpoint()
			if err := os.WriteFile(readyPath, readyBytes, 0600); err != nil {
				t.Fatal(err)
			}
			if rejected == nil {
				t.Fatal("checkpoint accepted a changed manifest")
			}
		})
	}
	checkCorruptManifest("filesystem-root-omission", func(value Checkpoint) Checkpoint {
		value.Filesystem.Roots = value.Filesystem.Roots[:2]
		return value
	})
	checkCorruptManifest("capture-timestamp-change", func(value Checkpoint) Checkpoint {
		value.CapturedAt = now()
		return value
	})
	t.Run("oversized-record-and-symlink", func(t *testing.T) {
		if err := os.WriteFile(readyPath, append(append([]byte(nil), readyBytes...), bytes.Repeat([]byte(" "), checkpointManifestLimit)...), 0600); err != nil {
			t.Fatal(err)
		}
		_, oversized := checkpoint()
		if err := os.Rename(readyPath, readyPath+".oversized"); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(readyPath+".oversized", readyPath); err != nil {
			t.Fatal(err)
		}
		_, symlink := checkpoint()
		if err := os.Remove(readyPath); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(readyPath, readyBytes, 0600); err != nil {
			t.Fatal(err)
		}
		if oversized == nil || symlink == nil {
			t.Fatal("checkpoint accepted an oversized or symlinked record", oversized, symlink)
		}
	})
	control, err := engine.lock()
	if err != nil {
		t.Fatal(err)
	}
	_, liveExportErr := engine.command(ctx, control, "factorybox-fs-snapshot",
		"--file", fsStatePath, "--owner", view.Record.Owner, view.Record.RuntimeID)
	control.Close()
	if liveExportErr == nil {
		t.Fatal("unfrozen live source exported as an atomic snapshot")
	}
	afterHostPanic, err := os.ReadFile("/proc/sys/kernel/panic")
	if err != nil || !bytes.Equal(afterHostPanic, hostPanic) {
		t.Fatal("private virtual setting changed the worker kernel value", err)
	}
	after, err := readCaptureActor(view.Runtime.PID, "advance")
	expected := append([]byte(nil), before.Payload...)
	expected[0] ^= 0xff
	if err != nil || after.Counter != 1 || !bytes.Equal(after.Payload, expected) {
		t.Fatalf("original RAM process did not continue independently: %v", err)
	}
	for path, expected := range map[string]string{
		"/filesystem/upper/state/marker":            "retained-source-file\n",
		"/filesystem/volumes/docker/capture-marker": "private-daemon-before\n",
	} {
		data, err := os.ReadFile(generation + path)
		if err != nil || string(data) != expected {
			t.Fatalf("source filesystem mutation changed checkpoint: %s: %v", path, err)
		}
	}
	for path, expected := range hashes {
		data, err := os.ReadFile(path)
		if err != nil || sum(data) != expected {
			t.Fatalf("source mutation changed captured page backing: %s: %v", path, err)
		}
	}
	observed, err := engine.Inspect(ctx, "captured")
	if err != nil || observed.Runtime == nil || observed.Runtime.PID != view.Runtime.PID {
		t.Fatalf("capture replaced or lost its source: %+v: %v", observed, err)
	}
	// Simulate losing only the final publication after a fully persisted
	// capture and source release. Recovery must reuse the captured result,
	// even though the source's RAM, files and virtual state have advanced.
	if err := os.Rename(readyPath, generation+"/checkpoint-original.json"); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(generation+"/capture-result.json", generation+"/capture-result-hidden.json"); err != nil {
		t.Fatal(err)
	}
	_, incomplete := checkpoint()
	if err := os.Rename(generation+"/capture-result-hidden.json", generation+"/capture-result.json"); err != nil {
		t.Fatal(err)
	}
	if incomplete == nil || !strings.Contains(incomplete.Error(), "refusing a second capture") {
		t.Fatal("incomplete generation silently repeated capture", incomplete)
	}
	guardOutcome := generation + "/guard/guard-outcome.json"
	if err := os.Rename(guardOutcome, guardOutcome+".hidden"); err != nil {
		t.Fatal(err)
	}
	_, unresolved := checkpoint()
	if err := os.Rename(guardOutcome+".hidden", guardOutcome); err != nil {
		t.Fatal(err)
	}
	if unresolved == nil || !strings.Contains(unresolved.Error(), "guard outcome is unresolved") {
		t.Fatal("checkpoint recovery raced an unresolved guard", unresolved)
	}
	recovered, err := checkpoint()
	if err != nil || recovered.CapturedAt != saved.CapturedAt || recovered.SourcePID != saved.SourcePID {
		t.Fatal("completed capture was repeated or failed publication recovery", err)
	}
	again, err := checkpoint()
	if err != nil || !replayedCheckpoint(recovered, again) {
		t.Fatal("recovered checkpoint replay changed its result", err)
	}
	// A second lost publication must reuse the durable recovery outcome too.
	if err := os.Rename(readyPath, generation+"/checkpoint-first-recovery.json"); err != nil {
		t.Fatal(err)
	}
	again, err = checkpoint()
	if err != nil || !replayedCheckpoint(recovered, again) {
		t.Fatal("repeated publication recovery changed its result", err)
	}
	if _, err := engine.Delete(ctx, "captured"); err != nil {
		t.Fatal("delete original after complete capture", err)
	}
	afterDelete, err := checkpoint()
	if err != nil || !replayedCheckpoint(recovered, afterDelete) {
		t.Fatal("ready checkpoint depended on its original runtime", err)
	}
	if err := os.Rename(readyPath, generation+"/checkpoint-before-delete.json"); err != nil {
		t.Fatal(err)
	}
	afterDelete, err = checkpoint()
	if err != nil || !replayedCheckpoint(recovered, afterDelete) {
		t.Fatal("completed publication recovery depended on its original runtime", err)
	}
	// The caller-owned original overlay is no longer available at any saved
	// source path. Keep its bytes offline, then restore the fixture's layout
	// only for exact archival and the original cleanup hook.
	if err := unix.Unmount(bundle+"/rootfs", 0); err != nil {
		t.Fatal("detach original source overlay after runtime deletion", err)
	}
	offlineBundle := bundle + "-offline"
	if err := os.Rename(bundle, offlineBundle); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Rename(offlineBundle, bundle); err != nil {
			t.Error("restore owned source archive layout", err)
			return
		}
		if err := unix.Mount("overlay", bundle+"/rootfs", "overlay", 0,
			"lowerdir="+bundle+"/lower,upperdir="+bundle+"/upper,workdir="+bundle+"/work"); err != nil {
			t.Error("restore owned source fixture mount for exact cleanup", err)
		}
	})
	if _, err := os.Lstat(view.Record.Rootfs); !os.IsNotExist(err) {
		t.Fatal("original source filesystem path remained available", err)
	}
	for path, expected := range map[string]string{
		"/filesystem/upper/state/marker":            "retained-source-file\n",
		"/filesystem/volumes/docker/capture-marker": "private-daemon-before\n",
	} {
		data, err := os.ReadFile(generation + path)
		if err != nil || string(data) != expected {
			t.Fatalf("source deletion removed captured filesystem state: %s: %v", path, err)
		}
	}
	t.Run("independent-filesystem-materialization", func(t *testing.T) {
		leftPath := filepath.Dir(generation) + "/" + strings.Repeat("a", 32)
		rightPath := filepath.Dir(generation) + "/" + strings.Repeat("b", 32)
		left, err := sysboxcapture.MaterializeFilesystemSnapshot(ctx, generation, leftPath, recovered.Filesystem)
		if err != nil {
			t.Fatal("materialize first filesystem after original runtime deletion", err)
		}
		right, err := sysboxcapture.MaterializeFilesystemSnapshot(ctx, generation, rightPath, recovered.Filesystem)
		if err != nil {
			t.Fatal("materialize second independent filesystem", err)
		}
		if err := sysboxcapture.VerifyFilesystemSnapshot(ctx, rightPath, right); err != nil {
			t.Fatal(err)
		}
		for _, tree := range left.Roots {
			if tree.Saved != leftPath+"/filesystem/"+tree.Name {
				t.Fatal("materialized filesystem names a shared saved root", tree.Saved)
			}
		}
		marker := "/filesystem/upper/state/marker"
		leftMarker, err := os.Stat(leftPath + marker)
		if err != nil {
			t.Fatal(err)
		}
		rightMarker, err := os.Stat(rightPath + marker)
		if err != nil || os.SameFile(leftMarker, rightMarker) {
			t.Fatal("materialized siblings share a writable inode", err)
		}
		leftLink, err := os.Stat(leftPath + marker + "-hardlink")
		if err != nil || !os.SameFile(leftMarker, leftLink) {
			t.Fatal("materialization broke an internal hardlink", err)
		}
		for path, value := range map[string]string{
			marker: "left-private-file\n",
			"/filesystem/volumes/docker/capture-marker": "left-private-volume\n",
		} {
			if err := os.WriteFile(leftPath+path, []byte(value), 0600); err != nil {
				t.Fatal(err)
			}
		}
		if err := sysboxcapture.VerifyFilesystemSnapshot(ctx, leftPath, left); err == nil {
			t.Fatal("materialized target mutation escaped its saved digest")
		}
		if err := sysboxcapture.VerifyFilesystemSnapshot(ctx, generation, recovered.Filesystem); err != nil {
			t.Fatal("target mutation changed its parent checkpoint", err)
		}
		if err := sysboxcapture.VerifyFilesystemSnapshot(ctx, rightPath, right); err != nil {
			t.Fatal("target mutation changed its sibling", err)
		}
		if _, err := sysboxcapture.MaterializeFilesystemSnapshot(ctx, generation, leftPath, recovered.Filesystem); err == nil {
			t.Fatal("materialization replaced an existing target")
		}
		if _, err := sysboxcapture.MaterializeFilesystemSnapshot(ctx, generation, generation+"/nested", recovered.Filesystem); err == nil {
			t.Fatal("materialization wrote inside its source checkpoint")
		}
		canceled, cancel := context.WithCancel(ctx)
		cancel()
		canceledPath := filepath.Dir(generation) + "/" + strings.Repeat("c", 32)
		if _, err := sysboxcapture.MaterializeFilesystemSnapshot(canceled, generation, canceledPath, recovered.Filesystem); err == nil {
			t.Fatal("materialization ignored cancellation")
		}
		if _, err := os.Lstat(canceledPath); !os.IsNotExist(err) {
			t.Fatal("canceled materialization created a partial target", err)
		}
		t.Log("two complete filesystem siblings survive original-runtime deletion; target writes change neither checkpoint nor sibling; internal hardlinks and separate inodes verified")
		t.Run("restore-without-original-and-fork-again", func(t *testing.T) {
			restored := forkNative(t, engine, view.Record, snapshotID, "restored", before)
			assertRestoredNative(t, engine, restored, before)
			forkFailuresNative(t, engine, view.Record.ID, snapshotID)
			forkControllerLossNative(t, engine, view.Record, snapshotID, before)
			forkFromRestoredNative(t, engine, restored, before)
			if err := sysboxcapture.VerifyFilesystemSnapshot(ctx, generation, recovered.Filesystem); err != nil {
				t.Fatal("restored descendant changed the original checkpoint", err)
			}
			t.Log("restored private RAM, virtual state and Docker-volume file after original runtime deletion; recaptured restored source and independently restored/mutated its descendant")
		})
	})
	afterHostPanic, err = os.ReadFile("/proc/sys/kernel/panic")
	if err != nil || !bytes.Equal(afterHostPanic, hostPanic) {
		t.Fatal("restored descendant's virtual-setting write changed the worker kernel", err)
	}
	t.Logf("actual capture under %s retained PID %d, included the 8192-byte private RAM payload in %d page files and preserved RAM, virtual state, overlay files and a managed Docker-volume file after source mutation; original runtime deleted and its rootfs path unavailable during public fork/descendant checks; worker kernel value unchanged; pause/resume remain separate gates",
		view.Record.RuntimeID, view.Runtime.PID, len(files))
	t.Logf("individual CLI observation: checkpoint=%s integrity-verified replay=%s; not a fork/resume or p95 benchmark", captureElapsed, replayElapsed)
}

func replayedCheckpoint(a, b Checkpoint) bool {
	left, _ := json.Marshal(a)
	right, _ := json.Marshal(b)
	return bytes.Equal(left, right)
}
