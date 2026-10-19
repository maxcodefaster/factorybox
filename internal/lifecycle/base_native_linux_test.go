//go:build linux

package lifecycle

import (
	"encoding/json"
	"os"
	"os/exec"
	"testing"

	"factorybox/internal/sysboxcapture"
	"golang.org/x/sys/unix"
)

// Run the unchanged full capture/fork and pause/resume assertions against both
// ordinary copied lowers and the explicitly prepared immutable-base path.
func prepareNativeBase(t *testing.T, engine *Engine, bundle string) {
	t.Helper()
	if os.Getenv("FACTORYBOX_NATIVE_IMMUTABLE_BASE") != "1" {
		return
	}
	command := exec.Command("/factorybox", "--root", engine.config.Root,
		"--runtime", engine.config.Runtime, "--runtime-sha256", engine.config.RuntimeSHA256,
		"prepare-base", "--source", bundle+"/lower", "native-base")
	output, err := command.CombinedOutput()
	var base Base
	if err != nil || json.Unmarshal(output, &base) != nil || base.ID != "native-base" ||
		base.Tree.Immutable == nil || base.PreparedAt == "" {
		t.Fatalf("public immutable-base preparation failed: %v: %s", err, output)
	}
	replayed, err := exec.Command(command.Path, command.Args[1:]...).CombinedOutput()
	var again Base
	if err != nil || json.Unmarshal(replayed, &again) != nil || again.PreparedAt != base.PreparedAt ||
		again.Tree.Immutable == nil || again.Tree.Immutable.MountID != base.Tree.Immutable.MountID {
		t.Fatalf("base replay did not retain its exact imported identity: %v: %s", err, replayed)
	}
	if err := unix.Unmount(bundle+"/rootfs", 0); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(bundle+"/lower", bundle+"/lower-input"); err != nil {
		t.Fatal(err)
	}
	if _, err := sysboxcapture.BindFilesystemBase(base.Tree, bundle+"/lower"); err != nil {
		t.Fatal(err)
	}
	if err := unix.Mount("overlay", bundle+"/rootfs", "overlay", 0,
		"lowerdir="+bundle+"/lower,upperdir="+bundle+"/upper,workdir="+bundle+"/work"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(bundle + "/config.json")
	if err != nil {
		t.Fatal(err)
	}
	var spec map[string]json.RawMessage
	if err := json.Unmarshal(data, &spec); err != nil {
		t.Fatal(err)
	}
	annotations := map[string]string{}
	if raw := spec["annotations"]; len(raw) != 0 {
		if err := json.Unmarshal(raw, &annotations); err != nil {
			t.Fatal(err)
		}
	}
	annotations[baseAnnotation] = base.ID
	spec["annotations"], _ = json.Marshal(annotations)
	data, err = json.Marshal(spec)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bundle+"/config.json", data, 0600); err != nil {
		t.Fatal(err)
	}
	t.Log("public CLI imported and verified a private immutable base before start; complete lifecycle assertions unchanged")
}
