//go:build linux

package storefs

import (
	"encoding/binary"
	"encoding/hex"
	"os"
	"testing"

	"golang.org/x/sys/unix"
)

func TestCopyOnWriteAttachmentSeparatesFilesystemAndLoopIdentities(t *testing.T) {
	active := Active{
		Image:      Image{Schema: 2, Bytes: 512 << 20},
		LoopNumber: 3, BlockDevice: unix.Mkdev(0, 100),
		Mount:         Object{Device: unix.Mkdev(0, 100), Inode: 256, MountID: 100},
		FilesystemMax: 512 << 20, DirectIO: true,
	}
	if err := ValidateAttachmentIdentity(active); err != nil {
		t.Fatal("a versioned CoW mount cannot retain its distinct filesystem identity", err)
	}
	old := active
	old.Image.Schema = 1
	if ValidateAttachmentIdentity(old) == nil {
		t.Fatal("an existing ext4 attachment was reinterpreted as a virtual device")
	}
	old.BlockDevice, old.Mount.Device = unix.Mkdev(7, 3), unix.Mkdev(7, 3)
	if err := ValidateAttachmentIdentity(old); err != nil {
		t.Fatal("the original ext4 identity was no longer accepted", err)
	}
	for _, change := range []func(*Active){
		func(value *Active) { value.Mount.Device++ },
		func(value *Active) { value.LoopNumber = -1 },
		func(value *Active) { value.Mount.Inode = 0 },
		func(value *Active) { value.Mount.MountID = 0 },
		func(value *Active) { value.DirectIO = false },
		func(value *Active) { value.FilesystemMax++ },
		func(value *Active) { value.Image.Schema = 3 },
	} {
		value := active
		change(&value)
		if ValidateAttachmentIdentity(value) == nil {
			t.Fatal("changed CoW attachment identity was accepted")
		}
	}
}

func TestCopyOnWriteImageKeepsItsOwnFormatAndGeometry(t *testing.T) {
	const owner = "0123456789abcdef0123456789abcdef"
	expected := Image{Schema: 2, Owner: owner, UUID: owner, Bytes: 512 << 20}
	header := make([]byte, 160)
	uuid, err := hex.DecodeString(owner)
	if err != nil {
		t.Fatal(err)
	}
	copy(header[32:48], uuid)
	copy(header[64:72], "_BHRfS_M")
	binary.LittleEndian.PutUint64(header[112:120], uint64(expected.Bytes))
	binary.LittleEndian.PutUint64(header[136:144], 1)
	for _, name := range []string{"exact", "wrong uuid", "wrong magic", "resized", "multiple devices", "old format"} {
		t.Run(name, func(t *testing.T) {
			path := t.TempDir() + "/image"
			file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			current := append([]byte(nil), header...)
			switch name {
			case "wrong uuid":
				current[32] ^= 1
			case "wrong magic":
				current[64] ^= 1
			case "resized":
				binary.LittleEndian.PutUint64(current[112:120], uint64(expected.Bytes)+4096)
			case "multiple devices":
				binary.LittleEndian.PutUint64(current[136:144], 2)
			case "old format":
				current = make([]byte, len(header))
				binary.LittleEndian.PutUint16(current[56:58], 0xef53)
				copy(current[104:120], uuid)
				if _, err := file.WriteAt(current, 1024); err != nil {
					t.Fatal(err)
				}
				current = make([]byte, len(header))
			}
			if _, err := file.WriteAt(current, 64<<10); err != nil {
				t.Fatal(err)
			}
			err = verifySuperblock(file, expected)
			if (err == nil) != (name == "exact") {
				t.Fatalf("format-specific identity was not enforced: %v", err)
			}
		})
	}
}
