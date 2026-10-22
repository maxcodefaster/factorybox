//go:build linux && !amd64

package lifecycle

import "fmt"

func runTimerMutator() error {
	return fmt.Errorf("timer continuation is not qualified on this architecture")
}
