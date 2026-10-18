package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

// The privileged capturer creates raw images as its worker UID, while Sysbox
// consumes private images as mapped root. Admit only that creator or the
// destination owner, preflight every entry, then update metadata through the
// original opened inodes. The readonly parent is never followed or changed.
func descendantOwnStreamCapture(head string, owner descendantLeaseIdentity) error {
	type entry struct {
		path string
		info os.FileInfo
	}
	var files, directories []entry
	var parent *entry
	streamRecord := false
	admitted := func(info os.FileInfo) bool {
		id := descendantLeaseFileIdentity(info)
		return (id.UID == uint32(os.Geteuid()) && id.GID == uint32(os.Getegid())) ||
			(id.UID == owner.UID && id.GID == owner.GID)
	}
	for _, directory := range []string{head, head + "/original-writer"} {
		info, err := os.Lstat(directory)
		if err != nil || !info.IsDir() || !admitted(info) {
			return fmt.Errorf("raw stream directory has a foreign creating identity: %v", err)
		}
		directories = append(directories, entry{directory, info})
		entries, err := os.ReadDir(directory)
		if err != nil || len(entries) > 8192 {
			return fmt.Errorf("raw stream directory population is outside its bound: %v", err)
		}
		for _, item := range entries {
			path := filepath.Join(directory, item.Name())
			info, err := os.Lstat(path)
			if err != nil || !admitted(info) {
				return fmt.Errorf("raw stream entry has a foreign creating identity: %v", err)
			}
			if directory == head && item.Name() == "original-writer" && info.IsDir() {
				continue
			}
			if directory == head && item.Name() == "parent" && info.Mode()&os.ModeSymlink != 0 {
				link, err := os.Readlink(path)
				if err != nil || filepath.IsAbs(link) {
					return fmt.Errorf("raw stream parent is not a relative private link: %v", err)
				}
				parent = &entry{path, info}
				continue
			}
			id := descendantLeaseFileIdentity(info)
			if !info.Mode().IsRegular() || id.Links != 1 {
				return fmt.Errorf("raw stream output is not one independently created regular inode")
			}
			files = append(files, entry{path, info})
			streamRecord = streamRecord || (directory == head &&
				strings.HasPrefix(item.Name(), "lazy-stream-") && strings.HasSuffix(item.Name(), ".bin"))
		}
	}
	if !streamRecord {
		return fmt.Errorf("raw stream output has no completion records")
	}
	for _, item := range append(directories, files...) {
		file, err := os.OpenFile(item.path, os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
		if err != nil {
			return err
		}
		current, statErr := file.Stat()
		if statErr != nil || !os.SameFile(current, item.info) || !admitted(current) {
			file.Close()
			return fmt.Errorf("raw stream inode changed before mapped ownership: %v", statErr)
		}
		mode := os.FileMode(0600)
		if current.IsDir() {
			mode = 0700
		}
		err = errors.Join(file.Chown(int(owner.UID), int(owner.GID)), file.Chmod(mode), file.Sync(), file.Close())
		if err != nil {
			return err
		}
	}
	if parent != nil {
		current, err := os.Lstat(parent.path)
		if err != nil || !os.SameFile(current, parent.info) {
			return fmt.Errorf("raw stream parent link changed before mapped ownership: %v", err)
		}
		return os.Lchown(parent.path, int(owner.UID), int(owner.GID))
	}
	return nil
}
