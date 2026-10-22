//go:build linux

package providerfixture

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// This verifies private filesystem IPC only. A fake reply is never evidence
// that a kernel process exited; the separate native host peer must prove that.
func operator(ctx context.Context, directory string, wrongRelease bool) <-chan error {
	done := make(chan error, 1)
	go func() {
		seen := map[string]bool{}
		for len(seen) < 2 {
			paths, err := filepath.Glob(filepath.Join(directory, "provider-request-*.json"))
			if err != nil {
				done <- err
				return
			}
			for _, path := range paths {
				if seen[path] {
					continue
				}
				body, err := os.ReadFile(path)
				if err != nil {
					done <- err
					return
				}
				var request Request
				if err := json.Unmarshal(body, &request); err != nil {
					done <- err
					return
				}
				info, err := os.Stat(path)
				if err != nil || info.Mode().Perm() != 0600 {
					done <- fmt.Errorf("request is not private: %v", err)
					return
				}
				reply := Reply{Request: request, Accepted: true, Stopped: request.Phase == "release", PID: 456, Birth: "synthetic-birth",
					SHA256: strings.Repeat("c", 64), Network: "net:[unit]"}
				if wrongRelease && reply.Stopped {
					reply.PID = 789
				}
				data, _ := json.Marshal(reply)
				target := filepath.Join(directory, "provider-reply-"+request.Nonce+"-"+request.Phase+".json")
				if err := os.WriteFile(target+".partial", data, 0600); err != nil {
					done <- err
					return
				}
				if err := os.Rename(target+".partial", target); err != nil {
					done <- err
					return
				}
				seen[path] = true
			}
			select {
			case <-ctx.Done():
				done <- ctx.Err()
				return
			case <-time.After(time.Millisecond):
			}
		}
		done <- nil
	}()
	return done
}

func TestPrivateGatewayLeaseRequiresItsOriginalReleaseIdentity(t *testing.T) {
	for _, wrong := range []bool{false, true} {
		t.Run(fmt.Sprint("wrong-release-", wrong), func(t *testing.T) {
			directory := t.TempDir()
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			done := operator(ctx, directory, wrong)
			target := Target{RunOwner: strings.Repeat("a", 32), Sandbox: "source", Owner: strings.Repeat("b", 32),
				Runtime: "owned-runtime", PID: 123, Created: "synthetic"}
			lease, err := Acquire(ctx, directory, "arm", target)
			if err != nil {
				t.Fatal(err)
			}
			err = lease.Close(ctx)
			if wrong {
				if err == nil || lease.closed {
					t.Fatal("another process's release was accepted")
				}
			} else {
				if err != nil || !lease.closed {
					t.Fatal("original release was lost", err)
				}
				if err := lease.Close(ctx); err != nil {
					t.Fatal("completed own release was not recognized", err)
				}
			}
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			requests, _ := filepath.Glob(filepath.Join(directory, "provider-request-*.json"))
			if len(requests) != 2 {
				t.Fatal("lease reissued or omitted an operation", requests)
			}
		})
	}
}
