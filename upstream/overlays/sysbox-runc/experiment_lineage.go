//go:build linux

package main

import (
	"fmt"
	"path/filepath"
	"regexp"
)

var experimentBranchID = regexp.MustCompile(`^branch-[0-9a-f]{32}$`)

type experimentSandboxLayout struct {
	ID, Root, Cgroup string
}

type experimentForkPlan struct {
	Source, Target experimentSandboxLayout
	Images         string
}

func experimentOwnedSandbox(id string) (experimentSandboxLayout, error) {
	switch id {
	case "source-sysbox":
		return experimentSandboxLayout{id, "/experiment/sysbox", "criu-probe-sysbox"}, nil
	case "copy-sysbox":
		return experimentSandboxLayout{id, "/experiment/sysbox-copy", "criu-probe-sysbox-copy"}, nil
	default:
		if !experimentBranchID.MatchString(id) {
			return experimentSandboxLayout{}, fmt.Errorf("foreign system-container identity")
		}
		return experimentSandboxLayout{id, filepath.Join("/experiment/branches", id), "criu-probe-" + id}, nil
	}
}

func experimentOwnedForkPlan(sourceID, targetID, targetRoot, images string) (experimentForkPlan, error) {
	source, err := experimentOwnedSandbox(sourceID)
	if err != nil {
		return experimentForkPlan{}, err
	}
	target, err := experimentOwnedSandbox(targetID)
	if err != nil {
		return experimentForkPlan{}, err
	}
	if sourceID == targetID || targetID == "source-sysbox" ||
		(targetID == "copy-sysbox" && sourceID != "source-sysbox") ||
		targetRoot != target.Root || images != target.Root+"/images" {
		return experimentForkPlan{}, fmt.Errorf("fork target identity/root/images are outside the owned lineage")
	}
	return experimentForkPlan{source, target, images}, nil
}
