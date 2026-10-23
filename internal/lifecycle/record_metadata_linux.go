//go:build linux

package lifecycle

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

const sandboxRecordLimit = 1 << 20
const sandboxErrorLimit = 4 << 10

// Keep the reader and atomic publisher on the same encoded-byte boundary.
// Refusal happens before a temporary file or a replacement is created.
func sandboxRecordBytes(record Record) ([]byte, error) {
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return nil, err
	}
	if len(data)+1 > sandboxRecordLimit {
		return nil, fmt.Errorf("sandbox record exceeds its 1-MiB reader limit: %d bytes", len(data)+1)
	}
	return data, nil
}

// Full errors still go to the caller and existing diagnostic evidence. Bound
// only the new durable summary; retain its prefix, original length and digest.
func sandboxErrorSummary(message string) string {
	if len(message) <= sandboxErrorLimit {
		return message
	}
	return fmt.Sprintf("%s\n[truncated error: %d bytes; sha256=%s]",
		message[:sandboxErrorLimit-160], len(message), sum([]byte(message)))
}

func recordJSONMaximum(value, bound string) string {
	current, _ := json.Marshal(value)
	maximum, _ := json.Marshal(bound)
	if len(current) > len(maximum) {
		return value
	}
	return bound
}

func recordOperationBound(value Operation) Operation {
	// Include nonzero fractional seconds and a numeric timezone offset. Public
	// operation timestamps use UTC; this also bounds runc's creation timestamp.
	const timestamp = "9999-12-31T23:59:59.999999999+23:59"
	return Operation{RequestedAt: recordJSONMaximum(value.RequestedAt, timestamp),
		CompletedAt: recordJSONMaximum(value.CompletedAt, timestamp),
		Error:       recordJSONMaximum(value.Error, strings.Repeat("\x00", sandboxErrorLimit)),
		Attempted:   false} // false has one more encoded byte than true.
}

func recordPauseBound() *PauseOperation {
	operation := recordOperationBound(Operation{})
	memory := SandboxMemory{Current: math.MaxUint64, Anon: math.MaxUint64,
		File: math.MaxUint64, Shmem: math.MaxUint64}
	return &PauseOperation{SnapshotID: strings.Repeat("f", 32), CheckpointSHA256: strings.Repeat("f", 64),
		RequestedAt: operation.RequestedAt, CompletedAt: operation.CompletedAt, RejectedAt: operation.CompletedAt,
		Error: operation.Error, SourcePID: math.MaxInt, Before: memory, After: memory,
		Snapshot: SnapshotMemory{Files: math.MaxInt, Bytes: math.MaxUint64,
			ResidentBytesBefore: math.MaxUint64, ResidentBytesAfter: math.MaxUint64, FileBacked: false},
		Resume: &operation}
}

// Admit the largest successful, failed and later resumed forms before touching
// the original transition. A resume additionally reserves its next pause, so a
// sandbox near its history limit can remain safely paused for worker replacement.
// This is a bound on one sandbox record, not on the complete worker exit ledger.
func (e *Engine) admitSandboxMetadata(record Record, afterResume bool) error {
	maximum := record
	maximum.Phase = "resuming"
	maximum.RootfsDevice, maximum.RootfsInode, maximum.RootfsMountID = math.MaxUint64, math.MaxUint64, math.MaxUint64
	maximum.CgroupParent = KernelObject{Device: math.MaxUint64, Inode: math.MaxUint64, MountID: math.MaxUint64}
	group := maximum.CgroupParent
	maximum.Cgroup = &group
	maximum.RuntimeCreated = recordJSONMaximum(record.RuntimeCreated, "9999-12-31T23:59:59.999999999+23:59")
	maximum.Start = recordOperationBound(record.Start)
	deletion := Operation{}
	if record.Delete != nil {
		deletion = *record.Delete
	}
	deletion = recordOperationBound(deletion)
	maximum.Delete = &deletion
	maximum.PauseHistory = append([]PauseOperation(nil), record.PauseHistory...)
	if record.Pause != nil {
		previous := *record.Pause
		if afterResume {
			resume := Operation{}
			if previous.Resume != nil {
				resume = *previous.Resume
			}
			resume = recordOperationBound(resume)
			previous.Resume = &resume
		}
		maximum.PauseHistory = append(maximum.PauseHistory, previous)
	}
	maximum.Pause = recordPauseBound()
	if _, err := sandboxRecordBytes(maximum); err != nil {
		return fmt.Errorf("new sandbox metadata refused: lifecycle history leaves no room for a safe pause/resume outcome: %w", err)
	}
	// A rejected capture appends this pause to history and clears Pause. The
	// different field/list encoding must fit independently of successful pause.
	maximum.PauseHistory = append(maximum.PauseHistory, *maximum.Pause)
	maximum.Pause = nil
	if _, err := sandboxRecordBytes(maximum); err != nil {
		return fmt.Errorf("new sandbox metadata refused: lifecycle history leaves no room for a safe rejected pause: %w", err)
	}
	// Guards contain the complete original record, and have a smaller packet
	// reader than the durable record reader. Bound the successful next-pause
	// source independently. Hypothetical failure summaries do not coexist with
	// a successful start/resume in that packet; already stored errors remain.
	guarded := record
	guarded.Phase = "resuming"
	guarded.RootfsDevice, guarded.RootfsInode, guarded.RootfsMountID = math.MaxUint64, math.MaxUint64, math.MaxUint64
	guarded.CgroupParent, guarded.Cgroup = maximum.CgroupParent, maximum.Cgroup
	guarded.RuntimeCreated = maximum.RuntimeCreated
	guarded.Start = recordOperationBound(record.Start)
	guarded.Start.Error = record.Start.Error
	guarded.PauseHistory = append([]PauseOperation(nil), record.PauseHistory...)
	if record.Pause != nil {
		previous := *record.Pause
		if afterResume {
			resume := Operation{}
			if previous.Resume != nil {
				resume = *previous.Resume
			}
			originalError := resume.Error
			resume = recordOperationBound(resume)
			resume.Error = originalError
			previous.Resume = &resume
		}
		guarded.PauseHistory = append(guarded.PauseHistory, previous)
	}
	guarded.Pause = recordPauseBound()
	guarded.Pause.Error, guarded.Pause.RejectedAt, guarded.Pause.Resume.Error = "", "", ""
	return e.admitGuardMetadata(guarded)
}

func (e *Engine) admitGuardMetadata(source Record) error {
	const hash = "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
	guard := captureGuardConfig{Version: 1, Source: source, PID: math.MaxInt,
		Start: strings.Repeat("9", 20), Token: strings.Repeat("f", 32),
		Evidence:   e.config.Root + "/objects/" + source.ID + "/captures/" + strings.Repeat("f", 32) + "/guard",
		Controller: math.MaxInt, Root: e.config.Root, RecordSHA: hash,
		StopSource: true, CriuSHA256: hash, Criu: e.config.Criu}
	packet, err := json.Marshal(guard)
	if err != nil || len(packet) > captureGuardPacketLimit {
		return fmt.Errorf("new sandbox metadata refused: complete source guard packet needs %d bytes, limit %d: %v",
			len(packet), captureGuardPacketLimit, err)
	}
	request, err := json.Marshal(captureRequest{Guard: guard, Criu: e.config.Criu, CriuSHA256: hash,
		TimerPlanSHA: hash, ManagerRoot: e.config.ManagerRoot})
	if err != nil || len(request) > captureRequestLimit {
		return fmt.Errorf("new sandbox metadata refused: complete capture request needs %d bytes, limit %d: %v",
			len(request), captureRequestLimit, err)
	}
	return nil
}
