//go:build linux

package lifecycle

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"

	"factorybox/internal/ownedtree"
	"golang.org/x/sys/unix"
)

func epochGuardAdmissionFixture(t *testing.T, name string, passed bool) (*epochInventoryBuilder, string) {
	t.Helper()
	directory := t.TempDir()
	root, err := os.OpenRoot(directory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.Close() })
	relative := "objects/source/captures/" + strings.Repeat("a", 32) + "/guard"
	if err := root.MkdirAll(relative, 0700); err != nil {
		t.Fatal(err)
	}
	anchor, err := root.Open(relative)
	if err != nil {
		t.Fatal(err)
	}
	descriptor := captureDescriptorAdmission{Version: 1, Token: strings.Repeat("a", 32),
		SourcePID: 42, SourceStart: "123", Attempts: 2, Seconds: .01,
		First: []captureDescriptorBlocker{{TID: 42, FD: 7, Kind: "foreign-proc-directory"}}, Passed: passed}
	if !passed {
		descriptor.Last = descriptor.First
	}
	var observation any = descriptor
	if name == "timer-admission.json" {
		timer := captureTimerAdmission{Version: 1, Token: descriptor.Token, SourcePID: descriptor.SourcePID,
			SourceStart: descriptor.SourceStart, Attempts: descriptor.Attempts, Seconds: descriptor.Seconds,
			First: &timerPendingCaughtSignal{TID: 42, Pending: 1, Caught: 1}, Passed: passed}
		if !passed {
			timer.Last = timer.First
		}
		observation = timer
	}
	if err := captureGuardWrite(anchor, name, observation); err != nil {
		anchor.Close()
		t.Fatal(err)
	}
	if err := anchor.Close(); err != nil {
		t.Fatal(err)
	}
	return &epochInventoryBuilder{engine: &Engine{root: root, config: Config{Root: directory}},
		ctx: context.Background(), allowed: map[string]uint32{}}, relative
}

func TestEpochRetirementRetainsAdmissionEvidence(t *testing.T) {
	for _, filename := range []string{"descriptor-admission.json", "timer-admission.json"} {
		t.Run(filename, func(t *testing.T) {
			for _, passed := range []bool{true, false} {
				name := "settled"
				if !passed {
					name = "rejected"
				}
				t.Run(name, func(t *testing.T) {
					builder, guard := epochGuardAdmissionFixture(t, filename, passed)
					file := guard + "/" + filename
					before, err := builder.engine.root.ReadFile(file)
					if err != nil {
						t.Fatal(err)
					}
					if err := builder.guard(guard, Record{}); err != nil {
						t.Fatal("runtime-created admission evidence blocks epoch retirement", err)
					}
					if builder.allowed[file] != unix.S_IFREG|0600 || builder.allowed[guard] != unix.S_IFDIR|0700 || len(builder.payloads) != 0 {
						t.Fatal("admission observation must remain private retained metadata and grant no payload deletion", builder.allowed, builder.payloads)
					}
					after, err := builder.engine.root.ReadFile(file)
					if err != nil || !bytes.Equal(before, after) {
						t.Fatal("inventory inspection changed original admission evidence", err)
					}
				})
			}
		})
	}
}

func TestEpochDescriptorEvidenceDoesNotAdmitUnknownGuardEntries(t *testing.T) {
	for _, kind := range []string{"lookalike", "directory", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			builder, guard := epochGuardAdmissionFixture(t, "descriptor-admission.json", true)
			name := guard + "/descriptor-admission.json.extra"
			var err error
			switch kind {
			case "lookalike":
				err = builder.engine.root.WriteFile(name, []byte("retain unknown metadata"), 0600)
			case "directory":
				err = builder.engine.root.Mkdir(name, 0700)
			case "symlink":
				err = builder.engine.root.Symlink("descriptor-admission.json", name)
			}
			if err != nil {
				t.Fatal(err)
			}
			before, err := ownedtree.Capture(context.Background(), builder.engine.root, []string{guard})
			if err != nil {
				t.Fatal(err)
			}
			if err := builder.guard(guard, Record{}); err == nil {
				t.Fatal("known descriptor evidence admitted a foreign guard entry")
			}
			if err := ownedtree.ValidateComplete(context.Background(), builder.engine.root, before); err != nil {
				t.Fatal("refused inventory changed original guard entries", err)
			}
		})
	}
}
