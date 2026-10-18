package main

import (
	"fmt"
	"strings"
	"time"
)

type descendantExpectedState struct {
	States, Execs map[string]map[string]any
	Compose       map[string]any
	Marker        string
	Browser       string
	History       map[string]any
	Virtual       string
	Identity      map[string]any
}

type descendantExpectedResult struct {
	State       descendantExpectedState
	Err         error
	Start, Done time.Time
}

// Only read the live child. Publish the resulting globals in the owner after
// the ancestor archive and this collector have both completed successfully.
func descendantCollectExpected(id string) (result descendantExpectedResult) {
	result.Start = time.Now()
	defer func() { result.Done = time.Now() }()
	state := &result.State
	state.States, state.Execs = map[string]map[string]any{}, map[string]map[string]any{}
	for _, inner := range []string{"", "alpha", "beta"} {
		state.States[inner], result.Err = criuState(id, inner, "/state")
		if result.Err != nil {
			return
		}
	}
	for _, inner := range []string{"", "alpha"} {
		state.Execs[inner], result.Err = criuLiveExecState(id, inner, "/state")
		if result.Err != nil {
			return
		}
	}
	state.Compose, result.Err = criuComposeState(id, "/state")
	if result.Err != nil {
		return
	}
	state.Marker, result.Err = criuComposeMarkerRead(id)
	if result.Err != nil {
		return
	}
	browser, err := criuRunc("exec", id, "node", "/opt/browser-state.mjs", "get")
	if err != nil || strings.TrimSpace(browser) == "" {
		result.Err = fmt.Errorf("read current child browser RAM before descendant capture: %s: %v", browser, err)
		return
	}
	state.Browser = strings.TrimSpace(browser)
	state.History, result.Err = descendantOpenCodeHistory(id)
	if result.Err != nil {
		return
	}
	virtual, err := criuRunc("exec", id, "cat", "/proc/sys/kernel/panic")
	if err != nil {
		result.Err = err
		return
	}
	state.Virtual = strings.TrimSpace(virtual)
	state.Identity, result.Err = criuRuntimeIdentity(id)
	return
}
