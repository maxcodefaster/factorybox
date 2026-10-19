//go:build linux

package lifecycle

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"

	"golang.org/x/sys/unix"
)

func TestNativeEmptyCgroupRetirement(t *testing.T) {
	prepare := func(t *testing.T) (*Engine, Record, string) {
		t.Helper()
		engine, bundle := nativeEngine(t, "sleep 300")
		input, err := os.ReadFile(bundle + "/config.json")
		if err != nil {
			t.Fatal(err)
		}
		record, err := engine.prepare("empty-owned", bundle, "objects/empty-owned", input)
		if err != nil {
			t.Fatal(err)
		}
		if err := engine.prepareCgroup(&record); err != nil {
			t.Fatal(err)
		}
		return engine, record, cgroupRoot + "/" + record.RuntimeID
	}
	t.Run("empty-nested-tree", func(t *testing.T) {
		engine, _, path := prepare(t)
		if err := os.MkdirAll(path+"/init.scope/nested/leaf", 0755); err != nil {
			t.Fatal(err)
		}
		view, err := engine.Delete(context.Background(), "empty-owned")
		if err != nil || view.Record.Phase != "deleted" {
			t.Fatal("delete could not retire its original empty descendant tree", err)
		}
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatal("successful deletion retained the original cgroup", err)
		}
	})
	t.Run("foreign-child-mount-retained", func(t *testing.T) {
		engine, record, path := prepare(t)
		child := path + "/init.scope"
		sibling := path + "/owned-empty-sibling"
		peer := cgroupRoot + "/factorybox-foreign-" + filepath.Base(engine.config.Root) + "-" + record.Owner
		for _, directory := range []string{child, sibling, peer} {
			if err := os.Mkdir(directory, 0755); err != nil {
				t.Fatal(err)
			}
		}
		peerIdentity, err := cgroupIdentity(peer)
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			if err := os.Remove(peer); err != nil {
				t.Error("remove only the empty owned negative fixture peer", err)
			}
		}()
		if err := unix.Mount(peer, child, "", unix.MS_BIND, ""); err != nil {
			t.Fatal(err)
		}
		_, rejected := engine.Delete(context.Background(), "empty-owned")
		after, identityErr := cgroupIdentity(peer)
		if rejected == nil || identityErr != nil || after != peerIdentity {
			t.Error("delete accepted or changed a foreign mounted child", rejected, identityErr)
		}
		if _, err := os.Stat(sibling); err != nil {
			t.Error("delete removed an owned sibling before validating the complete tree", err)
		}
		if err := unix.Unmount(child, 0); err != nil {
			t.Fatal(err)
		}
		if _, err := engine.Delete(context.Background(), "empty-owned"); err != nil {
			t.Fatal("retry could not retire the original tree after removing the foreign fixture mount", err)
		}
	})
	t.Run("populated-child-retained", func(t *testing.T) {
		engine, record, path := prepare(t)
		child := path + "/init.scope"
		if err := os.Mkdir(child, 0755); err != nil {
			t.Fatal(err)
		}
		process := exec.Command("/bin/sleep", "300")
		if err := process.Start(); err != nil {
			t.Fatal(err)
		}
		defer func() {
			process.Process.Kill()
			process.Wait()
		}()
		if err := os.WriteFile(child+"/cgroup.procs", []byte(strconv.Itoa(process.Process.Pid)), 0600); err != nil {
			t.Fatal(err)
		}
		_, rejected := engine.Delete(context.Background(), "empty-owned")
		current, identityErr := cgroupIdentity(path)
		if rejected == nil || identityErr != nil || current != *record.Cgroup ||
			unix.Kill(process.Process.Pid, 0) != nil {
			t.Fatal("delete changed or signaled its populated original tree", rejected, identityErr)
		}
		if err := process.Process.Kill(); err != nil {
			t.Fatal(err)
		}
		process.Wait()
		if _, err := engine.Delete(context.Background(), "empty-owned"); err != nil {
			t.Fatal("retry could not retire its now-empty original descendants", err)
		}
	})
}
