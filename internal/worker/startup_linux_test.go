//go:build linux

package worker

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestStartupFailureCapturesWaitingServiceAndChild(t *testing.T) {
	command := exec.Command("/bin/sh", "-c", "sleep 60 & echo $!; wait")
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	output, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	service, err := birth(command.Process.Pid)
	if err != nil {
		command.Process.Kill()
		command.Wait()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if live(service) == nil {
			unix.Kill(-service.PID, unix.SIGKILL)
		}
		command.Wait()
	})
	line, err := bufio.NewReader(output).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	childPID, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	status := Status{Schema: 1, StoreID: "test-owned-store", Epoch: "test-epoch", Filesystem: service}
	if err := recordStartupFailure(root, status, fmt.Errorf("injected missing readiness")); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "startup-failure.json"))
	if err != nil {
		t.Fatal(err)
	}
	var report startupFailure
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatal(err)
	}
	if report.Reason != "injected missing readiness" || report.Worker.StoreID != status.StoreID || report.Worker.Epoch != status.Epoch {
		t.Fatal("failure lost its original reason or owner", report)
	}
	parentFound, childFound, childLink := false, false, false
	for _, process := range report.Processes {
		if process.Service != "filesystem" || process.Process.PID == os.Getpid() {
			t.Fatal("collector included an unrelated process", process)
		}
		if process.Process == service {
			parentFound = true
			for _, task := range process.Tasks {
				for _, pid := range strings.Fields(task.Files["children"]) {
					childLink = childLink || pid == strconv.Itoa(childPID)
				}
			}
		}
		childFound = childFound || process.Process.PID == childPID
	}
	if !parentFound || !childFound || !childLink || live(service) != nil {
		t.Fatal("waiting parent/child not captured alive before cleanup", string(data))
	}
	info, err := os.Stat(filepath.Join(root, "startup-failure.json"))
	if err != nil || info.Mode().Perm() != 0600 || report.Bytes > startupMaxBytes || report.Tasks > startupMaxTasks || report.Truncated {
		t.Fatal("diagnostic privacy or bounds failed", info, err, report)
	}
	if err := recordStartupFailure(root, status, fmt.Errorf("replacement")); err == nil {
		t.Fatal("collector overwrote the first failure")
	}
	after, err := os.ReadFile(filepath.Join(root, "startup-failure.json"))
	if err != nil || string(after) != string(data) {
		t.Fatal("failed second write changed the first failure")
	}
}

func TestStartupFailureRejectsWrongBirthAndStopsAtBudget(t *testing.T) {
	identity, err := birth(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	wrong := identity
	wrong.Birth += "0"
	scan := startupScan{deadline: time.Now().Add(time.Second)}
	if _, err := scan.process(wrong, wrong, "filesystem"); err == nil || len(scan.report.Processes) != 0 {
		t.Fatal("wrong birth authorized process reads", err, scan.report)
	}
	scan.deadline = time.Now().Add(-time.Second)
	if _, err := scan.process(identity, identity, "filesystem"); err == nil || !scan.report.Truncated || scan.report.Bytes != 0 {
		t.Fatal("expired scan continued reading", err, scan.report)
	}
	scan = startupScan{deadline: time.Now().Add(time.Second), report: startupFailure{Bytes: startupMaxBytes}}
	if _, err := scan.process(identity, identity, "filesystem"); err == nil || !scan.report.Truncated || len(scan.report.Processes) != 0 {
		t.Fatal("byte budget allowed more process reads", err, scan.report)
	}
}
