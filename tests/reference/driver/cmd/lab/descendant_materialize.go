package main

import (
	"fmt"
	"path/filepath"
	"time"
)

func descendantMaterializeDaemons(timeout time.Duration) error {
	return descendantMaterializeDaemonsExcept(nil, timeout)
}

func descendantMaterializeDaemonsExcept(preserve *criuLazyDaemon, timeout time.Duration) error {
	for _, daemon := range criuLazyDaemons {
		if daemon == preserve {
			if daemon.pollCompletion() {
				return fmt.Errorf("owned inherited source pager exited before recapture")
			}
			continue
		}
		if !daemon.pollCompletion() {
			record("sysbox-descendant-materialize-owner-started", time.Now(),
				asJSON(map[string]any{"images": filepath.Dir(daemon.logPath), "daemon_pid": daemon.command.Process.Pid}), nil)
		}
		if err := daemon.materialize(timeout); err != nil {
			return fmt.Errorf("complete owned lazy pages for %s before descendant capture: %w", filepath.Dir(daemon.logPath), err)
		}
	}
	return nil
}

func descendantPageDaemon(images string) (*criuLazyDaemon, error) {
	var owner *criuLazyDaemon
	for _, daemon := range criuLazyDaemons {
		if filepath.Dir(daemon.logPath) == images {
			if owner != nil {
				return nil, fmt.Errorf("multiple page daemons own descendant images")
			}
			owner = daemon
		}
	}
	if owner == nil || owner.command == nil || owner.command.Process == nil ||
		!owner.captureControl || (owner.inheritSocket != "" && !descendantPagerEndpointOwned(owner, images)) {
		return nil, fmt.Errorf("descendant restore did not establish its private owned pager")
	}
	return owner, nil
}
