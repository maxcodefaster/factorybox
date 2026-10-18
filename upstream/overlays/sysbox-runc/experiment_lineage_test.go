//go:build linux

package main

import "testing"

func TestExperimentLineagePlan(t *testing.T) {
	id := "branch-0123456789abcdef0123456789abcdef"
	root := "/experiment/branches/" + id
	for _, source := range []string{"source-sysbox", "copy-sysbox", "branch-1123456789abcdef0123456789abcdef"} {
		plan, err := experimentOwnedForkPlan(source, id, root, root+"/images")
		if err != nil || plan.Source.ID != source || plan.Target.ID != id ||
			plan.Target.Cgroup != "criu-probe-"+id {
			t.Fatal("canonical descendant rejected", source, plan, err)
		}
	}
	if _, err := experimentOwnedForkPlan("source-sysbox", "copy-sysbox", "/experiment/sysbox-copy", "/experiment/sysbox-copy/images"); err != nil {
		t.Fatal("reference fork changed", err)
	}
	for _, bad := range [][4]string{
		{"copy-sysbox", "copy-sysbox", "/experiment/sysbox-copy", "/experiment/sysbox-copy/images"},
		{"copy-sysbox", "source-sysbox", "/experiment/sysbox", "/experiment/sysbox/images"},
		{"../host", id, root, root + "/images"},
		{"copy-sysbox", id, "/etc", "/etc/images"},
		{"copy-sysbox", id, root, root + "/../images"},
		{"copy-sysbox", "branch-0123", "/experiment/branches/branch-0123", "/experiment/branches/branch-0123/images"},
	} {
		if _, err := experimentOwnedForkPlan(bad[0], bad[1], bad[2], bad[3]); err == nil {
			t.Fatal("foreign, reused or self target accepted", bad)
		}
	}
}
