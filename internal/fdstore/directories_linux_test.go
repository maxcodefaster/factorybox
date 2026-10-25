//go:build linux

package fdstore

import (
	"encoding/hex"
	"os"
	"testing"
)

func TestDirectoryRenameKeepsExactInodeAndCurrentPath(t *testing.T) {
	project := t.TempDir()
	if err := os.Mkdir(project+"/old", 0700); err != nil {
		t.Fatal(err)
	}
	root, err := os.Open(project)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	original, err := os.Open(project + "/old")
	if err != nil {
		t.Fatal(err)
	}
	defer original.Close()
	directory := Directory{Name: "factorybox-dir:7", MountPoint: "/workspace/project", MountRootRelative: "."}
	saved := generation{names: []string{directory.Name, SourceRootName}, files: []*os.File{original, root}, directories: []Directory{directory}}
	if err := validateDirectories(saved.names, saved.files, saved.directories); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(project+"/old", project+"/renamed"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(project+"/old", 0700); err != nil {
		t.Fatal(err)
	}
	names, err := restoreNames(saved)
	if err != nil {
		t.Fatal(err)
	}
	if names[0] != "factorybox-dir:7:"+hex.EncodeToString([]byte("/workspace/project/renamed")) {
		t.Fatal("lost renamed cwd", names)
	}
	if err := os.Remove(project + "/renamed"); err != nil {
		t.Fatal(err)
	}
	if _, err := restoreNames(saved); err == nil {
		t.Fatal("accepted deleted directory or replacement inode")
	}
}

func TestDirectoryCannotMoveOutsideSourceOrFollowSymlink(t *testing.T) {
	parent := t.TempDir()
	project := parent + "/project"
	if err := os.MkdirAll(project+"/folder", 0700); err != nil {
		t.Fatal(err)
	}
	root, err := os.Open(project)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	original, err := os.Open(project + "/folder")
	if err != nil {
		t.Fatal(err)
	}
	defer original.Close()
	directory := Directory{Name: "factorybox-dir:2", MountPoint: "/workspace/project", MountRootRelative: "."}
	if err := os.Rename(project+"/folder", parent+"/outside"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(parent+"/outside", project+"/folder"); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveDirectory(root, original, directory); err == nil {
		t.Fatal("directory escaped canonical project")
	}
	for _, mapping := range []Directory{
		{Name: directory.Name, MountPoint: "/workspace/project", MountRootRelative: "../escape"},
		{Name: directory.Name, MountPoint: "relative", MountRootRelative: "."},
	} {
		if err := validateDirectories([]string{directory.Name, SourceRootName}, []*os.File{original, root}, []Directory{mapping}); err == nil {
			t.Fatal("accepted invalid mapping", mapping)
		}
	}
}
