package main

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestEpochReplayEmptyCgroupNative(t *testing.T) {
	if os.Getenv("EPOCH_REPLAY_CGROUP_NATIVE") != "1" {
		t.Skip("one owned private cgroup2 native fixture")
	}
	controller, err := os.ReadFile("/sys/fs/cgroup/cgroup.procs")
	if err != nil || !strings.Contains("\n"+string(controller), "\n"+strconv.Itoa(os.Getpid())+"\n") {
		t.Fatal("controller must belong to its private cgroup root", err)
	}
	memory, err := os.ReadFile("/sys/fs/cgroup/memory.max")
	bound, parseErr := strconv.ParseUint(strings.TrimSpace(string(memory)), 10, 64)
	if err != nil || parseErr != nil || bound > 256<<20 {
		t.Fatal("private fixture must be bounded to at most256MiB", err, parseErr)
	}
	fixture := func(t *testing.T) *epochReplayCgroupAnchor {
		t.Helper()
		if _, err := os.Lstat(epochReplayCgroup); !os.IsNotExist(err) {
			t.Fatal("fixture must own an absent replay cgroup", err)
		}
		if err := os.Mkdir(epochReplayCgroup, 0700); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			for _, path := range []string{"retired-empty-leaf/nested-empty", "retired-empty-leaf",
				"foreign-successor", "bound-foreign"} {
				_ = os.Remove(epochReplayCgroup + "/" + path)
			}
			_ = os.Remove(epochReplayCgroup)
		})
		anchor, err := epochReplayOpenCgroup()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := anchor.close(); err != nil {
				t.Error("original cgroup anchor did not close", err)
			}
		})
		return anchor
	}
	for _, kind := range []string{"domain", "threaded"} {
		t.Run("empty_nested_"+kind, func(t *testing.T) {
			anchor := fixture(t)
			if kind == "threaded" {
				if err := os.WriteFile(epochReplayCgroup+"/cgroup.type", []byte("threaded"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			for _, path := range []string{"retired-empty-leaf", "retired-empty-leaf/nested-empty"} {
				if err := os.Mkdir(epochReplayCgroup+"/"+path, 0700); err != nil {
					t.Fatal(err)
				}
				if kind == "threaded" {
					if err := os.WriteFile(epochReplayCgroup+"/"+path+"/cgroup.type", []byte("threaded"), 0600); err != nil {
						t.Fatal(err)
					}
				}
			}
			if err := anchor.removeEmpty(); err != nil {
				t.Fatal("empty nested replay cgroup did not retire", err)
			}
			if _, err := os.Lstat(epochReplayCgroup); !os.IsNotExist(err) {
				t.Fatal("retired exact cgroup still exists", err)
			}
			if anchor.removed != 3 {
				t.Fatal("retirement did not remove exactly the three original groups", anchor.removed)
			}
			if err := anchor.removeEmpty(); err != nil || anchor.removed != 3 {
				t.Fatal("completed cleanup replay performed another mutation", err, anchor.removed)
			}
		})
	}
	t.Run("populated_tree_preserved_before_any_removal", func(t *testing.T) {
		anchor := fixture(t)
		leaf := epochReplayCgroup + "/retired-empty-leaf"
		if err := os.Mkdir(leaf, 0700); err != nil {
			t.Fatal(err)
		}
		actor := exec.Command("/bin/sleep", "20")
		if err := actor.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			_ = actor.Process.Kill()
			_ = actor.Wait()
			if err := anchor.removeEmpty(); err != nil {
				t.Error("owned actor fixture cleanup failed", err)
			}
		})
		if err := os.WriteFile(leaf+"/cgroup.procs", []byte(strconv.Itoa(actor.Process.Pid)), 0600); err != nil {
			t.Fatal(err)
		}
		before, err := os.Lstat(leaf)
		if err != nil {
			t.Fatal(err)
		}
		if err := anchor.removeEmpty(); err == nil || anchor.removed != 0 {
			t.Fatal("populated tree was admitted or partially removed", err, anchor.removed)
		}
		after, err := os.Lstat(leaf)
		if err != nil || !os.SameFile(before, after) {
			t.Fatal("rejected retirement changed the original subgroup", err)
		}
		if err := actor.Process.Signal(unix.Signal(0)); err != nil {
			t.Fatal("rejected retirement affected the live actor", err)
		}
	})
	t.Run("foreign_successor_preserved", func(t *testing.T) {
		anchor := fixture(t)
		if err := os.Remove(epochReplayCgroup); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(epochReplayCgroup, 0700); err != nil {
			t.Fatal(err)
		}
		sentinel := epochReplayCgroup + "/foreign-successor"
		if err := os.Mkdir(sentinel, 0700); err != nil {
			t.Fatal(err)
		}
		before, err := os.Lstat(sentinel)
		if err != nil {
			t.Fatal(err)
		}
		if err := anchor.removeEmpty(); err == nil || anchor.removed != 0 {
			t.Fatal("foreign replacement was admitted", err, anchor.removed)
		}
		after, err := os.Lstat(sentinel)
		if err != nil || !os.SameFile(before, after) {
			t.Fatal("foreign successor was changed", err)
		}
	})
	t.Run("foreign_bind_mount_preserved", func(t *testing.T) {
		anchor := fixture(t)
		foreign := "/sys/fs/cgroup/epoch-replay-foreign"
		if _, err := os.Lstat(foreign); !os.IsNotExist(err) {
			t.Fatal("fixture must own an absent foreign control", err)
		}
		for _, path := range []string{foreign, foreign + "/sentinel",
			epochReplayCgroup + "/retired-empty-leaf", epochReplayCgroup + "/bound-foreign"} {
			if err := os.Mkdir(path, 0700); err != nil {
				t.Fatal(err)
			}
		}
		t.Cleanup(func() {
			_ = os.Remove(foreign + "/sentinel")
			_ = os.Remove(foreign)
		})
		bound := epochReplayCgroup + "/bound-foreign"
		if err := unix.Mount(foreign, bound, "", unix.MS_BIND, ""); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := unix.Unmount(bound, 0); err != nil {
				t.Error(err)
			}
		})
		before, err := os.Lstat(foreign + "/sentinel")
		if err != nil {
			t.Fatal(err)
		}
		if err := anchor.removeEmpty(); err == nil || !strings.Contains(err.Error(), "foreign mount") || anchor.removed != 0 {
			t.Fatal("foreign mount was traversed or any group was removed", err, anchor.removed)
		}
		after, err := os.Lstat(foreign + "/sentinel")
		if err != nil || !os.SameFile(before, after) {
			t.Fatal("foreign mounted subtree was changed", err)
		}
		if _, err := os.Lstat(epochReplayCgroup + "/retired-empty-leaf"); err != nil {
			t.Fatal("preflight failure partially removed the owned tree", err)
		}
	})
}
