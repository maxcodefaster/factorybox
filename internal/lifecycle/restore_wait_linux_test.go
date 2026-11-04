//go:build linux

package lifecycle

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func restoreWaitSocket(t *testing.T) (int, int) {
	t.Helper()
	fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_SEQPACKET|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		unix.Close(fds[0])
		unix.Close(fds[1])
	})
	return fds[0], fds[1]
}

func TestRestoreHoldKeepsCallerBudget(t *testing.T) {
	reader, writer := restoreWaitSocket(t)
	want := []byte("owned pre-resume identity")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	delivered := make(chan error, 1)
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		timer := time.NewTimer(16 * time.Second)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			delivered <- ctx.Err()
			return
		case <-timer.C:
		}
		_, err := unix.Write(writer, want)
		delivered <- err
	}()
	t.Cleanup(func() {
		cancel()
		<-finished
	})
	start := time.Now()
	packet, err := readRestoreHold(ctx, reader)
	if err != nil || !bytes.Equal(packet, want) || ctx.Err() != nil {
		t.Fatalf("restore shortened its live caller budget after %s: packet=%q error=%v caller=%v",
			time.Since(start), packet, err, ctx.Err())
	}
	if err := <-delivered; err != nil {
		t.Fatal(err)
	}
}

func TestRestoreHoldRejectsClosedSocket(t *testing.T) {
	reader, writer := restoreWaitSocket(t)
	if err := unix.Shutdown(writer, unix.SHUT_WR); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if packet, err := readRestoreHold(ctx, reader); err == nil || len(packet) != 0 {
		t.Fatalf("restore accepted a closed hold socket: packet=%q error=%v", packet, err)
	}
}

func TestRestoreHoldHonorsCallerCancellation(t *testing.T) {
	reader, _ := restoreWaitSocket(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := readRestoreHold(ctx, reader)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("restore did not retain caller cancellation: %v", err)
	}
}

func TestRestoreHoldHonorsExpiredCaller(t *testing.T) {
	reader, _ := restoreWaitSocket(t)
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	_, err := readRestoreHold(ctx, reader)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("restore revived an expired caller: %v", err)
	}
}

func TestRestoreHoldHonorsInFlightCancellation(t *testing.T) {
	reader, _ := restoreWaitSocket(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	timer := time.AfterFunc(20*time.Millisecond, cancel)
	defer timer.Stop()
	_, err := readRestoreHold(ctx, reader)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("restore ignored cancellation while waiting for its owned socket: %v", err)
	}
}

func TestRestoreHoldRetainsTheOriginalPacket(t *testing.T) {
	reader, writer := restoreWaitSocket(t)
	want := []byte("original sealed hold identity")
	if _, err := unix.Write(writer, want); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	packet, err := readRestoreHold(ctx, reader)
	if err != nil || !bytes.Equal(packet, want) {
		t.Fatalf("restore changed its hold packet: %q %v", packet, err)
	}
}

func TestRestoreCommandKeepsTheLifecycleBudget(t *testing.T) {
	for _, action := range []string{"factorybox-restore", "factorybox-resume"} {
		parent, cancelParent := context.WithTimeout(context.Background(), 7*time.Minute)
		defer cancelParent()
		ctx, cancel := runtimeCommandContext(parent, []string{"--criu", "/original/criu", action, "original-runtime"})
		defer cancel()
		want, _ := parent.Deadline()
		got, bounded := ctx.Deadline()
		if !bounded || !got.Equal(want) {
			t.Fatalf("%s shortened its caller deadline: got=%v want=%v", action, got, want)
		}
	}
}

func TestOrdinaryRuntimeCommandsKeepTheirExistingBound(t *testing.T) {
	for _, args := range [][]string{{"state", "original-runtime"}, {"delete", "original-runtime"}, {"--criu", "/original/criu", "different-command", "original-runtime"}} {
		parent, cancelParent := context.WithTimeout(context.Background(), 7*time.Minute)
		defer cancelParent()
		start := time.Now()
		ctx, cancel := runtimeCommandContext(parent, args)
		defer cancel()
		deadline, bounded := ctx.Deadline()
		if !bounded || deadline.Before(start.Add(time.Minute)) || deadline.After(time.Now().Add(time.Minute)) {
			t.Fatalf("non-restore command changed its existing bound: args=%v deadline=%v", args, deadline)
		}
	}
}

func TestRestoreWorkerRejectsExpiredSealedDeadline(t *testing.T) {
	if os.Getenv("FACTORYBOX_RESTORE_EXPIRED_DEADLINE") == "1" {
		handled, err := RestoreWorkerCommand([]string{"_restore-worker", "factorybox-resume", "original-runtime"})
		if !handled || !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("expired worker reached resource admission: handled=%t error=%v", handled, err)
		}
		return
	}
	fd, err := unix.MemfdCreate("restore-expired-deadline", unix.MFD_CLOEXEC|unix.MFD_ALLOW_SEALING)
	if err != nil {
		t.Fatal(err)
	}
	file := os.NewFile(uintptr(fd), "sealed expired restore")
	defer file.Close()
	request := restoreWorkerRequest{
		Version: 1, Deadline: time.Now().Add(-time.Second).UnixNano(),
		BootID: "original-boot", TargetID: "target", TargetSHA: strings.Repeat("a", 64),
		RequestSHA: strings.Repeat("b", 64), Operation: "resume",
	}
	data, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write(data); err != nil {
		t.Fatal(err)
	}
	if _, err := unix.FcntlInt(file.Fd(), unix.F_ADD_SEALS, captureRequestSeals); err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(executable, "-test.run=^TestRestoreWorkerRejectsExpiredSealedDeadline$")
	command.Env = append(os.Environ(), "FACTORYBOX_RESTORE_EXPIRED_DEADLINE=1")
	command.ExtraFiles = []*os.File{file}
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("sealed restore deadline was not honored: %v: %s", err, output)
	}
}
