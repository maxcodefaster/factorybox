package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

var criuParentProofRegistrationTime time.Duration

func criuSysboxInheritEnabled() bool {
	return os.Getenv("CRIU_SYSBOX_LAZY_INHERIT") == "1"
}

func criuSysboxUsesParent() bool {
	return os.Getenv("CRIU_SYSBOX_DELTA") == "1" || criuSysboxInheritEnabled()
}

func criuParentManifestReuseEnabled() bool {
	return os.Getenv("CRIU_SYSBOX_PARENT_MANIFEST_REUSE") == "1"
}

func criuLazyWPEnabled() bool {
	return os.Getenv("CRIU_SYSBOX_LAZY_WP") == "1"
}

func criuInheritGuard(root, id string) error {
	if err := criuCgroupPrefixGuard(root, id); err != nil {
		return err
	}
	if err := criuHotsetGuard(root, id); err != nil {
		return err
	}
	if err := criuChildDemandGuard(root, id); err != nil {
		return err
	}
	if err := criuLazyObserveGuard(root, id); err != nil {
		return err
	}
	if err := criuCopyPipelineGuard(root, id); err != nil {
		return err
	}
	switch os.Getenv("CRIU_SYSBOX_LAZY_WP") {
	case "", "0", "1":
	default:
		return fmt.Errorf("CRIU_SYSBOX_LAZY_WP must be 0 or 1")
	}
	switch os.Getenv("CRIU_SYSBOX_PARENT_MANIFEST_REUSE") {
	case "", "0", "1":
	default:
		return fmt.Errorf("CRIU_SYSBOX_PARENT_MANIFEST_REUSE must be 0 or 1")
	}
	switch os.Getenv("CRIU_SYSBOX_LAZY_INHERIT") {
	case "", "0":
		if criuLazyWPEnabled() {
			return fmt.Errorf("lazy copied-page tracking requires inheritance")
		}
		if criuParentManifestReuseEnabled() {
			return fmt.Errorf("parent manifest reuse requires lazy inheritance")
		}
		return nil
	case "1":
	default:
		return fmt.Errorf("CRIU_SYSBOX_LAZY_INHERIT must be 0 or 1")
	}
	if root != "/experiment" || id != "source-sysbox" {
		return fmt.Errorf("lazy inheritance is outside the owned source")
	}
	for _, name := range []string{"CRIU_SYSBOX_LAZY", "CRIU_SYSBOX_RETAIN_SOURCE", "CRIU_SYSBOX_PROBE"} {
		if os.Getenv(name) != "1" {
			return fmt.Errorf("lazy inheritance requires %s=1", name)
		}
	}
	for _, name := range []string{"CRIU_SYSBOX_WARM_PREPARE", "CRIU_SYSBOX_PRE_DUMP",
		"CRIU_SYSBOX_ASYNC_TRACK", "CRIU_SYSBOX_DELTA", "CRIU_SYSBOX_RAM_BASE", "CRIU_SYSBOX_REPEAT_DELTA"} {
		if value := os.Getenv(name); value != "" && value != "0" {
			return fmt.Errorf("lazy inheritance requires %s=0", name)
		}
	}
	if criuParentManifestReuseEnabled() && os.Getenv("CRIU_SYSBOX_RAM_CAPTURE") != "1" {
		return fmt.Errorf("parent manifest reuse requires verified RAM snapshot backing")
	}
	return nil
}

var criuInheritAliasName = regexp.MustCompile(`^lazy-parent-[1-9][0-9]*\.bin$`)

// CRIU's page reader consumes images and the explicit PID aliases. Live logs,
// statistics and its pre-existing transfer socket are outside saved payloads.
func criuInheritParentEntry(name string) bool {
	return filepath.Ext(name) == ".img" || criuInheritAliasName.MatchString(name)
}

func criuInheritSourceDaemon() (*criuLazyDaemon, error) {
	var source *criuLazyDaemon
	for _, daemon := range criuLazyDaemons {
		if filepath.Dir(daemon.logPath) == "/experiment/sysbox/images" {
			if source != nil {
				return nil, fmt.Errorf("multiple page daemons own source images")
			}
			source = daemon
		}
	}
	if source == nil || source.command == nil || source.command.Process == nil ||
		source.inheritSocket != "/experiment/sysbox/lazy-inherit.socket" {
		return nil, fmt.Errorf("source has no owned lazy inheritance endpoint")
	}
	if source.pollCompletion() {
		return nil, fmt.Errorf("source inheritance daemon exited: %v", source.exitErr)
	}
	info, err := os.Lstat(source.inheritSocket)
	if err != nil || info.Mode()&os.ModeSocket == 0 || info.Mode().Perm() != 0600 {
		return nil, fmt.Errorf("invalid source inheritance endpoint: %v", err)
	}
	return source, nil
}
