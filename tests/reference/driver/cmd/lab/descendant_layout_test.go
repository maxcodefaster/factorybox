package main

import "testing"

func TestDescendantLayoutKeepsDistinctOwnedRoots(t *testing.T) {
	original, err := descendantOwnedLayout("source-sysbox")
	if err != nil || original.Root != "/experiment/sysbox" {
		t.Fatal(original, err)
	}
	child, err := descendantOwnedLayout("copy-sysbox")
	if err != nil || child.Cgroup != "/sys/fs/cgroup/criu-probe-sysbox-copy" {
		t.Fatal(child, err)
	}
	branch, err := descendantOwnedLayout("branch-0123456789abcdef0123456789abcdef")
	if err != nil || branch.Root != "/experiment/branches/branch-0123456789abcdef0123456789abcdef" ||
		branch.Cgroup != "/sys/fs/cgroup/criu-probe-branch-0123456789abcdef0123456789abcdef" {
		t.Fatal(branch, err)
	}
	for _, bad := range []string{"", "source-other", "../source-sysbox", "branch-0123",
		"branch-0123456789abcdef0123456789abcdef/../../host", "branch-0123456789abcdef0123456789abcdeg"} {
		if _, err := descendantOwnedLayout(bad); err == nil {
			t.Fatal("foreign lineage identity accepted", bad)
		}
	}
}
