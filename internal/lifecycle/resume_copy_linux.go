//go:build linux

package lifecycle

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

type resumeCopyEntry struct {
	file    CheckpointFile
	source  unix.Stat_t
	target  *unix.Stat_t
	missing uint64
}

func resumeCopyOpen(root *os.File, path string, flags int, mode uint32) (*os.File, unix.Stat_t, error) {
	var stat unix.Stat_t
	fd, err := unix.Openat2(int(root.Fd()), path, &unix.OpenHow{
		Flags: uint64(flags | unix.O_NOFOLLOW | unix.O_CLOEXEC),
		Mode:  uint64(mode),
		Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS |
			unix.RESOLVE_NO_XDEV,
	})
	if err != nil {
		return nil, stat, err
	}
	file := os.NewFile(uintptr(fd), path)
	kind := uint32(unix.S_IFREG)
	if flags&unix.O_DIRECTORY != 0 {
		kind = unix.S_IFDIR
	}
	if err := unix.Fstat(fd, &stat); err != nil ||
		stat.Mode&unix.S_IFMT != kind || (kind == unix.S_IFREG && stat.Nlink != 1) {
		file.Close()
		return nil, stat, errors.Join(err, fmt.Errorf("Resume copy has an invalid file type or link ownership"))
	}
	return file, stat, nil
}

func sameResumeCopyDirectory(before, after unix.Stat_t) bool {
	return before.Dev == after.Dev && before.Ino == after.Ino &&
		before.Mode == after.Mode && before.Uid == after.Uid && before.Gid == after.Gid
}

func sameResumeCopyFile(before, after unix.Stat_t) bool {
	return before.Dev == after.Dev && before.Ino == after.Ino &&
		before.Mode == after.Mode && before.Uid == after.Uid && before.Gid == after.Gid &&
		before.Size == after.Size && before.Nlink == 1 && after.Nlink == 1 &&
		before.Mtim == after.Mtim && before.Ctim == after.Ctim
}

func resumeCopyPrefix(ctx context.Context, source, target *os.File, length int64) error {
	left, right := sha256.New(), sha256.New()
	for _, entry := range []struct {
		file *os.File
		hash io.Writer
	}{{source, left}, {target, right}} {
		if _, err := io.Copy(entry.hash, checkpointReader{ctx, io.NewSectionReader(entry.file, 0, length)}); err != nil {
			return err
		}
	}
	if !bytes.Equal(left.Sum(nil), right.Sum(nil)) {
		return fmt.Errorf("incomplete Resume copy differs from its original checkpoint prefix")
	}
	return nil
}

