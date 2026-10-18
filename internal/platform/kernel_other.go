//go:build !linux

package platform

func kernelRelease() (string, error) { return "", nil }
