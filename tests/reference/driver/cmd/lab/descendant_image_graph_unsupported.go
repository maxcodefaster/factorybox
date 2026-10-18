//go:build !linux

package main

import "fmt"

func criuCloneInheritedImageGraph(_ []string) error {
	return fmt.Errorf("independent inherited image graphs require the tested Linux worker")
}

func criuCloneInheritedImageGraphPolicy(_ []string, _ bool) error {
	return fmt.Errorf("independent inherited image graphs require the tested Linux worker")
}

func criuCompileRangeSnapshot(_ []string) error {
	return fmt.Errorf("complete range snapshots require the tested Linux worker")
}

func criuCompileRangeSnapshotPolicy(_ []string, _ bool) error {
	return fmt.Errorf("complete range snapshots require the tested Linux worker")
}

func criuCompileRangeSnapshotLeasedInputs(_ []string) error {
	return fmt.Errorf("complete range snapshots require the tested Linux worker")
}

func criuPrepareStreamOriginal(_ []string) error {
	return fmt.Errorf("stream original writers require the tested Linux worker")
}
