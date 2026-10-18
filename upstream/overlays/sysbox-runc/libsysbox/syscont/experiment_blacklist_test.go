//go:build linux

package syscont

import (
	"reflect"
	"testing"

	"github.com/opencontainers/runtime-spec/specs-go"
)

func TestExperimentSeccompBlacklistKeepsCallerDenials(t *testing.T) {
	for _, action := range []specs.LinuxSeccompAction{specs.ActErrno, specs.ActKill} {
		t.Run(string(action), func(t *testing.T) {
			errno := uint(1)
			args := []specs.LinuxSeccompArg{{Index: 0, Value: 128, Op: specs.OpEqualTo}}
			profile := &specs.LinuxSeccomp{
				DefaultAction: specs.ActAllow,
				Architectures: []specs.Arch{specs.ArchAARCH64, specs.ArchX86_64},
				Syscalls: []specs.LinuxSyscall{{
					Names:  []string{"mount", "io_uring_setup", "io_uring_enter", "io_uring_register"},
					Action: action, ErrnoRet: &errno, Args: args,
				}},
			}
			if err := cfgSeccomp(profile); err != nil {
				t.Fatal(err)
			}
			if len(profile.Syscalls) != 1 {
				t.Fatalf("caller denial disappeared: %+v", profile.Syscalls)
			}
			deny := profile.Syscalls[0]
			if !reflect.DeepEqual(deny.Names, []string{"io_uring_setup", "io_uring_enter", "io_uring_register"}) ||
				deny.Action != action || deny.ErrnoRet == nil || *deny.ErrnoRet != errno || !reflect.DeepEqual(deny.Args, args) {
				t.Fatalf("caller restrictions changed beyond required Sysbox mount exception: %+v", deny)
			}
		})
	}
}
