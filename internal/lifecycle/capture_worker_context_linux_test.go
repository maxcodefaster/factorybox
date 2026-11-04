//go:build linux

package lifecycle

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestCaptureWorkerUsesCallerBudget(t *testing.T) {
	for _, duration := range []time.Duration{7 * time.Minute, 20 * time.Second} {
		t.Run(duration.String(), func(t *testing.T) {
			parent, cancelParent := context.WithTimeout(context.Background(), duration)
			defer cancelParent()
			ctx, cancel := lifecycleWorkerContext(parent)
			defer cancel()
			want, _ := parent.Deadline()
			got, ok := ctx.Deadline()
			if !ok || !got.Equal(want) {
				t.Fatalf("capture shortened its caller's lifecycle budget: got %v, want %v", got, want)
			}
			cancelParent()
			if !errors.Is(ctx.Err(), context.Canceled) {
				t.Fatalf("capture lost its caller's cancellation: %v", ctx.Err())
			}
		})
	}
}

func TestCaptureWorkerBoundsAnUnboundedCaller(t *testing.T) {
	started := time.Now()
	ctx, cancel := lifecycleWorkerContext(context.Background())
	defer cancel()
	deadline, ok := ctx.Deadline()
	if !ok || deadline.Before(started.Add(7*time.Minute)) || deadline.After(time.Now().Add(7*time.Minute)) {
		t.Fatalf("unbounded caller requires the finite seven-minute capture budget: %v", deadline)
	}
	cancel()
	if !errors.Is(ctx.Err(), context.Canceled) {
		t.Fatalf("capture cancellation is not retained: %v", ctx.Err())
	}
}

func TestCaptureWorkerKeepsAnExpiredCaller(t *testing.T) {
	parent, cancelParent := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancelParent()
	ctx, cancel := lifecycleWorkerContext(parent)
	defer cancel()
	if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
		t.Fatalf("capture revived an expired caller: %v", ctx.Err())
	}
}

func TestCaptureWorkerRejectsExpiredSealedDeadline(t *testing.T) {
	if os.Getenv("FACTORYBOX_CAPTURE_EXPIRED_DEADLINE") == "1" {
		handled, err := CaptureWorkerCommand([]string{"_capture"})
		if !handled || !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("expired worker reached source admission instead of its deadline: handled=%t error=%v", handled, err)
		}
		return
	}
	fd, err := unix.MemfdCreate("capture-expired-deadline", unix.MFD_CLOEXEC|unix.MFD_ALLOW_SEALING)
	if err != nil {
		t.Fatal(err)
	}
	request := os.NewFile(uintptr(fd), "sealed expired capture deadline")
	defer request.Close()
	if err := json.NewEncoder(request).Encode(captureRequest{
		Deadline: time.Now().Add(-time.Second).UnixNano(),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := request.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := unix.FcntlInt(request.Fd(), unix.F_ADD_SEALS, captureRequestSeals); err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(executable, "-test.run=^TestCaptureWorkerRejectsExpiredSealedDeadline$")
	command.Env = append(os.Environ(), "FACTORYBOX_CAPTURE_EXPIRED_DEADLINE=1")
	command.ExtraFiles = []*os.File{request}
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("sealed capture deadline was not honored: %v: %s", err, output)
	}
}
