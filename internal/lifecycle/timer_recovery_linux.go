//go:build linux

package lifecycle

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"

	"golang.org/x/sys/unix"
)

type timerRecoveryProof struct {
	CaptureID  string            `json:"capture_id"`
	ActorPID   int               `json:"actor_pid"`
	ActorStart string            `json:"actor_start"`
	Witness    map[string]string `json:"witness_sha256"`
}

var timerRecoveryOriginalFiles = []string{
	"criu-retained-arm.json", "criu-retained-request.json", "criu-retained-result.json",
	"capture-network-rollback.json", "capture-process-restored.json",
	"timer-prepare-intent.json", "timer-prepare-complete.json",
}

var timerRecoveryPhaseFiles = []string{
	"guard-intent.json", "guard-armed.json", "guard-capture-entered.json", "guard-capture-returned.json",
	"guard-outcome.json", "guard-timer-activation-watched.json", "timer-activation-worker-intent.json",
	"timer-activation-mutator-ready.json", "timer-activation-mutator-exit-held.json",
	"timer-activation-intent.json", "timer-activation-complete.json",
}

func timerRecoveryEvidence(config captureGuardConfig, anchors captureGuardAnchors, id string) (*os.File, error) {
	if anchors.root == nil || !tokenID.MatchString(id) || id == config.Token {
		return nil, fmt.Errorf("timer recovery lacks its distinct rooted guard")
	}
	object, err := objectPath(config.Source.ID)
	if err != nil {
		return nil, err
	}
	fd, err := unix.Openat2(int(anchors.root.Fd()), filepath.Join(object, "captures", id, "guard"), &unix.OpenHow{
		Flags:   unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC,
		Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS,
	})
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), "completed timer recovery guard"), nil
}

func timerRecoveryWitness(config captureGuardConfig, anchors captureGuardAnchors, phase *os.File) (map[string]string, error) {
	witness := map[string]string{}
	for prefix, input := range map[string]struct {
		file  *os.File
		names []string
	}{
		"original/":   {anchors.evidence, timerRecoveryOriginalFiles},
		"activation/": {phase, timerRecoveryPhaseFiles},
	} {
		for _, name := range input.names {
			data, err := captureReadFile(input.file, name, timerPlanLimit)
			if err != nil {
				return nil, err
			}
			witness[prefix+name] = sum(data)
		}
	}
	object, err := objectPath(config.Source.ID)
	if err != nil {
		return nil, err
	}
	data, err := captureCompletionBytes(anchors.root, filepath.Join(object, "captures", config.Token, "source-state.json"), checkpointManifestLimit)
	if err != nil {
		return nil, err
	}
	witness["source/source-state.json"] = sum(data)
	return witness, nil
}

func timerRecoveryAuthority(config captureGuardConfig, anchors captureGuardAnchors, pid int, birth string) error {
	arm, worker, err := captureRetainedRecords(config, anchors)
	if err != nil {
		return err
	}
	if (pid != arm.GuardPID || birth != arm.GuardStart) && (pid != arm.ControllerPID || birth != arm.ControllerStart) {
		return fmt.Errorf("timer recovery is not owned by the original guard or controller")
	}
	if err := captureRetainedResult(config, anchors, arm); err != nil {
		return err
	}
	var restored captureProcessRestoration
	if err := captureCompletionRead(anchors.evidence, "capture-process-restored.json", &restored); err != nil {
		return err
	}
	hashes, err := captureRetainedHashes(anchors)
	if err != nil || restored.Version != 1 || restored.Worker != worker || restored.SourceStart != config.Start ||
		restored.ArmSHA != hashes[0] || restored.RequestSHA != hashes[1] || restored.ResultSHA != hashes[2] || restored.NetworkSHA != hashes[3] {
		return fmt.Errorf("timer recovery lacks the exact completed retained cure: %v", err)
	}
	var complete struct {
		Version      int               `json:"version"`
		Token        string            `json:"token"`
		Passed       bool              `json:"passed"`
		Frozen       bool              `json:"source_frozen"`
		Detached     bool              `json:"tracers_detached"`
		Targets      []json.RawMessage `json:"targets"`
		CodeSHA      string            `json:"code_sha256"`
		PrivateBytes int               `json:"private_bytes_per_new_target"`
	}
	if err := captureCompletionReadLimit(anchors.evidence, "timer-prepare-complete.json", &complete, timerPlanLimit); err != nil {
		return err
	}
	if complete.Version != 1 || complete.Token != config.Token || !complete.Passed || !complete.Frozen || !complete.Detached || len(complete.Targets) == 0 {
		return fmt.Errorf("retained timer preparation is not complete")
	}
	if _, err := captureReadFile(anchors.evidence, "timer-activation-intent.json", timerPlanLimit); !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("an already attempted original timer activation remains uncertain: %v", err)
	}
	return nil
}

