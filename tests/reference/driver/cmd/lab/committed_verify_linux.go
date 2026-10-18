//go:build linux

package main

import (
	"fmt"
	"os"
	"time"

	"golang.org/x/sys/unix"
)

type committedVerification struct {
	Path  string
	Owner *committedSnapshot
}

func committedSameIdentity(a, b os.FileInfo) bool {
	return committedIdentity(a) == committedIdentity(b)
}

func checkCommittedVerification(session *committedVerification) error {
	if session == nil {
		return nil
	}
	owner := session.Owner
	id, err := committedCurrentMount(session.Path, owner.Directory, owner.MountID)
	if err != nil || id != owner.MountID {
		return fmt.Errorf("committed verification mount changed: %v", err)
	}
	info, err := owner.Directory.Stat()
	if err != nil {
		return err
	}
	current := committedIdentity(info)
	if current.Device != owner.Identity.Device || current.Inode != owner.Identity.Inode ||
		current.Mode != owner.Identity.Mode || current.UID != owner.Identity.UID ||
		current.GID != owner.Identity.GID {
		return fmt.Errorf("committed directory ownership/identity differs")
	}
	return nil
}

func beginCommittedVerification(path string) (*committedVerification, error) {
	enabled, err := committedSnapshotMode()
	if err != nil || !enabled {
		return nil, err
	}
	if path != "/experiment/sysbox/images-again" {
		return nil, fmt.Errorf("committed verification outside owned parent")
	}
	owner := committedSnapshots[path]
	if owner == nil {
		if criuRAMSnapshotFind(path) != nil {
			return nil, fmt.Errorf("live RAM parent lost its certificate")
		}
		var fs unix.Statfs_t
		if err := unix.Statfs(path, &fs); err != nil || fs.Type == unix.TMPFS_MAGIC {
			return nil, fmt.Errorf("unregistered parent is not verified file backing: %v", err)
		}
		record("sysbox-committed-file-backed-full-hash-fallback", time.Now(),
			asJSON(map[string]any{"images": path, "filesystem": fs.Type,
				"scope": "old RAM certificate absent after release; actual full reads/hashes required for file backing"}), nil)
		return nil, nil
	}
	if len(owner.Pages) == 0 {
		return nil, fmt.Errorf("no published page identity anchors for this parent")
	}
	session := &committedVerification{path, owner}
	if err := checkCommittedVerification(session); err != nil {
		return nil, err
	}
	return session, nil
}

func endCommittedVerification(session *committedVerification) error {
	return checkCommittedVerification(session)
}

func verifyCommittedPage(session *committedVerification, name, digest string, info os.FileInfo) (bool, error) {
	if session == nil || !criuRAMPageName.MatchString(name) {
		return false, nil
	}
	page, ok := session.Owner.Pages[name]
	if !ok || digest != page.Digest || committedIdentity(info) != page.Identity {
		return false, fmt.Errorf("committed page expectation/inode differs: %s", name)
	}
	anchor, err := page.File.Stat()
	if err != nil || committedIdentity(anchor) != page.Identity {
		return false, fmt.Errorf("committed page anchor changed: %s: %v", name, err)
	}
	// Both the held original inode and this exact opened file retain the
	// committed identity. The owned superblock is checked read-only at both
	// ends of verification. Initial digest was read after publication.
	return true, nil
}
