//go:build !linux

package main

import (
	"fmt"
	"os"
)

func criuSysboxPinParent(string) error {
	if os.Getenv("CRIU_SYSBOX_DELTA") == "1" || criuSysboxInheritEnabled() {
		return fmt.Errorf("incremental parent binding requires Linux")
	}
	return nil
}

func criuSysboxBindCopyParent() error { return criuSysboxPinParent("") }
func criuSysboxParentCleanup() error  { return nil }

func criuSysboxParentGuardProof(string) error { return criuSysboxPinParent("") }
func criuSysboxParentManifestProof() error    { return criuSysboxPinParent("") }
