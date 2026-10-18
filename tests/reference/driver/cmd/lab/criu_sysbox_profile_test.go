package main

import "testing"

func TestProfileCgroupCannotEscapeMountedHierarchy(t *testing.T) {
	for _, path := range []string{"", ".", "/", "/sys/fs", "/sys/fs/cgroup-other", "/sys/fs/cgroup/../other", "/sys/fs/cgroup/source/../../other"} {
		if selector, err := criuSysboxProfileCgroupSelector(path); err == nil {
			t.Fatalf("unsafe cgroup %q admitted as %q", path, selector)
		}
	}
	for path, want := range map[string]string{"/sys/fs/cgroup": "/", "/sys/fs/cgroup/source/init.scope": "/source/init.scope"} {
		selector, err := criuSysboxProfileCgroupSelector(path)
		if err != nil || selector != want {
			t.Fatalf("owned cgroup %q: selector=%q err=%v", path, selector, err)
		}
	}
}
