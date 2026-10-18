package main

import (
	"errors"
	"io"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestLazyRestoreRejectsInvalidPrefetchBeforeStartingRuntime(t *testing.T) {
	t.Setenv("CRIU_SYSBOX_LAZY", "1")
	t.Setenv("CRIU_SYSBOX_LAZY_LOW_LOG", "0")
	for _, mode := range []string{"2", "-1", "off", " 0", "00"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("CRIU_SYSBOX_LAZY_PREFETCH", mode)
			output, err := criuSysboxRestore("/experiment/sysbox/images", "/unavailable-runtime")
			if err == nil || !strings.Contains(err.Error(), "CRIU_SYSBOX_LAZY_PREFETCH must be 0 or 1") || output != "" {
				t.Fatalf("invalid mode reached runtime setup: output=%q err=%v", output, err)
			}
		})
	}
}

func TestLazyMaterializationRetainsFailureAcrossAttempts(t *testing.T) {
	done := make(chan error, 1)
	failure := errors.New("page daemon failed")
	done <- failure
	close(done)
	d := &criuLazyDaemon{done: done}
	for range 2 {
		if err := d.materialize(time.Second); !errors.Is(err, failure) {
			t.Fatalf("failed daemon was accepted after completion was consumed: %v", err)
		}
	}
}

func TestLazyMaterializationDoesNotAcceptSignalDeliveryAsCompletion(t *testing.T) {
	command := exec.Command("sh", "-c", "trap '' USR1; printf ready; exec sleep 5")
	ready, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	d := &criuLazyDaemon{command: command, done: make(chan error, 1), captureControl: true}
	go func() { d.done <- command.Wait(); close(d.done) }()
	defer func() {
		command.Process.Kill()
		if !d.pollCompletion() {
			<-d.done
		}
	}()
	bytes := make([]byte, 5)
	if _, err := io.ReadFull(ready, bytes); err != nil || string(bytes) != "ready" {
		t.Fatalf("owned process did not establish its ignored signal: %q %v", bytes, err)
	}
	if err := d.materialize(25 * time.Millisecond); err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("signal delivery was accepted as materialization: %v", err)
	}
	if d.finished {
		t.Fatal("still-running daemon was reported finished")
	}
}
