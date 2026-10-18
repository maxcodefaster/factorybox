//go:build !linux

package main

import (
	"fmt"
	"os"
)

func criuCommitSnapshot(string) error {
	_, err := committedSnapshotMode()
	return err
}
func criuRegisterCommittedParent(string, map[string]string) error {
	return criuCommitSnapshot("")
}
func criuCloseCommittedSnapshot(string) error { return criuCommitSnapshot("") }
func committedSnapshotMode() (bool, error) {
	if value := os.Getenv("RUNTIME_FORK_COMMITTED_SNAPSHOT"); value != "" && value != "0" {
		return false, fmt.Errorf("committed snapshot filesystem requires native Linux")
	}
	return false, nil
}
