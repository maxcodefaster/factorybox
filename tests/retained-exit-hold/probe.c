// SPDX-License-Identifier: Apache-2.0
// An owned, single-threaded kernel reproducer. No CRIU or application stand-ins.
#define _GNU_SOURCE
#include <errno.h>
#include <fcntl.h>
#include <poll.h>
#include <signal.h>
#include <stdbool.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/stat.h>
#include <sys/ptrace.h>
#include <sys/socket.h>
#include <linux/ptrace.h>
#include <sys/syscall.h>
#include <sys/types.h>
#include <sys/wait.h>
#include <time.h>
#include <unistd.h>

#include "criu-exit-observer.h"

static pid_t source, tracer, observer;
static int source_fd = -1;
static char group[256];
static bool child_observer, ordinary_cancel;
static volatile sig_atomic_t cancel_requested;

static void timeout_exit(int signal)
{
	(void)signal;
	_exit(124);
}

static void request_cancel(int signal)
{
	(void)signal;
	cancel_requested = 1;
}

static void fail(const char *message)
{
	perror(message);
	exit(2);
}

static void write_file(const char *name, const char *value)
{
	char path[512];
	int fd;
	snprintf(path, sizeof(path), "%s/%s", group, name);
	fd = open(path, O_WRONLY | O_CLOEXEC | O_NOFOLLOW);
	if (fd < 0 || write(fd, value, strlen(value)) != (ssize_t)strlen(value))
		fail("write owned cgroup");
	close(fd);
}

static void cleanup(void)
{
	int status;
	if (tracer > 1) {
		kill(tracer, SIGKILL);
		ptrace(PTRACE_CONT, tracer, NULL, NULL);
		while (waitpid(tracer, &status, __WALL) < 0 && errno == EINTR) {}
	}
	if (source > 1) {
		if (source_fd >= 0)
			syscall(SYS_pidfd_send_signal, source_fd, SIGKILL, NULL, 0);
		if (group[0])
			write_file("cgroup.freeze", "0");
		while (waitpid(source, &status, __WALL) < 0 && errno == EINTR) {}
	}
	if (source_fd >= 0)
		close(source_fd);
	if (observer > 1) {
		kill(observer, SIGKILL);
		while (waitpid(observer, &status, __WALL) < 0 && errno == EINTR) {}
	}
	if (group[0] && rmdir(group))
		perror("remove owned cgroup");
}

static int wait_exact(pid_t pid)
{
	int status;
	pid_t result;
	do {
		result = waitpid(pid, &status, __WALL);
	} while (result < 0 && errno == EINTR);
	if (result != pid)
		fail("wait exact owned PID");
	return status;
}

static bool alive(void)
{
	struct pollfd event = {.fd = source_fd, .events = POLLIN};
	int ready = poll(&event, 1, 0);
	if (ready < 0)
		fail("poll original source pidfd");
	return ready == 0;
}

static void freeze(void)
{
	char path[512], data[256];
	snprintf(path, sizeof(path), "%s/cgroup.events", group);
	write_file("cgroup.freeze", "1");
	for (int i = 0; i < 1000; i++) {
		int fd = open(path, O_RDONLY | O_CLOEXEC | O_NOFOLLOW);
		ssize_t count = fd < 0 ? -1 : read(fd, data, sizeof(data) - 1);
		if (fd >= 0)
			close(fd);
		if (count < 0)
			fail("read owned cgroup events");
		data[count] = 0;
		if (strstr(data, "frozen 1\n"))
			return;
		usleep(1000);
	}
	errno = ETIMEDOUT;
	fail("owned source did not freeze");
}

static void source_main(int start, int marker)
{
	char byte;
	struct timespec duration = {.tv_nsec = 100000000};
	if (read(start, &byte, 1) != 1)
		_exit(20);
	// Expiry includes time spent stopped, matching a real timed sleep.
	if (syscall(SYS_clock_nanosleep, CLOCK_MONOTONIC, 0, &duration, NULL) < 0 &&
	    errno != EINTR)
		_exit(21);
	if (write(marker, "X", 1) != 1)
		_exit(22);
	_exit(0);
}

static int hold_source(void *unused)
{
	(void)unused;
	if (!alive())
		return -1;
	usleep(100000);
	if (!alive())
		return -1;
	freeze();
	return 0;
}

static void observe_parent(pid_t parent, int ready, int life)
{
	struct factorybox_exit_observer observer;
	if (factorybox_observer_arm(&observer, parent) ||
	    write(ready, "G", 1) != 1)
		_exit(40);
	int outcome = factorybox_observer_wait(&observer, life, hold_source, NULL);
	if ((ordinary_cancel && (outcome != 0 || observer.attached || observer.held)) ||
	    (!ordinary_cancel && (outcome != 1 || observer.attached || !observer.held || !alive())))
		_exit(41);
	close(observer.signals);
	_exit(0);
}

