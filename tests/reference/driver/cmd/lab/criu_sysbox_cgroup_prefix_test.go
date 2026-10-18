package main

import "testing"

func TestCgroupPrefixProofRequiresActualBoundedNativeReuse(t *testing.T) {
	valid := "(00.1) Warn (criu/cgroup.c:1): cg: RUNTIME_FORK_CGROUP_PREFIX_REUSE normalized=287 reused=282 sets=8\n"
	got, err := criuParseCgroupPrefixProof(valid)
	if err != nil || got.Normalized != 287 || got.Reused != 282 || got.Sets != 8 {
		t.Fatalf("native proof parse: %+v %v", got, err)
	}
	for _, value := range []string{"", valid + valid,
		"RUNTIME_FORK_CGROUP_PREFIX_REUSE normalized=1 reused=0 sets=8\n",
		"RUNTIME_FORK_CGROUP_PREFIX_REUSE normalized=1 reused=2 sets=8\n",
		"RUNTIME_FORK_CGROUP_PREFIX_REUSE normalized=1 reused=1 sets=0\n",
		"RUNTIME_FORK_CGROUP_PREFIX_REUSE normalized=4294967296 reused=1 sets=8\n"} {
		if _, err := criuParseCgroupPrefixProof(value); err == nil {
			t.Fatalf("accepted invalid/native-unused proof %q", value)
		}
	}
}

func TestCgroupPrefixGuardRequiresOwnedFrozenCapture(t *testing.T) {
	t.Setenv("CRIU_SYSBOX_CGROUP_PREFIX_REUSE", "0")
	if err := criuCgroupPrefixGuard("/other", "other"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CRIU_SYSBOX_CGROUP_PREFIX_REUSE", "invalid")
	if err := criuCgroupPrefixGuard("/experiment", "source-sysbox"); err == nil {
		t.Fatal("accepted invalid mode")
	}
	t.Setenv("CRIU_SYSBOX_CGROUP_PREFIX_REUSE", "1")
	required := []string{"CRIU_SYSBOX_PROBE", "CRIU_SYSBOX_RAM_CAPTURE",
		"CRIU_SYSBOX_RETAIN_SOURCE", "CRIU_SYSBOX_LAZY_INHERIT"}
	for _, name := range required {
		t.Setenv(name, "1")
	}
	if err := criuCgroupPrefixGuard("/experiment", "source-sysbox"); err != nil {
		t.Fatal(err)
	}
	if err := criuCgroupPrefixGuard("/experiment", "copy-sysbox"); err == nil {
		t.Fatal("accepted unrelated source")
	}
	for _, name := range required {
		t.Setenv(name, "0")
		if err := criuCgroupPrefixGuard("/experiment", "source-sysbox"); err == nil {
			t.Fatalf("accepted missing %s", name)
		}
		t.Setenv(name, "1")
	}
}
