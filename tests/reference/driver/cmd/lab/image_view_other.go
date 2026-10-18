//go:build !linux

package main

import (
	"io"
	"os"
)

func criuLogicalImage(file *os.File) (io.Reader, func() error, error) {
	return file, func() error { return nil }, nil
}
