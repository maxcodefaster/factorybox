package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

func criuParentImageLocation(root, images string) bool {
	if filepath.Clean(root) != root || !filepath.IsAbs(root) {
		return false
	}
	for _, path := range []string{"sysbox/images-again", "sysbox/images-again-disk", "sysbox/images-repeat", "sysbox/images-repeat-disk", "sysbox-copy/images", "sysbox-copy/images-disk"} {
		if images == filepath.Join(root, path) {
			return true
		}
	}
	return false
}

var criuDirtyKeeper *criuLazyDaemon

func criuSysboxDirtyKeeperStart() error {
	if os.Getenv("CRIU_SYSBOX_ASYNC_TRACK") != "1" {
		return nil
	}
	if criuRoot != "/experiment" || os.Getenv("CRIU_SYSBOX_PRE_DUMP") != "1" || os.Getenv("CRIU_SYSBOX_RETAIN_SOURCE") != "1" {
		return fmt.Errorf("tracking keeper is outside the owned pre-dump experiment")
	}
	start := time.Now()
	d := &criuLazyDaemon{done: make(chan error, 1), logPath: criuRoot + "/dirty-tracking.log"}
	log, err := os.Create(d.logPath)
	if err != nil {
		return err
	}
	d.command = exec.Command("/ramctl", "dirty-broker")
	d.command.Stdout, d.command.Stderr = log, log
	if err := d.command.Start(); err != nil {
		log.Close()
		return err
	}
	criuDirtyKeeper = d
	go func() { d.done <- d.command.Wait(); log.Close(); close(d.done) }()
	deadline := time.Now().Add(3 * time.Second)
	for {
		select {
		case err := <-d.done:
			return fmt.Errorf("tracking keeper exited before ready: %v", err)
		default:
		}
		info, err := os.Lstat(criuRoot + "/dirty-tracking.sock")
		if err == nil && info.Mode()&os.ModeSocket != 0 && info.Mode().Perm() == 0600 {
			record("sysbox-async-tracking-keeper-ready", start, fmt.Sprintf("pid=%d outside captured source; private socket, PID/start-time and exact cgroup/mapping checks", d.command.Process.Pid), nil)
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("tracking keeper readiness timed out")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func criuSysboxDirtyKeeperCleanup() error {
	d := criuDirtyKeeper
	if d == nil {
		return nil
	}
	criuDirtyKeeper = nil
	start := time.Now()
	_ = d.command.Process.Signal(syscall.SIGTERM)
	var err error
	select {
	case err = <-d.done:
	case <-time.After(3 * time.Second):
		_ = d.command.Process.Kill()
		<-d.done
		err = fmt.Errorf("tracking keeper graceful teardown timed out")
	}
	data, readErr := os.ReadFile(d.logPath)
	if err == nil {
		err = readErr
	}
	record("sysbox-async-tracking-keeper-stopped", start, string(data), err)
	return err
}

// Prove CRIU's tracked-memory pre-dump before admitting a parent-image chain.
// The existing complete capture and independent fork remain the control.
func criuSysboxPreDumpSource(id string) error {
	if os.Getenv("CRIU_SYSBOX_PRE_DUMP") != "1" {
		return nil
	}
	if id != "source-sysbox" || criuRoot != "/experiment" || os.Getenv("CRIU_SYSBOX_RETAIN_SOURCE") != "1" {
		return fmt.Errorf("pre-dump is outside the owned retained-source experiment")
	}
	before, err := criuRuntimeIdentity(id)
	if err != nil {
		return err
	}
	images := filepath.Join(criuRoot, "sysbox/images-base")
	if _, err := os.Lstat(images); !os.IsNotExist(err) {
		return fmt.Errorf("pre-dump base already exists or cannot be inspected: %v", err)
	}
	if os.Getenv("CRIU_SYSBOX_RAM_BASE") == "1" {
		if os.Getenv("CRIU_SYSBOX_RAM_CAPTURE") != "1" || os.Getenv("CRIU_SYSBOX_DELTA") != "1" || os.Getenv("CRIU_SYSBOX_ASYNC_TRACK") != "1" {
			return fmt.Errorf("RAM base requires the owned RAM capture and tracked delta mode")
		}
		if err := criuSysboxPrepareRAMCapture(images); err != nil {
			return err
		}
	}
	start := time.Now()
	out, err := criuCommand(40*time.Second, "/ramctl", "pre-dump-retain-source", criuRoot+"/runtime", id, images)
	record("sysbox-pre-dump-running-source", start, out+"; preparatory tracked-memory capture, not live-fork availability latency", err)
	if err != nil {
		return err
	}
	after, err := criuRuntimeIdentity(id)
	if err == nil && asJSON(before) != asJSON(after) {
		err = fmt.Errorf("source runtime/PID/namespaces changed during pre-dump")
	}
	if err == nil {
		freeze, freezeErr := os.ReadFile(criuOwnedSourceCgroup + "/cgroup.freeze")
		data, eventsErr := os.ReadFile(criuOwnedSourceCgroup + "/cgroup.events")
		events, parseErr := criuRAMCounters(string(data))
		if freezeErr != nil || eventsErr != nil || parseErr != nil || strings.TrimSpace(string(freeze)) != "0" || events["frozen"] != 0 || events["populated"] != 1 {
			err = fmt.Errorf("pre-dump did not leave the populated source thawed: %s %v %v %v", data, freezeErr, eventsErr, parseErr)
		}
	}
	var pageFiles int
	var pageBytes int64
	entries, readErr := os.ReadDir(images)
	if err == nil {
		err = readErr
	}
	if err == nil {
		for _, entry := range entries {
			if criuRAMPageName.MatchString(entry.Name()) {
				info, statErr := entry.Info()
				if statErr != nil || !info.Mode().IsRegular() {
					err = fmt.Errorf("invalid pre-dump page payload: %v", statErr)
					break
				}
				pageFiles++
				pageBytes += info.Size()
			}
		}
		if err == nil && (pageFiles == 0 || pageBytes == 0) {
			err = fmt.Errorf("pre-dump produced no saved page payload")
		}
	}
	record("sysbox-pre-dump-source-identity-and-pages", start, asJSON(map[string]any{
		"images": images, "source_identity": after, "page_files": pageFiles, "page_bytes": pageBytes,
		"scope": "running source PID/namespaces retained; CRIU pre-dump checks memory tracking; complete fork remains the control"}), err)
	if err != nil {
		return err
	}
	if err := criuSysboxRAMCaptureProof(images); err != nil {
		return err
	}
	return criuSysboxPinParent(images)
}
