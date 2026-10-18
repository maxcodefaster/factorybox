//go:build !linux

package main

import "fmt"

func criuWorkerCanary(string) error {
	return fmt.Errorf("native worker canary requires Linux")
}
