//go:build linux

package main

import (
	"fmt"
	"os"
	"time"

	"golang.org/x/sys/unix"
)

// All workloads and page daemons are already stopped by criuProbe's existing
// deferred cleanup before this function can replace a snapshot's backing.
func criuEpochRAMCleanup() error {
	const parent = "/experiment/sysbox/images-again"
	const childParent = "/experiment/sysbox-copy/images-again"
	start := time.Now()
	if err := criuSysboxReleaseRAMCapture("/experiment/sysbox/images"); err != nil {
		return err
	}
	if criuRAMSnapshotFind(parent) == nil && criuRAMSnapshotFind("/experiment/sysbox/images-repeat") == nil {
		return nil
	}
	// The youngest snapshot still resolves P1 while it is exported and
	// read back. P1 still resolves P0. Never retire an ancestor first.
	if err := criuSysboxReleaseRAMCapture("/experiment/sysbox/images-repeat"); err != nil {
		return err
	}
	for i := len(criuParentBindings) - 1; i >= 0; i-- {
		path := criuParentBindings[i]
		if path != parent && path != childParent {
			continue
		}
		if err := unix.Unmount(path, 0); err != nil {
			return err
		}
		criuParentBindings = append(criuParentBindings[:i], criuParentBindings[i+1:]...)
	}
	if snapshot := criuRAMSnapshotFind(parent); snapshot != nil && snapshot.CopyImages != "" {
		return fmt.Errorf("first child must be fully retired before RAM parent teardown")
	}
	if err := criuSysboxReleaseRAMCapture(parent); err != nil {
		return err
	}
	if criuEpochNextParent {
		// Keep the backed chain at the same exact paths. These new binds
		// refer to file backing, so they retain no old tmpfs page payload.
		if err := criuParentReadonlyBind(parent, parent); err != nil {
			return err
		}
		criuParentBindings = append(criuParentBindings, parent)
		if criuEpochResumeChildParentAbsent(childParent) {
			record("sysbox-epoch-ram-parent-backed-chain", start,
				"source-only suspended chain: P2 then P1 synced/read back, old RAM removed, P1 repinned on file backing; second child not yet created", nil)
			return nil
		}
		if info, err := os.Lstat(childParent); err != nil || !info.IsDir() {
			return fmt.Errorf("second child's exact parent target is absent: %v", err)
		}
		if err := criuParentReadonlyBind(parent, childParent); err != nil {
			return err
		}
		criuParentBindings = append(criuParentBindings, childParent)
		if err := criuEpochChainParentLink("/experiment/sysbox/images-repeat"); err != nil {
			return err
		}
		if criuEpochPipelineCancelled {
			for _, path := range []string{"/experiment/runtime/copy-sysbox", "/experiment/sysbox-copy/images"} {
				if _, err := os.Lstat(path); !os.IsNotExist(err) {
					return fmt.Errorf("cancelled pipeline retained child state %s: %v", path, err)
				}
			}
			record("sysbox-epoch-pipeline-abort-backed-source-only", start,
				"cancelled helper/registrations already retired and source verified; all snapshot RAM released; backed source chain valid; no child runtime or images expected", nil)
			return nil
		}
		if err := criuEpochChainParentLink("/experiment/sysbox-copy/images"); err != nil {
			return err
		}
	}
	record("sysbox-epoch-ram-parent-backed-chain", start,
		"all owned workloads/pagers stopped; P2 exported before P1; old readonly directory binds detached, verified P1 tmpfs files retired, both snapshot RAM charges released; exact readonly file-backed chain resolves afterward", nil)
	return nil
}
