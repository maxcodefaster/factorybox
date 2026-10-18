//go:build linux

package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// A separate, read-only anonymous page makes the repeated-delta regression
// sensitive to a lost dirty bit. Go heap activity cannot rewrite this page.
type fixtureRAMToken struct{ page []byte }

func newFixtureRAMToken() (*fixtureRAMToken, error) {
	page, err := unix.Mmap(-1, 0, os.Getpagesize(), unix.PROT_READ|unix.PROT_WRITE, unix.MAP_PRIVATE|unix.MAP_ANON)
	if err != nil {
		return nil, err
	}
	if err := unix.Madvise(page, unix.MADV_NOHUGEPAGE); err != nil {
		unix.Munmap(page)
		return nil, err
	}
	if _, err := rand.Read(page[:16]); err != nil {
		unix.Munmap(page)
		return nil, err
	}
	if err := unix.Mprotect(page, unix.PROT_READ); err != nil {
		unix.Munmap(page)
		return nil, err
	}
	return &fixtureRAMToken{page}, nil
}

func (t *fixtureRAMToken) set(value string) error {
	bytes, err := hex.DecodeString(value)
	if err != nil || len(bytes) != 16 {
		return fmt.Errorf("RAM token requires exactly 16 hex-encoded bytes")
	}
	if err := unix.Mprotect(t.page, unix.PROT_READ|unix.PROT_WRITE); err != nil {
		return err
	}
	copy(t.page[:16], bytes)
	return unix.Mprotect(t.page, unix.PROT_READ)
}

func (t *fixtureRAMToken) get() string { return hex.EncodeToString(t.page[:16]) }
func (t *fixtureRAMToken) close()      { unix.Munmap(t.page) }
