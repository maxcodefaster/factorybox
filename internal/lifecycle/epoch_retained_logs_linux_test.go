//go:build linux

package lifecycle

import (
	"reflect"
	"testing"
)

func TestEpochRetainedLogsUseCompleteOriginalProcessTree(t *testing.T) {
	arm := retainedCaptureArm{Version: 3, Mode: "process_tree_retained", SourcePID: 131, SourceStart: "100",
		ActivePID: 2304, ActiveStart: "400", ActiveVirtualPID: 2,
		Tree: []retainedCaptureThread{{131, 131, "100"}, {173, 173, "200"}, {174, 173, "300"}, {2304, 2304, "400"}, {2462, 2462, "500"}}}
	want := map[string]bool{"criu-retained-131.log": true, "criu-retained-173.log": true,
		"criu-retained-2304.log": true, "criu-retained-2462.log": true}
	actual, err := epochRetainedLogNames(arm)
	if err != nil || !reflect.DeepEqual(actual, want) {
		t.Fatal("complete capture's original process logs were refused or thread/foreign logs authorized", actual, err)
	}
	for _, scenario := range []string{"empty", "duplicate", "unordered", "missing-leader", "root-start", "active-start", "active-thread", "oversized", "wrong-version"} {
		t.Run(scenario, func(t *testing.T) {
			changed := arm
			changed.Tree = append([]retainedCaptureThread(nil), arm.Tree...)
			switch scenario {
			case "empty":
				changed.Tree = nil
			case "duplicate":
				changed.Tree[1] = changed.Tree[0]
			case "unordered":
				changed.Tree[0], changed.Tree[1] = changed.Tree[1], changed.Tree[0]
			case "missing-leader":
				changed.Tree[2].Process = 175
			case "root-start":
				changed.SourceStart = "different"
			case "active-start":
				changed.ActiveStart = "different"
			case "active-thread":
				changed.ActivePID, changed.ActiveStart = 174, "300"
			case "oversized":
				changed.Tree = make([]retainedCaptureThread, 513)
			case "wrong-version":
				changed.Version = 1
			}
			if names, err := epochRetainedLogNames(changed); err == nil || len(names) != 0 {
				t.Fatal("incomplete or changed original tree authorized retirement log names", names, err)
			}
		})
	}
}
