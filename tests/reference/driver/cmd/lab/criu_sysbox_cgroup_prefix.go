package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"time"

	"golang.org/x/sys/unix"
)

func criuCgroupPrefixEnabled() bool {
	return os.Getenv("CRIU_SYSBOX_CGROUP_PREFIX_REUSE") == "1"
}

func criuCgroupPrefixGuard(root, id string) error {
	switch os.Getenv("CRIU_SYSBOX_CGROUP_PREFIX_REUSE") {
	case "", "0":
		return nil
	case "1":
	default:
		return fmt.Errorf("CRIU_SYSBOX_CGROUP_PREFIX_REUSE must be 0 or 1")
	}
	if root != "/experiment" || id != "source-sysbox" {
		return fmt.Errorf("cgroup prefix reuse requires the owned source")
	}
	for _, name := range []string{"CRIU_SYSBOX_PROBE", "CRIU_SYSBOX_RAM_CAPTURE",
		"CRIU_SYSBOX_RETAIN_SOURCE", "CRIU_SYSBOX_LAZY_INHERIT"} {
		if os.Getenv(name) != "1" {
			return fmt.Errorf("cgroup prefix reuse requires %s=1", name)
		}
	}
	return nil
}

var criuCgroupPrefixLine = regexp.MustCompile(`RUNTIME_FORK_CGROUP_PREFIX_REUSE normalized=([0-9]+) reused=([0-9]+) sets=([0-9]+)\n`)

type criuCgroupPrefixResult struct {
	Normalized uint64 `json:"normalized"`
	Reused     uint64 `json:"reused"`
	Sets       uint64 `json:"sets"`
}

func criuParseCgroupPrefixProof(text string) (result criuCgroupPrefixResult, resultErr error) {
	matches := criuCgroupPrefixLine.FindAllStringSubmatch(text, -1)
	if len(matches) != 1 {
		return result, fmt.Errorf("expected one native cgroup prefix summary")
	}
	values := []*uint64{&result.Normalized, &result.Reused, &result.Sets}
	for index, target := range values {
		value, err := strconv.ParseUint(matches[0][index+1], 10, 32)
		if err != nil {
			return result, err
		}
		*target = value
	}
	if result.Normalized == 0 || result.Reused == 0 || result.Reused > result.Normalized || result.Sets < 2 {
		return result, fmt.Errorf("native capture did not prove actual cgroup identity reuse")
	}
	return result, nil
}

func criuSysboxCgroupPrefixProof(images string) (resultErr error) {
	if !criuCgroupPrefixEnabled() {
		return nil
	}
	start := time.Now()
	var proof criuCgroupPrefixResult
	defer func() {
		record("sysbox-cgroup-prefix-reuse-proof", start, asJSON(map[string]any{
			"images": images, "proof": proof,
			"scope": "namespace prefix normalized before identity lookup; every thread membership still parsed; distinct qualified memberships retained; actual native summary after successful cgroup image write",
		}), resultErr)
	}()
	if images != "/experiment/sysbox/images" && images != "/experiment/sysbox/images-again" && !(os.Getenv("RUNTIME_FORK_RICH_EPOCH_CHAIN") == "1" && images == "/experiment/sysbox/images-repeat") {
		return fmt.Errorf("cgroup prefix proof is outside owned captures")
	}
	path := filepath.Join(images, "dump.log")
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(fd), path)
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > 16<<20 {
		return fmt.Errorf("cgroup prefix proof log is not a bounded regular file: %v", err)
	}
	data, err := io.ReadAll(io.LimitReader(file, (16<<20)+1))
	if err != nil || len(data) > 16<<20 {
		return fmt.Errorf("cgroup prefix proof log exceeded limit: %v", err)
	}
	proof, err = criuParseCgroupPrefixProof(string(data))
	return err
}
