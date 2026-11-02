//go:build linux

package lifecycle

import (
	"context"
	"os"
	"reflect"
	"testing"

	"factorybox/internal/sysboxcapture"
)

func TestCaptureAliasIntentIsNotARestoreImage(t *testing.T) {
	path := t.TempDir()
	if err := os.MkdirAll(path+"/generation/images", 0700); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	engine := &Engine{root: root, config: Config{Root: path}}
	for _, name := range []string{"criu.conf", "filesystem.json", "source-state.json", "sysbox-state.json", "images/inventory.img"} {
		if err := root.WriteFile("generation/"+name, []byte(name), 0600); err != nil {
			t.Fatal(err)
		}
	}
	before, err := engine.checkpointFiles(context.Background(), "generation")
	if err != nil || len(before) != 5 {
		t.Fatal("fixture lacks its complete runtime image inventory", before, err)
	}
	const receipt = "capture-only immutable lower alias evidence"
	if err := root.WriteFile("generation/"+sysboxcapture.CaptureLowerAliasIntent, []byte(receipt), 0600); err != nil {
		t.Fatal(err)
	}
	after, err := engine.checkpointFiles(context.Background(), "generation")
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("capture-only alias intent changed the runtime image set copied by Resume", before, after, err)
	}
	saved, err := root.ReadFile("generation/" + sysboxcapture.CaptureLowerAliasIntent)
	if err != nil || string(saved) != receipt {
		t.Fatal("runtime inventory changed the separate capture ownership evidence", err)
	}
}