// Before a native restore is attempted, a failed cp can leave exact prefixes
// of the immutable image set. Complete only those prefixes. Never truncate,
// overwrite, remove or adopt damaged, linked, mounted or unexpected backing.
// The caller holds the native operation lock and has verified the checkpoint,
// original Resume intent and empty original cgroup before reaching this stage.
func (e *Engine) continueResumeBacking(ctx context.Context, record Record, generation, relative string, saved []CheckpointFile) error {
	if record.Phase != "resuming" || record.Pause == nil || record.Pause.Resume == nil ||
		record.Pause.CompletedAt == "" || record.Pause.Resume.Attempted ||
		record.Pause.Resume.CompletedAt != "" ||
		relative != "objects/"+record.ID+"/resumes/"+record.Pause.SnapshotID {
		return fmt.Errorf("Resume copy continuation requires its unattempted original preparation")
	}
	for _, name := range []string{"resume-prepared.json", "resume-held.json", "resume-result.json"} {
		if _, err := e.root.Lstat(relative + "/" + name); !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("Resume copy has a prepared or attempted successor: %s: %v", name, err)
		}
	}
	root, err := e.root.Open(".")
	if err != nil {
		return err
	}
	defer root.Close()
	allowed := map[string]bool{}
	for _, file := range saved {
		switch file.Name {
		case "criu.conf", "filesystem.json", "source-state.json", "sysbox-state.json":
		default:
			if !strings.HasPrefix(file.Name, "images/") ||
				file.Name != "images/"+filepath.Base(file.Name) {
				return fmt.Errorf("Resume checkpoint has an unexpected private input")
			}
			allowed[filepath.Base(file.Name)] = true
		}
	}
	if len(allowed) == 0 {
		return fmt.Errorf("Resume copy has no complete immutable image inventory")
	}
	images := relative + "/images"
	directoryFlags := unix.O_RDONLY | unix.O_DIRECTORY | unix.O_NOATIME
	sourceDirectory, sourceDirectoryStat, err := resumeCopyOpen(root, generation+"/images", directoryFlags, 0)
	if err != nil {
		return err
	}
	defer sourceDirectory.Close()
	directory, directoryStat, err := resumeCopyOpen(root, images, directoryFlags, 0)
	if errors.Is(err, os.ErrNotExist) {
		directory = nil
	} else if err != nil {
		return err
	} else {
		defer directory.Close()
		initial := directoryStat.Uid == 0 && directoryStat.Gid == 0 && directoryStat.Mode&07777 == 0700
		copied := directoryStat.Uid == sourceDirectoryStat.Uid && directoryStat.Gid == sourceDirectoryStat.Gid &&
			directoryStat.Mode == sourceDirectoryStat.Mode
		if !initial && !copied {
			return fmt.Errorf("incomplete Resume image directory has foreign ownership or mode")
		}
		entries, readErr := directory.ReadDir(-1)
		if readErr != nil {
			return readErr
		}
		for _, entry := range entries {
			if !allowed[entry.Name()] || entry.Type()&os.ModeType != 0 {
				return fmt.Errorf("incomplete Resume images contain an unexpected entry")
			}
		}
	}
	plan := make([]resumeCopyEntry, 0, len(saved))
	var missing uint64
	for _, expected := range saved {
		if err := ctx.Err(); err != nil {
			return err
		}
		source, sourceStat, err := resumeCopyOpen(root, generation+"/"+expected.Name, unix.O_RDONLY|unix.O_NOATIME, 0)
		if err != nil {
			return err
		}
		if sourceStat.Size < 0 || sourceStat.Size != expected.Bytes {
			source.Close()
			return fmt.Errorf("Resume checkpoint file size changed")
		}
		entry := resumeCopyEntry{file: expected, source: sourceStat, missing: uint64(expected.Bytes)}
		target, targetStat, targetErr := resumeCopyOpen(root, relative+"/"+expected.Name, unix.O_RDONLY|unix.O_NOATIME, 0)
		if targetErr == nil {
			copiedOwner := targetStat.Uid == sourceStat.Uid && targetStat.Gid == sourceStat.Gid
			initialOwner := targetStat.Uid == 0 && targetStat.Gid == 0
			if targetStat.Size < 0 || targetStat.Size > sourceStat.Size ||
				targetStat.Mode != sourceStat.Mode || (!copiedOwner && !initialOwner) {
				target.Close()
				source.Close()
				return fmt.Errorf("incomplete Resume copy has changed size, owner or mode")
			}
			prefixErr := resumeCopyPrefix(ctx, source, target, targetStat.Size)
			entry.target, entry.missing = &targetStat, uint64(sourceStat.Size-targetStat.Size)
			if err := errors.Join(prefixErr, target.Close()); err != nil {
				source.Close()
				return err
			}
		} else if !errors.Is(targetErr, os.ErrNotExist) {
			source.Close()
			return targetErr
		}
		if err := source.Close(); err != nil {
			return err
		}
		if entry.missing > math.MaxUint64-missing {
			return fmt.Errorf("Resume copy allocation overflows")
		}
		missing += entry.missing
		plan = append(plan, entry)
	}
	if err := e.admitStorageAllocation(missing); err != nil {
		return err
	}
	if directory == nil {
		if err := e.root.Mkdir(images, 0700); err != nil {
			return err
		}
		directory, directoryStat, err = resumeCopyOpen(root, images, directoryFlags, 0)
		if err != nil {
			return err
		}
		defer directory.Close()
	}
	for _, entry := range plan {
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.missing == 0 && entry.target != nil &&
			entry.target.Uid == entry.source.Uid && entry.target.Gid == entry.source.Gid {
			continue
		}
		source, sourceStat, err := resumeCopyOpen(root, generation+"/"+entry.file.Name, unix.O_RDONLY|unix.O_NOATIME, 0)
		if err != nil {
			return err
		}
		if !sameResumeCopyFile(entry.source, sourceStat) {
			source.Close()
			return fmt.Errorf("Resume copy source changed before continuation")
		}
		flags, mode := unix.O_WRONLY|unix.O_APPEND, uint32(0)
		if entry.target == nil {
			flags |= unix.O_CREAT | unix.O_EXCL
			mode = sourceStat.Mode & 0777
		}
		target, targetStat, err := resumeCopyOpen(root, relative+"/"+entry.file.Name, flags, mode)
		if err != nil {
			source.Close()
			return err
		}
		if entry.target != nil && !sameResumeCopyFile(*entry.target, targetStat) {
			target.Close()
			source.Close()
			return fmt.Errorf("Resume copy target changed before continuation")
		}
		// ENOSPC can interrupt cp before its final chown, even for an
		// already complete file. Only an independently verified prefix
		// with the source mode and initial root owner reaches this step.
		if entry.target == nil || targetStat.Uid != sourceStat.Uid || targetStat.Gid != sourceStat.Gid {
			if err := target.Chown(int(sourceStat.Uid), int(sourceStat.Gid)); err != nil {
				target.Close()
				source.Close()
				return err
			}
			if err := unix.Fchmod(int(target.Fd()), sourceStat.Mode&07777); err != nil {
				target.Close()
				source.Close()
				return err
			}
		}
		offset := sourceStat.Size - int64(entry.missing)
		_, copyErr := io.Copy(target, checkpointReader{ctx, io.NewSectionReader(source, offset, int64(entry.missing))})
		syncErr := target.Sync()
		if err := errors.Join(copyErr, syncErr, target.Close(), source.Close()); err != nil {
			return err
		}
	}
	current, currentStat, err := resumeCopyOpen(root, images, directoryFlags, 0)
	if err != nil {
		return err
	}
	if err := current.Close(); err != nil {
		return err
	}
	if !sameResumeCopyDirectory(directoryStat, currentStat) {
		return fmt.Errorf("Resume image directory changed during continuation")
	}
	// cp -a applies the source directory's mapped owner after its contents.
	// An interrupted copy may not reach that step. Keep this same inode and
	// complete only the original image directory's required restore access.
	if err := directory.Chown(int(sourceDirectoryStat.Uid), int(sourceDirectoryStat.Gid)); err != nil {
		return err
	}
	if err := unix.Fchmod(int(directory.Fd()), sourceDirectoryStat.Mode&07777); err != nil {
		return err
	}
	return directory.Sync()
}
