//go:build linux

package worker

import (
	"errors"
	"strings"
	"testing"
)

func TestCheckPrivateCgroupRoot(t *testing.T) {
	if err := checkPrivateCgroupRoot([]byte("0::/\n"), nil); err != nil {
		t.Fatal("private root refused", err)
	}
	host := []byte("0::/kubepods.slice/kubepods-besteffort.slice/cri-containerd-abc.scope\n")
	err := checkPrivateCgroupRoot(host, nil)
	if err == nil || !strings.Contains(err.Error(), "lacks a private cgroup namespace") ||
		!strings.Contains(err.Error(), "kubepods.slice") {
		t.Fatal("host cgroup path was accepted or its value was not named", err)
	}
	if strings.Contains(err.Error(), "<nil>") {
		t.Fatal("refusal formats a nil error", err)
	}
	read := errors.New("no such file")
	if err := checkPrivateCgroupRoot(nil, read); !errors.Is(err, read) {
		t.Fatal("read failure was not wrapped", err)
	}
	if err := checkPrivateCgroupRoot([]byte("12:memory:/x\n0::/x\n"), nil); err == nil {
		t.Fatal("cgroup v1 style listing was accepted")
	}
}

func TestPrivateCgroupNamespaceStepsAreNoOpsOutsideTheWorker(t *testing.T) {
	// The test binary is not PID 1 and shares its own namespace, so both steps
	// must leave the process unchanged and never re-execute it.
	if err := EnsurePrivateCgroupNamespace(); err != nil {
		t.Fatal(err)
	}
	if err := JoinWorkerCgroupNamespace(); err != nil {
		t.Fatal(err)
	}
}
