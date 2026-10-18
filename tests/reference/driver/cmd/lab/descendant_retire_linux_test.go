package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestDescendantTransportRetirementNative(t *testing.T) {
	if os.Getenv("DESCENDANT_RETIRE_NATIVE") != "1" {
		t.Skip("isolated writable Linux container only")
	}
	source, err := descendantOwnedLayout("copy-sysbox")
	if err != nil {
		t.Fatal(err)
	}
	for _, variant := range []string{"complete", "foreign-path", "symlink", "directory"} {
		t.Run(variant, func(t *testing.T) {
			if _, err := os.Lstat(source.Root); !os.IsNotExist(err) {
				t.Fatal("fixture must own an absent root", err)
			}
			t.Cleanup(func() { os.RemoveAll(source.Root) })
			images := source.Root + "/images-0123456789abcdef0123456789abcdef"
			evidence := source.Root + "/proof"
			clone := source.Root + "/private-clone"
			for _, path := range []string{images, evidence, clone, source.Root + "/images"} {
				if err := os.MkdirAll(path, 0700); err != nil {
					t.Fatal(err)
				}
			}
			payloads := map[string][]byte{
				"inventory.img": []byte("complete inventory"), "pstree.img": []byte("owned processes"),
				"runtime-config.json": []byte(`{"saved":"source"}`), "mm-1.img": []byte("owned mappings"),
				"descriptors.json": []byte(`["/dev/null","owned-stdout","owned-stderr"]`),
				"pages-1.img":      bytes.Repeat([]byte("native-page-data"), 1<<19),
				"dump.log":         []byte("full retained capture diagnostics"), "stats-dump": []byte("native counters"),
			}
			for name, data := range payloads {
				for _, path := range []string{images, clone} {
					if err := os.WriteFile(filepath.Join(path, name), data, 0600); err != nil {
						t.Fatal(err)
					}
				}
			}
			sentinel := source.Root + "/images/retained-original"
			if err := os.WriteFile(sentinel, []byte("original restore remains"), 0600); err != nil {
				t.Fatal(err)
			}
			target := images
			switch variant {
			case "foreign-path":
				target = source.Root + "/images"
			case "symlink":
				if err := os.Symlink(sentinel, images+"/foreign"); err != nil {
					t.Fatal(err)
				}
			case "directory":
				if err := os.Mkdir(images+"/foreign", 0700); err != nil {
					t.Fatal(err)
				}
			}
			proof, err := descendantRetireTransport(source, target, evidence)
			if variant == "complete" {
				if err != nil || proof["retired"] != true || proof["logical_bytes"].(int64) < 8<<20 {
					t.Fatal(proof, err)
				}
				if _, err := os.Lstat(images); !os.IsNotExist(err) {
					t.Fatal("transport remains", err)
				}
				for _, name := range []string{"inventory.img", "pstree.img", "runtime-config.json", "descriptors.json", "dump.log", "stats-dump"} {
					data, err := os.ReadFile(evidence + "/retired-source-capture/" + name)
					if err != nil || !bytes.Equal(data, payloads[name]) {
						t.Fatal("retained diagnostics changed", name, err)
					}
				}
			} else {
				if err == nil {
					t.Fatal("unsafe retirement accepted", variant, proof)
				}
				for name, expected := range payloads {
					data, err := os.ReadFile(filepath.Join(images, name))
					if err != nil || !bytes.Equal(data, expected) {
						t.Fatal("rejected retirement changed capture", name, err)
					}
				}
			}
			for name, expected := range payloads {
				data, err := os.ReadFile(filepath.Join(clone, name))
				if err != nil || !bytes.Equal(data, expected) {
					t.Fatal("clone changed after retirement", name, err)
				}
			}
			data, err := os.ReadFile(sentinel)
			if err != nil || string(data) != "original restore remains" {
				t.Fatal("original restore backing changed", err)
			}
		})
	}
}
