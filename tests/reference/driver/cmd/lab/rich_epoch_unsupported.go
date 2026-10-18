//go:build !linux

package main

import "fmt"

func criuRichEpochRebase(string, string, map[int]string) error {
	return fmt.Errorf("source epoch proof requires the existing Linux worker")
}
