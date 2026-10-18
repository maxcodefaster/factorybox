package main

import (
	"encoding/hex"
	"fmt"
	"path/filepath"

	"golang.org/x/sys/unix"
)

type criuVerifiedParentIdentity struct {
	Device, Inode, Links uint64
	Size                 int64
	Mode, UID, GID       uint32
	Modified, Changed    unix.Timespec
}

type criuVerifiedParentFile struct {
	Identity criuVerifiedParentIdentity
	SHA256   string
}

var criuVerifiedParentFiles map[string]criuVerifiedParentFile
var criuVerifiedParentMisses = map[string]int{}
var criuVerifiedParentFirstMiss any

func criuParentFileIdentity(path string) (criuVerifiedParentIdentity, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return criuVerifiedParentIdentity{}, err
	}
	defer unix.Close(fd)
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		return criuVerifiedParentIdentity{}, err
	}
	if st.Mode&unix.S_IFMT != unix.S_IFREG {
		return criuVerifiedParentIdentity{}, fmt.Errorf("parent cache requires a regular file")
	}
	return criuVerifiedParentIdentity{
		Device: uint64(st.Dev), Inode: st.Ino, Links: uint64(st.Nlink), Size: st.Size,
		Mode: st.Mode, UID: st.Uid, GID: st.Gid, Modified: st.Mtim, Changed: st.Ctim,
	}, nil
}

// Called only after the complete bytes have verified, synced disk backing.
// atime is deliberately excluded: reads are not writes. ctime, inode, links,
// ownership and mode detect replacement and same-size writes with reset mtime.
func criuRegisterVerifiedParentFiles(directory string, manifest map[string]criuRAMFile) error {
	files := map[string]criuVerifiedParentFile{}
	for name, entry := range manifest {
		if filepath.Base(name) != name || name != entry.Name || entry.Link != "" {
			return fmt.Errorf("invalid verified parent entry %q", name)
		}
		if !criuInheritParentEntry(name) {
			continue
		}
		hash, err := hex.DecodeString(entry.SHA256)
		if err != nil || len(hash) != 32 {
			return fmt.Errorf("invalid verified parent digest %q", name)
		}
		identity, err := criuParentFileIdentity(filepath.Join(directory, name))
		if err != nil {
			return err
		}
		if identity.Size != entry.Bytes || identity.UID != entry.UID || identity.GID != entry.GID ||
			identity.Mode&07777 != entry.Mode {
			return fmt.Errorf("verified parent metadata changed for %q", name)
		}
		files[name] = criuVerifiedParentFile{Identity: identity, SHA256: entry.SHA256}
	}
	if len(files) == 0 {
		return fmt.Errorf("verified parent has no image payloads")
	}
	criuVerifiedParentFiles = files
	return nil
}

func criuVerifiedParentDigest(path string) (string, bool, error) {
	file, ok := criuVerifiedParentFiles[filepath.Base(path)]
	if !ok {
		criuVerifiedParentMisses["missing_name"]++
		return "", false, nil
	}
	identity, err := criuParentFileIdentity(path)
	if err != nil {
		return "", false, err
	}
	if identity != file.Identity {
		for key, changed := range map[string]bool{
			"device": identity.Device != file.Identity.Device, "inode": identity.Inode != file.Identity.Inode,
			"links": identity.Links != file.Identity.Links, "size": identity.Size != file.Identity.Size,
			"mode": identity.Mode != file.Identity.Mode, "uid": identity.UID != file.Identity.UID,
			"gid": identity.GID != file.Identity.GID, "mtime": identity.Modified != file.Identity.Modified,
			"ctime": identity.Changed != file.Identity.Changed,
		} {
			if changed {
				criuVerifiedParentMisses[key]++
			}
		}
		if criuVerifiedParentFirstMiss == nil {
			criuVerifiedParentFirstMiss = map[string]any{"name": filepath.Base(path), "expected": file.Identity, "actual": identity}
		}
		return "", false, nil
	}
	return file.SHA256, true, nil
}
