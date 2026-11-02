//go:build linux

package lifecycle

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"testing"
)

func resumeCopyFixture(t *testing.T) (*Engine, Record, string, string, []CheckpointFile) {
	t.Helper()
	path := t.TempDir()
	record := Record{ID: "source", Phase: "resuming", Pause: &PauseOperation{
		SnapshotID: strings.Repeat("a", 32), CompletedAt: "original-pause",
		Resume: &Operation{RequestedAt: "original-Resume"},
	}}
	generation := "objects/source/captures/" + record.Pause.SnapshotID
	relative := "objects/source/resumes/" + record.Pause.SnapshotID
	for _, directory := range []string{generation + "/images", relative + "/images"} {
		if err := os.MkdirAll(path+"/"+directory, 0700); err != nil {
			t.Fatal(err)
		}
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.Close() })
	engine := &Engine{root: root, config: Config{Root: path}}
	var files []CheckpointFile
	for name, data := range map[string][]byte{
		"criu.conf": []byte("original configuration\n"), "filesystem.json": []byte("{}\n"),
		"source-state.json": []byte("original source\n"), "sysbox-state.json": []byte("original manager\n"),
		"images/inventory.img": []byte("original inventory\n"),
		"images/pages-1.img":   bytes.Repeat([]byte("immutable RAM pages\n"), 16384),
	} {
		if err := root.WriteFile(generation+"/"+name, data, 0600); err != nil {
			t.Fatal(err)
		}
		files = append(files, CheckpointFile{Name: name, SHA256: sum(data), Bytes: int64(len(data))})
		if name == "images/pages-1.img" {
			if err := root.WriteFile(relative+"/"+name, data[:4096], 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name })
	return engine, record, generation, relative, files
}

func TestResumeCopyCompletesOnlyOriginalPrefixesWithoutReplacingThem(t *testing.T) {
	engine, record, generation, relative, files := resumeCopyFixture(t)
	before, err := engine.root.Stat(relative + "/images/pages-1.img")
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.continueResumeBacking(context.Background(), record, generation, relative, files); err != nil {
		t.Fatal(err)
	}
	after, err := engine.root.Stat(relative + "/images/pages-1.img")
	if err != nil || !os.SameFile(before, after) {
		t.Fatal("continuation replaced or removed its already copied original bytes", err)
	}
	for _, saved := range files {
		source, err := engine.root.ReadFile(generation + "/" + saved.Name)
		if err != nil || sum(source) != saved.SHA256 || int64(len(source)) != saved.Bytes {
			t.Fatal("continuation changed the immutable source", saved.Name, err)
		}
		target, err := engine.root.ReadFile(relative + "/" + saved.Name)
		if err != nil || !bytes.Equal(source, target) {
			t.Fatal("continuation did not complete the exact checkpoint byte set", saved.Name, err)
		}
	}
	if record.Pause.Resume.RequestedAt != "original-Resume" || record.Pause.Resume.Attempted {
		t.Fatal("copy continuation changed execution authority")
	}
}

func TestResumeCopyCompletesOriginalImageDirectoryOwnership(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("mapped image-directory ownership requires the root metadata lane")
	}
	engine, record, generation, relative, files := resumeCopyFixture(t)
	source := engine.config.Root + "/" + generation + "/images"
	target := engine.config.Root + "/" + relative + "/images"
	if err := os.Chown(source, 165536, 165536); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(source, 0750); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.continueResumeBacking(context.Background(), record, generation, relative, files); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(target)
	if err != nil || !os.SameFile(before, after) {
		t.Fatal("continuation replaced its original image directory", err)
	}
	stat := after.Sys().(*syscall.Stat_t)
	if stat.Uid != 165536 || stat.Gid != 165536 || after.Mode().Perm() != 0750 {
		t.Fatal("continued image directory is inaccessible to its mapped restore owner", stat.Uid, stat.Gid, after.Mode())
	}
}

