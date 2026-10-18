package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestDiagnosticPayloadRetirementPreservesAllRuntimeSnapshots(t *testing.T) {
	if os.Getenv("RUNTIME_FORK_GRAPH_NATIVE") != "1" {
		t.Skip("requires the owned native worker fixture")
	}
	for _, variant := range []string{"complete", "stream", "stream-single", "stream-single-fd", "stream-parent", "stream-live-fd", "live-fd", "mapped", "mounted", "symlink", "wrong-intent", "unverified", "fast", "fast-fd", "fast-wrong-intent", "fast-unverified", "fast-shadow"} {
		t.Run(variant, func(t *testing.T) {
			source, _ := descendantOwnedLayout("copy-sysbox")
			target, _ := descendantOwnedLayout("branch-33333333333333333333333333333333")
			for _, path := range []string{source.Root, target.Root} {
				if _, err := os.Lstat(path); !os.IsNotExist(err) {
					t.Fatal("native test must own absent runtime roots", path, err)
				}
			}
			t.Cleanup(func() { os.RemoveAll(source.Root); os.RemoveAll(target.Root) })
			evidence := source.Root + "/proof"
			parent := source.Root + "/images"
			capture := &descendantInheritedCapture{
				OriginalImages: source.Root + "/images-11111111111111111111111111111111",
				Images:         source.Root + "/images-22222222222222222222222222222222",
				Proof:          map[string]any{"same_capture_original_writer": true, "parent_images": parent},
			}
			if strings.HasPrefix(variant, "stream") {
				capture.OriginalImages = capture.Images + "/original-writer"
				capture.Proof["native_stream_original_writer"] = true
			}
			fast := strings.HasPrefix(variant, "fast")
			single := strings.HasPrefix(variant, "stream-single") || fast
			if fast {
				capture.OriginalImages = ""
				capture.Proof["ordinary_fast_collector"] = true
				capture.Proof["same_capture_original_writer"] = false
			}
			capture.Proof["single_source_capture"] = single
			paths := []string{capture.OriginalImages, capture.Images, target.Root + "/control-images"}
			if single {
				paths = paths[:2]
			}
			if fast {
				paths = []string{capture.Images}
			}
			payload := bytes.Repeat([]byte{0x83, 0x21, 0xA7, 0x59}, 2048)
			preserved := map[string][]byte{
				parent + "/retained-original":                                 []byte("original restore remains"),
				source.Root + "/images-44444444444444444444444444444444/keep": []byte("stopped ancestor remains"),
				target.Root + "/images/pages-99.img":                          payload,
				target.Root + "/parent-1/pages-88.img":                        payload,
			}
			for path, data := range preserved {
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.MkdirAll(evidence, 0700); err != nil {
				t.Fatal(err)
			}
			for index, path := range paths {
				if err := os.MkdirAll(path, 0700); err != nil {
					t.Fatal(err)
				}
				for name, data := range map[string][]byte{"pages-1.img": payload, "inventory.img": []byte("saved metadata")} {
					file := filepath.Join(path, name)
					if err := os.WriteFile(file, data, 0600); err != nil {
						t.Fatal(err)
					}
					if err := os.Chown(file, 165536, 165536); err != nil {
						t.Fatal(err)
					}
				}
				if err := os.Chown(path, 165536, 165536); err != nil {
					t.Fatal(err)
				}
				if index < 2 && !(strings.HasPrefix(variant, "stream") && variant != "stream-parent" && index == 0) {
					if err := os.Symlink("../images", path+"/parent"); err != nil {
						t.Fatal(err)
					}
				}
			}
			intent := map[string]any{"runtime": source, "images": capture.Images,
				"original_writer_images": capture.OriginalImages, "parent_images": parent,
				"native_stream_original_writer": strings.HasPrefix(variant, "stream"), "single_source_capture": single,
				"ordinary_fast_collector": fast}
			data, _ := json.Marshal(intent)
			if err := os.WriteFile(evidence+"/inherited-capture-intent.json", data, 0600); err != nil {
				t.Fatal(err)
			}
			memory := map[string]any{"all_original_writer_bytes_match": true}
			if fast {
				memory = map[string]any{"all_captured_graph_bytes_match": true, "reference_is_captured_graph": true}
			}
			graph := map[string]any{"all_graph_payload_hashes_match": true}
			page := paths[0] + "/pages-1.img"
			switch variant {
			case "live-fd", "stream-live-fd", "stream-single-fd", "fast-fd":
				file, err := os.Open(page)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { file.Close() })
			case "mapped":
				file, err := os.Open(page)
				if err != nil {
					t.Fatal(err)
				}
				mapped, err := unix.Mmap(int(file.Fd()), 0, len(payload), unix.PROT_READ, unix.MAP_SHARED)
				file.Close()
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { unix.Munmap(mapped) })
			case "mounted":
				mount := evidence + "/borrowed-page"
				if err := os.WriteFile(mount, nil, 0600); err != nil {
					t.Fatal(err)
				}
				if err := unix.Mount(page, mount, "", unix.MS_BIND, ""); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					if err := unix.Unmount(mount, 0); err != nil {
						t.Error(err)
					}
				})
			case "symlink":
				if err := os.Remove(page); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(parent+"/retained-original", page); err != nil {
					t.Fatal(err)
				}
			case "wrong-intent", "fast-wrong-intent":
				capture.Images = source.Root + "/images-44444444444444444444444444444444"
			case "unverified":
				memory["all_original_writer_bytes_match"] = false
			case "fast-unverified":
				memory["all_captured_graph_bytes_match"] = false
			case "fast-shadow":
				capture.OriginalImages = parent
			}
			result, err := descendantRetireInheritedDiagnostics(source, target, capture, evidence, memory, graph)
			success := variant == "complete" || variant == "stream" || variant == "stream-single" || variant == "fast"
			if success {
				if err != nil || result["retired"] != true || result["retired_page_files"] != len(paths) ||
					result["logical_bytes"] != int64(len(paths)*len(payload)) {
					t.Fatal("unused diagnostic data did not retire", result, err)
				}
			} else {
				if err == nil {
					t.Fatal("unsafe diagnostic retirement was accepted", variant)
				}
				if variant == "mounted" && !strings.Contains(err.Error(), "mount reference") {
					t.Fatal("mounted source was rejected for the wrong reason", err)
				}
			}
			for _, path := range paths {
				page := path + "/pages-1.img"
				if success {
					if _, err := os.Lstat(page); !os.IsNotExist(err) {
						t.Fatal("diagnostic payload remains", path, err)
					}
				} else if variant != "symlink" || path != paths[0] {
					actual, err := os.ReadFile(page)
					if err != nil || !bytes.Equal(actual, payload) {
						t.Fatal("rejected batch changed diagnostic data", path, err)
					}
				}
				metadata, err := os.ReadFile(path + "/inventory.img")
				if err != nil || string(metadata) != "saved metadata" {
					t.Fatal("retirement lost compact metadata", path, err)
				}
			}
			for path, expected := range preserved {
				actual, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(actual, expected) {
					t.Fatal("retirement changed an actual runtime snapshot", path, err)
				}
			}
		})
	}
}
