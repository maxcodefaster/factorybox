//go:build !linux

package main

import "fmt"

var criuEpochResumedSourceIdentity map[string]any

func criuEpochBackedSourceResume(string, string, map[int]string) error {
	return fmt.Errorf("backed source resume requires the existing Linux worker")
}

func criuEpochBackedImagesUnchanged() error {
	return fmt.Errorf("backed image proof requires the existing Linux worker")
}
