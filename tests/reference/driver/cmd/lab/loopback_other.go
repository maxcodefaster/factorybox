//go:build !linux

package main

import "fmt"

func loopbackUp() error {
	return fmt.Errorf("sandbox loopback setup runs on the Linux worker")
}