func verifyTimerRecoveryCompletion(config captureGuardConfig, anchors captureGuardAnchors, codeSHA string, raw json.RawMessage) error {
	var proof timerRecoveryProof
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&proof); err != nil {
		return err
	}
	if proof.ActorPID <= 1 || proof.ActorStart == "" || !tokenID.MatchString(proof.CaptureID) || proof.CaptureID == config.Token {
		return fmt.Errorf("timer recovery completion has invalid original authority")
	}
	if err := timerRecoveryAuthority(config, anchors, proof.ActorPID, proof.ActorStart); err != nil {
		return err
	}
	phase, err := timerRecoveryEvidence(config, anchors, proof.CaptureID)
	if err != nil {
		return err
	}
	defer phase.Close()
	witness, err := timerRecoveryWitness(config, anchors, phase)
	if err != nil || !reflect.DeepEqual(witness, proof.Witness) {
		return fmt.Errorf("timer recovery evidence changed: %v", err)
	}
	var original captureProcessRestoration
	if err := captureCompletionRead(anchors.evidence, "capture-process-restored.json", &original); err != nil {
		return err
	}
	if original.SourceStateSHA != witness["source/source-state.json"] {
		return fmt.Errorf("timer recovery changed its retained source state")
	}
	var child captureGuardConfig
	if err := captureCompletionRead(phase, "guard-intent.json", &child); err != nil {
		return err
	}
	object, _ := objectPath(config.Source.ID)
	if child.Version != 1 || child.TimerRecoveryOf != config.Token || child.Token != proof.CaptureID ||
		child.PID != config.PID || child.Start != config.Start || child.Controller != proof.ActorPID ||
		child.Root != config.Root || child.RecordSHA != config.RecordSHA || !reflect.DeepEqual(child.Source, config.Source) ||
		child.StopSource || child.Criu != "" || child.CriuSHA256 != "" ||
		child.Evidence != filepath.Join(config.Root, object, "captures", proof.CaptureID, "guard") {
		return fmt.Errorf("timer activation guard differs from its original retained source")
	}
	var outcome struct {
		Config   captureGuardConfig `json:"config"`
		Reason   string             `json:"reason"`
		Status   string             `json:"status"`
		InFlight bool               `json:"capture_in_flight"`
		Passed   bool               `json:"passed"`
		Error    string             `json:"error,omitempty"`
	}
	if err := captureCompletionRead(phase, "guard-outcome.json", &outcome); err != nil {
		return err
	}
	if !reflect.DeepEqual(outcome.Config, child) || !outcome.Passed || outcome.InFlight || outcome.Error != "" ||
		outcome.Reason != "controller_disarmed_held" || outcome.Status != "source_held_guard_disarmed" {
		return fmt.Errorf("timer recovery guard did not complete while retaining its source hold")
	}
	var complete struct {
		Version  int             `json:"version"`
		Token    string          `json:"token"`
		Passed   bool            `json:"passed"`
		Frozen   bool            `json:"source_frozen"`
		Detached bool            `json:"tracers_detached"`
		CodeSHA  string          `json:"code_sha256"`
		Recovery json.RawMessage `json:"recovery,omitempty"`
		Targets  json.RawMessage `json:"targets,omitempty"`
	}
	completeData, err := captureReadFile(phase, "timer-activation-complete.json", timerPlanLimit)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(completeData, &complete); err != nil {
		return err
	}
	if complete.Version != 1 || complete.Token != child.Token || !complete.Passed || !complete.Frozen || !complete.Detached ||
		complete.CodeSHA != codeSHA || len(complete.Recovery) != 0 {
		return fmt.Errorf("timer recovery has no independent complete native activation")
	}
	return nil
}

