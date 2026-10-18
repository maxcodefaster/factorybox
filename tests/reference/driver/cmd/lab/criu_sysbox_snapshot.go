package main

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

const criuRAMSnapshotLimit = uint64(1 << 30)

var criuRAMPageName = regexp.MustCompile(`^pages-(?:shmem-)?[0-9]+\.img$`)

func criuRAMSnapshotPath(root, images string) error {
	if !filepath.IsAbs(root) || !filepath.IsAbs(images) || filepath.Clean(root) != root || filepath.Clean(images) != images {
		return fmt.Errorf("unclean snapshot path")
	}
	for _, relative := range []string{"sysbox/images", "sysbox/images-base", "sysbox/images-again", "sysbox/images-repeat", "sysbox-copy/images"} {
		if images == filepath.Join(root, relative) {
			return nil
		}
	}
	return fmt.Errorf("RAM snapshot is outside the owned fixture: %q", images)
}

func criuRAMCgroupPath(data string) (string, error) {
	var parent string
	for _, line := range strings.Split(data, "\n") {
		if strings.HasPrefix(line, "0::") {
			if parent != "" {
				return "", fmt.Errorf("multiple cgroup-v2 memberships")
			}
			parent = strings.TrimPrefix(line, "0::")
		}
	}
	if !strings.HasPrefix(parent, "/") || filepath.Clean(parent) != parent || strings.Contains(parent, "..") {
		return "", fmt.Errorf("invalid owned cgroup-v2 membership: %q", parent)
	}
	return filepath.Join("/sys/fs/cgroup", parent), nil
}

func criuRAMCounters(data string) (map[string]uint64, error) {
	output := map[string]uint64{}
	for _, line := range strings.Split(strings.TrimSpace(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			return nil, fmt.Errorf("invalid cgroup counter line: %q", line)
		}
		if _, exists := output[fields[0]]; exists {
			return nil, fmt.Errorf("duplicate cgroup counter: %q", fields[0])
		}
		value, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			return nil, err
		}
		output[fields[0]] = value
	}
	return output, nil
}
