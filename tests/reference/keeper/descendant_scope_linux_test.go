package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestDescendantKeeperScopeNative(t *testing.T) {
	if os.Getenv("DESCENDANT_KEEPER_SCOPE_NATIVE") != "1" {
		t.Skip("one bounded private-cgroup existing-worker container only")
	}
	data, err := os.ReadFile("/sys/fs/cgroup/cgroup.procs")
	if err != nil || !strings.Contains("\n"+string(data), fmt.Sprintf("\n%d\n", os.Getpid())) {
		t.Fatal("fixture is outside its private cgroup root", err)
	}
	maximum, err := os.ReadFile("/sys/fs/cgroup/memory.max")
	bound, parseErr := strconv.ParseUint(strings.TrimSpace(string(maximum)), 10, 64)
	if err != nil || parseErr != nil || bound > 256<<20 {
		t.Fatal("private fixture memory limit is absent", string(maximum), err, parseErr)
	}
	token := "0123456789abcdef0123456789abcdef"
	branch := "branch-" + token
	for _, path := range []string{"/experiment/sysbox-copy", "/experiment/branches"} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatal("fixture must own absent roots", path, err)
		}
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.RemoveAll(path) })
	}
	actors := map[string]*exec.Cmd{}
	for _, id := range []string{"copy", "branch", "foreign"} {
		group := ""
		if id != "foreign" {
			name := "criu-probe-sysbox-copy"
			if id == "branch" {
				name = "criu-probe-" + branch
			}
			group = "/sys/fs/cgroup/" + name
			if err := os.Mkdir(group, 0700); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { os.Remove(group) })
		}
		command := exec.Command("/bin/sleep", "25")
		if err := command.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { command.Process.Kill(); command.Wait() })
		if group != "" {
			if err := os.WriteFile(group+"/cgroup.procs", []byte(strconv.Itoa(command.Process.Pid)), 0600); err != nil {
				t.Fatal(err)
			}
		}
		actors[id] = command
	}
	cases := []struct {
		name, path, actor string
		accept            bool
		productionRoot    bool
	}{
		{"copy_exact_fd_and_matching_source", "/experiment/sysbox-copy/images-" + token + "/pages-7.img", "copy", true, true},
		{"branch_exact_fd_and_matching_source", "/experiment/branches/" + branch + "/images-" + token + "/pages-7.img", "branch", true, true},
		{"copy_path_with_foreign_source", "/experiment/sysbox-copy/images-11111111111111111111111111111111/pages-7.img", "foreign", false, true},
		{"branch_path_with_sibling_source", "/experiment/branches/" + branch + "/images-22222222222222222222222222222222/pages-7.img", "copy", false, true},
		{"malformed_generation", "/experiment/sysbox-copy/images-1234/pages-7.img", "copy", false, true},
		{"malformed_branch", "/experiment/branches/branch-1234/images-" + token + "/pages-7.img", "branch", false, true},
		{"nonproduction_store_cannot_adopt_descendant", "/experiment/sysbox-copy/images-33333333333333333333333333333333/pages-7.img", "copy", false, false},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if err := os.MkdirAll(filepath.Dir(test.path), 0700); err != nil {
				t.Fatal(err)
			}
			file, err := os.OpenFile(test.path, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0600)
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			if err := file.Truncate(4096); err != nil {
				t.Fatal(err)
			}
			source, err := sourceAt(actors[test.actor].Process.Pid)
			if err != nil {
				t.Fatal(err)
			}
			imageRoot := "/experiment/sysbox"
			if !test.productionRoot {
				imageRoot = t.TempDir()
			}
			store := &captureStore{imageRoot: imageRoot, output: t.TempDir()}
			image, err := store.begin(captureMessage{op: 1, a: 7, b: uint64(source.PID), fds: []int{int(file.Fd())}})
			if test.accept {
				if err != nil || image == nil {
					t.Fatal("owned descendant descriptor rejected", err)
				}
				if image.name != test.path || image.source != source || store.liveImages != 1 {
					t.Fatal("accepted capture lost its exact descriptor/source identity")
				}
				store.discard(image)
			} else {
				if err == nil || image != nil {
					if image != nil {
						store.discard(image)
					}
					t.Fatal("foreign/malformed capture admitted", err)
				}
			}
			if store.liveImages != 0 || store.heldBytes != 0 || store.heldFDs != 0 || len(store.pending) != 0 {
				t.Fatal("admission/rejection leaked capture credits")
			}
		})
	}
}
