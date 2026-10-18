//go:build !linux

package main

import "fmt"

func criuInheritedOracleCommand(_ string, _ []string) error {
	return fmt.Errorf("same-capture memory proof requires the tested Linux worker")
}