static void tracer_main(pid_t target, int ready)
{
	struct ptrace_syscall_info info;
	int status, lifetime = -1;
	pid_t child = 0;
	alarm(10);
	if (signal(SIGUSR1, request_cancel) == SIG_ERR)
		_exit(51);
	if (child_observer) {
		int armed[2], life[2];
		char byte;
		pid_t parent = getpid();
		if (pipe2(armed, O_CLOEXEC) ||
		    socketpair(AF_UNIX, SOCK_SEQPACKET | SOCK_CLOEXEC, 0, life))
			_exit(48);
		child = fork();
		if (child < 0)
			_exit(49);
		if (!child) {
			close(life[1]);
			observe_parent(parent, armed[1], life[0]);
		}
		close(life[0]);
		lifetime = life[1];
		if (read(armed[0], &byte, 1) != 1 || byte != 'G' ||
		    write(ready, &child, sizeof(child)) != sizeof(child))
			_exit(50);
	}
	if (ptrace(PTRACE_SEIZE, target, NULL, (void *)PTRACE_O_TRACESYSGOOD) ||
	    ptrace(PTRACE_INTERRUPT, target, NULL, NULL))
		_exit(30);
	status = wait_exact(target);
	if (!WIFSTOPPED(status) || write(ready, "A", 1) != 1)
		_exit(36);
	while (WIFSTOPPED(status)) {
		if (ptrace(PTRACE_SYSCALL, target, NULL, NULL))
			_exit(31);
		status = wait_exact(target);
		memset(&info, 0, sizeof(info));
		if (ptrace(PTRACE_GET_SYSCALL_INFO, target, sizeof(info), &info) < 0)
			_exit(32);
		if (info.op == PTRACE_SYSCALL_INFO_ENTRY && info.entry.nr == SYS_clock_nanosleep)
			break;
	}
	if (!WIFSTOPPED(status) || ptrace(PTRACE_CONT, target, NULL, NULL))
		_exit(33);
	// Interrupt an actual in-flight timed syscall, not a synthetic stopped task.
	usleep(10000);
	if (ptrace(PTRACE_INTERRUPT, target, NULL, NULL))
		_exit(34);
	status = wait_exact(target);
	if (!WIFSTOPPED(status) || write(ready, "R", 1) != 1)
		_exit(35);
	while (!cancel_requested)
		pause();
	if (!ordinary_cancel || write(lifetime, "C", 1) != 1)
		_exit(52);
	close(lifetime);
	status = wait_exact(child);
	if (!WIFEXITED(status) || WEXITSTATUS(status) ||
	    ptrace(PTRACE_DETACH, target, NULL, NULL))
		_exit(53);
	_exit(0);
}

