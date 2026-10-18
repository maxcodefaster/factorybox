package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestLazyObservationRejectsStaleAndUnsafeFiles(t *testing.T) {
	counters := map[string]uint64{}
	for _, field := range criuObserveFields {
		counters[field] = 0
	}
	value := criuLazyObservation{Schema: 1, DaemonPID: 1234, Sequence: 1, ClockNS: 100,
		Total: counters, Active: []criuLazyObservationReader{
			{ID: 1, ImagePID: 784, NativePID: 4500, Counts: counters},
			{ID: 2, ImagePID: 784, NativePID: -1, Counts: counters},
		}}
	directory := t.TempDir()
	path := filepath.Join(directory, "observation.json")
	write := func(observation criuLazyObservation) {
		data, err := json.Marshal(observation)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(value)
	if _, err := criuObserveRead(path, 1234); err != nil {
		t.Fatal(err)
	}
	if _, err := criuObserveRead(path, 2222); err == nil {
		t.Fatal("accepted a stale daemon's observation")
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := criuObserveRead(path, 1234); err == nil {
		t.Fatal("accepted public observation data")
	}
	os.Chmod(path, 0600)
	symlink := filepath.Join(directory, "symlink.json")
	if err := os.Symlink(path, symlink); err != nil {
		t.Fatal(err)
	}
	if _, err := criuObserveRead(symlink, 1234); err == nil {
		t.Fatal("followed a symlink to observation data")
	}
	fifo := filepath.Join(directory, "fifo")
	if err := unix.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := criuObserveRead(fifo, 1234); err == nil {
		t.Fatal("accepted a blocking/special observation file")
	}
	value.Active[1].ID = 1
	write(value)
	if _, err := criuObserveRead(path, 1234); err == nil {
		t.Fatal("accepted duplicate reader identities")
	}
	value.Active[1].ID = 2
	delete(value.Total, "copy_ns")
	write(value)
	if _, err := criuObserveRead(path, 1234); err == nil {
		t.Fatal("accepted incomplete native counters")
	}
	value.Total["copy_ns"] = 0
	write(value)
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	file.WriteString("\n{}")
	file.Close()
	if _, err := criuObserveRead(path, 1234); err == nil {
		t.Fatal("accepted trailing observation messages")
	}
}

func TestLazyObservationRequiresOwnedDiagnosticScope(t *testing.T) {
	t.Setenv("CRIU_SYSBOX_LAZY_OBSERVE", "1")
	for _, name := range []string{"CRIU_SYSBOX_LAZY_INHERIT", "CRIU_SYSBOX_RETAIN_SOURCE",
		"CRIU_SYSBOX_LAZY", "CRIU_SYSBOX_PROBE"} {
		t.Setenv(name, "1")
	}
	t.Setenv("CRIU_SYSBOX_PROFILE", "0")
	if err := criuLazyObserveGuard("/experiment", "source-sysbox"); err != nil {
		t.Fatal(err)
	}
	if err := criuLazyObserveGuard("/other", "source-sysbox"); err == nil {
		t.Fatal("accepted an unowned observation root")
	}
	t.Setenv("CRIU_SYSBOX_LAZY_INHERIT", "0")
	if err := criuLazyObserveGuard("/experiment", "source-sysbox"); err == nil {
		t.Fatal("observation accepted without owned daemon lifecycle")
	}
	t.Setenv("CRIU_SYSBOX_LAZY_OBSERVE", "0")
	if err := criuLazyObserveGuard("/other", "unrelated"); err != nil {
		t.Fatalf("disabled observation changed control: %v", err)
	}
	t.Setenv("CRIU_SYSBOX_LAZY_OBSERVE", "true")
	if err := criuLazyObserveGuard("/experiment", "source-sysbox"); err == nil {
		t.Fatal("accepted an invalid diagnostic flag")
	}
}
