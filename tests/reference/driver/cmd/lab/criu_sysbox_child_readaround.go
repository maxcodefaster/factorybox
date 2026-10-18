package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

const criuChildDemandBudget = 64 << 20
const criuChildDemandMaximum = 256 << 10

var criuDemandProgress = regexp.MustCompile(`Adaptive demand request=([0-9]+) extra=([0-9]+) used=([0-9]+) budget=([0-9]+) expanded=([0-9]+)`)

type criuDemandProof struct {
	ReportedExtraBytes uint64 `json:"reported_extra_bytes"`
	ReportedExpanded   uint64 `json:"reported_expanded_requests"`
	ObservedMaxRequest uint64 `json:"observed_max_request_bytes"`
	BudgetBytes        uint64 `json:"budget_bytes"`
}

func criuChildDemandEnabled() bool {
	return os.Getenv("CRIU_SYSBOX_CHILD_READAROUND") == "1"
}

func criuChildDemandGuard(root, id string) error {
	switch os.Getenv("CRIU_SYSBOX_CHILD_READAROUND") {
	case "", "0":
		return nil
	case "1":
	default:
		return fmt.Errorf("CRIU_SYSBOX_CHILD_READAROUND must be 0 or 1")
	}
	if root != "/experiment" || id != "source-sysbox" {
		return fmt.Errorf("child readaround is outside the owned source")
	}
	for _, name := range []string{"CRIU_SYSBOX_LAZY_INHERIT", "CRIU_SYSBOX_RETAIN_SOURCE",
		"CRIU_SYSBOX_LAZY", "CRIU_SYSBOX_PROBE", "CRIU_SYSBOX_COPY_PIPELINE"} {
		if os.Getenv(name) != "1" {
			return fmt.Errorf("child readaround requires %s=1", name)
		}
	}
	if os.Getenv("CRIU_SYSBOX_LAZY_PREFETCH") != "0" {
		return fmt.Errorf("child readaround excludes unbounded background prefetch")
	}
	return nil
}

func criuParseDemandProof(text string) (result criuDemandProof, resultErr error) {
	header := fmt.Sprintf("Adaptive demand limit=%d budget=%d", criuChildDemandMaximum, criuChildDemandBudget)
	if strings.Count(text, header) != 1 {
		return result, fmt.Errorf("adaptive demand configuration not proved")
	}
	for _, match := range criuDemandProgress.FindAllStringSubmatch(text, -1) {
		values := make([]uint64, 5)
		for i := range values {
			value, err := strconv.ParseUint(match[i+1], 10, 64)
			if err != nil {
				return result, err
			}
			values[i] = value
		}
		request, extra, used, budget, expanded := values[0], values[1], values[2], values[3], values[4]
		if request > criuChildDemandMaximum || request <= 64<<10 || extra != request-(64<<10) ||
			request%4096 != 0 || budget != criuChildDemandBudget || used > budget || used < extra ||
			used <= result.ReportedExtraBytes || expanded <= result.ReportedExpanded {
			return result, fmt.Errorf("adaptive demand exceeded or reset its bounds")
		}
		if result.ReportedExpanded == 0 && (expanded != 1 || used != extra) {
			return result, fmt.Errorf("adaptive demand first expansion is missing")
		}
		result.ReportedExtraBytes, result.ReportedExpanded = used, expanded
		result.ObservedMaxRequest = max(result.ObservedMaxRequest, request)
		result.BudgetBytes = budget
	}
	if result.ReportedExpanded == 0 {
		return result, fmt.Errorf("no actual adaptive demand expansion proved")
	}
	return result, nil
}

func criuChildDemandProof(id, phase string) error {
	if !criuChildDemandEnabled() || id != "copy-sysbox" || phase != "fork-restored" {
		return nil
	}
	start := time.Now()
	read := func(images string) (string, error) {
		path := filepath.Join(images, "lazy-pages.log")
		fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
		if err != nil {
			return "", err
		}
		file := os.NewFile(uintptr(fd), path)
		defer file.Close()
		info, err := file.Stat()
		if err != nil || !info.Mode().IsRegular() {
			return "", fmt.Errorf("adaptive demand proof requires a regular log: %v", err)
		}
		data, err := io.ReadAll(io.LimitReader(file, (16<<20)+1))
		if len(data) > 16<<20 {
			return "", fmt.Errorf("adaptive demand log exceeds diagnostic limit")
		}
		return string(data), err
	}
	child, err := read("/experiment/sysbox-copy/images")
	var proof criuDemandProof
	if err == nil {
		proof, err = criuParseDemandProof(child)
	}
	if err == nil {
		var source string
		source, err = read("/experiment/sysbox/images")
		if err == nil && strings.Contains(source, "Adaptive demand ") {
			err = fmt.Errorf("child policy unexpectedly applied to original source")
		}
	}
	record("sysbox-child-readaround-policy-proof", start, asJSON(map[string]any{
		"runtime_id": id, "proof": proof, "source_policy_disabled": err == nil,
		"scope": "policy observed before complete readiness; coarse cumulative reports are lower bounds, native selector tests prove the shared hard limit; at most 256KiB per contiguous request and 64MiB requested extra over baseline; original source unchanged",
	}), err)
	return err
}