int main(int argc, char **argv)
{
	int start[2], marker[2], ready[2], status;
	char byte;
	bool exit_stop, survived, released, job_stop;
	struct timespec began, ended;
	if (argc != 2 || (strcmp(argv[1], "pidfd") && strcmp(argv[1], "exit-stop") &&
			 strcmp(argv[1], "child-exit-stop") && strcmp(argv[1], "child-cancel") &&
			 strcmp(argv[1], "child-job-stop")))
		return 2;
	ordinary_cancel = !strcmp(argv[1], "child-cancel");
	job_stop = !strcmp(argv[1], "child-job-stop");
	child_observer = ordinary_cancel || job_stop || !strcmp(argv[1], "child-exit-stop");
	exit_stop = child_observer || !strcmp(argv[1], "exit-stop");
	if (signal(SIGALRM, timeout_exit) == SIG_ERR)
		fail("install owned probe deadline");
	alarm(15);
	clock_gettime(CLOCK_MONOTONIC, &began);
	if (pipe2(start, O_CLOEXEC) || pipe2(marker, O_CLOEXEC | O_NONBLOCK) ||
	    pipe2(ready, O_CLOEXEC))
		fail("make private probe pipes");
	snprintf(group, sizeof(group), "/sys/fs/cgroup/factorybox-exit-hold-%d", getpid());
	if (mkdir(group, 0700))
		fail("create owned source cgroup");
	if (atexit(cleanup))
		fail("register exact cleanup");
	source = fork();
	if (source < 0)
		fail("fork owned source");
	if (!source) {
		close(start[1]);
		close(marker[0]);
		close(ready[0]);
		close(ready[1]);
		source_main(start[0], marker[1]);
	}
	source_fd = syscall(SYS_pidfd_open, source, 0);
	if (source_fd < 0)
		fail("pin owned original source");
	char pid_text[32];
	snprintf(pid_text, sizeof(pid_text), "%d", source);
	write_file("cgroup.procs", pid_text);
	tracer = fork();
	if (tracer < 0)
		fail("fork owned tracer");
	if (!tracer) {
		close(start[0]);
		close(start[1]);
		close(marker[0]);
		close(marker[1]);
		close(ready[0]);
		tracer_main(source, ready[1]);
	}
	close(start[0]);
	close(marker[1]);
	close(ready[1]);
	if (child_observer && (read(ready[0], &observer, sizeof(observer)) != sizeof(observer) ||
			       observer <= 1 || observer == source || observer == tracer))
		fail("pin original child observer");
	if (read(ready[0], &byte, 1) != 1 || byte != 'A' ||
	    write(start[1], "S", 1) != 1 || read(ready[0], &byte, 1) != 1 || byte != 'R')
		fail("reach actual timed source trace stop");
	if (exit_stop && !child_observer &&
	    ptrace(PTRACE_SEIZE, tracer, NULL, (void *)PTRACE_O_TRACEEXIT))
		fail("observe only owned tracer exit");
	// The original sleep deadline must pass while its process remains traced.
	usleep(200000);
	if (!alive())
		fail("source exited before tracer death");
	if (job_stop) {
		char path[64], line[256];
		bool stopped = false;
		snprintf(path, sizeof(path), "/proc/%d/status", tracer);
		if (kill(tracer, SIGSTOP))
			fail("stop only the owned tracer");
		for (int attempt = 0; attempt < 1000 && !stopped; attempt++) {
			FILE *file = fopen(path, "r");
			if (!file)
				fail("observe original tracer job stop");
			while (fgets(line, sizeof(line), file))
				if (!strncmp(line, "State:\tT", 8) || !strncmp(line, "State:\tt", 8))
					stopped = true;
			fclose(file);
			if (!stopped)
				usleep(1000);
		}
		if (!stopped)
			fail("original tracer did not reach its job stop");
	}
	if (kill(tracer, ordinary_cancel ? SIGUSR1 : SIGKILL))
		fail("kill exact owned tracer");
	status = wait_exact(tracer);
	if (ordinary_cancel) {
		if (!WIFEXITED(status) || WEXITSTATUS(status))
			fail("healthy tracer could not cancel and reap observer");
		observer = 0;
		usleep(100000);
	} else if (child_observer) {
		int observed = wait_exact(observer);
		if (!WIFEXITED(observed) || WEXITSTATUS(observed) || !alive())
			fail("original child observer failed to hold its parent exit");
		observer = 0;
	} else if (exit_stop) {
		if (!WIFSTOPPED(status) || (status >> 16) != PTRACE_EVENT_EXIT || !alive())
			fail("missing pre-detach tracer exit stop");
		// The barrier must survive real observer scheduling delay.
		usleep(100000);
		if (!alive() || read(marker[0], &byte, 1) != -1 || errno != EAGAIN)
			fail("source ran before exit-stop release");
		freeze();
		if (ptrace(PTRACE_CONT, tracer, NULL, NULL))
			fail("release only pinned tracer exit");
		status = wait_exact(tracer);
	} else {
		// A delayed pidfd observer cannot freeze before it is scheduled.
		usleep(100000);
		freeze();
	}
	if (!ordinary_cancel && (!WIFSIGNALED(status) || WTERMSIG(status) != SIGKILL))
		fail("owned tracer did not exit by injected SIGKILL");
	tracer = 0;
	usleep(100000);
	survived = alive();
	released = read(marker[0], &byte, 1) == 1 && byte == 'X';
	if (!survived) {
		status = wait_exact(source);
		source = 0;
		if (!WIFEXITED(status) || WEXITSTATUS(status))
			fail("source died by an unexpected signal or error");
	}
	clock_gettime(CLOCK_MONOTONIC, &ended);
	printf("{\"mode\":\"%s\",\"source_preserved\":%s,\"source_ran_after_detach\":%s,"
	       "\"tracer_exit_stop\":%s,\"source_normal_exit\":%s,\"ordinary_cancel\":%s,\"seconds\":%.6f}\n",
	       argv[1], survived ? "true" : "false", released ? "true" : "false",
	       exit_stop && !ordinary_cancel ? "true" : "false", source ? "false" : "true",
	       ordinary_cancel ? "true" : "false",
	       (double)(ended.tv_sec - began.tv_sec) + (ended.tv_nsec - began.tv_nsec) / 1e9);
	// The negative control deliberately returns red on lost original state.
	return ordinary_cancel ? (released && !survived ? 0 : 1) : (survived && !released ? 0 : 1);
}
