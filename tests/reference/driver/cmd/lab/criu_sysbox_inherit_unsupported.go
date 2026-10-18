//go:build !linux

package main

import "fmt"

func criuSysboxPrepareInheritParent(string) error {
	if criuSysboxInheritEnabled() {
		return fmt.Errorf("lazy inheritance parent binding requires Linux")
	}
	return nil
}
