package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

type epochVerificationResult struct {
	ReadBytes   int64         `json:"read_bytes"`
	KernelFiles int           `json:"kernel_certified_files"`
	KernelBytes int64         `json:"kernel_certified_bytes"`
	Files       int           `json:"verified_files"`
	Bytes       int64         `json:"verified_bytes"`
	Elapsed     time.Duration `json:"-"`
	FinishedNS  int64         `json:"finished_unix_ns"`
	Err         error         `json:"-"`
}

// One fixed-capacity job per second fork. Cancellation is cooperative between
// bounded local-file reads. Every caller waits for the actual worker exit,
// including failure/timeout paths, before snapshot storage may be retired.
type epochVerificationJob struct {
	cancel context.CancelFunc
	done   chan epochVerificationResult
	once   sync.Once
	result epochVerificationResult
}

func startEpochVerification(parent context.Context, parallel bool,
	check func(context.Context) epochVerificationResult) *epochVerificationJob {
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	job := &epochVerificationJob{cancel: cancel, done: make(chan epochVerificationResult, 1)}
	run := func() {
		start := time.Now()
		result := check(ctx)
		result.Elapsed = time.Since(start)
		result.FinishedNS = time.Now().UnixNano()
		job.done <- result
	}
	if parallel {
		go run()
	} else {
		run()
	}
	return job
}

func (job *epochVerificationJob) join(cancel bool) epochVerificationResult {
	if cancel {
		job.cancel()
	}
	job.once.Do(func() {
		job.result = <-job.done
		job.cancel()
	})
	return job.result
}

func checkEpochParentFiles(ctx context.Context, directory string, expected map[string]string,
	byteLimit int64) (result epochVerificationResult) {
	if byteLimit <= 0 || byteLimit > int64(criuRAMSnapshotLimit) || len(expected) == 0 || len(expected) > 8192 {
		result.Err = fmt.Errorf("parent verification exceeds its fixed bounds")
		return
	}
	info, err := os.Lstat(directory)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		result.Err = fmt.Errorf("parent verifier directory is not private/real: %v", err)
		return
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		result.Err = err
		return
	}
	if len(entries) > 8192 {
		result.Err = fmt.Errorf("too many parent entries")
		return
	}
	committed, err := beginCommittedVerification(directory)
	if err != nil {
		result.Err = err
		return
	}
	defer func() { result.Err = errors.Join(result.Err, endCommittedVerification(committed)) }()
	observed := make(map[string]string, len(expected))
	buffer := make([]byte, 64<<10)
	for _, entry := range entries {
		if result.Err = ctx.Err(); result.Err != nil {
			return
		}
		name := entry.Name()
		path := filepath.Join(directory, name)
		info, err := os.Lstat(path)
		if err != nil {
			result.Err = err
			return
		}
		if name == "parent" && info.Mode()&os.ModeSymlink != 0 {
			link, err := os.Readlink(path)
			if err != nil || link != "../images-base" || expected[name] != "link:"+link {
				result.Err = fmt.Errorf("parent directory link changed: %q %v", link, err)
				return
			}
			observed[name] = "link:" + link
			continue
		}
		if !info.Mode().IsRegular() {
			result.Err = fmt.Errorf("non-regular parent entry %q", name)
			return
		}
		if !criuInheritParentEntry(name) {
			continue
		}
		if expected[name] == "" || info.Size() < 0 || info.Size() > byteLimit-result.Bytes {
			result.Err = fmt.Errorf("unrecognized or oversized immutable parent file %q", name)
			return
		}
		file, err := os.OpenFile(path, os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
		if err != nil {
			result.Err = err
			return
		}
		before, err := file.Stat()
		if err != nil || !before.Mode().IsRegular() || !os.SameFile(info, before) || before.Size() != info.Size() {
			file.Close()
			result.Err = fmt.Errorf("parent file identity changed before read %q: %v", name, err)
			return
		}
		certified, certErr := verifyCommittedPage(committed, name, expected[name], before)
		if certErr != nil {
			file.Close()
			result.Err = certErr
			return
		}
		if certified {
			after, statErr := file.Stat()
			closeErr := file.Close()
			current, pathErr := os.Lstat(path)
			if statErr != nil || closeErr != nil || pathErr != nil {
				result.Err = errors.Join(statErr, closeErr, pathErr)
				return
			}
			if !committedSameIdentity(before, after) || !committedSameIdentity(after, current) {
				result.Err = fmt.Errorf("committed page path changed during proof %q", name)
				return
			}
			result.Bytes += before.Size()
			result.KernelBytes += before.Size()
			result.Files++
			result.KernelFiles++
			observed[name] = expected[name]
			continue
		}
		logical, release, openErr := criuLogicalImage(file)
		if openErr != nil {
			file.Close()
			result.Err = openErr
			return
		}
		hash := sha256.New()
		readBytes := int64(0)
		for {
			if err = ctx.Err(); err != nil {
				break
			}
			var count int
			count, err = logical.Read(buffer)
			if count > 0 {
				readBytes += int64(count)
				if readBytes > before.Size() || readBytes > byteLimit-result.Bytes {
					err = fmt.Errorf("parent file grew beyond its captured bound")
					break
				}
				_, _ = hash.Write(buffer[:count])
			}
			if count == 0 && err == nil {
				err = io.ErrNoProgress
			}
			if err != nil {
				if errors.Is(err, io.EOF) {
					err = nil
				}
				break
			}
		}
		after, statErr := file.Stat()
		closeErr := errors.Join(release(), file.Close())
		current, pathErr := os.Lstat(path)
		if err != nil || statErr != nil || closeErr != nil || pathErr != nil {
			result.Err = errors.Join(err, statErr, closeErr, pathErr)
			return
		}
		if !os.SameFile(before, after) || !os.SameFile(after, current) ||
			after.Size() != before.Size() || readBytes != before.Size() ||
			!after.ModTime().Equal(before.ModTime()) || !current.ModTime().Equal(before.ModTime()) {
			result.Err = fmt.Errorf("parent file changed during verification %q", name)
			return
		}
		digest := hex.EncodeToString(hash.Sum(nil))
		if digest != expected[name] {
			result.Err = fmt.Errorf("immutable parent hash changed for %q", name)
			return
		}
		result.Bytes += readBytes
		result.ReadBytes += readBytes
		result.Files++
		observed[name] = digest
	}
	if len(observed) != len(expected) {
		result.Err = fmt.Errorf("parent manifest entry set changed")
		return
	}
	for name, digest := range expected {
		if filepath.Base(name) != name || strings.ContainsRune(name, 0) || observed[name] != digest {
			result.Err = fmt.Errorf("parent manifest differs for %q", name)
			return
		}
	}
	return
}
