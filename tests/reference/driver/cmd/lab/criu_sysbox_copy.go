package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

type criuSysboxCopy struct {
	Images       string
	Start        time.Time
	RestoreStart time.Time
	Output       string
}

// Clone while the source is dead or kernel-frozen. Only the child is restored
// when retaining the source; otherwise the later source restore remains serial
// because both restore requests share the owned CRIU configuration.
func criuSysboxCopyRestore(sourceID, sourceImages string) (copy criuSysboxCopy, resultErr error) {
	copy, err := criuSysboxCopyFiles()
	if err != nil {
		return copy, err
	}
	if err := criuSysboxCopyImages(&copy, sourceImages); err != nil {
		return copy, err
	}
	if err := criuSysboxCopyMounts(); err != nil {
		return copy, err
	}
	copy.RestoreStart = time.Now()
	copy.Output, err = criuSysboxRestoreOwned("copy-sysbox", copy.Images, "/usr/local/sbin/sysbox-runc-experiment",
		"--root", criuRoot+"/runtime", "experiment-restore-copy", "--image-path", copy.Images, sourceID)
	return copy, err
}

func criuSysboxCopyFiles() (copy criuSysboxCopy, resultErr error) {
	const copyDir = "/experiment/sysbox-copy"
	if err := os.Mkdir(copyDir, 0755); err != nil {
		return copy, err
	}
	if err := criuSysboxBindCopyParent(); err != nil {
		return copy, err
	}
	copy.Start = time.Now()
	// Share only the immutable lower through a read-only bind. Copy writable
	// backing inodes and whiteouts, never the controller's stale merged view.
	copyTimeout := 30 * time.Second
	if os.Getenv("CRIU_SYSBOX_COMPOSE") == "1" {
		copyTimeout = 90 * time.Second
	}
	if err := criuSysboxSharedLower(copyDir); err != nil {
		return copy, err
	}
	if out, err := criuCommand(copyTimeout, "cp", "-a", "--reflink=auto", "--", "/experiment/sysbox/upper", filepath.Join(copyDir, "upper")); err != nil {
		return copy, fmt.Errorf("clone Sysbox upper: %s: %w", out, err)
	}
	copy.Images = filepath.Join(copyDir, "images")
	return copy, nil
}

func criuSysboxCopyImages(copy *criuSysboxCopy, sourceImages string) error {
	handled, err := criuSysboxCloneRAMImages(sourceImages, copy.Images)
	var out string
	if err == nil && !handled {
		out, err = criuCommand(30*time.Second, "cp", "-a", "--reflink=auto", "--", sourceImages, copy.Images)
	}
	// With pipelining this actual span includes concurrent capture. Full
	// readiness still charges all preparation; this span is not additive.
	record("sysbox-copy-layers-images", copy.Start, out, err)
	if err == nil {
		err = criuSysboxApplyHotset(sourceImages, copy.Images)
	}
	return err
}

func criuSysboxCopyMounts() error {
	const copyDir = "/experiment/sysbox-copy"
	for _, child := range []string{"rootfs", "overlay-work"} {
		if err := os.Mkdir(filepath.Join(copyDir, child), 0755); err != nil {
			return err
		}
	}
	options := fmt.Sprintf("lowerdir=%s/lower,upperdir=%s/upper,workdir=%s/overlay-work", copyDir, copyDir, copyDir)
	if out, err := criuCommand(10*time.Second, "mount", "-t", "overlay", "overlay", "-o", options, filepath.Join(copyDir, "rootfs")); err != nil {
		return fmt.Errorf("copy overlay: %s: %w", out, err)
	}
	if os.Getenv("CRIU_SYSBOX_RETAIN_SOURCE") == "1" && os.Getenv("CRIU_SYSBOX_TOOLS") == "1" {
		ownershipStart := time.Now()
		owners := map[string]any{}
		for _, base := range []string{"/experiment/sysbox", copyDir} {
			info, statErr := os.Stat(filepath.Join(base, "upper/workspace/chrome/Default/Code Cache/pc"))
			if statErr != nil {
				return fmt.Errorf("Chrome private cache parent: %w", statErr)
			}
			owner := info.Sys().(*syscall.Stat_t)
			owners[base] = map[string]any{"uid": owner.Uid, "gid": owner.Gid, "mode": info.Mode().Perm()}
			if owner.Uid != 165536 || owner.Gid != 165536 {
				return fmt.Errorf("Chrome private cache lost shifted owner: uid=%d gid=%d", owner.Uid, owner.Gid)
			}
		}
		record("sysbox-copy-retained-source-upper-ownership", ownershipStart, asJSON(owners), nil)
	}
	if err := criuCgroup("sysbox-copy"); err != nil {
		return err
	}
	return nil
}

func criuSysboxCopyProof(copy criuSysboxCopy, inners []string, before map[string]map[string]any) error {
	const copyID = "copy-sysbox"
	out := copy.Output
	restored, err := criuRestoredStates(copyID, inners, before)
	if err == nil {
		out += asJSON(restored)
	}
	logData, _ := os.ReadFile(filepath.Join(copy.Images, "restore.log"))
	record("sysbox-copy-restore-ram", copy.RestoreStart, out+string(logData), err)
	if err != nil {
		return err
	}
	if err := criuSysboxNetworkProof(copyID, "fork-restored"); err != nil {
		return err
	}
	if err := criuSysboxComposeProof(copyID, "fork-restored"); err != nil {
		return err
	}
	record("sysbox-fork-complete-runtime-assertions", copy.Start,
		"clone and child restore through correctness assertions; this span also includes source recovery and both branches' readiness. Use live-fork-full-readiness for availability latency.", nil)
	start := time.Now()
	out, err = criuRunc("exec", copyID, "/bin/sh", "-c", "test \"$(cat /workspace/post-restore.txt)\" = retained-second-resume && docker run --rm --name after-fork --network=none --pids-limit=64 --entrypoint=/bin/sh criu-probe-inner:4.2.1 -c 'test -x /lab'")
	record("sysbox-copy-docker-files-lifecycle", start, out, err)
	if err != nil {
		return err
	}
	return criuSysboxProcProtection(copyID, "copy-restored")
}