func TestResumeCopyCompletesUnfinishedMappedFileOwnership(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("unfinished mapped file ownership requires the root metadata lane")
	}
	engine, record, generation, relative, files := resumeCopyFixture(t)
	for _, file := range files {
		if strings.HasPrefix(file.Name, "images/") {
			if err := os.Chown(engine.config.Root+"/"+generation+"/"+file.Name, 100000, 100000); err != nil {
				t.Fatal(err)
			}
		}
	}
	complete := relative + "/images/inventory.img"
	data, err := engine.root.ReadFile(generation + "/images/inventory.img")
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.root.WriteFile(complete, data, 0600); err != nil {
		t.Fatal(err)
	}
	identities := map[string]os.FileInfo{}
	for _, name := range []string{complete, relative + "/images/pages-1.img"} {
		identities[name], err = engine.root.Stat(name)
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := engine.continueResumeBacking(context.Background(), record, generation, relative, files); err != nil {
		t.Fatal(err)
	}
	for name, before := range identities {
		after, err := engine.root.Stat(name)
		if err != nil || !os.SameFile(before, after) {
			t.Fatal("ownership completion replaced a verified original file", err)
		}
		stat := after.Sys().(*syscall.Stat_t)
		if stat.Uid != 100000 || stat.Gid != 100000 || after.Mode().Perm() != 0600 {
			t.Fatal("verified complete or partial copy retained unfinished ownership", name)
		}
	}
}

func TestResumeCopyRefusesForeignDataAndExecutionWithoutWriting(t *testing.T) {
	for _, fault := range []string{"prefix", "oversized", "hardlink", "symlink", "unexpected", "mode", "owner",
		"directory-mode", "directory-owner", "prepared", "held", "result", "attempted", "completed", "phase", "path", "cancelled"} {
		t.Run(fault, func(t *testing.T) {
			engine, record, generation, relative, files := resumeCopyFixture(t)
			ctx := context.Background()
			name := relative + "/images/pages-1.img"
			switch fault {
			case "prefix":
				if err := engine.root.WriteFile(name, []byte("foreign bytes"), 0600); err != nil {
					t.Fatal(err)
				}
			case "oversized":
				if err := engine.root.WriteFile(name, make([]byte, 1<<20), 0600); err != nil {
					t.Fatal(err)
				}
			case "hardlink":
				if err := os.Link(engine.config.Root+"/"+name, engine.config.Root+"/foreign-link"); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err := engine.root.Remove(name); err != nil {
					t.Fatal(err)
				}
				link, err := filepath.Rel(filepath.Dir(name), generation+"/images/pages-1.img")
				if err != nil {
					t.Fatal(err)
				}
				if err := engine.root.Symlink(link, name); err != nil {
					t.Fatal(err)
				}
			case "unexpected":
				if err := engine.root.WriteFile(relative+"/images/foreign.img", []byte("preserve"), 0600); err != nil {
					t.Fatal(err)
				}
			case "mode":
				if err := os.Chmod(engine.config.Root+"/"+name, 0644); err != nil {
					t.Fatal(err)
				}
			case "owner":
				if os.Geteuid() != 0 {
					t.Skip("foreign file owner requires the root metadata lane")
				}
				if err := os.Chown(engine.config.Root+"/"+name, 1001, 1001); err != nil {
					t.Fatal(err)
				}
			case "directory-mode":
				if err := os.Chmod(engine.config.Root+"/"+relative+"/images", 0770); err != nil {
					t.Fatal(err)
				}
			case "directory-owner":
				if os.Geteuid() != 0 {
					t.Skip("foreign directory owner requires the root metadata lane")
				}
				if err := os.Chown(engine.config.Root+"/"+relative+"/images", 1001, 1001); err != nil {
					t.Fatal(err)
				}
			case "prepared", "held", "result":
				marker := map[string]string{"prepared": "resume-prepared.json", "held": "resume-held.json", "result": "resume-result.json"}[fault]
				if err := engine.root.WriteFile(relative+"/"+marker, []byte("{}"), 0600); err != nil {
					t.Fatal(err)
				}
			case "attempted":
				record.Pause.Resume.Attempted = true
			case "completed":
				record.Pause.Resume.CompletedAt = "completed"
			case "phase":
				record.Phase = "running"
			case "path":
				relative += "-foreign"
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			before, err := engine.root.ReadFile(name)
			if err != nil {
				t.Fatal(err)
			}
			if err := engine.continueResumeBacking(ctx, record, generation, relative, files); err == nil {
				t.Fatal("unsafe or attempted Resume preparation was continued")
			}
			after, err := engine.root.ReadFile(name)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("refusal changed existing private bytes", err)
			}
			for _, missing := range []string{"criu.conf", "filesystem.json", "source-state.json", "sysbox-state.json"} {
				if _, err := engine.root.Lstat(filepath.Dir(filepath.Dir(name)) + "/" + missing); !os.IsNotExist(err) {
					t.Fatal("refusal created metadata before validating all private input", missing, err)
				}
			}
		})
	}
}
