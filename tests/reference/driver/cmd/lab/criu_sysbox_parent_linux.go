package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

var criuParentBindings []string

func criuSysboxParentEntries(images string) ([]os.DirEntry, error) {
	entries, err := criuRAMImageEntries(images, criuSysboxInheritEnabled())
	if err != nil || !criuSysboxInheritEnabled() {
		return entries, err
	}
	filtered := entries[:0]
	for _, entry := range entries {
		if criuInheritParentEntry(entry.Name()) {
			filtered = append(filtered, entry)
		}
	}
	return filtered, nil
}

func criuParentReadonlyBind(source, target string) (resultErr error) {
	if err := unix.Mount(source, target, "", unix.MS_BIND, ""); err != nil {
		return err
	}
	defer func() {
		if resultErr != nil {
			resultErr = errors.Join(resultErr, unix.Unmount(target, 0))
		}
	}()
	if err := unix.Mount("", target, "", unix.MS_BIND|unix.MS_REMOUNT|unix.MS_RDONLY|unix.MS_NOSUID|unix.MS_NODEV|unix.MS_NOEXEC, ""); err != nil {
		return err
	}
	file, err := os.OpenFile(filepath.Join(target, "inventory.img"), os.O_WRONLY, 0)
	if file != nil {
		file.Close()
	}
	if !errors.Is(err, syscall.EROFS) {
		return fmt.Errorf("immutable parent inventory accepted writes: %v", err)
	}
	return nil
}

func criuSysboxPinParent(images string) (resultErr error) {
	if !criuSysboxUsesParent() {
		return nil
	}
	start := time.Now()
	output := map[string]any{"images": images, "manifest": criuRoot + "/incremental-base.json", "scope": "read-only complete parent pages prepared before live fork; preparatory cost reported separately"}
	defer func() { record("sysbox-incremental-base-pinned", start, asJSON(output), resultErr) }()
	if criuRoot != "/experiment" || images != criuRoot+"/sysbox/images-base" ||
		(!criuSysboxInheritEnabled() && os.Getenv("CRIU_SYSBOX_ASYNC_TRACK") != "1") {
		return fmt.Errorf("parent image is outside the owned incremental experiment")
	}
	manifest := map[string]string{}
	entries, err := criuSysboxParentEntries(images)
	if err != nil {
		return err
	}
	var reused, rehashed int
	var rehashedBytes int64
	if criuParentManifestReuseEnabled() {
		var fs unix.Statfs_t
		if err := unix.Statfs(images, &fs); err != nil || fs.Flags&unix.ST_RDONLY == 0 {
			return fmt.Errorf("cached parent payload is not pinned readonly: %v", err)
		}
	}
	for _, entry := range entries {
		path := filepath.Join(images, entry.Name())
		var digest string
		var hit bool
		var err error
		if criuParentManifestReuseEnabled() {
			digest, hit, err = criuVerifiedParentDigest(path)
			if err != nil {
				return err
			}
		}
		if hit {
			reused++
		} else {
			digest, err = criuRAMHashFile(path)
			rehashed++
			if info, statErr := entry.Info(); statErr == nil {
				rehashedBytes += info.Size()
			}
		}
		if err != nil {
			return err
		}
		manifest[entry.Name()] = digest
	}
	if len(manifest) == 0 {
		return fmt.Errorf("incremental parent is empty")
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	// Save identity before binding immutable pages into either branch.
	if err := os.WriteFile(criuRoot+"/incremental-base.json", append(data, '\n'), 0600); err != nil {
		return err
	}
	if !criuSysboxInheritEnabled() {
		if err := criuParentReadonlyBind(images, images); err != nil {
			return err
		}
		criuParentBindings = append(criuParentBindings, images)
	}
	output["files_verified"] = len(manifest)
	output["verified_digests_reused"] = reused
	output["files_rehashed"] = rehashed
	output["bytes_rehashed"] = rehashedBytes
	output["manifest_reuse_enabled"] = criuParentManifestReuseEnabled()
	output["cached_files"] = len(criuVerifiedParentFiles)
	output["cache_miss_fields"] = criuVerifiedParentMisses
	output["cache_first_miss"] = criuVerifiedParentFirstMiss
	return nil
}

func criuSysboxBindCopyParent() error {
	if !criuSysboxUsesParent() {
		return nil
	}
	source := criuRoot + "/sysbox/images-base"
	target := criuRoot + "/sysbox-copy/images-base"
	if err := os.Mkdir(target, 0700); err != nil {
		return err
	}
	if err := criuParentReadonlyBind(source, target); err != nil {
		return err
	}
	criuParentBindings = append(criuParentBindings, target)
	if os.Getenv("RUNTIME_FORK_RICH_EPOCH_CHAIN") == "1" {
		return criuEpochChainBindParent()
	}
	return nil
}

// Every other special file stays forbidden. The one accepted link must refer
// to the same immutable directory in a fixed sibling location for each branch.
func criuSysboxParentLink(images string) error {
	if os.Getenv("RUNTIME_FORK_RICH_EPOCH_CHAIN") == "1" {
		return criuEpochChainParentLink(images)
	}
	if !criuSysboxUsesParent() || !criuParentImageLocation(criuRoot, images) {
		return fmt.Errorf("parent link is outside the owned image chain")
	}
	target, err := os.Readlink(filepath.Join(images, "parent"))
	if err != nil || target != "../images-base" {
		return fmt.Errorf("unexpected parent image link: %q %v", target, err)
	}
	base := filepath.Join(filepath.Dir(images), "images-base")
	info, err := os.Lstat(base)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("invalid parent image directory: %v", err)
	}
	source, err := os.Stat(criuRoot + "/sysbox/images-base")
	if err != nil || !os.SameFile(source, info) {
		return fmt.Errorf("branch parent does not share the pinned base inode: %v", err)
	}
	var fs unix.Statfs_t
	if err := unix.Statfs(base, &fs); err != nil || fs.Flags&unix.ST_RDONLY == 0 {
		return fmt.Errorf("parent image directory is writable: %v", err)
	}
	return nil
}

