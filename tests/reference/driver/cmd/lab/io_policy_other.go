//go:build !linux

package main

import "fmt"

func ioPolicyCheck() error {
	return fmt.Errorf("I/O policy proof runs on the Linux worker")
}
