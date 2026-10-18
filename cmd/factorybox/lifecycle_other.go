//go:build !linux

package main

import "fmt"

func runLifecycle([]string) error {
	return fmt.Errorf("usage: factorybox version|doctor; lifecycle commands execute on the qualified Linux worker")
}
