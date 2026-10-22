//go:build !linux || !amd64

package timerstate

import "fmt"

func Read(tasks []Task) (Observation, error) {
	return Observation{}, fmt.Errorf("kernel timer capture is not qualified on this platform")
}