func criuSysboxSharedLower(copyDir string) error {
	start := time.Now()
	const source = "/experiment/sysbox/lower"
	target := filepath.Join(copyDir, "lower")
	if err := os.Mkdir(target, 0755); err != nil {
		return err
	}
	out, err := criuCommand(10*time.Second, "mount", "--bind", source, target)
	if err == nil {
		out, err = criuCommand(10*time.Second, "mount", "-o", "remount,bind,ro", target)
	}
	if err == nil {
		var a, b os.FileInfo
		a, err = os.Stat(source + "/lab")
		if err == nil {
			b, err = os.Stat(target + "/lab")
		}
		if err == nil && !os.SameFile(a, b) {
			err = fmt.Errorf("copy lower does not share the immutable source inode")
		}
	}
	if err == nil {
		file, openErr := os.OpenFile(target+"/lab", os.O_WRONLY, 0)
		if file != nil {
			file.Close()
		}
		if !errors.Is(openErr, syscall.EROFS) {
			err = fmt.Errorf("copy lower did not reject write access as read-only: %v", openErr)
		}
	}
	record("sysbox-copy-shared-readonly-lower", start, out+"shared /lab inode; lower write-open rejected", err)
	return err
}

func criuSysboxVirtualProof(sourceID, copyID, outerBefore string) error {
	start := time.Now()
	output := ""
	var err error
	for _, id := range []string{sourceID, copyID} {
		var out string
		out, err = criuRunc("exec", id, "/bin/sh", "-c", "test \"$(cat /proc/sys/kernel/panic)\" = 17")
		output += id + ": " + out
		if err != nil {
			break
		}
	}
	if err == nil {
		for i, id := range []string{sourceID, copyID} {
			out, writeErr := criuRunc("exec", id, "/bin/sh", "-c", fmt.Sprintf("printf '%d\\n' > /proc/sys/kernel/panic", 23+6*i))
			output += out
			if writeErr != nil {
				err = writeErr
				break
			}
		}
	}
	if err == nil {
		for i, id := range []string{sourceID, copyID} {
			out, checkErr := criuRunc("exec", id, "/bin/sh", "-c", fmt.Sprintf("cat /proc/sys/kernel/panic; test \"$(cat /proc/sys/kernel/panic)\" = %d", 23+6*i))
			output += id + ": " + out
			if checkErr != nil {
				err = checkErr
				break
			}
		}
	}
	if err == nil {
		outerAfter, readErr := os.ReadFile("/proc/sys/kernel/panic")
		output += "controller: " + string(outerAfter)
		if readErr != nil || string(outerAfter) != outerBefore {
			err = fmt.Errorf("virtual write changed controller state: before=%q after=%q error=%v", outerBefore, outerAfter, readErr)
		}
	}
	record("sysbox-copy-virtual-files-diverge", start, output, err)
	return err
}

// Change the copy's actual host limit after both restores. Saved directory FDs
// must follow that copy, while the source and inner Docker limits stay intact.
func criuSysboxResourceProof(sourceID, copyID string) error {
	start := time.Now()
	states := map[string]any{}
	err := os.WriteFile("/sys/fs/cgroup/criu-probe-sysbox-copy/pids.max", []byte("384"), 0644)
	if err == nil {
		for branch, id := range []string{sourceID, copyID} {
			outerLimit := "512"
			if branch == 1 {
				outerLimit = "384"
			}
			mode := "sysbox"
			if branch == 1 {
				mode = "sysbox-copy"
			}
			var actual []byte
			actual, err = os.ReadFile(filepath.Join("/sys/fs/cgroup", "criu-probe-"+mode, "pids.max"))
			if err == nil && string(actual) != outerLimit+"\n" {
				err = fmt.Errorf("unexpected host PID limit for %s: %q", id, actual)
			}
			if err != nil {
				break
			}
			for _, inner := range []string{"", "alpha", "beta"} {
				limit := outerLimit
				if inner != "" {
					limit = "64"
				}
				for _, liveExec := range []bool{false, true} {
					if liveExec && inner == "beta" {
						continue
					}
					var state map[string]any
					if liveExec {
						state, err = criuLiveExecState(id, inner, "/state")
					} else {
						state, err = criuState(id, inner, "/state")
					}
					states[fmt.Sprintf("%s/%s/exec=%t", id, inner, liveExec)] = state
					if err == nil && (state["pids_limit"] != limit || state["cgroup_fd_pids_limit"] != limit) {
						err = fmt.Errorf("restored PID limit view differs for %s/%s exec=%t: expected=%s state=%v", id, inner, liveExec, limit, state)
					}
					if err != nil {
						break
					}
				}
				if err != nil {
					break
				}
			}
			if err != nil {
				break
			}
		}
	}
	record("sysbox-copy-resource-views-diverge", start, asJSON(states), err)
	return err
}
