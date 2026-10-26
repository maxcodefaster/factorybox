//go:build linux

package lifecycle

import (
	"strings"
	"testing"
)

func TestSandboxBackingRetirementAdmission(t *testing.T) {
	request := SandboxBackingRequest{"source", strings.Repeat("a", 32), strings.Repeat("b", 64)}
	record := Record{ID: request.SandboxID, Owner: request.SandboxOwner, InputSHA256: request.InputSHA256,
		InputBundle: "/store/input/source", Rootfs: "/store/input/source/rootfs", InputDirectory: &KernelObject{Inode: 1},
		Phase: "deleted", Start: Operation{CompletedAt: "started"}, Delete: &Operation{Attempted: true, CompletedAt: "deleted"}}
	if err := sandboxBackingRecord(record, request); err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*Record){
		"owner":        func(r *Record) { r.Owner = strings.Repeat("c", 32) },
		"input":        func(r *Record) { r.InputSHA256 = strings.Repeat("c", 64) },
		"sandbox":      func(r *Record) { r.ID = "other" },
		"running":      func(r *Record) { r.Phase = "running" },
		"uncertain":    func(r *Record) { r.Delete = &Operation{Attempted: true} },
		"unattempted":  func(r *Record) { r.Delete = &Operation{CompletedAt: "deleted"} },
		"failed":       func(r *Record) { r.Delete = &Operation{Attempted: true, CompletedAt: "deleted", Error: "failed"} },
		"no-start":     func(r *Record) { r.Start.CompletedAt = "" },
		"unowned-root": func(r *Record) { r.Rootfs = "/foreign/rootfs" },
		"no-identity":  func(r *Record) { r.InputDirectory = nil },
	} {
		t.Run(name, func(t *testing.T) {
			changed := record
			change(&changed)
			if err := sandboxBackingRecord(changed, request); err == nil {
				t.Fatal("unsafe backing retirement was admitted")
			}
		})
	}
}
