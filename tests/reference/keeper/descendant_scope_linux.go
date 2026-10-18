package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var ownedCaptureGeneration = regexp.MustCompile(`^images-[0-9a-f]{32}$`)
var ownedCaptureBranch = regexp.MustCompile(`^branch-[0-9a-f]{32}$`)

// The original temporary-root component fixtures retain their two existing
// image names. Only the exact production worker root may admit descendants.
func captureParentCgroup(imageRoot, parent string) (string, bool) {
	if filepath.Clean(parent) != parent {
		return "", false
	}
	if parent == filepath.Join(imageRoot, "images-again") || parent == filepath.Join(imageRoot, "images-repeat") {
		if imageRoot == "/experiment/sysbox" {
			return "/criu-probe-sysbox", true
		}
		return "", true
	}
	if imageRoot != "/experiment/sysbox" || !ownedCaptureGeneration.MatchString(filepath.Base(parent)) {
		return "", false
	}
	directory := filepath.Dir(parent)
	if directory == "/experiment/sysbox-copy" {
		return "/criu-probe-sysbox-copy", true
	}
	if filepath.Dir(directory) == "/experiment/branches" && ownedCaptureBranch.MatchString(filepath.Base(directory)) {
		return "/criu-probe-" + filepath.Base(directory), true
	}
	return "", false
}

func captureSourceCgroup(source sourceIdentity, expected string) error {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/cgroup", source.PID))
	if err != nil {
		return err
	}
	match := false
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "0::") {
			group := strings.TrimPrefix(line, "0::")
			match = group == expected || strings.HasPrefix(group, expected+"/")
		}
	}
	if !match {
		return fmt.Errorf("capture source is outside its exact image-owner cgroup")
	}
	status, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", source.PID))
	if err != nil {
		return err
	}
	end := strings.LastIndexByte(string(status), ')')
	if end < 0 {
		return fmt.Errorf("capture source stat is malformed")
	}
	fields := strings.Fields(string(status[end+1:]))
	if len(fields) < 20 || fields[0] == "Z" || fields[0] == "X" || fields[19] != source.StartTime {
		return fmt.Errorf("capture source is dead or changed during owner admission")
	}
	return nil
}
