//go:build linux

package lifecycle

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"

	"factorybox/internal/timerstate"
	"golang.org/x/sys/unix"
)

const timerPlanLimit = 16 << 20

type captureTimerPlan struct {
	Version     int                    `json:"version"`
	Token       string                 `json:"token"`
	Threads     map[int]string         `json:"threads"`
	Observation timerstate.Observation `json:"observation"`
	Targets     []timerstate.State     `json:"targets"`
}

type timerMutatorReady struct {
	Token       string `json:"token"`
	ParentPID   int    `json:"parent_pid"`
	ParentStart string `json:"parent_start"`
	PID         int    `json:"pid"`
	TID         int    `json:"tid"`
	Start       string `json:"start"`
}

type timerPendingCaughtSignal struct {
	TID     int    `json:"tid"`
	Pending uint64 `json:"pending"`
	Blocked uint64 `json:"blocked"`
	Caught  uint64 `json:"caught"`
}

func (signal *timerPendingCaughtSignal) Error() string {
	return fmt.Sprintf("relative timer has an unprocessed caught signal (tid %d, pending %#x, blocked %#x, caught %#x); source continuation remains unconfirmed",
		signal.TID, signal.Pending, signal.Blocked, signal.Caught)
}

func (guard *captureGuard) admitCheckpointTimers() error {
	if err := captureGuardValidate(guard.config, guard.anchor); err != nil {
		return err
	}
	events, err := captureGuardEvents(guard.anchor.group)
	if err != nil || events["frozen"] != 1 {
		return fmt.Errorf("timer admission requires the original frozen source: %v", err)
	}
	threads, err := captureGuardThreads(guard.config, guard.anchor)
	if err != nil {
		return err
	}
	plan := captureTimerPlan{Version: 1, Token: guard.config.Token, Threads: threads}
	var candidates []timerstate.Task
	for tid, birth := range threads {
		data, err := os.ReadFile(fmt.Sprintf("/proc/%d/syscall", tid))
		if err != nil {
			return err
		}
		fields := strings.Fields(string(data))
		if len(fields) == 0 {
			return fmt.Errorf("empty frozen task syscall state")
		}
		if fields[0] != strconv.Itoa(unix.SYS_NANOSLEEP) &&
			fields[0] != strconv.Itoa(unix.SYS_CLOCK_NANOSLEEP) &&
			fields[0] != strconv.Itoa(unix.SYS_RESTART_SYSCALL) {
			continue
		}
		start, err := strconv.ParseUint(birth, 10, 64)
		if err != nil {
			return err
		}
		candidates = append(candidates, timerstate.Task{PID: tid, StartTicks: start})
	}
	if len(candidates) != 0 {
		sort.Slice(candidates, func(i, j int) bool { return candidates[i].PID < candidates[j].PID })
		plan.Observation, err = timerstate.Read(candidates)
		if err != nil {
			return err
		}
		if !plan.Observation.ProgramReleased || !plan.Observation.MapReleased {
			return fmt.Errorf("timer admission left a live kernel reader")
		}
		for _, state := range plan.Observation.States {
			if !state.RelativeTimer {
				continue
			}
			if (state.Clock != 1 && state.Clock != 7) || state.ExpiresNS <= 0 ||
				(state.TimespecType != 0 && state.TimespecType != 1) ||
				(state.TimespecType == 0) != (state.Remainder == 0) {
				return fmt.Errorf("unsupported native relative timer restart state for TID %d", state.PID)
			}
			if err := timerNoPendingCaughtSignal(state.PID); err != nil {
				return err
			}
			plan.Targets = append(plan.Targets, state)
		}
	}
	if err := captureGuardValidate(guard.config, guard.anchor); err != nil {
		return err
	}
	if err := captureGuardWrite(guard.anchor.evidence, "timer-state-plan.json", plan); err != nil {
		return err
	}
	data, err := captureReadFile(guard.anchor.evidence, "timer-state-plan.json", timerPlanLimit)
	if err != nil {
		return err
	}
	guard.timerPlanSHA = sum(data)
	return nil
}

