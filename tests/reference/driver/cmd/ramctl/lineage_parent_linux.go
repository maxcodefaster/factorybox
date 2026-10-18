package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"golang.org/x/sys/unix"
)

func lineageInheritedParent(images string) (string, string, error) {
	if os.Getenv("RUNTIME_FORK_DESCENDANT_INHERIT_CAPTURE") != "1" ||
		os.Getenv("RUNTIME_FORK_DESCENDANT_INHERIT_PROOF") != "1" ||
		os.Getenv("RUNTIME_FORK_DESCENDANT") != "1" ||
		os.Getenv("CRIU_SYSBOX_LAZY_INHERIT") != "1" ||
		os.Getenv("RUNTIME_FORK_COW_CAPTURE") != "0" ||
		os.Getenv("RUNTIME_FORK_PIPE_CAPTURE") != "0" {
		return "", "", fmt.Errorf("inherited lineage capture lacks its explicit owned proof policy")
	}
	root := filepath.Dir(images)
	if root != "/experiment/sysbox-copy" &&
		!regexp.MustCompile(`^/experiment/branches/branch-[0-9a-f]{32}$`).MatchString(root) {
		return "", "", fmt.Errorf("inherited lineage parent is outside a canonical runtime root")
	}
	if !regexp.MustCompile(`^images-[0-9a-f]{32}$`).MatchString(filepath.Base(images)) {
		return "", "", fmt.Errorf("inherited lineage capture lacks a fresh image identity")
	}
	parent := os.Getenv("RUNTIME_FORK_DESCENDANT_PARENT")
	if filepath.Clean(parent) != parent || parent == images || !strings.HasPrefix(parent, root+"/") {
		return "", "", fmt.Errorf("inherited lineage parent escaped its source runtime")
	}
	actual, err := filepath.EvalSymlinks(parent)
	if err != nil || actual != parent {
		return "", "", fmt.Errorf("inherited lineage parent is not canonical: %v", err)
	}
	info, err := os.Lstat(parent)
	if err != nil || !info.IsDir() {
		return "", "", fmt.Errorf("inherited lineage parent is not a real directory: %v", err)
	}
	var fs unix.Statfs_t
	if err := unix.Statfs(parent, &fs); err != nil || fs.Flags&unix.ST_RDONLY == 0 {
		return "", "", fmt.Errorf("inherited lineage parent is not kernel readonly: %v", err)
	}
	relative, err := filepath.Rel(images, parent)
	if err != nil {
		return "", "", err
	}
	return parent, relative, nil
}
