//go:build linux

package lifecycle

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestDelegatedCgroupForkNative(t *testing.T) {
	if os.Getenv("FACTORYBOX_NATIVE_DELEGATION") != "1" {
		t.Skip("requires the owned delegated-cgroup checkpoint fixture")
	}
	delegatedCgroupForkNative(t, "cpuset cpu io memory pids", false)
}

func TestAuxiliaryDelegatedCgroupForkNative(t *testing.T) {
	if os.Getenv("FACTORYBOX_NATIVE_AUXILIARY_DELEGATION") != "1" {
		t.Skip("requires this worker's complete cgroup-v2 controller fixture")
	}
	delegatedCgroupForkNative(t, "cpuset cpu io memory hugetlb pids rdma misc dmem", true)
}

func delegatedCgroupForkNative(t *testing.T, controllers string, auxiliary bool) {
	engine, bundle := nativeEngine(t, "/bin/env FACTORYBOX_CAPTURE_ACTOR=1 /bin/capture-actor -test.run=^TestCaptureActor$ -test.timeout=0")
	binary, err := os.ReadFile(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bundle+"/lower/bin/capture-actor", binary, 0755); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(bundle + "/config.json")
	if err != nil {
		t.Fatal(err)
	}
	var spec map[string]any
	if err := json.Unmarshal(data, &spec); err != nil {
		t.Fatal(err)
	}
	for _, value := range spec["mounts"].([]any) {
		mount := value.(map[string]any)
		if mount["destination"] == "/sys/fs/cgroup" {
			mount["options"] = []string{"nosuid", "noexec", "nodev", "rw"}
		}
	}
	data, err = json.Marshal(spec)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bundle+"/config.json", data, 0600); err != nil {
		t.Fatal(err)
	}
	engine.config.Criu, engine.config.CriuSHA256 = "/out/criu", os.Getenv("FACTORYBOX_CRIU_SHA256")
	engine.config.ManagerRoot = "/var/lib/sysbox/manager"
	ctx := context.Background()
	source, err := engine.Start(ctx, "delegated", bundle)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := engine.Delete(ctx, "delegated"); err != nil {
			t.Error(err)
		}
	})
	deadline := time.Now().Add(5 * time.Second)
	var before captureActorState
	for {
		before, err = readCaptureActor(source.Runtime.PID, "get")
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("delegation actor not ready", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	control, err := engine.lock()
	if err != nil {
		t.Fatal(err)
	}
	script := `exec >/state/delegation-setup.log 2>&1
set -eux
cat /sys/fs/cgroup/cgroup.controllers
cat /sys/fs/cgroup/cgroup.subtree_control
mkdir -p /sys/fs/cgroup/init.scope
printf '1\n' > /sys/fs/cgroup/init.scope/cgroup.procs
printf '%s\n' "$$" > /sys/fs/cgroup/init.scope/cgroup.procs
for controller in cpuset cpu io memory pids; do
  printf '+%s\n' "$controller" > /sys/fs/cgroup/cgroup.subtree_control
done
mkdir /sys/fs/cgroup/docker
for controller in cpuset cpu io memory pids; do
  printf '+%s\n' "$controller" > /sys/fs/cgroup/docker/cgroup.subtree_control
done
for branch in first second; do
  mkdir "/sys/fs/cgroup/docker/$branch"
  /bin/sleep inf </dev/null >/dev/null 2>&1 &
  printf '%s\n' "$!" > "/sys/fs/cgroup/docker/$branch/cgroup.procs"
done
printf '67108864\n' > /sys/fs/cgroup/docker/first/memory.max
printf '100663296\n' > /sys/fs/cgroup/docker/second/memory.max
printf '32\n' > /sys/fs/cgroup/docker/first/pids.max
printf '48\n' > /sys/fs/cgroup/docker/second/pids.max
printf '50000 100000\n' > /sys/fs/cgroup/docker/first/cpu.max
printf '25000 100000\n' > /sys/fs/cgroup/docker/second/cpu.max
`
	script = strings.ReplaceAll(script, "for controller in cpuset cpu io memory pids; do", "for controller in "+controllers+"; do")
	_, setupErr := engine.command(ctx, control, "exec", source.Record.RuntimeID, "/bin/sh", "-ec", script)
	closeErr := control.Close()
	if setupErr != nil || closeErr != nil {
		details, readErr := os.ReadFile(bundle + "/rootfs/state/delegation-setup.log")
		t.Fatalf("prepare delegated cgroups: %v %v; workload log=%s (%v)", setupErr, closeErr, details, readErr)
	}
	auxiliaryLimits := map[string]map[string]string{}
	if auxiliary {
		auxiliaryLimits = map[string]map[string]string{
			"first":  {"hugetlb.2MB.max": "4194304", "hugetlb.2MB.rsvd.max": "2097152", "hugetlb.1GB.max": "1073741824", "hugetlb.1GB.rsvd.max": "1073741824"},
			"second": {"hugetlb.2MB.max": "8388608", "hugetlb.2MB.rsvd.max": "4194304", "hugetlb.1GB.max": "2147483648", "hugetlb.1GB.rsvd.max": "1073741824"},
		}
		for branch, limits := range auxiliaryLimits {
			path := cgroupRoot + "/" + source.Record.RuntimeID + "/docker/" + branch
			for property, value := range limits {
				if err := os.WriteFile(path+"/"+property, []byte(value), 0600); err != nil {
					t.Fatal("owned hugepage limit setup without page allocation", err)
				}
			}
			for _, property := range []string{"rdma.max", "misc.max", "dmem.max"} {
				data, err := os.ReadFile(path + "/" + property)
				if err != nil {
					t.Fatal("read actual auxiliary controller limit", err)
				}
				limits[property] = strings.TrimSpace(string(data))
			}
		}
	}
	checkAuxiliaryLimits := func(id string) {
		t.Helper()
		for branch, limits := range auxiliaryLimits {
			for property, expected := range limits {
				data, err := os.ReadFile(cgroupRoot + "/" + id + "/docker/" + branch + "/" + property)
				if err != nil || strings.TrimSpace(string(data)) != expected {
					t.Fatal("auxiliary controller limit changed", id, branch, property, string(data), expected, err)
				}
			}
		}
	}
	before, err = readCaptureActor(source.Runtime.PID, "open-cgroup-limit")
	if err != nil || before.CgroupLimit != "67108864" {
		t.Fatal("open delegated controller descriptor", before.CgroupLimit, err)
	}
	observedPIDs := map[string]bool{}
	logMembership := func(phase string, ids ...string) {
		t.Helper()
		for _, id := range ids {
			for _, leaf := range []string{"init.scope", "docker/first", "docker/second"} {
				members, err := os.ReadFile(cgroupRoot + "/" + id + "/" + leaf + "/cgroup.procs")
				t.Logf("%s %s/%s members=%q error=%v", phase, id, leaf, members, err)
				if err != nil || len(strings.Fields(string(members))) != 1 {
					t.Fatalf("%s lost its delegated leaf process: %s/%s: %q: %v", phase, id, leaf, members, err)
				}
				for _, pid := range strings.Fields(string(members)) {
					observedPIDs[pid] = true
					group, groupErr := os.ReadFile("/proc/" + pid + "/cgroup")
					status, statusErr := os.ReadFile("/proc/" + pid + "/status")
					var identity []string
					for _, line := range strings.Split(string(status), "\n") {
						if strings.HasPrefix(line, "Name:") || strings.HasPrefix(line, "State:") ||
							strings.HasPrefix(line, "PPid:") || strings.HasPrefix(line, "NSpid:") {
							identity = append(identity, line)
						}
					}
					t.Logf("member %s cgroup=%q identity=%q errors=%v/%v", pid, group, identity, groupErr, statusErr)
				}
			}
		}
		for pid := range observedPIDs {
			stat, err := os.ReadFile("/proc/" + pid + "/stat")
			t.Logf("%s retained pid=%s stat=%q error=%v", phase, pid, stat, err)
		}
	}
	logMembership("before checkpoint", source.Record.RuntimeID)
	snapshot := strings.Repeat("e", 32)
	start := time.Now()
	captured, err := engine.Checkpoint(ctx, "delegated", snapshot)
	if err != nil || captured.ReleasedAt == "" {
		t.Fatal("capture delegated subtree", err)
	}
	t.Logf("delegated cgroup checkpoint=%s", time.Since(start))
	logMembership("after checkpoint", source.Record.RuntimeID)
	start = time.Now()
	forked, err := forkNativeCLI(engine, "delegated", snapshot, "delegated-fork")
	if err != nil {
		t.Fatal("restore delegated cgroup subtree", err)
	}
	logMembership("after fork", source.Record.RuntimeID, forked.Record.RuntimeID)
	checkAuxiliaryLimits(source.Record.RuntimeID)
	checkAuxiliaryLimits(forked.Record.RuntimeID)
	t.Cleanup(func() {
		if _, err := engine.Delete(ctx, "delegated-fork"); err != nil {
			t.Error(err)
		}
	})
	after, err := readCaptureActor(forked.Runtime.PID, "get")
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("delegated restore state mismatch: error=%v payload_equal=%t counter=%d/%d memory.max=%q/%q",
			err, reflect.DeepEqual(before.Payload, after.Payload), before.Counter, after.Counter,
			before.CgroupLimit, after.CgroupLimit)
	}
	for _, id := range []string{source.Record.RuntimeID, forked.Record.RuntimeID} {
		for _, suffix := range []string{"", "/docker"} {
			path := cgroupRoot + "/" + id + suffix + "/cgroup.subtree_control"
			data, err := os.ReadFile(path)
			if err != nil || strings.TrimSpace(string(data)) != controllers {
				t.Fatal("delegated controllers changed", id, suffix, string(data), err)
			}
			data, err = os.ReadFile(cgroupRoot + "/" + id + suffix + "/cgroup.procs")
			if err != nil || len(strings.Fields(string(data))) != 0 {
				t.Fatal("delegated parent populated", id, suffix, string(data), err)
			}
		}
		data, err := os.ReadFile(cgroupRoot + "/" + id + "/init.scope/cgroup.procs")
		if err != nil || len(strings.Fields(string(data))) != 1 {
			t.Fatal("init leaf membership changed", id, string(data), err)
		}
		for _, branch := range []string{"first", "second"} {
			data, err := os.ReadFile(cgroupRoot + "/" + id + "/docker/" + branch + "/cgroup.procs")
			if err != nil || len(strings.Fields(string(data))) != 1 {
				t.Fatal("delegated child membership changed", id, branch, string(data), err)
			}
			expected := "67108864"
			if branch == "second" {
				expected = "100663296"
			}
			data, err = os.ReadFile(cgroupRoot + "/" + id + "/docker/" + branch + "/memory.max")
			if err != nil || strings.TrimSpace(string(data)) != expected {
				t.Fatal("delegated memory limit changed", id, branch, string(data), err)
			}
			expectedPIDs, expectedCPU := "32", "50000 100000"
			if branch == "second" {
				expectedPIDs, expectedCPU = "48", "25000 100000"
			}
			for property, expected := range map[string]string{"pids.max": expectedPIDs, "cpu.max": expectedCPU} {
				data, err := os.ReadFile(cgroupRoot + "/" + id + "/docker/" + branch + "/" + property)
				if err != nil || strings.TrimSpace(string(data)) != expected {
					t.Fatal("delegated controller limit changed", id, branch, property, string(data), err)
				}
			}
		}
	}
	if err := os.WriteFile(cgroupRoot+"/"+forked.Record.RuntimeID+"/docker/first/memory.max", []byte("134217728"), 0600); err != nil {
		t.Fatal(err)
	}
	mutated, err := readCaptureActor(forked.Runtime.PID, "advance")
	if err != nil || mutated.CgroupLimit != "134217728" {
		t.Fatal("restored descriptor does not address the fork's controller", mutated.CgroupLimit, err)
	}
	unchanged, err := readCaptureActor(source.Runtime.PID, "get")
	if err != nil || !reflect.DeepEqual(before, unchanged) {
		t.Fatal("delegated fork changed original RAM", err)
	}
	t.Logf("delegated cgroup fork and first useful RAM=%s", time.Since(start))
	pauseID := strings.Repeat("f", 32)
	paused, err := pauseNativeCLI(engine, "delegated-fork", pauseID)
	if err != nil {
		t.Fatal("pause independently useful delegated fork", err)
	}
	if paused.Record.Phase != "paused" || paused.Record.Pause == nil ||
		paused.Record.Pause.Snapshot.ResidentBytesAfter != 0 {
		t.Fatal("delegated pause did not retain its original identity and release snapshot RAM")
	}
	// Lose the controller after complete private backing and empty-tree
	// preparation, before the durable runtime-attempt flag. The public
	// command must safely resume that exact operation without new inputs.
	prepared := paused.Record
	prepared.Phase = "resuming"
	preparedPause := *prepared.Pause
	preparedPause.Resume = &Operation{RequestedAt: now()}
	prepared.Pause = &preparedPause
	control, err = engine.lock()
	if err != nil {
		t.Fatal(err)
	}
	prepareErr := engine.write(prepared)
	if prepareErr == nil {
		prepareErr = engine.prepareResume(ctx, control, prepared,
			"objects/delegated-fork/resumes/"+pauseID, engine.resumeRequest(prepared))
	}
	if prepareErr == nil {
		prepareErr = engine.prepareEmptyResumeCgroup(prepared)
	}
	closeErr = control.Close()
	if prepareErr != nil || closeErr != nil {
		t.Fatal("prepare interrupted delegated resume", prepareErr, closeErr)
	}
	retained, err := engine.read("delegated-fork")
	if err != nil || retained.Pause == nil || retained.Pause.Resume == nil || retained.Pause.Resume.Attempted {
		t.Fatal("empty-tree preparation prematurely recorded a runtime attempt", err)
	}
	start = time.Now()
	resumed, err := transitionNativeCLI(engine, "resume", "delegated-fork", pauseID)
	if err != nil {
		t.Fatal("resume original delegated cgroup subtree", err)
	}
	checkAuxiliaryLimits(resumed.Record.RuntimeID)
	if resumed.Record.RuntimeID != forked.Record.RuntimeID ||
		!reflect.DeepEqual(resumed.Record.Cgroup, forked.Record.Cgroup) {
		t.Fatal("resume replaced the original delegated cgroup identity")
	}
	resumedActor, err := readCaptureActor(resumed.Runtime.PID, "get")
	if err != nil || !reflect.DeepEqual(mutated, resumedActor) {
		t.Fatal("delegated resume lost independent RAM or the open memory.max descriptor", err)
	}
	for _, suffix := range []string{"", "/docker"} {
		path := cgroupRoot + "/" + resumed.Record.RuntimeID + suffix
		observedControllers, err := os.ReadFile(path + "/cgroup.subtree_control")
		if err != nil || strings.TrimSpace(string(observedControllers)) != controllers {
			t.Fatal("resume changed delegated controllers", suffix, string(observedControllers), err)
		}
		members, err := os.ReadFile(path + "/cgroup.procs")
		if err != nil || len(strings.Fields(string(members))) != 0 {
			t.Fatal("resume populated a delegated parent", suffix, string(members), err)
		}
	}
	for _, leaf := range []string{"init.scope", "docker/first", "docker/second"} {
		members, err := os.ReadFile(cgroupRoot + "/" + resumed.Record.RuntimeID + "/" + leaf + "/cgroup.procs")
		if err != nil || len(strings.Fields(string(members))) != 1 {
			t.Fatal("resume changed delegated leaf membership", leaf, string(members), err)
		}
	}
	secondLimit, err := os.ReadFile(cgroupRoot + "/" + resumed.Record.RuntimeID + "/docker/second/memory.max")
	if err != nil || strings.TrimSpace(string(secondLimit)) != "100663296" {
		t.Fatal("resume changed the second delegated memory limit", string(secondLimit), err)
	}
	for branch, limits := range map[string]map[string]string{
		"first":  {"pids.max": "32", "cpu.max": "50000 100000"},
		"second": {"pids.max": "48", "cpu.max": "25000 100000"},
	} {
		for property, expected := range limits {
			data, err := os.ReadFile(cgroupRoot + "/" + resumed.Record.RuntimeID + "/docker/" + branch + "/" + property)
			if err != nil || strings.TrimSpace(string(data)) != expected {
				t.Fatal("resume changed a delegated controller limit", branch, property, string(data), err)
			}
		}
	}
	if err := os.WriteFile(cgroupRoot+"/"+resumed.Record.RuntimeID+"/docker/first/memory.max", []byte("201326592"), 0600); err != nil {
		t.Fatal(err)
	}
	resumedActor, err = readCaptureActor(resumed.Runtime.PID, "advance")
	if err != nil || resumedActor.CgroupLimit != "201326592" {
		t.Fatal("resumed descriptor does not address its retained controller", resumedActor.CgroupLimit, err)
	}
	unchanged, err = readCaptureActor(source.Runtime.PID, "get")
	if err != nil || !reflect.DeepEqual(before, unchanged) {
		t.Fatal("delegated resume changed the original source", err)
	}
	t.Logf("delegated prepared resume and first useful RAM=%s", time.Since(start))
	if _, err := engine.Delete(ctx, "delegated"); err != nil {
		t.Fatal("delete original source before forking the resumed runtime", err)
	}
	resumedSnapshot := strings.Repeat("d", 32)
	if _, err := engine.Checkpoint(ctx, "delegated-fork", resumedSnapshot); err != nil {
		t.Fatal("capture resumed delegated runtime", err)
	}
	child, err := forkNativeCLI(engine, "delegated-fork", resumedSnapshot, "delegated-resumed-fork")
	if err != nil {
		t.Fatal("fork resumed delegated runtime", err)
	}
	t.Cleanup(func() {
		if _, err := engine.Delete(ctx, "delegated-resumed-fork"); err != nil {
			t.Error(err)
		}
	})
	childActor, err := readCaptureActor(child.Runtime.PID, "get")
	if err != nil || !reflect.DeepEqual(resumedActor, childActor) {
		t.Fatal("fork of resumed delegated runtime lost complete RAM or its controller descriptor", err)
	}
	checkAuxiliaryLimits(resumed.Record.RuntimeID)
	checkAuxiliaryLimits(child.Record.RuntimeID)
	if auxiliary {
		if err := os.WriteFile(cgroupRoot+"/"+child.Record.RuntimeID+"/docker/first/hugetlb.2MB.max", []byte("16777216"), 0600); err != nil {
			t.Fatal(err)
		}
		checkAuxiliaryLimits(resumed.Record.RuntimeID)
		t.Log("all nine worker controllers, finite 2-MiB/1-GiB hugepage limits without page allocation, actual RDMA/misc/dmem limits and independent resumed-child mutation passed")
	}
	if _, err := readCaptureActor(child.Runtime.PID, "advance"); err != nil {
		t.Fatal(err)
	}
	parentActor, err := readCaptureActor(resumed.Runtime.PID, "get")
	if err != nil || !reflect.DeepEqual(resumedActor, parentActor) {
		t.Fatal("fork of resumed delegated runtime changed its parent's private state", err)
	}
}
