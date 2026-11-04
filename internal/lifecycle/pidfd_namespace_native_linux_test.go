//go:build linux

package lifecycle

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestNativePidfdNamespaces(t *testing.T) {
	if os.Getenv("FACTORYBOX_NATIVE_PIDFD_NAMESPACES") != "1" {
		t.Skip("owned nested PID namespace pidfd fixture only")
	}
	cases := map[string]any{}
	for _, mode := range []string{"flat", "nested", "shared-table"} {
		t.Run(mode, func(t *testing.T) {
			cases[mode] = nativePidfdNamespaceCase(t, mode)
		})
	}
	if t.Failed() {
		return
	}
	boundedLossSave(t, "pidfd-namespaces.json", map[string]any{
		"passed": true, "exact_nested_target_signaled_before": true,
		"exact_nested_target_signaled_after": true, "same_sandbox": true, "cases": cases,
	})
}

func nativePidfdNamespaceCase(t *testing.T, mode string) map[string]any {
	t.Helper()
	id := "pidfd-" + mode
	flat := mode == "flat"
	status := boundedLossStatusNow(t)
	if err := os.WriteFile("/output/"+id+".c", []byte(nativePidfdNamespacesC), 0644); err != nil {
		t.Fatal(err)
	}
	options := []string{"-static", "-O2", "-Wall", "-Wextra", "-Werror",
		"/output/" + id + ".c", "-o", "/output/" + id}
	if flat {
		options = append(options, "-DPIDFD_FLAT")
	}
	if mode == "shared-table" {
		options = append(options, "-DPIDFD_SHARED_TABLE")
	}
	if output, err := exec.Command("cc", options...).CombinedOutput(); err != nil {
		t.Fatal("compile namespace fixture", err, string(output))
	}
	program, err := os.ReadFile("/output/" + id)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if data, err := os.ReadFile(status.Store + "/" + id + "-bundle/upper/pidfd-startup.log"); err == nil {
			_ = os.WriteFile("/output/"+id+"-startup.log", data, 0600)
		}
	})
	view := timerReaderFixtureFilesWithConfiguration(t, status, id,
		"exec /bin/sh -c 'exec /bin/pidfd-namespaces >/pidfd-startup.log 2>&1'",
		false, map[string][]byte{"pidfd-namespaces": program}, nil, func(spec map[string]any) {
			// Match the admitted Factory consumer, without changing ordinary fixtures.
			caps := spec["process"].(map[string]any)["capabilities"].(map[string]any)
			for key, values := range caps {
				caps[key] = append(values.([]any), "CAP_SYS_ADMIN", "CAP_NET_ADMIN")
			}
		})
	type coordinates struct {
		Parent int `json:"parent"`
		Target int `json:"target"`
		Own    int `json:"own"`
		FD     int `json:"fd"`
	}
	var before coordinates
	wait := func(pid int, check func() bool) {
		t.Helper()
		deadline, ok := t.Deadline()
		if !ok {
			deadline = time.Now().Add(420 * time.Second)
		}
		for !check() {
			stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
			if err != nil || len(stat) == 0 {
				log, _ := os.ReadFile(status.Store + "/" + id + "-bundle/upper/pidfd-startup.log")
				t.Fatal("pidfd fixture exited before completing its observation", err, string(log))
			}
			if time.Now().After(deadline) {
				t.Fatal("exact pidfd namespace fixture did not become useful")
			}
			time.Sleep(time.Millisecond)
		}
	}
	wait(view.Runtime.PID, func() bool {
		data, err := os.ReadFile(fmt.Sprintf("/proc/%d/root/tmp/pidfd-state.json", view.Runtime.PID))
		return err == nil && json.Unmarshal(data, &before) == nil
	})
	if before.Parent != 1 || before.Own <= 1 || before.Target <= 1 || flat != (before.Own == before.Target) {
		t.Fatal("fixture did not create its declared PID coordinates", flat, before)
	}
	signalAndRequire := func(current View, expected string) {
		t.Helper()
		if current.Runtime == nil || current.Runtime.Status != "running" {
			t.Fatal("pidfd fixture has no running original runtime", current)
		}
		if err := unix.Kill(current.Runtime.PID, unix.SIGUSR1); err != nil {
			t.Fatal("signal exact original pidfd monitor", err)
		}
		wait(current.Runtime.PID, func() bool {
			data, err := os.ReadFile(fmt.Sprintf("/proc/%d/root/tmp/pidfd-signals", current.Runtime.PID))
			return err == nil && string(data) == expected
		})
	}
	signalAndRequire(view, "x")
	cycles := []map[string]any{}
	for cycle := range 3 {
		snapshot := fmt.Sprintf("da%030x", cycle+1)
		pauseStarted := time.Now()
		paused := boundedLossView(t, "pause", "--snapshot-id", snapshot, view.Record.ID)
		pauseSeconds := time.Since(pauseStarted).Seconds()
		if paused.Record.Phase != "paused" || paused.Record.Pause == nil || paused.Record.Pause.CompletedAt == "" {
			t.Fatal("namespace fixture lacks a committed original Pause", paused)
		}
		resumeStarted := time.Now()
		data, resumeErr := boundedLossCommand("--worker-run-root", "/run/factorybox",
			"resume", "--snapshot-id", snapshot, view.Record.ID)
		resumeSeconds := time.Since(resumeStarted).Seconds()
		if resumeErr != nil {
			path := status.Lifecycle.Root + "/objects/" + view.Record.ID + "/resumes/" + snapshot + "/images/restore.log"
			if log, err := os.ReadFile(path); err == nil {
				_ = os.WriteFile(fmt.Sprintf("/output/%s-%d-restore.log", id, cycle), log, 0600)
			}
			t.Fatal("resume original cross-namespace pidfd", resumeErr)
		}
		var resumed View
		if err := json.Unmarshal(data, &resumed); err != nil {
			t.Fatal(err)
		}
		if resumed.Record.ID != view.Record.ID {
			t.Fatal("Resume replaced the original sandbox", resumed.Record.ID, view.Record.ID)
		}
		signalAndRequire(resumed, strings.Repeat("x", cycle+2))
		cycles = append(cycles, map[string]any{
			"snapshot": snapshot, "pause_seconds": pauseSeconds, "resume_seconds": resumeSeconds,
			"original_handle_signaled_exact_target": true,
		})
		view = resumed
	}
	forked := boundedLossView(t, "fork", "--source", id,
		"--snapshot-id", cycles[len(cycles)-1]["snapshot"].(string), id+"-branch")
	t.Cleanup(func() {
		if _, err := boundedLossCommand("--worker-run-root", "/run/factorybox", "delete", forked.Record.ID); err != nil {
			t.Error("delete exact pidfd branch", err)
		}
	})
	signalAndRequire(forked, "xxxx")
	forkSnapshot := "db000000000000000000000000000001"
	branchPause := boundedLossView(t, "pause", "--snapshot-id", forkSnapshot, forked.Record.ID)
	if branchPause.Record.Phase != "paused" || branchPause.Record.Pause == nil || branchPause.Record.Pause.CompletedAt == "" {
		t.Fatal("pidfd branch has no committed Pause", branchPause)
	}
	branchResume := boundedLossView(t, "resume", "--snapshot-id", forkSnapshot, forked.Record.ID)
	signalAndRequire(branchResume, "xxxxx")
	originalSignals, err := os.ReadFile(fmt.Sprintf("/proc/%d/root/tmp/pidfd-signals", view.Runtime.PID))
	if err != nil || string(originalSignals) != "xxxx" {
		t.Fatal("forked pidfd changed its original source", err, string(originalSignals))
	}
	return map[string]any{
		"passed": true, "original": before, "exact_nested_target_signaled_before": true,
		"exact_nested_target_signaled_after": true, "same_sandbox": true,
		"cycles": cycles, "aliases_and_independent_flags": true, "dead_pidfd_stays_dead": true,
		"self_pidfd_stays_live": true, "shared_file_table": mode == "shared-table",
		"fork_targets_independent": true, "fork_pause_resume": true,
	}
}