func readTimerPlan(request captureRequest, anchor captureGuardAnchors) (captureTimerPlan, error) {
	var plan captureTimerPlan
	if !digest.MatchString(request.TimerPlanSHA) {
		return plan, fmt.Errorf("capture has no exact timer admission plan")
	}
	data, err := captureReadFile(anchor.evidence, "timer-state-plan.json", timerPlanLimit)
	if err != nil || sum(data) != request.TimerPlanSHA {
		return plan, fmt.Errorf("frozen timer plan changed: %v", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&plan); err != nil {
		return plan, err
	}
	if plan.Version != 1 || plan.Token != request.Guard.Token {
		return plan, fmt.Errorf("timer plan differs from exact capture")
	}
	return plan, nil
}

func timerNoPendingCaughtSignal(tid int) error {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", tid))
	if err != nil {
		return err
	}
	values := map[string]uint64{}
	for _, row := range strings.Split(string(data), "\n") {
		fields := strings.Fields(row)
		if len(fields) != 2 {
			continue
		}
		switch fields[0] {
		case "SigPnd:", "ShdPnd:", "SigBlk:", "SigCgt:":
			value, err := strconv.ParseUint(fields[1], 16, 64)
			if err != nil {
				return err
			}
			values[fields[0]] = value
		}
	}
	if len(values) != 4 {
		return fmt.Errorf("timer task signal state is incomplete")
	}
	if (values["SigPnd:"]|values["ShdPnd:"])&values["SigCgt:"] & ^values["SigBlk:"] != 0 {
		return &timerPendingCaughtSignal{TID: tid, Pending: values["SigPnd:"] | values["ShdPnd:"],
			Blocked: values["SigBlk:"], Caught: values["SigCgt:"]}
	}
	return nil
}

func readCaptureTimerRequest() (captureRequest, captureGuardAnchors, *os.File, error) {
	var request captureRequest
	file := os.NewFile(3, "sealed timer capture request")
	anchors := captureGuardAnchors{os.NewFile(4, "timer source cgroup"), os.NewFile(5, "timer original pidfd"),
		os.NewFile(6, "timer evidence"), os.NewFile(7, "timer controller root")}
	seals, err := unix.FcntlInt(file.Fd(), unix.F_GET_SEALS, 0)
	if err != nil || seals&captureRequestSeals != captureRequestSeals {
		return request, anchors, file, fmt.Errorf("timer request is not sealed: %v", err)
	}
	data, err := io.ReadAll(io.NewSectionReader(file, 0, captureRequestLimit+1))
	if err != nil || len(data) > captureRequestLimit {
		return request, anchors, file, fmt.Errorf("timer request exceeds bound: %v", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		return request, anchors, file, err
	}
	if err := captureGuardValidate(request.Guard, anchors); err != nil {
		return request, anchors, file, err
	}
	return request, anchors, file, nil
}

func prepareCaptureTimers(request captureRequest, anchors captureGuardAnchors, sealed, guardControl *os.File) error {
	plan, err := readTimerPlan(request, anchors)
	if err != nil {
		return err
	}
	if len(plan.Targets) == 0 {
		return nil
	}
	sockets, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_SEQPACKET|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		return err
	}
	client, child := os.NewFile(uintptr(sockets[0]), "timer mutation control"), os.NewFile(uintptr(sockets[1]), "timer mutation child control")
	defer client.Close()
	defer child.Close()
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	command := exec.CommandContext(ctx, executable, "_timer-prepare")
	command.Env = []string{"LANG=C", "GOMAXPROCS=1", "GOMEMLIMIT=64MiB"}
	command.ExtraFiles = []*os.File{sealed, anchors.group, anchors.pid, anchors.evidence, anchors.root, child}
	command.Stdout, command.Stderr = os.Stderr, os.Stderr
	if err := command.Start(); err != nil {
		return err
	}
	waited := false
	defer func() {
		if !waited {
			_ = command.Process.Kill()
			_ = command.Wait()
		}
	}()
	data, err := captureGuardRead(int(client.Fd()), 2000)
	if err != nil {
		return err
	}
	var ready timerMutatorReady
	if json.Unmarshal(data, &ready) != nil || ready.Token != request.Guard.Token || ready.PID != command.Process.Pid || ready.ParentPID != os.Getpid() {
		return fmt.Errorf("timer mutator readiness differs from its original child")
	}
	if err := captureGuardWrite(anchors.evidence, "timer-mutator-ready.json", ready); err != nil {
		return err
	}
	// FD 9 is the original source guard's control endpoint. The controller is
	// waiting for this worker and does not concurrently use that endpoint.
	if _, err := guardControl.Write([]byte("timer-watch:" + request.Guard.Token)); err != nil {
		return err
	}
	ack, err := captureGuardRead(int(guardControl.Fd()), 2000)
	if err != nil || string(ack) != "timer-watched:"+request.Guard.Token {
		return fmt.Errorf("independent timer exit guard did not arm: %s: %v", ack, err)
	}
	if _, err := client.Write([]byte("prepare:" + request.Guard.Token)); err != nil {
		return err
	}
	err = command.Wait()
	waited = true
	if err != nil {
		return fmt.Errorf("guarded timer preparation: %w", err)
	}
	data, err = captureReadFile(anchors.evidence, "timer-prepare-complete.json", timerPlanLimit)
	if err != nil {
		return err
	}
	var complete struct {
		Token  string `json:"token"`
		Passed bool   `json:"passed"`
	}
	if json.Unmarshal(data, &complete) != nil || complete.Token != request.Guard.Token || !complete.Passed {
		return fmt.Errorf("timer preparation has no exact completed source hold")
	}
	return captureGuardCaptureReturned(request.Guard, anchors)
}

// The second child has a distinct independent exit watch. The first preparer
// has already exited and all CRIU tracees have returned to their frozen group.
func activateCaptureTimers(request captureRequest, anchors captureGuardAnchors, sealed, guardControl *os.File) error {
	if err := captureGuardCaptureReturned(request.Guard, anchors); err != nil {
		return err
	}
	complete, err := captureTimerActivationComplete(request.Guard, anchors)
	if err != nil || complete {
		return err
	}
	birth, err := processBirth(os.Getpid())
	if err != nil {
		return err
	}
	// Activation can belong to the capture worker or to its original caller
	// after verified process rollback. Preserve the first worker's evidence.
	if err := captureGuardWrite(anchors.evidence, "timer-activation-worker-intent.json", captureWorkerIntent{
		Token: request.Guard.Token, PID: os.Getpid(), Start: birth, SourcePID: request.Guard.PID,
		CriuSHA256: request.Guard.CriuSHA256, Scope: "guarded source timer activation"}); err != nil {
		return err
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	sockets, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_SEQPACKET|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		return err
	}
	client, child := os.NewFile(uintptr(sockets[0]), "timer activation controller"), os.NewFile(uintptr(sockets[1]), "timer activation child")
	defer client.Close()
	defer child.Close()
	command := exec.Command(executable, "_timer-activate")
	command.Env = []string{"LANG=C", "GOMAXPROCS=1", "GOMEMLIMIT=64MiB"}
	command.ExtraFiles = []*os.File{sealed, anchors.group, anchors.pid, anchors.evidence, anchors.root, child}
	command.Stdout, command.Stderr = os.Stderr, os.Stderr
	if err := command.Start(); err != nil {
		return err
	}
	waited := false
	defer func() {
		if !waited {
			_ = command.Process.Kill()
			_ = command.Wait()
		}
	}()
	data, err := captureGuardRead(int(client.Fd()), 2000)
	var ready timerMutatorReady
	if err != nil || json.Unmarshal(data, &ready) != nil || ready.Token != request.Guard.Token || ready.PID != command.Process.Pid || ready.ParentPID != os.Getpid() {
		return fmt.Errorf("activation readiness differs from its original child: %v", err)
	}
	if err := captureGuardWrite(anchors.evidence, "timer-activation-mutator-ready.json", ready); err != nil {
		return err
	}
	if _, err := guardControl.Write([]byte("timer-activation-watch:" + request.Guard.Token)); err != nil {
		return err
	}
	ack, err := captureGuardRead(int(guardControl.Fd()), 2000)
	if err != nil || string(ack) != "timer-activation-watched:"+request.Guard.Token {
		return fmt.Errorf("activation exit guard did not arm: %s: %v", ack, err)
	}
	if _, err := client.Write([]byte("activate:" + request.Guard.Token)); err != nil {
		return err
	}
	err = command.Wait()
	waited = true
	if err != nil {
		return fmt.Errorf("guarded timer activation: %w", err)
	}
	data, err = captureReadFile(anchors.evidence, "timer-activation-complete.json", timerPlanLimit)
	var completion struct {
		Token  string `json:"token"`
		Passed bool   `json:"passed"`
	}
	if err != nil || json.Unmarshal(data, &completion) != nil || completion.Token != request.Guard.Token || !completion.Passed {
		return fmt.Errorf("activation lacks its exact completed hold: %v", err)
	}
	return captureGuardCaptureReturned(request.Guard, anchors)
}

func captureTimerActivationComplete(config captureGuardConfig, anchors captureGuardAnchors) (bool, error) {
	data, err := captureReadFile(anchors.evidence, "timer-activation-complete.json", timerPlanLimit)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var complete struct {
		Version  int             `json:"version"`
		Token    string          `json:"token"`
		Passed   bool            `json:"passed"`
		Frozen   bool            `json:"source_frozen"`
		Detached bool            `json:"tracers_detached"`
		CodeSHA  string          `json:"code_sha256"`
		Recovery json.RawMessage `json:"recovery,omitempty"`
	}
	if json.Unmarshal(data, &complete) != nil || complete.Version != 1 || complete.Token != config.Token ||
		!complete.Passed || !complete.Frozen || !complete.Detached || !digest.MatchString(complete.CodeSHA) {
		return false, fmt.Errorf("timer activation receipt does not confirm its original frozen transaction")
	}
	if len(complete.Recovery) != 0 {
		if err := verifyTimerRecoveryCompletion(config, anchors, complete.CodeSHA, complete.Recovery); err != nil {
			return false, err
		}
	}
	return true, nil
}

// A process/network cure alone does not restore a staged timer. In particular,
// controller or guard loss must not authorize its old continuation to execute.
func captureGuardTimersSafe(config captureGuardConfig, anchors captureGuardAnchors) error {
	complete, err := captureTimerActivationComplete(config, anchors)
	if err != nil || complete {
		return err
	}
	for _, name := range []string{"timer-prepare-intent.json", "timer-activation-intent.json"} {
		if _, err := captureReadFile(anchors.evidence, name, timerPlanLimit); err == nil {
			return fmt.Errorf("timer mutation remains unconfirmed; original source must remain held")
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func (guard *captureGuard) activateTimers() error {
	request := captureRequest{Guard: guard.config}
	data, err := json.Marshal(request)
	if err != nil {
		return err
	}
	fd, err := unix.MemfdCreate("factorybox-source-timer-request", unix.MFD_CLOEXEC|unix.MFD_ALLOW_SEALING)
	if err != nil {
		return err
	}
	sealed := os.NewFile(uintptr(fd), "sealed source timer request")
	defer sealed.Close()
	if _, err := sealed.Write(data); err != nil {
		return err
	}
	if _, err := unix.FcntlInt(sealed.Fd(), unix.F_ADD_SEALS, captureRequestSeals); err != nil {
		return err
	}
	return activateCaptureTimers(request, guard.anchor, sealed, guard.socket)
}

func (e *Engine) activateRestoredTimers(record Record, pid int) (resultErr error) {
	guard, err := e.startCaptureGuard(record, pid)
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
	if err := guard.freeze(false); err != nil {
		return err
	}
	resultErr = guard.finish(false)
	finished = true
	return resultErr
}

// TimerWorkerCommand is a private sealed-descriptor entry point, like the
// capture worker. The public CLI does not construct requests from user paths.
func TimerWorkerCommand(args []string) (bool, error) {
	if len(args) == 1 && args[0] == "_timer-activate" {
		return true, runTimerActivation()
	}
	if len(args) != 1 || args[0] != "_timer-prepare" {
		return false, nil
	}
	return true, runTimerMutator()
}
