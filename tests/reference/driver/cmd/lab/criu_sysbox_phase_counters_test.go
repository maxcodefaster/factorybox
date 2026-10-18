package main

import (
	"reflect"
	"testing"
)

func TestSysboxPhaseIOAndPressure(t *testing.T) {
	io, err := criuPhaseIO("8:0 rbytes=12 wbytes=34 rios=1 wios=2\n8:1 rbytes=0 wbytes=9\n")
	if err != nil || io["8:0"]["wbytes"] != 34 || io["8:1"]["wbytes"] != 9 {
		t.Fatalf("device counters: %v %v", io, err)
	}
	if empty, err := criuPhaseIO(""); err != nil || len(empty) != 0 {
		t.Fatalf("idle device counters: %v %v", empty, err)
	}
	if empty, err := criuPhaseIO("253:0 \n254:16 rbytes=12 wbytes=34\n"); err != nil || len(empty["253:0"]) != 0 || empty["254:16"]["rbytes"] != 12 {
		t.Fatalf("actual Kubernetes idle header format: %v %v", empty, err)
	}
	for _, data := range []string{"8 rbytes=1", "x:0 rbytes=1", "8:0 rbytes=-1", "8:0 rbytes=1 rbytes=2", "8:0 rbytes=1\n8:0 wbytes=1", "8:0 broken", "8:0 =1", "8:0 rbytes=1\n \n8:1 rbytes=1"} {
		if _, err := criuPhaseIO(data); err == nil {
			t.Fatalf("accepted malformed I/O counters: %q", data)
		}
	}
	pressure, err := criuPhasePressure("some avg10=1.0 avg60=2.0 avg300=3.0 total=456\nfull avg10=0.0 total=123\n")
	if err != nil || !reflect.DeepEqual(pressure, map[string]uint64{"some": 456, "full": 123}) {
		t.Fatalf("cumulative pressure: %v %v", pressure, err)
	}
	if _, err := criuPhasePressure("some total=0\n"); err != nil {
		t.Fatal(err)
	}
	for _, data := range []string{"", "full total=1", "other total=1", "some avg10=1", "some total=-1", "some total=1 total=2", "some total=1\nsome total=2"} {
		if _, err := criuPhasePressure(data); err == nil {
			t.Fatalf("accepted malformed pressure: %q", data)
		}
	}
}

func TestSysboxPhaseDeltasKeepGaugesAndRejectReset(t *testing.T) {
	before := criuPhaseCounters{Cgroup: "/sys/fs/cgroup", Device: 1, Inode: 2,
		CPU:          map[string]uint64{"usage_usec": 100, "nr_throttled": 3},
		IO:           map[string]map[string]uint64{"8:0": {"rbytes": 100}},
		Pressure:     map[string]map[string]uint64{"cpu": {"some": 10, "full": 5}},
		Memory:       map[string]uint64{"anon": 1000, "file": 1000, "shmem": 1000, "pgmajfault": 3},
		MemoryEvents: map[string]uint64{"oom_kill": 0}}
	after := criuPhaseCounters{Cgroup: "/sys/fs/cgroup", Device: 1, Inode: 2,
		CPU:          map[string]uint64{"usage_usec": 160, "nr_throttled": 5},
		IO:           map[string]map[string]uint64{"8:0": {"rbytes": 130}, "8:1": {"rbytes": 7}},
		Pressure:     map[string]map[string]uint64{"cpu": {"some": 30, "full": 9}},
		Memory:       map[string]uint64{"anon": 1, "file": 1, "shmem": 1, "pgmajfault": 5},
		MemoryEvents: map[string]uint64{"oom_kill": 0}}
	delta, err := criuPhaseDifference(before, after)
	if err != nil || delta.CPU["usage_usec"] != 60 || delta.CPU["nr_throttled"] != 2 || delta.IO["8:0"]["rbytes"] != 30 || delta.IO["8:1"]["rbytes"] != 7 || delta.Pressure["cpu"]["some"] != 20 || delta.Memory["pgmajfault"] != 2 || len(delta.Memory) != 1 {
		t.Fatalf("counter delta: %+v %v", delta, err)
	}
	for _, mutate := range []func(*criuPhaseCounters){
		func(x *criuPhaseCounters) { x.Cgroup = "/sys/fs/cgroup/other" },
		func(x *criuPhaseCounters) { x.Inode++ },
		func(x *criuPhaseCounters) { x.Device++ },
		func(x *criuPhaseCounters) { x.PressureUnavailable = map[string]string{"cpu": "missing"} },
	} {
		changed := after
		mutate(&changed)
		if _, err := criuPhaseDifference(before, changed); err == nil {
			t.Fatal("accepted replaced cgroup identity")
		}
	}
	for _, current := range []map[string]uint64{{"counter": 1}, {}, {"counter": 2}} {
		_, err := criuPhaseCounterDelta(map[string]uint64{"counter": 3}, current)
		if err == nil {
			t.Fatal("accepted missing/reset monotonic counter")
		}
	}
	if _, err := criuPhaseNestedDelta(before.IO, map[string]map[string]uint64{}); err == nil {
		t.Fatal("accepted vanished device counters")
	}
	before.Pressure = map[string]map[string]uint64{}
	after.Pressure = map[string]map[string]uint64{}
	before.PressureUnavailable = map[string]string{"cpu": "worker lacks PSI"}
	after.PressureUnavailable = map[string]string{"cpu": "worker lacks PSI"}
	if delta, err := criuPhaseDifference(before, after); err != nil || len(delta.Pressure) != 0 {
		t.Fatalf("stable unsupported PSI must remain explicit, not zero stalls: %+v %v", delta, err)
	}
}
