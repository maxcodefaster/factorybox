// Package timerstate reads exact owned tasks' kernel restart state. It does
// not trace tasks, write task memory or attach a persistent event hook.
package timerstate

type Task struct {
	PID        int    `json:"pid"`
	StartTicks uint64 `json:"start_ticks"`
}

type State struct {
	Task
	BirthNS       uint64     `json:"birth_ns"`
	RelativeTimer bool       `json:"relative_timer"`
	Clock         int32      `json:"clock"`
	TimespecType  int32      `json:"timespec_type"`
	Remainder     uint64     `json:"remainder"`
	ExpiresNS     int64      `json:"expires_ns"`
	Registers     *Registers `json:"arm64_registers,omitempty"`
}

// Registers is the native ARM64 saved user context needed to distinguish a
// live frozen wait from a stale restart block. It is read without ptrace.
type Registers struct {
	Syscall    uint64 `json:"x8"`
	PC         uint64 `json:"pc"`
	Argument0  uint64 `json:"x0"`
	Argument1  uint64 `json:"x1"`
	ObservedNS int64  `json:"observed_clock_ns"`
}

type Observation struct {
	States        []State `json:"states"`
	KernelBTFHash string  `json:"kernel_btf_sha256"`
	ProgramHash   string  `json:"program_sha256"`
	// MapID is zero for the inline owned-PID filter. No kernel map is created.
	ProgramID, MapID             uint32
	ProgramReleased, MapReleased bool
}
