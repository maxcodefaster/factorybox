//go:build linux

package main

import (
	"context"
	"fmt"
	"os"
	"strings"
)

func startEpochParentVerification(images string, expected map[string]string) (*epochVerificationJob, error) {
	mode := os.Getenv("RUNTIME_FORK_VERIFY_OVERLAP")
	if images != "/experiment/sysbox/images-again" || (mode != "0" && mode != "1") {
		return nil, fmt.Errorf("verification outside the owned second-fork parent")
	}
	if err := criuSysboxParentLink(images); err != nil {
		return nil, err
	}
	copyExpected := make(map[string]string, len(expected))
	for name, digest := range expected {
		copyExpected[name] = digest
	}
	if os.Getenv("RUNTIME_FORK_VERIFY_TEST_FAIL") == "1" {
		// Alter only this owned expectation, never the saved source bytes.
		if copyExpected["inventory.img"] == "" {
			return nil, fmt.Errorf("failure fixture has no inventory")
		}
		copyExpected["inventory.img"] = strings.Repeat("0", 64)
	}
	return startEpochVerification(context.Background(), mode == "1", func(ctx context.Context) epochVerificationResult {
		return checkEpochParentFiles(ctx, images, copyExpected, int64(criuRAMSnapshotLimit))
	}), nil
}
