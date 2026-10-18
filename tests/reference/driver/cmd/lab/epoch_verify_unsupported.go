//go:build !linux

package main

import "fmt"

func startEpochParentVerification(string, map[string]string) (*epochVerificationJob, error) {
	return nil, fmt.Errorf("actual frozen parent verification requires the existing Linux worker")
}
