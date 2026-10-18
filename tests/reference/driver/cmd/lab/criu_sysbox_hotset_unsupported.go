//go:build !linux

package main

import (
	"fmt"
	"time"
)

var criuHotTraceRegistrationTime time.Duration

func criuSysboxRememberHotTrace(string) error {
	if criuHotsetEnabled() {
		return fmt.Errorf("startup hot-page hints require Linux")
	}
	return nil
}

func criuSysboxApplyHotset(string, string) error {
	return criuSysboxRememberHotTrace("")
}