// Only a fully authorized retained process cure may start a new activation
// phase. The separate existing guard watches its mutator and closes with the
// source still frozen. Missing grants and interrupted mutations keep the old
// fail-closed behavior; the original operation is never captured again.
func recoverCuredCaptureTimers(config captureGuardConfig, anchors captureGuardAnchors) (resultErr error) {
	if err := captureGuardTimersSafe(config, anchors); err == nil {
		return nil
	}
	if _, err := captureReadFile(anchors.evidence, "timer-activation-complete.json", timerPlanLimit); !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("original timer completion cannot be replaced: %v", err)
	}
	birth, err := processBirth(os.Getpid())
	if err != nil {
		return err
	}
	if err := timerRecoveryAuthority(config, anchors, os.Getpid(), birth); err != nil {
		return err
	}
	if _, err := captureGuardRestoredReceipt(config, anchors); err != nil {
		return err
	}
	if err := captureGuardCaptureReturned(config, anchors); err != nil {
		return err
	}
	root, err := os.OpenRoot(fmt.Sprintf("/proc/self/fd/%d", anchors.root.Fd()))
	if err != nil {
		return err
	}
	defer root.Close()
	engine := &Engine{config: Config{Root: config.Root}, root: root}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	guard, err := engine.startCheckpointGuardMode(config.Source, config.PID, executable, "", nil, config.Token)
	if err != nil {
		return err
	}
	finished := false
	defer func() {
		if !finished {
			resultErr = errors.Join(resultErr, guard.finish(true))
		}
	}()
	if err := guard.capturePhase(true); err != nil {
		return err
	}
	if err := guard.activateTimers(); err != nil {
		return err
	}
	if err := guard.capturePhase(false); err != nil {
		return err
	}
	finished = true
	if err := guard.finishRequest("disarm-held:"); err != nil {
		return err
	}
	phase, err := timerRecoveryEvidence(config, anchors, guard.config.Token)
	if err != nil {
		return err
	}
	defer phase.Close()
	witness, err := timerRecoveryWitness(config, anchors, phase)
	if err != nil {
		return err
	}
	proof := timerRecoveryProof{CaptureID: guard.config.Token, ActorPID: os.Getpid(), ActorStart: birth, Witness: witness}
	data, err := captureReadFile(phase, "timer-activation-complete.json", timerPlanLimit)
	if err != nil {
		return err
	}
	var completion map[string]json.RawMessage
	if err := json.Unmarshal(data, &completion); err != nil {
		return err
	}
	completion["token"], _ = json.Marshal(config.Token)
	completion["recovery"], err = json.Marshal(proof)
	if err != nil {
		return err
	}
	var codeSHA string
	if err := json.Unmarshal(completion["code_sha256"], &codeSHA); err != nil {
		return err
	}
	if err := verifyTimerRecoveryCompletion(config, anchors, codeSHA, completion["recovery"]); err != nil {
		return err
	}
	if err := captureGuardCaptureReturned(config, anchors); err != nil {
		return err
	}
	return captureGuardWrite(anchors.evidence, "timer-activation-complete.json", completion)
}
