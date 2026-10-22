//go:build linux

package lifecycle

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestCaptureSessionTopologyBoundary(t *testing.T) {
	first := captureSessionRoot{TID: 17, Namespace: [2]uint64{1, 41}}
	second := captureSessionRoot{TID: 23, Namespace: first.Namespace}
	other := captureSessionRoot{TID: 31, Namespace: [2]uint64{1, 42}}
	for name, roots := range map[string][]captureSessionRoot{"ordinary": nil, "factory-single": {first}, "separate-containers": {first, other}} {
		t.Run(name, func(t *testing.T) {
			if got := captureMultipleSessionRoots(roots); len(got) != 0 {
				t.Fatal("evidenced single-root or independent-namespace topology was rejected", got)
			}
		})
	}
	want := []captureDescriptorBlocker{{TID: first.TID, FD: -1, Kind: "nested-exec-session"}, {TID: second.TID, FD: -1, Kind: "nested-exec-session"}}
	if got := captureMultipleSessionRoots([]captureSessionRoot{first, other, second}); !reflect.DeepEqual(got, want) {
		t.Fatal("multiple exec roots in one namespace were not identified exactly", got, want)
	}
}

func TestCaptureSessionParentIdentity(t *testing.T) {
	for name, status := range map[string]string{
		"missing": "Name:\tservice\n", "zero": "PPid:\t0\n", "negative": "PPid:\t-1\n",
		"ambiguous": "PPid:\t12\nPPid:\t13\n", "malformed": "PPid:\t12\t13\n",
		"text": "PPid:\tother\n", "excessive": strings.Repeat(" ", (16<<10)+1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := captureStatusParent([]byte(status)); err == nil {
				t.Fatal("invalid parent identity admitted")
			}
		})
	}
	parent, err := captureStatusParent([]byte("Name:\tservice\nPPid:\t42\n"))
	if err != nil || parent != 42 {
		t.Fatal("valid kernel parent lost", parent, err)
	}
	if foreign, err := captureExternalSessionRoot(-1, 1, nil); foreign || err != nil {
		t.Fatal("namespace init incorrectly requires a parent in its own namespace", foreign, err)
	}
	status, err := os.ReadFile("/proc/self/status")
	if err != nil {
		t.Fatal(err)
	}
	if foreign, err := captureExternalSessionRoot(os.Getpid(), os.Getpid(), status); foreign || err != nil {
		t.Fatal("ordinary same-namespace child rejected", foreign, err)
	}
}
