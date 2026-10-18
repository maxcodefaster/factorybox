//go:build !linux

package main

import (
	"fmt"
	"os"
)

func criuSysboxPrepareRAMCapture(string) error {
	if os.Getenv("CRIU_SYSBOX_RAM_CAPTURE") == "1" {
		return fmt.Errorf("RAM-backed Sysbox capture requires Linux")
	}
	return nil
}

func criuSysboxRAMCaptureProof(string) error                { return nil }
func criuSysboxReleaseRAMCapture(string) error              { return nil }
func criuSysboxCloneRAMImages(string, string) (bool, error) { return false, nil }
func criuSysboxRAMCaptureCleanup() error                    { return nil }
func criuSysboxRAMLiveAccounting(string) error              { return nil }
func criuSysboxRAMSnapshotScope(string) string              { return "complete file-backed snapshot" }