const nativePidfdNamespacesC = `
#define _GNU_SOURCE
#include <errno.h>
#include <fcntl.h>
#include <sched.h>
#include <signal.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/mount.h>
#include <sys/socket.h>
#include <sys/syscall.h>
#include <sys/types.h>
#include <sys/wait.h>
#include <unistd.h>
static int target_fd = -1, alias_fd = -1, independent_fd = -1;
static int self_fd = -1, dead_fd = -1, signals = 0;
static int marker_fd = -1, channels[2], gate[2];
static void fail(const char *what) { perror(what); exit(1); }
static void signal_child(int n) {
	(void)n;
	if (fcntl(alias_fd, F_SETFL, O_NONBLOCK) ||
	    !(fcntl(target_fd, F_GETFL) & O_NONBLOCK) ||
	    fcntl(alias_fd, F_SETFL, 0) ||
	    (fcntl(target_fd, F_GETFL) & O_NONBLOCK) ||
	    !(fcntl(independent_fd, F_GETFL) & O_NONBLOCK)) _exit(73);
	if (fcntl(alias_fd, F_GETFD) != 0 ||
	    fcntl(target_fd, F_GETFD) != FD_CLOEXEC ||
	    fcntl(independent_fd, F_GETFD) != FD_CLOEXEC) _exit(74);
	if (syscall(SYS_pidfd_send_signal, self_fd, 0, NULL, 0)) _exit(75);
	errno = 0;
	if (syscall(SYS_pidfd_send_signal, dead_fd, 0, NULL, 0) != -1 || errno != ESRCH)
		_exit(76);
	int handles[] = {target_fd, alias_fd, independent_fd};
	if (syscall(SYS_pidfd_send_signal, handles[signals++ % 3], SIGUSR2, NULL, 0)) _exit(71);
}
static void signaled_child(int n) {
	(void)n;
	if (write(marker_fd, "x", 1) != 1) _exit(72);
}
static int inner(void *unused) {
	char token; int status; (void)unused;
	close(gate[1]); close(channels[0]);
	if (read(gate[0], &token, 1) != 1) fail("namespace admission");
	close(gate[0]);
	for (int i = 0; i < 64; i++) {
		pid_t child = fork();
		if (child < 0) fail("fixture fork");
		if (!child) _exit(0);
		if (waitpid(child, &status, 0) != child || status) fail("fixture reap");
	}
	pid_t target = fork();
	if (target < 0) fail("target fork");
	if (!target) {
		marker_fd = open("/tmp/pidfd-signals", O_WRONLY | O_APPEND | O_CREAT, 0666);
		if (marker_fd < 0) fail("signal marker");
		if (signal(SIGUSR2, signaled_child) == SIG_ERR) fail("target handler");
		pid_t own_pid = getpid();
		if (send(channels[1], &own_pid, sizeof(own_pid), 0) != (ssize_t)sizeof(own_pid))
			fail("target coordinate");
		close(channels[1]);
		for (;;) pause();
	}
	close(channels[1]);
	while (waitpid(target, &status, 0) < 0)
		if (errno != EINTR) fail("target wait");
	return 0;
}
#ifdef PIDFD_SHARED_TABLE
static int shared_table_peer(void *unused) {
	(void)unused;
	for (;;) pause();
	return 0;
}
#endif
int main(void) {
	if (getsid(0) != getpid() && setsid() < 0) fail("fixture session");
	int dead_gate[2], status;
	if (pipe(dead_gate)) fail("dead target gate");
	pid_t dead = fork();
	if (dead < 0) fail("dead target fork");
	if (!dead) {
		char token;
		close(dead_gate[1]);
		if (read(dead_gate[0], &token, 1) != 1) _exit(77);
		_exit(0);
	}
	close(dead_gate[0]);
	dead_fd = syscall(SYS_pidfd_open, dead, 0);
	if (dead_fd < 0) fail("dead target pidfd");
	if (write(dead_gate[1], "x", 1) != 1) fail("dead target release");
	close(dead_gate[1]);
	if (waitpid(dead, &status, 0) != dead || status) fail("dead target reap");
	if (socketpair(AF_UNIX, SOCK_SEQPACKET, 0, channels) || pipe(gate)) fail("channels");
	int enabled = 1;
	if (setsockopt(channels[0], SOL_SOCKET, SO_PASSCRED, &enabled, sizeof(enabled)))
		fail("credential coordinates");
	char *stack = malloc(1 << 20);
	if (!stack) fail("clone stack");
	int clone_flags = SIGCHLD;
#ifndef PIDFD_FLAT
	clone_flags |= CLONE_NEWPID;
#endif
	pid_t bootstrap = clone(inner, stack + (1 << 20), clone_flags, NULL);
	if (bootstrap < 0) fail("nested namespace clone");
	close(gate[0]); close(channels[1]);
	if (write(gate[1], "x", 1) != 1) fail("release bootstrap");
	close(gate[1]);
	pid_t own_pid = -1, outer_pid = -1;
	char control[CMSG_SPACE(sizeof(struct ucred))];
	struct iovec iov = { .iov_base = &own_pid, .iov_len = sizeof(own_pid) };
	struct msghdr message = {
		.msg_iov = &iov, .msg_iovlen = 1, .msg_control = control,
		.msg_controllen = sizeof(control),
	};
	if (recvmsg(channels[0], &message, 0) != (ssize_t)sizeof(own_pid)) fail("target identity");
	for (struct cmsghdr *c = CMSG_FIRSTHDR(&message); c; c = CMSG_NXTHDR(&message, c))
		if (c->cmsg_level == SOL_SOCKET && c->cmsg_type == SCM_CREDENTIALS)
			outer_pid = ((struct ucred *)CMSG_DATA(c))->pid;
	close(channels[0]);
	if (own_pid <= 1 || outer_pid <= 1) fail("target coordinates");
	target_fd = syscall(SYS_pidfd_open, outer_pid, 0);
	if (target_fd < 0) fail("original pidfd open");
	alias_fd = dup(target_fd);
	independent_fd = syscall(SYS_pidfd_open, outer_pid, O_NONBLOCK);
	self_fd = syscall(SYS_pidfd_open, getpid(), 0);
	if (alias_fd < 0 || independent_fd < 0 || self_fd < 0) fail("additional pidfd descriptions");
	if (signal(SIGUSR1, signal_child) == SIG_ERR) fail("monitor handler");
#ifdef PIDFD_SHARED_TABLE
	char *peer_stack = malloc(1 << 20);
	if (!peer_stack || clone(shared_table_peer, peer_stack + (1 << 20),
				 CLONE_FILES | SIGCHLD, NULL) < 0) fail("shared descriptor table");
#endif
	FILE *state = fopen("/tmp/pidfd-state.json", "w");
	if (!state) fail("fixture state");
	fprintf(state, "{\"parent\":%d,\"target\":%d,\"own\":%d,\"fd\":%d}\n",
		getpid(), outer_pid, own_pid, target_fd);
	if (fclose(state)) fail("publish fixture state");
	for (;;) pause();
}
`
