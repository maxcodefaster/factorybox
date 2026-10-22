//go:build linux && factorybox_worker_probe

package devx_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"factorybox/internal/lifecycle"
	"factorybox/internal/sysboxcapture"
	"golang.org/x/sys/unix"
)

// This composes the existing prepared-base API into two fresh application
// starts. It is not a new base-cache algorithm or a fork of application RAM.
func TestNativeWarmPreparation(t *testing.T) {
	if os.Getenv("FACTORYBOX_DEVX_WARM_PREPARATION") != "1" || os.Getenv("FACTORYBOX_DEVX_OPENCODE_FLOW") == "1" {
		t.Fatal("requires its exact fresh-start preparation fixture")
	}
	f, cold, started := prepareConsumerFixture(t)
	minimal := os.Getenv("FACTORYBOX_DEVX_MINIMAL") == "1"
	var initialMinimal minimalState
	var initialFactory observedState
	coldUseful := 0.0
	if minimal {
		initialMinimal = minimalWorkerState(t, cold, "", 180*time.Second)
		coldUseful = time.Since(started).Seconds()
		minimalWorkerState(t, cold, `{"marker":"cold-private"}`, 5*time.Second)
	} else {
		f.attachIDE(cold)
		initialFactory = f.useful("prepared-start-first-use", cold, started, "")
		coldUseful = time.Since(started).Seconds()
		f.useful("cold-private", cold, time.Now(), `{"increment":1,"marker":"cold-private"}`)
	}
	var base lifecycle.Base
	raw, err := os.ReadFile(f.root + "/bases/developer-base/base.json")
	if err != nil || json.Unmarshal(raw, &base) != nil || base.Tree.Immutable == nil {
		t.Fatal("missing exact previously imported base", err)
	}
	warmStart := time.Now()
	var replay lifecycle.Base
	if err := json.Unmarshal(f.command("prepare-base", "--source", base.Source, base.ID), &replay); err != nil || !reflect.DeepEqual(replay, base) {
		t.Fatal("warm base replay changed its import", err)
	}
	bundle := "/var/lib/sysbox/devx/warm-bundle"
	if err := os.Mkdir(bundle, 0700); err != nil {
		t.Fatal(err)
	}
	alias, err := sysboxcapture.BindFilesystemBase(base.Tree, bundle+"/lower")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := sysboxcapture.RetireImmutableAlias(bundle+"/lower", alias); err != nil {
			t.Error(err)
		}
	})
	for _, name := range []string{"rootfs", "upper", "work"} {
		if err := os.Mkdir(bundle+"/"+name, 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := unix.Mount("overlay", bundle+"/rootfs", "overlay", 0, "index=off,lowerdir="+bundle+"/lower,upperdir="+bundle+"/upper,workdir="+bundle+"/work"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := unix.Unmount(bundle+"/rootfs", 0); err != nil {
			t.Error(err)
		}
	})
	raw, err = os.ReadFile(cold.Record.InputBundle + "/config.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bundle+"/config.json", raw, 0600); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"upper", "work"} {
		old, err := os.Stat(filepath.Join(cold.Record.InputBundle, name))
		if err != nil {
			t.Fatal(err)
		}
		fresh, err := os.Stat(filepath.Join(bundle, name))
		if err != nil || os.SameFile(old, fresh) {
			t.Fatal("fresh start shares writable backing", name, err)
		}
	}
	warmPreparation := time.Since(warmStart).Seconds()
	start := time.Now()
	warm := f.view("start", "--bundle", bundle, "warm")
	f.cleanup("warm")
	if warm.Record.Fork != nil || warm.Record.Owner == cold.Record.Owner || warm.Runtime == nil || warm.Runtime.ID == cold.Runtime.ID {
		t.Fatal("prepared fresh start became a fork or reused authority")
	}
	var stableMinimal minimalState
	var stableFactory observedState
	if minimal {
		stableMinimal = minimalWorkerState(t, warm, "", 180*time.Second)
		if stableMinimal.Counter != 0 || stableMinimal.Marker != "source" || stableMinimal.RAMSHA256 == initialMinimal.RAMSHA256 {
			t.Fatal("warm start inherited cold application RAM/files")
		}
	} else {
		stableFactory = f.useful("warm-start-first-use", warm, start, "")
		if stableFactory.App.Counter != 0 || stableFactory.App.Marker != "source" || stableFactory.Authority == initialFactory.Authority || stableFactory.App.Instance == initialFactory.App.Instance {
			t.Fatal("warm start inherited cold application identity/RAM/files")
		}
	}
	warmUseful := time.Since(start).Seconds()
	f.view("delete", "source")
	if minimal {
		if actual := minimalWorkerState(t, warm, "", 5*time.Second); !reflect.DeepEqual(actual, stableMinimal) {
			t.Fatal("cold source deletion changed warm independent consumer")
		}
	} else {
		if actual := f.useful("warm-after-cold-delete", warm, time.Now(), ""); !reflect.DeepEqual(actual, stableFactory) {
			t.Fatal("cold source deletion changed warm independent consumer")
		}
		if f.ide != nil {
			f.ide.finish()
			f.ide = nil
		}
	}
	children, err := os.ReadDir(f.root + "/bases")
	if err != nil || len(children) != 1 || children[0].Name() != base.ID {
		t.Fatal("warm preparation silently imported another base", err)
	}
	report := map[string]any{"schema": 1, "cold_start_first_use_seconds": coldUseful, "warm_incremental_preparation_seconds": warmPreparation,
		"warm_start_first_use_seconds": warmUseful, "base_id": base.ID, "prepared_at": base.PreparedAt, "base_sha256": base.Tree.SHA256,
		"base_import_count": 1, "source_and_warm_are_fresh_independent_starts": true,
		"scope": "Existing immutable-base reuse composed for a second fresh consumer. Full initial rootfs/base preparation remains in preparation.json; no claim of a new cache algorithm or checkpoint/RAM template reuse."}
	raw, err = json.MarshalIndent(report, "", "  ")
	if err != nil || os.WriteFile("/output/warm-preparation.json", append(raw, '\n'), 0600) != nil {
		t.Fatal("retain warm preparation evidence", err)
	}
	readableConsumerOutput(t)
}
