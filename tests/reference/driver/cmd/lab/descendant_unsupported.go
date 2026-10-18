//go:build !linux

package main

import "fmt"

func criuDescendantForkProof() error {
	return fmt.Errorf("full descendant proof requires the native Linux worker")
}
