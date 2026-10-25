// Package timerstate reads exact owned tasks' kernel restart state. It does
// not trace tasks, write task memory or attach a persistent event hook.
package timerstate

type Task struct {
	PID        int    `json:"pid"`
	StartTicks uint64 `json:"start_ticks"`
}

type State struct {
	Task
	BirthNS       uint64 `json:"birth_ns"`
	RelativeTimer bool   `json:"relative_timer"`
	Clock         int32  `json:"clock"`
	TimespecType  int32  `json:"timespec_type"`
	Remainder     uint64 `json:"remainder"`
	ExpiresNS     int64  `json:"expires_ns"`
}

type Observation struct {
	States        []State `json:"states"`
	KernelBTFHash string  `json:"kernel_btf_sha256"`
	ProgramHash   string  `json:"program_sha256"`
	// MapID is zero for the inline owned-PID filter. No kernel map is created.
	ProgramID, MapID             uint32
	ProgramReleased, MapReleased bool
}
