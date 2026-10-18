//go:build !linux

package main

import (
	"fmt"
	"time"
)

var criuEpochSecondForkStart time.Time
var criuEpochPreparation time.Duration

func criuEpochChainCopies(string, string, []string, map[string]map[string]any, map[int]string) (criuSysboxCopy, string, error) {
	return criuSysboxCopy{}, "", fmt.Errorf("real source epoch chain requires the existing Linux worker")
}
