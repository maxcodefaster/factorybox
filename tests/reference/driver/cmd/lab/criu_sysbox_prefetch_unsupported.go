//go:build !linux

package main

import (
	"fmt"
	"os"
)

func criuStartEagerPrefetch(images string) (func() error, error) {
	if os.Getenv("CRIU_SYSBOX_EAGER_PREFETCH") == "1" {
		return nil, fmt.Errorf("native eager prefetch requires Linux")
	}
	return func() error { return nil }, nil
}
