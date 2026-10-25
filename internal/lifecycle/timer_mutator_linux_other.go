//go:build linux && !amd64 && !arm64

package lifecycle

import "fmt"

func runTimerMutator() error {
	return fmt.Errorf("timer continuation is not qualified on this architecture")
}

func runTimerActivation() error {
	return fmt.Errorf("timer activation is not qualified on this architecture")
}
