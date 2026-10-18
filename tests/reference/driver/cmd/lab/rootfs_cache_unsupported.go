//go:build !linux

package main

import (
	"fmt"
	"os"
	"time"
)

func criuUnpackTemplate(_ string, _ time.Duration) (bool, error) {
	if os.Getenv("RUNTIME_FORK_ROOTFS_CACHE") == "" {
		return false, nil
	}
	return true, fmt.Errorf("rootfs template cloning requires the natively tested Linux worker")
}
