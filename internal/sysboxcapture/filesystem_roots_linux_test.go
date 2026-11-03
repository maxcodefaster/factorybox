//go:build linux

package sysboxcapture

import (
	"context"
	"errors"
	"testing"
)

func TestFilesystemRootsVisitEveryResult(t *testing.T) {
	results := make([]int, 9)
	if err := visitFilesystemRoots(context.Background(), len(results), func(_ context.Context, index int) error {
		results[index] = index + 1
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for index, result := range results {
		if result != index+1 {
			t.Fatal("root result was omitted or reordered", results)
		}
	}
}

func TestFilesystemRootsKeepCanceledCaller(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := visitFilesystemRoots(ctx, 9, func(context.Context, int) error {
		t.Error("canceled caller started root work")
		return nil
	}); !errors.Is(err, context.Canceled) {
		t.Fatal("root work lost caller cancellation", err)
	}
}

func TestFilesystemRootsRetainFailure(t *testing.T) {
	failure := errors.New("root verification failed")
	if err := visitFilesystemRoots(context.Background(), 9, func(_ context.Context, index int) error {
		if index == 1 {
			return failure
		}
		return nil
	}); !errors.Is(err, failure) {
		t.Fatal("root verification failure was lost", err)
	}
}
