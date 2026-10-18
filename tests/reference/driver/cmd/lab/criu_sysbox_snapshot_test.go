package main

import (
	"reflect"
	"testing"
)

func TestRAMSnapshotPathsAndPayloadNames(t *testing.T) {
	for _, path := range []string{"/experiment/sysbox/images", "/experiment/sysbox/images-base", "/experiment/sysbox/images-again", "/experiment/sysbox-copy/images"} {
		if err := criuRAMSnapshotPath("/experiment", path); err != nil {
			t.Fatal(err)
		}
	}
	for _, path := range []string{"/experiment/sysbox/../sysbox/images", "/elsewhere/sysbox/images", "/experiment/sysbox/images-extra", "/experiment/sysbox-copy/images-again", "sysbox/images"} {
		if err := criuRAMSnapshotPath("/experiment", path); err == nil {
			t.Fatalf("accepted %q", path)
		}
	}
	for _, name := range []string{"pages-1.img", "pages-1234.img", "pages-shmem-12.img"} {
		if !criuRAMPageName.MatchString(name) {
			t.Fatalf("lost raw page image %q", name)
		}
	}
	for _, name := range []string{"pages-1.img/child", "pagemap-1.img", "pages-other.img", "pages-1.img.log"} {
		if criuRAMPageName.MatchString(name) {
			t.Fatalf("classified private metadata as shared pages: %q", name)
		}
	}
}

func TestRAMSnapshotCgroupAndCounters(t *testing.T) {
	for input, expected := range map[string]string{"0::/\n": "/sys/fs/cgroup", "0::/kubepods/pod/container\n": "/sys/fs/cgroup/kubepods/pod/container"} {
		actual, err := criuRAMCgroupPath(input)
		if err != nil || actual != expected {
			t.Fatalf("membership %q: %q %v", input, actual, err)
		}
	}
	for _, input := range []string{"", "0::/../root", "0::relative", "0::/one\n0::/two", "1:memory:/one"} {
		if _, err := criuRAMCgroupPath(input); err == nil {
			t.Fatalf("accepted membership %q", input)
		}
	}
	actual, err := criuRAMCounters("anon 42\nfile 123\nshmem 67\n")
	if err != nil || !reflect.DeepEqual(actual, map[string]uint64{"anon": 42, "file": 123, "shmem": 67}) {
		t.Fatalf("counters: %v %v", actual, err)
	}
	for _, input := range []string{"", "shmem -1", "shmem 1\nshmem 2", "shmem", "shmem 1 extra", "shmem 18446744073709551616"} {
		if _, err := criuRAMCounters(input); err == nil {
			t.Fatalf("accepted counters %q", input)
		}
	}
}
