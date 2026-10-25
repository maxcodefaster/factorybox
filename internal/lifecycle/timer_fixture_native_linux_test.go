//go:build linux && (amd64 || arm64)

package lifecycle

import (
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func timerReaderFixture(t *testing.T, status boundedLossStatus, id, command string, actor bool) View {
	return timerReaderFixtureFiles(t, status, id, command, actor, nil)
}

func timerReaderFixtureFiles(t *testing.T, status boundedLossStatus, id, command string, actor bool, files map[string][]byte) View {
	return timerReaderFixtureFilesWithCleanup(t, status, id, command, actor, files, nil)
}

// A rejection fixture may retain an unresolved operation and its mounts until
// the exact-owned outer worker is removed. Ordinary reader fixtures retain
// their original per-sandbox deletion and overlay-unmount assertions.
func timerReaderFixtureFilesWithCleanup(t *testing.T, status boundedLossStatus, id, command string, actor bool, files map[string][]byte, cleanup func(*testing.T, View)) View {
	t.Helper()
	bundle := status.Store + "/" + id + "-bundle"
	for _, name := range []string{"", "/upper", "/work", "/rootfs"} {
		mode := os.FileMode(0755)
		if name == "" {
			mode = 0700
		}
		if err := os.Mkdir(bundle+name, mode); err != nil {
			t.Fatal(err)
		}
	}
	if out, err := exec.Command("cp", "-a", "/busybox", bundle+"/lower").CombinedOutput(); err != nil {
		t.Fatal(string(out), err)
	}
	if actor {
		data, err := os.ReadFile(os.Args[0])
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(bundle+"/lower/bin/capture-actor", data, 0755); err != nil {
			t.Fatal(err)
		}
	}
	for name, data := range files {
		if strings.Contains(name, "/") || name == "" || name == "." || name == ".." {
			t.Fatal("fixture executable must have one local name")
		}
		if err := os.WriteFile(bundle+"/lower/bin/"+name, data, 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := unix.Mount("overlay", bundle+"/rootfs", "overlay", 0, "index=off,lowerdir="+bundle+"/lower,upperdir="+bundle+"/upper,workdir="+bundle+"/work"); err != nil {
		t.Fatal(err)
	}
	if cleanup == nil {
		t.Cleanup(func() {
			if err := unix.Unmount(bundle+"/rootfs", 0); err != nil {
				t.Error("release exact reader fixture overlay", err)
			}
		})
	}
	if out, err := exec.Command(status.Lifecycle.Runtime, "spec", "--bundle", bundle).CombinedOutput(); err != nil {
		t.Fatal(string(out), err)
	}
	data, err := os.ReadFile(bundle + "/config.json")
	if err != nil {
		t.Fatal(err)
	}
	var spec map[string]any
	if err := json.Unmarshal(data, &spec); err != nil {
		t.Fatal(err)
	}
	process := spec["process"].(map[string]any)
	process["terminal"], process["cwd"] = false, "/"
	process["args"] = []string{"/bin/sh", "-c", command + " </dev/null >/dev/null 2>&1"}
	process["env"] = []string{"HOME=/root", "PATH=/bin:/sbin:/usr/bin:/usr/sbin"}
	spec["root"].(map[string]any)["readonly"] = false
	spec["linux"].(map[string]any)["resources"] = map[string]any{"pids": map[string]any{"limit": 128}, "devices": []map[string]any{{"allow": true, "access": "rwm"}}}
	data, err = json.Marshal(spec)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bundle+"/config.json", data, 0600); err != nil {
		t.Fatal(err)
	}
	view := boundedLossView(t, "start", "--bundle", bundle, id)
	t.Cleanup(func() {
		if cleanup != nil {
			cleanup(t, view)
			return
		}
		if _, err := boundedLossCommand("--worker-run-root", "/run/factorybox", "delete", id); err != nil {
			t.Error("delete exact reader fixture", id, err)
		}
	})
	return view
}
