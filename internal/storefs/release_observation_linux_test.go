//go:build linux

package storefs

import (
	"os"
	"testing"
)

func TestLoopBackingObservationDoesNotTreatUnknownStateAsRelease(t *testing.T) {
	root := t.TempDir()
	path := root + "/backing_file"
	for _, contents := range []string{"", "\n", "/owned/store.img\n", "a path with spaces\n"} {
		if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
			t.Fatal(err)
		}
		present, err := loopBackingPresent(path)
		expected := contents != "" && contents != "\n"
		if err != nil || present != expected {
			t.Fatal("sysfs backing observation changed its configured state", contents, present, err)
		}
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if present, err := loopBackingPresent(path); err != nil || present {
		t.Fatal("withdrawn sysfs backing was not a candidate for independent ioctl verification", present, err)
	}
	if _, err := loopBackingPresent(root); err == nil {
		t.Fatal("unreadable sysfs state was treated as confirmed release")
	}
}
