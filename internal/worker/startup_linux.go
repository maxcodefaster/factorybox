//go:build linux

package worker

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

const (
	startupMaxProcesses = 32
	startupMaxTasks     = 128
	startupMaxBytes     = 256 << 10
	startupScanBudget   = 2 * time.Second
)

type startupTask struct {
	TID   int               `json:"tid"`
	Files map[string]string `json:"files"`
}

type startupProcess struct {
	Service string            `json:"service"`
	Process Process           `json:"process"`
	Files   map[string]string `json:"files"`
	Tasks   []startupTask     `json:"tasks"`
}

type startupFailure struct {
	Schema    int              `json:"schema"`
	At        string           `json:"at"`
	Reason    string           `json:"reason"`
	Worker    Status           `json:"worker"`
	Processes []startupProcess `json:"processes"`
	Notes     []string         `json:"notes"`
	Bytes     int              `json:"proc_bytes"`
	Tasks     int              `json:"task_count"`
	Truncated bool             `json:"truncated"`
	Seconds   float64          `json:"seconds"`
}

type startupScan struct {
	report   startupFailure
	deadline time.Time
	attempts int
}

func (s *startupScan) available() bool {
	if s.report.Bytes >= startupMaxBytes || time.Now().After(s.deadline) {
		s.report.Truncated = true
		return false
	}
	return true
}

// All names are fixed procfs entries or decimal IDs. The directory descriptor
// pins the original process even if its numeric PID is later reused. No exe,
// command line, environment, memory or arbitrary process file is read.
func (s *startupScan) read(directory int, name string) (string, error) {
	if !s.available() {
		return "", fmt.Errorf("startup diagnostic scan limit reached")
	}
	fd, err := unix.Openat(directory, name, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if err != nil {
		return "", err
	}
	file := os.NewFile(uintptr(fd), name)
	defer file.Close()
	limit := min(4096, startupMaxBytes-s.report.Bytes)
	data, err := io.ReadAll(io.LimitReader(file, int64(limit)))
	s.report.Bytes += len(data)
	if len(data) == limit {
		s.report.Truncated = true
	}
	return string(data), err
}

func (s *startupScan) files(directory int, names ...string) map[string]string {
	files := make(map[string]string)
	for _, name := range names {
		if !s.available() {
			break
		}
		data, err := s.read(directory, name)
		if err != nil {
			files[name] = "unavailable: " + err.Error()
		} else {
			files[name] = data
		}
	}
	return files
}

func procIdentity(pid int, stat string) (Process, error) {
	index := strings.LastIndex(stat, ")")
	if index < 0 {
		return Process{}, fmt.Errorf("invalid process identity")
	}
	fields := strings.Fields(stat[index+1:])
	if len(fields) < 20 || fields[0] == "Z" || fields[0] == "X" {
		return Process{}, fmt.Errorf("process is absent or exited")
	}
	return Process{PID: pid, Birth: fields[19]}, nil
}

func (s *startupScan) process(identity, leader Process, service string) ([]int, error) {
	if !s.available() || len(s.report.Processes) >= startupMaxProcesses {
		s.report.Truncated = true
		return nil, fmt.Errorf("startup diagnostic process limit reached")
	}
	if !descendant(identity, leader) {
		return nil, fmt.Errorf("process no longer belongs to the saved service")
	}
	fd, err := unix.Open("/proc/"+strconv.Itoa(identity.PID), unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	defer unix.Close(fd)
	stat, err := s.read(fd, "stat")
	if err != nil {
		return nil, err
	}
	actual, err := procIdentity(identity.PID, stat)
	if err != nil || actual != identity {
		return nil, fmt.Errorf("saved process birth changed: %v", err)
	}
	process := startupProcess{Service: service, Process: actual, Files: s.files(fd, "status", "wchan", "syscall", "stack")}
	process.Files["stat"] = stat
	taskfd, err := unix.Openat(fd, "task", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		s.report.Notes = append(s.report.Notes, fmt.Sprintf("%s PID %d tasks unavailable: %v", service, actual.PID, err))
		s.report.Processes = append(s.report.Processes, process)
		return nil, nil
	}
	tasks := os.NewFile(uintptr(taskfd), "task")
	defer tasks.Close()
	remaining := startupMaxTasks - s.report.Tasks
	entries, err := tasks.Readdirnames(remaining + 1)
	if err != nil && err != io.EOF {
		s.report.Notes = append(s.report.Notes, fmt.Sprintf("%s PID %d task listing unavailable: %v", service, actual.PID, err))
	}
	if len(entries) > remaining {
		s.report.Truncated = true
		entries = entries[:remaining]
	}
	var children []int
	for _, name := range entries {
		if !s.available() {
			break
		}
		tid, err := strconv.Atoi(name)
		if err != nil || tid <= 0 {
			continue
		}
		tidfd, err := unix.Openat(taskfd, name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if err != nil {
			continue
		}
		files := s.files(tidfd, "stat", "wchan", "syscall", "stack", "children")
		unix.Close(tidfd)
		s.report.Tasks++
		process.Tasks = append(process.Tasks, startupTask{TID: tid, Files: files})
		for _, value := range strings.Fields(files["children"]) {
			pid, err := strconv.Atoi(value)
			if err == nil && pid > 0 && len(children) < startupMaxProcesses {
				children = append(children, pid)
			}
		}
	}
	s.report.Processes = append(s.report.Processes, process)
	return children, nil
}

// Startup collection is best effort and never authorizes readiness or changes
// the original failure. It runs before the owned services are stopped. Limits
// bound procfs reads and traversal; the time budget is checked between reads.
func recordStartupFailure(root string, status Status, failure error) error {
	started := time.Now()
	scan := startupScan{deadline: started.Add(startupScanBudget), report: startupFailure{
		Schema: 1, At: started.UTC().Format(time.RFC3339Nano), Worker: status,
		Reason: failure.Error(),
	}}
	if len(scan.report.Reason) > 4096 {
		scan.report.Reason = scan.report.Reason[:4096]
		scan.report.Truncated = true
	}
	for _, service := range []struct {
		name   string
		leader Process
	}{{"manager", status.Manager}, {"filesystem", status.Filesystem}} {
		pending := []Process{service.leader}
		seen := make(map[int]bool)
		for len(pending) > 0 && scan.available() && scan.attempts < startupMaxProcesses {
			identity := pending[0]
			pending = pending[1:]
			if seen[identity.PID] {
				continue
			}
			seen[identity.PID] = true
			scan.attempts++
			children, err := scan.process(identity, service.leader, service.name)
			if err != nil {
				scan.report.Notes = append(scan.report.Notes, fmt.Sprintf("%s PID %d: %v", service.name, identity.PID, err))
				continue
			}
			for _, pid := range children {
				if !seen[pid] && len(pending) < startupMaxProcesses {
					if child, err := birth(pid); err == nil {
						pending = append(pending, child)
					}
				}
			}
		}
		if len(pending) > 0 {
			scan.report.Truncated = true
		}
	}
	scan.report.Seconds = time.Since(started).Seconds()
	return writeNew(root+"/startup-failure.json", scan.report)
}
