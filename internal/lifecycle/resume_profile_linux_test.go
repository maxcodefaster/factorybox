//go:build linux

package lifecycle

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"strconv"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// Timing receipt for the Resume preparation steps on a real filesystem. It
// writes synthetic RAM images and measures the hash passes and the image copy.
// Run: FACTORYBOX_PROFILE_DIR=/mnt/store FACTORYBOX_PROFILE_MIB=1200 go test -run TestResumePreparationTimingReceipt -v
func TestResumePreparationTimingReceipt(t *testing.T) {
	directory := os.Getenv("FACTORYBOX_PROFILE_DIR")
	if directory == "" {
		t.Skip("set FACTORYBOX_PROFILE_DIR to a scratch directory on the filesystem under test")
	}
	mib, _ := strconv.Atoi(os.Getenv("FACTORYBOX_PROFILE_MIB"))
	if mib == 0 {
		mib = 1200
	}
	scratch, err := os.MkdirTemp(directory, "factorybox-profile-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(scratch)
	sizes := map[string]int{"pages-1.img": mib / 2 << 20, "pages-2.img": mib / 8 << 20, "pages-3.img": mib / 8 << 20,
		"pages-4.img": mib / 8 << 20, "pages-5.img": mib / 16 << 20, "pages-6.img": mib / 16 << 20}
	for index := 0; index < 80; index++ {
		sizes[fmt.Sprintf("small-%02d.img", index)] = 4096 + rand.Intn(60000)
	}
	engine := hashFixture(t, scratch, sizes)
	names, err := engine.checkpointFileNames("generation")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	measure := func(label string, run func() error) time.Duration {
		started := time.Now()
		if err := run(); err != nil {
			t.Fatal(label, err)
		}
		elapsed := time.Since(started)
		t.Logf("%-42s %8.0f ms", label, float64(elapsed.Microseconds())/1000)
		return elapsed
	}
	t.Logf("image set: %d MiB in %d files, GOMAXPROCS %d", mib, len(names), runtimeProcs())
	for _, workers := range []int{1, 2, 4} {
		for pass := 0; pass < 2; pass++ {
			measure(fmt.Sprintf("hash workers=%d pass=%d", workers, pass), func() error {
				_, err := engine.hashCheckpointFiles(ctx, "generation", names, workers)
				return err
			})
		}
	}
	for _, variant := range []string{"per-file-fsync", "parallel-fsync", "per-file-fsync", "parallel-fsync"} {
		unix.Sync() // the source starts clean, as a published generation is
		destination := fmt.Sprintf("copy-%s-%d", variant, time.Now().UnixNano())
		if err := engine.root.Mkdir(destination, 0700); err != nil {
			t.Fatal(err)
		}
		if err := engine.root.Mkdir(destination+"/images", 0700); err != nil {
			t.Fatal(err)
		}
		measure("cp --reflink=auto "+variant, func() error {
			command := exec.Command("cp", "-a", "--reflink=auto", "--", scratch+"/generation/images/.", scratch+"/"+destination+"/images/")
			if output, err := command.CombinedOutput(); err != nil {
				return fmt.Errorf("%v: %s", err, output)
			}
			target, err := engine.root.Open(destination + "/images")
			if err != nil {
				return err
			}
			defer target.Close()
			entries, err := target.ReadDir(-1)
			if err != nil {
				return err
			}
			if variant == "parallel-fsync" {
				paths := make([]string, len(entries))
				for index, entry := range entries {
					paths[index] = destination + "/images/" + entry.Name()
				}
				if err := syncFiles(ctx, engine.root, paths, checkpointHashWorkers()); err != nil {
					return err
				}
				return target.Sync()
			}
			for _, entry := range entries {
				file, err := engine.root.Open(destination + "/images/" + entry.Name())
				if err != nil {
					return err
				}
				if err := file.Sync(); err != nil {
					file.Close()
					return err
				}
				file.Close()
			}
			return target.Sync()
		})
	}
}

func runtimeProcs() int { return checkpointHashWorkers() }
