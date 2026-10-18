package main

import (
	"fmt"
	"path/filepath"
	"regexp"
)

var descendantID = regexp.MustCompile(`^branch-[0-9a-f]{32}$`)

type descendantLayout struct {
	ID, Root, Cgroup string
}

func descendantOwnedLayout(id string) (descendantLayout, error) {
	switch id {
	case "source-sysbox":
		return descendantLayout{id, "/experiment/sysbox", "/sys/fs/cgroup/criu-probe-sysbox"}, nil
	case "copy-sysbox":
		return descendantLayout{id, "/experiment/sysbox-copy", "/sys/fs/cgroup/criu-probe-sysbox-copy"}, nil
	default:
		if !descendantID.MatchString(id) {
			return descendantLayout{}, fmt.Errorf("runtime identity is outside the owned lineage")
		}
		return descendantLayout{id, filepath.Join("/experiment/branches", id), "/sys/fs/cgroup/criu-probe-" + id}, nil
	}
}
