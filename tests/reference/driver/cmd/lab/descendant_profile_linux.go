package main

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime/pprof"
	"time"

	"golang.org/x/sys/unix"
)

type descendantCPUProfileWriter struct {
	File  *os.File
	Bytes int
	Err   error
}

func (writer *descendantCPUProfileWriter) Write(data []byte) (int, error) {
	if writer.Err != nil {
		return 0, writer.Err
	}
	if len(data) > (1<<20)-writer.Bytes {
		writer.Err = fmt.Errorf("owned target CPU profile exceeds 1MiB")
		return 0, writer.Err
	}
	count, err := writer.File.Write(data)
	writer.Bytes += count
	writer.Err = err
	return count, err
}

func descendantProfileTargetGraph(evidence string, operation func() error) (map[string]any, error) {
	if os.Getenv("RUNTIME_FORK_DESCENDANT_GRAPH_PROFILE") != "1" {
		return nil, operation()
	}
	if filepath.Clean(evidence) != evidence ||
		(evidence != "/experiment/descendant" && filepath.Dir(evidence) != "/experiment/descendant") {
		return nil, fmt.Errorf("CPU profile is outside the owned descendant evidence")
	}
	actual, err := filepath.EvalSymlinks(evidence)
	if err != nil || actual != evidence {
		return nil, fmt.Errorf("CPU profile evidence directory changed: %v", err)
	}
	path := filepath.Join(evidence, "target-graph.cpu.pprof")
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, err
	}
	writer := &descendantCPUProfileWriter{File: file}
	begin := time.Now()
	if err := pprof.StartCPUProfile(writer); err != nil {
		return nil, errors.Join(err, file.Close())
	}
	startUS := time.Since(begin).Microseconds()
	operationStart := time.Now()
	operationErr := operation()
	operationUS := time.Since(operationStart).Microseconds()
	stop := time.Now()
	pprof.StopCPUProfile()
	stopUS := time.Since(stop).Microseconds()
	if err := errors.Join(operationErr, writer.Err, file.Close()); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 || len(data) > 1<<20 || len(data) != writer.Bytes {
		return nil, fmt.Errorf("CPU profile is incomplete: %v", err)
	}
	return map[string]any{
		"path": path, "bytes": len(data), "sha256": fmt.Sprintf("%x", sha256.Sum256(data)),
		"start_us": startUS, "operation_us": operationUS, "stop_us": stopUS,
		"scope": "Optional CPU profile of complete target compilation and its full logical byte comparison. All profile start/stop and output handling remain charged before readiness.",
	}, nil
}
