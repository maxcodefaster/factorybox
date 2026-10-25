//go:build linux

package sysboxcapture

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestProcessFDImageUsesSavedFileTableIdentity(t *testing.T) {
	// The native shared-source failure had process 1 with file table 2.
	// CRIU names fdinfo images by files_id, not the process PID.
	images := t.TempDir()
	for name, value := range map[string]string{
		"ids-1.img":    "1943565430204354180000000802100218022002280a300b380c400d480f50095810600e",
		"fdinfo-2.img": "1943565432372156080000000805100118012003",
	} {
		data, err := hex.DecodeString(value)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(images, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	records, err := processFDImage(images, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || imageInteger(records[0], 1) != 5 || imageInteger(records[0], 4) != 3 {
		t.Fatalf("lost original shared FD 3, description 5: %v", records)
	}
}

func TestSharedExternalIdentityRetainsUnlinkedObject(t *testing.T) {
	directory := t.TempDir()
	source, err := IdentifySharedSource(SharedSource{Source: directory, Destination: "/project"})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "held")
	if err := os.WriteFile(path, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	original, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer original.Close()
	before, err := sharedExternalIdentity(int(original.Fd()), source)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("editor replacement"), 0600); err != nil {
		t.Fatal(err)
	}
	after, err := sharedExternalIdentity(int(original.Fd()), source)
	if err != nil || after != before {
		t.Fatalf("lost the pinned unlinked identity: %q %q %v", before, after, err)
	}
	replacement, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer replacement.Close()
	other, err := sharedExternalIdentity(int(replacement.Fd()), source)
	if err != nil || other == before {
		t.Fatal("replacement pathname granted the original object identity", other, err)
	}
	foreign := *source
	foreign.Device++
	if _, err := sharedExternalIdentity(int(original.Fd()), &foreign); err == nil {
		t.Fatal("accepted another source filesystem")
	}
	root, err := unix.Open(directory, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(root)
	if _, err := sharedExternalIdentity(root, source); err == nil {
		t.Fatal("accepted an unqualified directory description")
	}
}
