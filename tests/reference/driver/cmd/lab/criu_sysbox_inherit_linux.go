package main

import (
	"fmt"
	"os"
	"time"
)

func criuSysboxPrepareInheritParent(id string) (resultErr error) {
	if err := criuInheritGuard(criuRoot, id); err != nil {
		return err
	}
	if !criuSysboxInheritEnabled() {
		return nil
	}
	start := time.Now()
	defer func() {
		record("sysbox-lazy-inherit-parent-prepared", start,
			"original saved images pinned read-only; source and child share immutable payloads; no source materialization or pre-dump", resultErr)
	}()
	if _, err := criuInheritSourceDaemon(); err != nil {
		return err
	}
	source := criuRoot + "/sysbox/images"
	base := criuRoot + "/sysbox/images-base"
	if _, err := os.Lstat(base); !os.IsNotExist(err) {
		return fmt.Errorf("inheritance parent already exists or cannot be inspected: %v", err)
	}
	if err := os.Mkdir(base, 0700); err != nil {
		return err
	}
	// The daemon already holds read-only payload descriptors and open log
	// descriptors. Protect both paths against new payload writes; live log
	// descriptors remain outside the immutable payload manifest.
	if err := criuParentReadonlyBind(source, source); err != nil {
		return err
	}
	criuParentBindings = append(criuParentBindings, source)
	if err := criuParentReadonlyBind(source, base); err != nil {
		return err
	}
	criuParentBindings = append(criuParentBindings, base)
	return criuSysboxPinParent(base)
}
