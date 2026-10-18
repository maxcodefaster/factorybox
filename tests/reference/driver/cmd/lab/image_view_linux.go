package main

import (
	"io"
	"os"
	"path/filepath"

	"runtime-fork-experiment/pageview"
)

func criuLogicalImage(file *os.File) (io.Reader, func() error, error) {
	view, err := pageview.OpenAt("/experiment/pipe-store/read.sock", file)
	if err != nil {
		return nil, nil, err
	}
	if view == nil {
		return file, func() error { return nil }, nil
	}
	return view, view.Close, nil
}

func criuLogicalRetireDirectory(images, backing string) error {
	entries, err := os.ReadDir(images)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !entry.Type().IsRegular() {
			continue
		}
		original, err := os.Open(filepath.Join(images, entry.Name()))
		if err != nil {
			return err
		}
		marked, err := pageview.Captured(original)
		if err != nil {
			original.Close()
			return err
		}
		if !marked {
			original.Close()
			continue
		}
		target, err := os.Open(filepath.Join(backing, entry.Name()))
		if err != nil {
			original.Close()
			return err
		}
		err = pageview.RetireAt("/experiment/pipe-store/read.sock", original, target)
		target.Close()
		original.Close()
		if err != nil {
			return err
		}
	}
	return nil
}
