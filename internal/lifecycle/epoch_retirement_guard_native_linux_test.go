//go:build linux

package lifecycle

import (
	"errors"
	"os"
	"testing"
)

type epochAdmissionNativeEvidence struct {
	Guard   string            `json:"guard"`
	SHA256  map[string]string `json:"sha256"`
	Created map[string]bool   `json:"fixture_created"`
}

// Force both optional admission receipts through the complete public native
// retirement, including interruption and replacement. Fixture-created receipts
// exercise metadata retention; they do not claim an actual admission wait.
func epochAdmissionNativePrepare(t *testing.T) {
	t.Helper()
	var pause boundedLossPause
	epochFenceRead(t, "bounded-loss-pause.json", &pause)
	engine, err := Open(boundedLossStatusNow(t).Lifecycle)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	relative, err := checkpointPath(pause.Request.SourceID, pause.Request.SnapshotID)
	if err != nil {
		t.Fatal(err)
	}
	guard := relative + "/guard"
	var config captureGuardConfig
	if err := engine.readCheckpointJSON(guard+"/guard-intent.json", &config); err != nil {
		t.Fatal(err)
	}
	anchor, err := engine.root.Open(guard)
	if err != nil {
		t.Fatal(err)
	}
	defer anchor.Close()
	evidence := epochAdmissionNativeEvidence{Guard: guard, SHA256: map[string]string{}, Created: map[string]bool{}}
	for name, observation := range map[string]any{
		"descriptor-admission.json": captureDescriptorAdmission{Version: 1, Token: config.Token,
			SourcePID: config.PID, SourceStart: config.Start, Attempts: 2, Passed: true,
			First: []captureDescriptorBlocker{{TID: config.PID, FD: 7, Kind: "foreign-proc-directory"}}},
		"timer-admission.json": captureTimerAdmission{Version: 1, Token: config.Token,
			SourcePID: config.PID, SourceStart: config.Start, Attempts: 2, Passed: true,
			First: &timerPendingCaughtSignal{TID: config.PID, Pending: 1, Caught: 1}},
	} {
		data, err := engine.root.ReadFile(guard + "/" + name)
		if errors.Is(err, os.ErrNotExist) {
			if err := captureGuardWrite(anchor, name, observation); err != nil {
				t.Fatal(err)
			}
			evidence.Created[name] = true
			data, err = engine.root.ReadFile(guard + "/" + name)
		}
		if err != nil {
			t.Fatal(err)
		}
		evidence.SHA256[name] = sum(data)
	}
	boundedLossSave(t, "epoch-retirement-admission.json", evidence)
}

func epochAdmissionNativeVerify(t *testing.T, old *Engine) {
	t.Helper()
	var evidence epochAdmissionNativeEvidence
	epochFenceRead(t, "epoch-retirement-admission.json", &evidence)
	if len(evidence.SHA256) != 2 {
		t.Fatal("native admission metadata fixture is incomplete")
	}
	for name, expected := range evidence.SHA256 {
		data, err := old.root.ReadFile(evidence.Guard + "/" + name)
		if err != nil || sum(data) != expected {
			t.Fatal("epoch retirement changed retained admission evidence", name, err)
		}
	}
	boundedLossSave(t, "epoch-retirement-admission-retained.json", map[string]any{"passed": true,
		"both_optional_receipts_preserved": true, "fixture": evidence})
}
