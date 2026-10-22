//go:build linux

package worker

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestWorkerRefusesHostProcess(t *testing.T) {
	if os.Getpid() == 1 {
		t.Skip("portable host-process guard is tested outside the worker")
	}
	if err := Run(context.Background(), Config{}); err == nil || !strings.Contains(err.Error(), "PID 1") {
		t.Fatal("worker was allowed outside its Docker init boundary", err)
	}
}

func TestProcessBirthIdentity(t *testing.T) {
	actual, err := birth(os.Getpid())
	if err != nil || actual.PID != os.Getpid() || actual.Birth == "" {
		t.Fatal(actual, err)
	}
	if err := live(actual); err != nil {
		t.Fatal(err)
	}
	wrong := actual
	wrong.Birth += "0"
	if err := live(wrong); err == nil {
		t.Fatal("PID without its exact birth was accepted")
	}
	if err := live(Process{PID: -1, Birth: actual.Birth}); err == nil {
		t.Fatal("invalid process was accepted")
	}
}

func TestWorkerRecordsRejectSymlinks(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "record.json")
	if err := os.WriteFile(file, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(file, filepath.Join(root, "link.json")); err != nil {
		t.Fatal(err)
	}
	var value any
	if err := readJSON(filepath.Join(root, "link.json"), &value); !errors.Is(err, unix.ELOOP) {
		t.Fatal("private worker record followed a symlink", err)
	}
}
