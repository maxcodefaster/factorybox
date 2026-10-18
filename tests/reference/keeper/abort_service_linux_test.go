package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestKeeperServiceCaptureEOFContainmentNative(t *testing.T) {
	keeperFailureServiceNative(t, false)
}

func TestKeeperServiceCommitReplyLossNative(t *testing.T) {
	keeperFailureServiceNative(t, true)
}

func sendCommitWithoutReply(connection *net.UnixConn, bytes uint64) error {
	if err := connection.CloseRead(); err != nil {
		return err
	}
	var packet [40]byte
	binary.LittleEndian.PutUint32(packet[:], captureMagic)
	binary.LittleEndian.PutUint32(packet[4:], 3)
	binary.LittleEndian.PutUint64(packet[8:], bytes)
	n, _, err := connection.WriteMsgUnix(packet[:], nil, nil)
	if err != nil || n != len(packet) {
		return fmt.Errorf("unacknowledged COMMIT send failed: %d %v", n, err)
	}
	return connection.Close()
}

func keeperFailureServiceNative(t *testing.T, replyLoss bool) {
	if os.Getenv("KEEPER_ABORT_NATIVE") != "1" {
		t.Skip("exactly owned private-cgroup native service fixture only")
	}
	evidence := os.Getenv("KEEPER_ABORT_EVIDENCE")
	if evidence == "" || os.Getenv("PIPE_READ_NATIVE") == "" {
		t.Fatal("native evidence and actual C reader paths are required")
	}
	const imageRoot = "/experiment/sysbox"
	const group = "/sys/fs/cgroup/criu-probe-sysbox"
	for _, path := range []string{imageRoot, "/experiment/pipe-store", group} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatal("fixture requires absent exact roots", path, err)
		}
	}
	t.Cleanup(func() { os.RemoveAll(imageRoot) })
	for _, parent := range []string{"images-again", "images-repeat"} {
		path := imageRoot + "/" + parent
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatal(err)
		}
		if err := unix.Mount("owned-keeper-abort-image", path, "tmpfs",
			unix.MS_NOSUID|unix.MS_NODEV|unix.MS_NOEXEC, "size=2097152,mode=0700"); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := unix.Unmount(path, 0); err != nil {
				t.Error("owned raw image mount remained", path, err)
			}
		})
	}
	if err := os.Mkdir(group, 0700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Remove(group); err != nil {
			t.Error("owned source cgroup remained", err)
		}
	})
	source := exec.Command("/bin/sleep", "60")
	if err := source.Start(); err != nil {
		t.Fatal(err)
	}
	sourceStopped := false
	t.Cleanup(func() {
		if !sourceStopped {
			source.Process.Kill()
			source.Wait()
		}
	})
	if err := os.WriteFile(group+"/cgroup.procs", []byte(strconv.Itoa(source.Process.Pid)), 0600); err != nil {
		t.Fatal(err)
	}
	sourceIdentity, err := sourceAt(source.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	log, err := os.OpenFile(filepath.Join(evidence, "keeper.log"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	keeper := exec.Command("/pipe-keeper")
	keeper.Env = append(os.Environ(), "RUNTIME_FORK_PIPE_READ=lazy", "RUNTIME_FORK_PIPE_RAW_BASE=1",
		"RUNTIME_FORK_PIPE_COMBINED_DIGEST=1")
	keeper.Stdout, keeper.Stderr = log, log
	if err := keeper.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- keeper.Wait() }()
	keeperStopped := false
	t.Cleanup(func() {
		if !keeperStopped {
			select {
			case <-done:
			default:
				keeper.Process.Kill()
				<-done
			}
		}
		if data, err := os.ReadFile("/experiment/pipe-store/result.json"); err == nil {
			if err := os.WriteFile(filepath.Join(evidence, "keeper-result.json"), data, 0600); err != nil {
				t.Error(err)
			}
		}
		os.RemoveAll("/experiment/pipe-store")
	})
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := os.Stat("/experiment/pipe-store/ready.json"); err == nil {
			break
		}
		select {
		case err := <-done:
			keeperStopped = true
			t.Fatal("actual keeper exited before ready", err)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("actual keeper did not become ready")
		}
		time.Sleep(5 * time.Millisecond)
	}
	payload := append(bytes.Repeat([]byte{17}, 4096), bytes.Repeat([]byte{29}, 4096)...)
	begin := func(id int, includeBuffer bool) (*net.UnixConn, string) {
		t.Helper()
		parent := "images-repeat"
		if id == 7 {
			parent = "images-again"
		}
		path := fmt.Sprintf("%s/%s/pages-%d.img", imageRoot, parent, id)
		file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		if _, err := file.Write(payload); err != nil {
			t.Fatal(err)
		}
		connection, err := net.DialUnix("unixpacket", nil,
			&net.UnixAddr{Name: "/experiment/pipe-store/control.sock", Net: "unixpacket"})
		if err != nil {
			t.Fatal("keeper control service lost after an uncommitted capture", err)
		}
		t.Cleanup(func() { connection.Close() })
		if err := connection.SetDeadline(time.Now().Add(1500 * time.Millisecond)); err != nil {
			t.Fatal(err)
		}
		if err := sendFixture(connection, captureMessage{
			op: 1, a: uint64(id), b: uint64(source.Process.Pid), fds: []int{int(file.Fd())},
		}); err != nil {
			if replyLoss {
				t.Fatal("keeper cannot admit another capture after committed reply loss", err)
			}
			t.Fatal("keeper cannot admit another capture after producer EOF", err)
		}
		if includeBuffer {
			var pipe [2]int
			if err := unix.Pipe2(pipe[:], unix.O_CLOEXEC); err != nil {
				t.Fatal(err)
			}
			defer unix.Close(pipe[0])
			defer unix.Close(pipe[1])
			if n, err := unix.Write(pipe[1], payload); err != nil || n != len(payload) {
				t.Fatal("complete owned pipe payload unavailable", n, err)
			}
			if err := sendFixture(connection, captureMessage{
				op: 2, b: 8192, c: 2, fds: []int{pipe[0]},
				spans: []captureSpan{{0, 4096, 0, 0}, {4096, 4096, 4096, 1}},
			}); err != nil {
				t.Fatal(err)
			}
		}
		return connection, path
	}
	commit := func(id int) string {
		t.Helper()
		connection, path := begin(id, true)
		if err := sendFixture(connection, captureMessage{op: 3, a: 8192}); err != nil {
			t.Fatal("actual committed capture rejected", err)
		}
		connection.Close()
		return path
	}
	first := commit(7)
	publish := func(parent string) {
		t.Helper()
		if err := unix.Mount("", imageRoot+"/"+parent, "",
			unix.MS_REMOUNT|unix.MS_RDONLY|unix.MS_NOSUID|unix.MS_NODEV|unix.MS_NOEXEC, ""); err != nil {
			t.Fatal("raw C reader publication must be filesystem-wide readonly", err)
		}
	}
	publish("images-again")
	readers := 0
	readCommitted := func(path string) {
		t.Helper()
		nativeReadCommand(t, "/experiment/pipe-store/read.sock", path, "normal")
		readers++
	}
	readCommitted(first)
	const disruptions = 70
	var lostReplyImages []string
	for index := 0; index < disruptions; index++ {
		// The next acknowledged BEGIN proves the preceding connection was
		// handled before the old committed image is read again.
		connection, path := begin(8+index, replyLoss || index%2 == 0)
		if index > 0 {
			readCommitted(first)
		}
		if replyLoss {
			if err := sendCommitWithoutReply(connection, 8192); err != nil {
				t.Fatal(err)
			}
			deadline := time.Now().Add(1500 * time.Millisecond)
			for {
				var token [16]byte
				n, err := unix.Getxattr(path, readImageAttribute, token[:])
				if err == nil && n == len(token) {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("COMMIT never reached immutable image publication", path, n, err)
				}
				time.Sleep(time.Millisecond)
			}
			lostReplyImages = append(lostReplyImages, path)
		} else {
			connection.Close()
		}
	}
	if !replyLoss {
		self, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		client := exec.Command(self, "-test.run=^TestKeeperCaptureAbortClientHelper$", "-test.timeout=10s")
		client.Env = append(os.Environ(), "KEEPER_ABORT_CLIENT=1",
			"KEEPER_ABORT_SOURCE_PID="+strconv.Itoa(source.Process.Pid))
		clientLog, err := os.OpenFile(filepath.Join(evidence, "killed-client.log"),
			os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			t.Fatal(err)
		}
		defer clientLog.Close()
		client.Stderr = clientLog
		ready, err := client.StdoutPipe()
		if err != nil {
			t.Fatal(err)
		}
		defer ready.Close()
		if err := client.Start(); err != nil {
			t.Fatal(err)
		}
		clientStopped := false
		defer func() {
			if !clientStopped {
				client.Process.Kill()
				client.Wait()
			}
		}()
		prepared := make(chan error, 1)
		go func() {
			line, err := bufio.NewReader(ready).ReadString('\n')
			if err == nil && line != "capture-ready\n" {
				err = fmt.Errorf("unexpected producer readiness: %q", line)
			}
			prepared <- err
		}()
		select {
		case err := <-prepared:
			if err != nil {
				t.Fatal("real capture producer did not transfer its pending descriptors", err)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("real capture producer did not become ready for SIGKILL")
		}
		if err := client.Process.Kill(); err != nil {
			t.Fatal(err)
		}
		waitErr := client.Wait()
		clientStopped = true
		var killed *exec.ExitError
		if !errors.As(waitErr, &killed) || killed.Sys().(syscall.WaitStatus).Signal() != syscall.SIGKILL {
			t.Fatal("capture producer was not actually killed by SIGKILL", waitErr)
		}
	}
	currentSource, err := sourceAt(source.Process.Pid)
	if err != nil || currentSource != sourceIdentity {
		t.Fatal("capture producer death changed the original source identity", currentSource, err)
	}
	second := commit(10000)
	publish("images-repeat")
	readCommitted(first)
	readCommitted(second)
	for _, path := range lostReplyImages {
		readCommitted(path)
	}
	entries, err := os.ReadDir(fmt.Sprintf("/proc/%d/fd", keeper.Process.Pid))
	if err != nil {
		t.Fatal("actual keeper disappeared before its retained descriptors could be inspected", err)
	}
	actualFDs, rawFDs := map[string]string{}, map[string]int{}
	pipeModes := map[string][]int{}
	for _, entry := range entries {
		target, err := os.Readlink(fmt.Sprintf("/proc/%d/fd/%s", keeper.Process.Pid, entry.Name()))
		if err != nil {
			if os.IsNotExist(err) {
				continue // An already completed read's socket/counter can close here.
			}
			t.Fatal(err)
		}
		actualFDs[entry.Name()] = target
		if strings.HasPrefix(target, imageRoot+"/") {
			rawFDs[target]++
		}
		if strings.HasPrefix(target, "pipe:[") {
			info, err := os.ReadFile(fmt.Sprintf("/proc/%d/fdinfo/%s", keeper.Process.Pid, entry.Name()))
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, line := range strings.Split(string(info), "\n") {
				fields := strings.Fields(line)
				if len(fields) == 2 && fields[0] == "flags:" {
					flags, err := strconv.ParseUint(fields[1], 8, 32)
					if err != nil {
						t.Fatal(err)
					}
					pipeModes[target] = append(pipeModes[target], int(flags)&unix.O_ACCMODE)
					found = true
				}
			}
			if !found {
				t.Fatal("actual pipe lacks access flags")
			}
		}
	}
	expectedImages := 2 + len(lostReplyImages)
	retainedPipes, cachedScratchPipes := 0, 0
	for target, modes := range pipeModes {
		if len(modes) == 1 && modes[0] == unix.O_RDONLY {
			retainedPipes++
		} else if len(modes) == 2 &&
			((modes[0] == unix.O_RDONLY && modes[1] == unix.O_WRONLY) ||
				(modes[1] == unix.O_RDONLY && modes[0] == unix.O_WRONLY)) {
			cachedScratchPipes++
		} else {
			t.Fatal("actual keeper owns an unexpected pipe descriptor set", target, modes)
		}
	}
	if len(rawFDs) != expectedImages || rawFDs[first] != 1 || rawFDs[second] != 1 ||
		retainedPipes != expectedImages || cachedScratchPipes < expectedImages || cachedScratchPipes > 4*expectedImages {
		t.Fatal("actual keeper image/pipe FD ownership differs", actualFDs)
	}
	for _, path := range lostReplyImages {
		if rawFDs[path] != 1 {
			t.Fatal("committed image with lost reply lost its exact raw anchor", path, actualFDs)
		}
	}
	fdEvidence, err := json.MarshalIndent(actualFDs, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(evidence, "keeper-fds-after-aborts.json"), fdEvidence, 0600); err != nil {
		t.Fatal(err)
	}
	if err := source.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err := source.Wait(); err == nil {
		t.Fatal("owned source did not terminate by the requested signal")
	}
	sourceStopped = true
	if err := os.WriteFile("/experiment/pipe-store/finish", nil, 0600); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		keeperStopped = true
		if err != nil {
			t.Fatal("actual keeper did not finish cleanly", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("actual keeper remained after exact finish")
	}
	data, err := os.ReadFile("/experiment/pipe-store/result.json")
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	aborted := disruptions + 1
	if replyLoss {
		aborted = 0
	}
	if result["status"] != "passed" || result["aborted_captures_total"] != float64(aborted) ||
		result["aborted_capture_history_limit"] != float64(64) ||
		result["registered_images"] != float64(expectedImages) ||
		result["active_capture_images"] != float64(0) ||
		result["active_capture_credit_bytes"] != float64(0) ||
		result["active_capture_credit_descriptors"] != float64(0) {
		t.Fatal("aborted transfer isolation or bounded ownership accounting differs", result)
	}
	var abortRows []any
	if result["aborted_captures"] != nil {
		abortRows = result["aborted_captures"].([]any)
	}
	if len(abortRows) != min(aborted, 64) {
		t.Fatal("abort history is incomplete or unbounded", result)
	}
	for _, row := range abortRows {
		value := row.(map[string]any)
		identity := value["source"].(map[string]any)
		if identity["pid"] != float64(source.Process.Pid) ||
			identity["start_time"] != sourceIdentity.StartTime || value["reason"] != "producer_eof_before_commit" {
			t.Fatal("abort evidence lost exact source identity", value)
		}
	}
	if replyLoss {
		if result["commit_reply_failures_total"] != float64(disruptions) ||
			result["commit_reply_history_limit"] != float64(64) ||
			len(result["commit_reply_failures"].([]any)) != 64 {
			t.Fatal("committed reply loss history incomplete or unbounded", result)
		}
		for _, row := range result["commit_reply_failures"].([]any) {
			value := row.(map[string]any)
			identity := value["source"].(map[string]any)
			info, err := os.Stat(value["image"].(string))
			if err != nil {
				t.Fatal(err)
			}
			stat := info.Sys().(*syscall.Stat_t)
			if identity["pid"] != float64(source.Process.Pid) || identity["start_time"] != sourceIdentity.StartTime ||
				value["reason"] != "committed_reply_failed" || value["captured_bytes"] != float64(8192) ||
				value["sha256"] != fmt.Sprintf("%x", sha256.Sum256(payload)) ||
				value["device"] != float64(stat.Dev) || value["inode"] != float64(stat.Ino) {
				t.Fatal("committed reply failure lost immutable source/outcome identity", value)
			}
		}
		t.Logf("actual keeper retained %d committed images despite %d missing COMMIT replies; %d complete real C reads, actual raw/pipe FDs, synced exports and zero final credits verified", expectedImages, disruptions, readers)
	} else {
		t.Logf("actual keeper survived %d pre-commit disconnects including actual producer SIGKILL; %d full real C reads kept the committed bytes; actual raw/pipe FDs, two final synced exports and zero active capture credits verified", aborted, readers)
	}
}

func TestKeeperCaptureAbortClientHelper(t *testing.T) {
	if os.Getenv("KEEPER_ABORT_CLIENT") != "1" {
		t.Skip("subprocess of exactly owned keeper failure fixture only")
	}
	source, err := strconv.Atoi(os.Getenv("KEEPER_ABORT_SOURCE_PID"))
	if err != nil || source <= 0 {
		t.Fatal("actual source PID absent", err)
	}
	file, err := os.OpenFile("/experiment/sysbox/images-repeat/pages-9000.img",
		os.O_RDWR|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	payload := append(bytes.Repeat([]byte{17}, 4096), bytes.Repeat([]byte{29}, 4096)...)
	if _, err := file.Write(payload); err != nil {
		t.Fatal(err)
	}
	var pipe [2]int
	if err := unix.Pipe2(pipe[:], unix.O_CLOEXEC); err != nil {
		t.Fatal(err)
	}
	defer unix.Close(pipe[0])
	defer unix.Close(pipe[1])
	if n, err := unix.Write(pipe[1], payload); err != nil || n != len(payload) {
		t.Fatal("real producer pipe payload unavailable", n, err)
	}
	connection, err := net.DialUnix("unixpacket", nil,
		&net.UnixAddr{Name: "/experiment/pipe-store/control.sock", Net: "unixpacket"})
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	if err := sendFixture(connection, captureMessage{op: 1, a: 9000, b: uint64(source),
		fds: []int{int(file.Fd())}}); err != nil {
		t.Fatal(err)
	}
	if err := sendFixture(connection, captureMessage{op: 2, b: 8192, c: 2, fds: []int{pipe[0]},
		spans: []captureSpan{{0, 4096, 0, 0}, {4096, 4096, 4096, 1}}}); err != nil {
		t.Fatal(err)
	}
	fmt.Fprintln(os.Stdout, "capture-ready")
	<-time.After(5 * time.Second)
	t.Fatal("capture producer was not killed by its parent fixture")
}

func TestCaptureEOFWithDiscardFailureRemainsFatal(t *testing.T) {
	store, connection, done, file, _, source := captureFixture(t)
	if err := sendFixture(connection, captureMessage{
		op: 1, a: 7, b: uint64(source.Process.Pid), fds: []int{int(file.Fd())},
	}); err != nil {
		t.Fatal(err)
	}
	store.mu.RLock()
	image := store.pending[file.Name()]
	store.mu.RUnlock()
	if image == nil || image.golden == nil {
		t.Fatal("actual pending image missing")
	}
	// A deliberately invalidated owned handle makes cleanup uncertain.
	// EOF must not be classified as safely contained in that case.
	if err := image.golden.Close(); err != nil {
		t.Fatal(err)
	}
	connection.Close()
	err := <-done
	if err == nil || !errors.Is(err, os.ErrClosed) {
		t.Fatal("discard ownership error was lost", err)
	}
	if _, contained := err.(*captureAbortError); contained {
		t.Fatal("uncertain descriptor cleanup was accepted as a local abort")
	}
	if store.liveImages != 0 || store.heldBytes != 0 || store.heldFDs != 0 || len(store.pending) != 0 {
		t.Fatal("failed cleanup retained admission credits")
	}
}

func TestCaptureProtocolRejectionIsNotAnAbort(t *testing.T) {
	_, connection, done, _, _, _ := captureFixture(t)
	if err := sendFixture(connection, captureMessage{op: 99}); err == nil {
		t.Fatal("unknown capture command was accepted")
	}
	err := <-done
	var aborted *captureAbortError
	if err == nil || errors.As(err, &aborted) {
		t.Fatal("protocol rejection was weakened to a contained producer EOF", err)
	}
}

func TestFailedCommitWithLostReplyRemainsFatal(t *testing.T) {
	store, connection, done, file, pipe, source := captureFixture(t)
	for _, message := range []captureMessage{
		{op: 1, a: 7, b: uint64(source.Process.Pid), fds: []int{int(file.Fd())}},
		{op: 2, b: 8192, c: 2, fds: []int{pipe[0]},
			spans: []captureSpan{{0, 4096, 0, 0}, {4096, 4096, 4096, 1}}},
	} {
		if err := sendFixture(connection, message); err != nil {
			t.Fatal(err)
		}
	}
	// The original writer and retained private bytes now disagree.
	// Losing the reply must not conceal that failed commit.
	if _, err := file.WriteAt([]byte{18}, 0); err != nil {
		t.Fatal(err)
	}
	if err := sendCommitWithoutReply(connection, 8192); err != nil {
		t.Fatal(err)
	}
	err := <-done
	var reply *captureReplyError
	if err == nil || errors.As(err, &reply) || !strings.Contains(err.Error(), "capture digest failed") {
		t.Fatal("failed integrity check was weakened to successful commit/reply loss", err)
	}
	if store.registeredImages != 0 || len(store.images) != 0 || store.liveImages != 0 ||
		store.heldBytes != 0 || store.heldFDs != 0 || len(store.pending) != 0 {
		t.Fatal("failed commit retained a published image or ownership credits")
	}
	var token [16]byte
	if _, err := unix.Fgetxattr(int(file.Fd()), readImageAttribute, token[:]); !errors.Is(err, unix.ENODATA) {
		t.Fatal("failed commit published a retained image token", err)
	}
}
