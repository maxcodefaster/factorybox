/* SPDX-License-Identifier: Apache-2.0 */
#ifndef FACTORYBOX_EXIT_OBSERVER_H
#define FACTORYBOX_EXIT_OBSERVER_H

#include <errno.h>
#include <poll.h>
#include <signal.h>
#include <stdbool.h>
#include <sys/ptrace.h>
#include <sys/signalfd.h>
#include <sys/wait.h>
#include <time.h>
#include <unistd.h>

struct factorybox_exit_observer {
	pid_t parent;
	int signals;
	bool attached;
	bool held;
};

static int factorybox_observer_arm(struct factorybox_exit_observer *observer, pid_t parent)
{
	sigset_t blocked;
	observer->parent = parent;
	observer->signals = -1;
	observer->attached = observer->held = false;
	if (parent <= 1 || parent == getpid()) {
		errno = EINVAL;
		return -1;
	}
	sigemptyset(&blocked);
	sigaddset(&blocked, SIGCHLD);
	if (sigprocmask(SIG_BLOCK, &blocked, NULL))
		return -1;
	observer->signals = signalfd(-1, &blocked, SFD_CLOEXEC | SFD_NONBLOCK);
	if (observer->signals < 0)
		return -1;
	/* Never EXITKILL: loss of this helper must leave a healthy parent alive. */
	if (ptrace(PTRACE_SEIZE, parent, NULL, (void *)PTRACE_O_TRACEEXIT)) {
		close(observer->signals);
		observer->signals = -1;
		return -1;
	}
	observer->attached = true;
	return 0;
}

static long long factorybox_observer_milliseconds(void)
{
	struct timespec now;
	if (clock_gettime(CLOCK_MONOTONIC, &now))
		return -1;
	return (long long)now.tv_sec * 1000 + now.tv_nsec / 1000000;
}

static int factorybox_observer_drain(int signals)
{
	struct signalfd_siginfo event;
	ssize_t count;
	do {
		count = read(signals, &event, sizeof(event));
	} while (count == sizeof(event) || (count < 0 && errno == EINTR));
	return count < 0 && errno == EAGAIN ? 0 : -1;
}

/* Returns zero for a confirmed ordinary detach, one after holding a dying
 * parent before its tracees are released, and minus one on an unproved exit.
 * hold() receives only the caller's already pinned original cgroup context. */
static int factorybox_observer_wait(struct factorybox_exit_observer *observer, int life,
				   int (*hold)(void *), void *context)
{
	struct pollfd events[2] = {
		{.fd = life, .events = POLLIN},
		{.fd = observer->signals, .events = POLLIN},
	};
	bool cancelling = false, interrupted = false;
	long long cancel_deadline = 0;
	for (;;) {
		int status, signal, event, timeout = 15000, ready;
		pid_t waited;
		do {
			waited = waitpid(observer->parent, &status, __WALL | WNOHANG);
		} while (waited < 0 && errno == EINTR);
		if (waited < 0)
			return -1;
		if (waited == observer->parent) {
			if (WIFEXITED(status) || WIFSIGNALED(status)) {
				observer->attached = false;
				return observer->held ? 1 : -1;
			}
			if (!WIFSTOPPED(status))
				return -1;
			event = status >> 16;
			signal = WSTOPSIG(status);
			if (event == PTRACE_EVENT_EXIT) {
				if (hold(context))
					return -1;
				observer->held = true;
				/* Do not detach here: consume the exact terminal event
				 * before the original worker reaps its real child. */
				if (ptrace(PTRACE_CONT, observer->parent, NULL, NULL))
					return -1;
			} else if (cancelling) {
				if (ptrace(PTRACE_DETACH, observer->parent, NULL,
					   (void *)(long)(event ? 0 : signal)))
					return -1;
				observer->attached = false;
				return 0;
			} else if (event == PTRACE_EVENT_STOP && signal != SIGTRAP) {
				/* Preserve external job-control stops until SIGCONT
				 * or a fatal exit, including the native fault point. */
				if (ptrace(PTRACE_LISTEN, observer->parent, NULL, NULL))
					return -1;
			} else if (ptrace(PTRACE_CONT, observer->parent, NULL,
					  (void *)(long)(event ? 0 : signal)))
				return -1;
			continue;
		}
		if (cancelling && !interrupted) {
			if (ptrace(PTRACE_INTERRUPT, observer->parent, NULL, NULL))
				return -1;
			interrupted = true;
		}
		if (cancelling) {
			long long now = factorybox_observer_milliseconds();
			if (now < 0 || now >= cancel_deadline) {
				errno = ETIMEDOUT;
				return -1;
			}
			timeout = (int)(cancel_deadline - now);
		}
		do {
			ready = poll(events, 2, timeout);
		} while (ready < 0 && errno == EINTR);
		if (ready <= 0) {
			if (!ready)
				errno = ETIMEDOUT;
			return -1;
		}
		if (events[0].revents & POLLIN) {
			char command;
			if (read(life, &command, 1) != 1 || command != 'C')
				return -1;
			cancelling = true;
			cancel_deadline = factorybox_observer_milliseconds() + 2000;
		}
		if (events[1].revents & POLLIN && factorybox_observer_drain(observer->signals))
			return -1;
		if (events[0].revents & (POLLERR | POLLNVAL) ||
		    (events[0].revents & POLLHUP && !cancelling && !observer->held))
			return -1;
	}
}

#endif
