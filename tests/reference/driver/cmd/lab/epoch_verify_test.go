package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func epochVerifyFixture(t *testing.T) (string, map[string]string, int64) {
	t.Helper()
	dir := t.TempDir()
	pages := make([]byte, 8<<20)
	for index := range pages {
		pages[index] = byte(index*73 + 29)
	}
	if err := os.WriteFile(filepath.Join(dir, "pages-1.img"), pages, 0600); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(pages)
	return dir, map[string]string{"pages-1.img": hex.EncodeToString(hash[:])}, int64(len(pages))
}

func TestEpochVerificationReadsActualBytesInBothModes(t *testing.T) {
	dir, expected, count := epochVerifyFixture(t)
	for _, parallel := range []bool{false, true} {
		job := startEpochVerification(context.Background(), parallel, func(ctx context.Context) epochVerificationResult {
			return checkEpochParentFiles(ctx, dir, expected, count)
		})
		got := job.join(false)
		if got.Err != nil || got.Files != 1 || got.Bytes != count || got.FinishedNS == 0 {
			t.Fatalf("parallel=%t: %+v", parallel, got)
		}
		if again := job.join(true); again != got {
			t.Fatal("second join did not retain the actual completion")
		}
	}
	data, err := os.ReadFile(filepath.Join(dir, "pages-1.img"))
	if err != nil {
		t.Fatal(err)
	}
	data[len(data)/2] ^= 57
	if err := os.WriteFile(filepath.Join(dir, "pages-1.img"), data, 0600); err != nil {
		t.Fatal(err)
	}
	job := startEpochVerification(context.Background(), true, func(ctx context.Context) epochVerificationResult {
		return checkEpochParentFiles(ctx, dir, expected, count)
	})
	if got := job.join(false); got.Err == nil {
		t.Fatal("same-length real byte mutation was accepted")
	}
}

func TestEpochVerificationBoundsAndSpecialFiles(t *testing.T) {
	dir, expected, count := epochVerifyFixture(t)
	if got := checkEpochParentFiles(context.Background(), dir, expected, count-1); got.Err == nil {
		t.Fatal("oversized real input was read")
	}
	if err := os.WriteFile(filepath.Join(dir, "new.img"), []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	if got := checkEpochParentFiles(context.Background(), dir, expected, count); got.Err == nil {
		t.Fatal("new unmanifested image was accepted")
	}
	os.Remove(filepath.Join(dir, "new.img"))
	os.Remove(filepath.Join(dir, "pages-1.img"))
	if err := os.Symlink("/dev/zero", filepath.Join(dir, "pages-1.img")); err != nil {
		t.Fatal(err)
	}
	if got := checkEpochParentFiles(context.Background(), dir, expected, count); got.Err == nil {
		t.Fatal("special/symlink file was followed")
	}
}

func TestEpochVerificationJoinWaitsAndCancellationExits(t *testing.T) {
	dir, expected, count := epochVerifyFixture(t)
	hashed, release := make(chan struct{}), make(chan struct{})
	var exited atomic.Bool
	job := startEpochVerification(context.Background(), true, func(ctx context.Context) epochVerificationResult {
		result := checkEpochParentFiles(ctx, dir, expected, count)
		close(hashed)
		select {
		case <-release:
		case <-ctx.Done():
			result.Err = ctx.Err()
		}
		exited.Store(true)
		return result
	})
	<-hashed
	joined := make(chan epochVerificationResult, 1)
	go func() { joined <- job.join(false) }()
	select {
	case <-joined:
		t.Fatal("readiness join returned while worker was still active")
	case <-time.After(10 * time.Millisecond):
	}
	close(release)
	if result := <-joined; result.Err != nil || !exited.Load() {
		t.Fatalf("actual worker completion was not required: %+v", result)
	}
	started := make(chan struct{})
	cancelled := startEpochVerification(context.Background(), true, func(ctx context.Context) epochVerificationResult {
		close(started)
		<-ctx.Done()
		exited.Store(false)
		return epochVerificationResult{Err: ctx.Err()}
	})
	<-started
	if result := cancelled.join(true); !errors.Is(result.Err, context.Canceled) || exited.Load() {
		t.Fatalf("cancel returned before worker exit: %+v", result)
	}
}
