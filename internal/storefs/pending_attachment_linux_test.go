//go:build linux

package storefs

import (
	"errors"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestPendingAttachmentRequiresOneExactOriginalAssociation(t *testing.T) {
	image := Image{Owner: "11111111111111111111111111111111", Device: 41, Inode: 42, Bytes: 512 << 20}
	original := unix.LoopInfo64{Device: image.Device, Inode: image.Inode, Number: 3,
		Sizelimit: uint64(image.Bytes), Flags: unix.LO_FLAGS_AUTOCLEAR | unix.LO_FLAGS_DIRECT_IO}
	copy(original.File_name[:], "factorybox-"+image.Owner)
	pending := originalAssociation(image, 3, &original)
	if pending == nil || pending.Image != image || pending.LoopNumber != 3 ||
		pending.Status != original || !errors.Is(pending, unix.EWOULDBLOCK) {
		t.Fatal("original association lost its exact pending identity", pending)
	}
	if !strings.Contains(pending.Error(), "already has a loop association") {
		t.Fatal("an unleased association lost its independent refusal", pending)
	}
	pending.imageOwnerHeld = true
	if !strings.Contains(pending.Error(), "already has an owner") {
		t.Fatal("a held original lease lost its independent refusal", pending)
	}
	if originalAssociation(image, 3, nil) != nil || originalAssociation(image, -1, &original) != nil {
		t.Fatal("missing or invalid association was attributed to the image")
	}
	for _, change := range []func(*unix.LoopInfo64){
		func(value *unix.LoopInfo64) { value.Device++ },
		func(value *unix.LoopInfo64) { value.Inode++ },
		func(value *unix.LoopInfo64) { value.Number++ },
		func(value *unix.LoopInfo64) { value.Offset++ },
		func(value *unix.LoopInfo64) { value.Sizelimit++ },
		func(value *unix.LoopInfo64) { value.Flags &^= unix.LO_FLAGS_AUTOCLEAR },
		func(value *unix.LoopInfo64) { value.Flags |= unix.LO_FLAGS_READ_ONLY },
		func(value *unix.LoopInfo64) { value.Encrypt_type++ },
		func(value *unix.LoopInfo64) { value.Encrypt_key_size++ },
		func(value *unix.LoopInfo64) { value.File_name[0] ^= 1 },
	} {
		changed := original
		change(&changed)
		if originalAssociation(image, 3, &changed) != nil {
			t.Fatal("changed or unidentified association was allowed to wait", changed)
		}
	}
}
