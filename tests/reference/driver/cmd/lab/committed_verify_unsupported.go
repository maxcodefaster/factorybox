//go:build !linux

package main

import "os"

type committedVerification struct{}

func committedSameIdentity(a, b os.FileInfo) bool { return os.SameFile(a, b) }

func beginCommittedVerification(string) (*committedVerification, error) {
	_, err := committedSnapshotMode()
	return nil, err
}
func endCommittedVerification(*committedVerification) error { return nil }
func verifyCommittedPage(*committedVerification, string, string, os.FileInfo) (bool, error) {
	return false, nil
}