func criuSysboxParentGuardProof(images string) (resultErr error) {
	if !criuSysboxUsesParent() {
		return nil
	}
	start := time.Now()
	defer func() {
		record("sysbox-incremental-parent-guards", start, "external and noncanonical parent links rejected; readonly base rejects write/create/unlink/rename", resultErr)
	}()
	if images != criuRoot+"/sysbox/images-again" {
		return fmt.Errorf("parent guard fixture is outside the owned capture")
	}
	link := filepath.Join(images, "parent")
	for _, target := range []string{"/etc", "../../../../etc", "../images-base/../images-base", "../images-base/inventory.img"} {
		if err := os.Symlink(target, link); err != nil {
			return err
		}
		guardErr := criuSysboxParentLink(images)
		if err := os.Remove(link); err != nil {
			return err
		}
		if guardErr == nil {
			return fmt.Errorf("accepted unsafe parent link %q", target)
		}
	}
	if err := os.Symlink("../images-base", link); err != nil {
		return err
	}
	guardErr := criuSysboxParentLink(images)
	removeErr := os.Remove(link)
	if guardErr != nil || removeErr != nil {
		return errors.Join(guardErr, removeErr)
	}
	base := criuRoot + "/sysbox/images-base"
	for _, path := range []string{base + "/inventory.img", base + "/forbidden-write-probe"} {
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE, 0600)
		if file != nil {
			file.Close()
		}
		if !errors.Is(err, syscall.EROFS) {
			return fmt.Errorf("base did not reject write/create at %s: %v", path, err)
		}
	}
	if err := os.Remove(base + "/inventory.img"); !errors.Is(err, syscall.EROFS) {
		return fmt.Errorf("base did not reject unlink: %v", err)
	}
	if err := os.Rename(base+"/inventory.img", base+"/forbidden-rename-probe"); !errors.Is(err, syscall.EROFS) {
		return fmt.Errorf("base did not reject rename: %v", err)
	}
	return nil
}

func criuSysboxParentManifestProof() (resultErr error) {
	if !criuSysboxUsesParent() {
		return nil
	}
	start := time.Now()
	output := map[string]any{"scope": "immutable base hashes unchanged after delta dumps and child restore"}
	defer func() { record("sysbox-incremental-base-unchanged", start, asJSON(output), resultErr) }()
	data, err := os.ReadFile(criuRoot + "/incremental-base.json")
	if err != nil {
		return err
	}
	var manifest map[string]string
	if err := json.Unmarshal(data, &manifest); err != nil || len(manifest) == 0 {
		return fmt.Errorf("invalid parent manifest: %v", err)
	}
	base := criuRoot + "/sysbox/images-base"
	entries, err := criuSysboxParentEntries(base)
	if err != nil || len(entries) != len(manifest) {
		return fmt.Errorf("parent directory entries changed: %v", err)
	}
	for _, entry := range entries {
		digest, err := criuRAMHashFile(filepath.Join(base, entry.Name()))
		if err != nil || digest != manifest[entry.Name()] {
			return fmt.Errorf("parent bytes changed for %s: %v", entry.Name(), err)
		}
	}
	output["verified_files"] = len(manifest)
	return nil
}

func criuSysboxExportParentLink(source, destination string) (criuRAMFile, error) {
	if os.Getenv("RUNTIME_FORK_RICH_EPOCH_CHAIN") == "1" {
		return criuEpochChainExportLink(source, destination)
	}
	if err := criuSysboxParentLink(filepath.Dir(source)); err != nil {
		return criuRAMFile{}, err
	}
	if err := os.Symlink("../images-base", destination); err != nil {
		return criuRAMFile{}, err
	}
	if err := criuSysboxParentLink(filepath.Dir(destination)); err != nil {
		return criuRAMFile{}, err
	}
	return criuRAMFile{Name: "parent", Link: "../images-base"}, nil
}

func criuSysboxParentCleanup() error {
	var result error
	for i := len(criuParentBindings) - 1; i >= 0; i-- {
		result = errors.Join(result, unix.Unmount(criuParentBindings[i], 0))
	}
	criuParentBindings = nil
	return result
}
