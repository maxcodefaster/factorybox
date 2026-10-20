#define _GNU_SOURCE
/* Native mechanism proof only. This does not authorize production source
 * recovery: the actual interrupted-CRIU lifecycle gate remains separate. */
#include <dirent.h>
#include <errno.h>
#include <fcntl.h>
#include <poll.h>
#include <pthread.h>
#include <signal.h>
#include <stdint.h>
#include <stdatomic.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/mman.h>
#include <sys/prctl.h>
#include <sys/ptrace.h>
#include <sys/random.h>
#include <sys/stat.h>
#include <sys/syscall.h>
#include <sys/wait.h>
#include <unistd.h>

#include <compel/infect-rpc.h>
#include <compel/log.h>
#pragma GCC diagnostic push
#pragma GCC diagnostic ignored "-Wunused-parameter"
#include "parasite.h"
#pragma GCC diagnostic pop

struct actor_state {
	uint64_t nonce, counter, checksum, fp;
	uint64_t background[2], thread_errors;
	uint64_t pipe_default_unblocked;
};

struct armed_state {
	int tracer, guardian;
};

static int source_group = -1;
static atomic_uint_fast64_t background[2], thread_errors;

static void *background_actor(void *argument)
{
	uintptr_t index = (uintptr_t)argument;
	uint64_t marker = 0x400921fb54442d19ULL + index;
	uint64_t ram[1024];
	for (unsigned int offset = 0; offset < 1024; offset++)
		ram[offset] = marker ^ offset;
	asm volatile("fmov d8, %0" : : "r"(marker) : "d8");
	for (;;) {
		uint64_t actual;
		asm volatile("fmov %0, d8" : "=r"(actual));
		if (actual != marker) {
			atomic_fetch_add(&thread_errors, 1);
			return NULL;
		}
		for (unsigned int offset = 0; offset < 1024; offset++)
			if (ram[offset] != (marker ^ offset)) {
				atomic_fetch_add(&thread_errors, 1);
				return NULL;
			}
		atomic_fetch_add(&background[index], 1);
		usleep(1000);
	}
}

static int freezer(int frozen)
{
	int fd = openat(source_group, "cgroup.freeze", O_WRONLY | O_CLOEXEC | O_NOFOLLOW);
	if (fd < 0)
		return -1;
	char value = frozen ? '1' : '0';
	int failed = write(fd, &value, 1) != 1;
	close(fd);
	if (failed)
		return -1;
	for (int attempt = 0; attempt < 100; attempt++) {
		char events[4096] = {0};
		fd = openat(source_group, "cgroup.events", O_RDONLY | O_CLOEXEC | O_NOFOLLOW);
		if (fd < 0)
			return -1;
		ssize_t count = read(fd, events, sizeof(events) - 1);
		close(fd);
		if (count < 0)
			return -1;
		if (strstr(events, frozen ? "frozen 1\n" : "frozen 0\n"))
			return 0;
		usleep(1000);
	}
	return -1;
}

static int source_is_frozen(void)
{
	char value = 0;
	int fd = openat(source_group, "cgroup.freeze", O_RDONLY | O_CLOEXEC | O_NOFOLLOW);
	if (fd < 0)
		return 0;
	int count = read(fd, &value, 1);
	close(fd);
	return count == 1 && value == '1';
}

static void message(unsigned int level, const char *format, va_list args)
{
	fprintf(stderr, "compel[%u] ", level);
	vfprintf(stderr, format, args);
}

static int receive(int fd, void *data, size_t length)
{
	struct pollfd event = {.fd = fd, .events = POLLIN};
	size_t offset = 0;
	while (offset < length) {
		int ready = poll(&event, 1, 5000);
		if (ready != 1)
			return -1;
		ssize_t count = read(fd, (char *)data + offset, length - offset);
		if (count <= 0)
			return -1;
		offset += count;
	}
	return 0;
}

static int actor(int threaded)
{
	struct sigaction pipe_action = {.sa_handler = SIG_DFL};
	sigset_t pipe_mask;
	sigemptyset(&pipe_action.sa_mask);
	sigemptyset(&pipe_mask);
	sigaddset(&pipe_mask, SIGPIPE);
	if (sigaction(SIGPIPE, &pipe_action, NULL) || pthread_sigmask(SIG_UNBLOCK, &pipe_mask, NULL))
		return 3;
	size_t size = 32UL << 20;
	unsigned char *ram = mmap(NULL, size, PROT_READ | PROT_WRITE,
				 MAP_PRIVATE | MAP_ANONYMOUS, -1, 0);
	struct actor_state state = {0};
	if (ram == MAP_FAILED || getrandom(&state.nonce, sizeof(state.nonce), 0) != sizeof(state.nonce))
		return 1;
	for (size_t index = 0; index < size; index++)
		ram[index] = (unsigned char)(index * 37 + 11);
	if (threaded) {
		pthread_t threads[2];
		for (uintptr_t index = 0; index < 2; index++)
			if (pthread_create(&threads[index], NULL, background_actor, (void *)index))
				return 2;
	}
#if defined(__aarch64__)
	uint64_t marker = 0x400921fb54442d18ULL;
	asm volatile("fmov d8, %0" : : "r"(marker) : "d8");
#else
#error "This prototype only qualifies the selected ARM64 worker."
#endif
	for (;;) {
		int request;
		if (read(STDIN_FILENO, &request, sizeof(request)) != sizeof(request))
			return 1;
		if (request == 1) {
			state.counter++;
			ram[(state.counter * 4096) % size] ^= (unsigned char)state.counter;
		}
		state.checksum = 1469598103934665603ULL;
		for (size_t index = 0; index < size; index++)
			state.checksum = (state.checksum ^ ram[index]) * 1099511628211ULL;
		asm volatile("fmov %0, d8" : "=r"(state.fp));
		for (unsigned int index = 0; index < 2; index++)
			state.background[index] = atomic_load(&background[index]);
		state.thread_errors = atomic_load(&thread_errors);
		if (sigaction(SIGPIPE, NULL, &pipe_action) || pthread_sigmask(SIG_BLOCK, NULL, &pipe_mask))
			return 4;
		state.pipe_default_unblocked = pipe_action.sa_handler == SIG_DFL && !sigismember(&pipe_mask, SIGPIPE);
		if (write(STDOUT_FILENO, &state, sizeof(state)) != sizeof(state))
			return 1;
	}
}

static int ask(int input, int output, int request, struct actor_state *state)
{
	if (write(input, &request, sizeof(request)) != sizeof(request))
		return -1;
	return receive(output, state, sizeof(*state));
}

static int injected_maps(pid_t pid)
{
	char path[64], line[4096];
	int found = 0;
	snprintf(path, sizeof(path), "/proc/%d/maps", pid);
	FILE *maps = fopen(path, "r");
	if (!maps)
		return -1;
	while (fgets(line, sizeof(line), maps))
		if (strstr(line, "CRIUMFD"))
			found++;
	fclose(maps);
	return found;
}

static uint64_t descriptor_digest(pid_t pid)
{
	char directory[64], path[128], target[4096];
	uint64_t result = 1469598103934665603ULL;
	snprintf(directory, sizeof(directory), "/proc/%d/fd", pid);
	struct dirent **entries;
	int count = scandir(directory, &entries, NULL, alphasort);
	if (count < 0)
		return 0;
	for (int index = 0; index < count; index++) {
		struct dirent *entry = entries[index];
		if (entry->d_name[0] != '.') {
			int fd;
			if (sscanf(entry->d_name, "%d", &fd) != 1)
				exit(30);
			snprintf(path, sizeof(path), "%s/%d", directory, fd);
			ssize_t length = readlink(path, target, sizeof(target) - 1);
			if (length < 0)
				exit(31);
			for (char *name = entry->d_name; *name; name++)
				result = (result ^ (unsigned char)*name) * 1099511628211ULL;
			for (ssize_t offset = 0; offset < length; offset++)
				result = (result ^ (unsigned char)target[offset]) * 1099511628211ULL;
		}
		free(entry);
	}
	free(entries);
	return result;
}

struct tracked_thread {
	int tid, state;
};

static int tracer(pid_t target, int report, int with_guardian, int threaded, int busy)
{
	struct parasite_ctl *ctl;
	struct infect_ctx *context;
	int original_state, lifetime[2];
	pid_t guardian = 0;
	struct tracked_thread threads[64];
	int thread_count = 0;
	compel_log_init(message, COMPEL_LOG_DEBUG);
	char task_path[64];
	snprintf(task_path, sizeof(task_path), "/proc/%d/task", target);
	DIR *tasks = opendir(task_path);
	if (!tasks)
		return 17;
	struct dirent *entry;
	while ((entry = readdir(tasks))) {
		if (entry->d_name[0] == '.')
			continue;
		if (thread_count == 64 || sscanf(entry->d_name, "%d", &threads[thread_count].tid) != 1)
			return 18;
		thread_count++;
	}
	closedir(tasks);
	if (thread_count != (threaded ? 3 : 1))
		return 19;
	original_state = -1;
	for (int index = 0; index < thread_count; index++) {
		threads[index].state = compel_stop_task(threads[index].tid);
		if (threads[index].state < 0)
			return 10;
		if (threads[index].tid == target)
			original_state = threads[index].state;
	}
	if (original_state < 0)
		return 10;
	ctl = compel_prepare(target);
	if (!ctl)
		return 11;
	context = compel_infect_ctx(ctl);
	context->log_fd = STDERR_FILENO;
	parasite_setup_c_header(ctl);
	if (compel_infect(ctl, thread_count, sizeof(int)))
		return 12;
	if (with_guardian) {
		int original_tracer = syscall(SYS_pidfd_open, getpid(), 0);
		if (original_tracer < 0)
			return 16;
		if (pipe(lifetime))
			return 13;
		guardian = fork();
		if (guardian < 0)
			return 14;
		if (guardian == 0) {
			char status;
			struct sigaction children = {.sa_handler = SIG_DFL};
			sigemptyset(&children.sa_mask);
			if (sigaction(SIGCHLD, &children, NULL))
				_exit(32);
			close(lifetime[1]);
			alarm(10);
			/* This is a C fork after complete infection. Keep the live
			 * ctl, shared parasite mapping and duplicate RPC socket.
			 * No postmortem guess about original registers is made. */
			if (read(lifetime[0], &status, 1) != 0)
				_exit(20);
			struct pollfd dead_tracer = {.fd = original_tracer, .events = POLLIN};
			if (poll(&dead_tracer, 1, 5000) != 1 || !(dead_tracer.revents & POLLIN))
				_exit(27);
			if (source_group >= 0 && freezer(1))
				_exit(24);
			/* Seize and queue an interrupt for every non-leader while the
			 * original cgroup is still frozen. They must stop before
			 * reaching user code when the parasite leader is allowed to
			 * execute its existing cure routine. */
			for (int index = 0; index < thread_count; index++) {
				int tid = threads[index].tid;
				if (ptrace(PTRACE_SEIZE, tid, NULL, PTRACE_O_TRACESYSGOOD)) {
					perror("guardian PTRACE_SEIZE after original tracer pidfd exit");
					_exit(21);
				}
				if (tid != target && ptrace(PTRACE_INTERRUPT, tid, NULL, NULL))
					_exit(28);
			}
			if (source_group >= 0 && freezer(0))
				_exit(25);
			for (int index = 0; index < thread_count; index++) {
				int stopped;
				int tid = threads[index].tid;
				if (tid != target && (waitpid(tid, &stopped, __WALL) != tid || !WIFSTOPPED(stopped)))
					_exit(29);
			}
			if (compel_cure(ctl))
				_exit(22);
			if (source_group >= 0 && freezer(1))
				_exit(26);
			for (int index = 0; index < thread_count; index++)
				if (compel_resume_task(threads[index].tid, threads[index].state, threads[index].state))
					_exit(23);
			_exit(0);
		}
		close(lifetime[0]);
		close(original_tracer);
	}
	if (busy) {
		volatile int *started = compel_parasite_args(ctl, int);
		*started = 0;
		if (compel_rpc_call(PARASITE_USER_CMDS, ctl))
			return 33;
		int attempts = 0;
		while (!*started && attempts++ < 5000)
			usleep(100);
		if (!*started)
			return 34;
	}
	struct armed_state armed = {.tracer = getpid(), .guardian = guardian};
	if (write(report, &armed, sizeof(armed)) != sizeof(armed))
		return 15;
	/* The coordinator kills this actual tracer after its retained context
	 * is armed, while the parasite is alive in the original source. */
	for (;;)
		pause();
}

int main(int argc, char **argv)
{
	const char *enabled = getenv("FACTORYBOX_COMPEL_RECOVERY");
	if (!enabled || strcmp(enabled, "1"))
		return 2;
	if (argc == 2 && !strcmp(argv[1], "--actor"))
		return actor(0);
	if (argc == 2 && !strcmp(argv[1], "--actor-threaded"))
		return actor(1);
	int input[2], output[2], report[2], status, result = 1;
	pid_t target = -1, trace = -1, guardian = -1;
	struct actor_state before, after, advanced;
	struct armed_state armed;
	int busy = argc == 2 && !strcmp(argv[1], "busy-guardian");
	int threaded = argc == 2 && (!strcmp(argv[1], "threaded-guardian") || busy);
	int with_freezer = argc == 2 && (!strcmp(argv[1], "frozen-guardian") || threaded);
	int with_guardian = argc == 2 && (!strcmp(argv[1], "guardian") || with_freezer);
	char group_path[128] = {0};
	char membership[256] = {0};
	unsigned long long bound = 0;
	FILE *limit = fopen("/sys/fs/cgroup/memory.max", "r");
	FILE *own_group = fopen("/proc/self/cgroup", "r");
	if (!limit || !own_group || fscanf(limit, "%llu", &bound) != 1 ||
	    !bound || bound > (3ULL << 30) || !fgets(membership, sizeof(membership), own_group) ||
	    strcmp(membership, "0::/factorybox-unit-controller\n"))
		return 8;
	fclose(limit);
	fclose(own_group);
	setbuf(stdout, NULL);
	struct sigaction owned_children = {.sa_handler = SIG_DFL};
	sigemptyset(&owned_children.sa_mask);
	if (sigaction(SIGCHLD, &owned_children, NULL))
		return 9;
	if (prctl(PR_SET_CHILD_SUBREAPER, 1) || pipe(input) || pipe(output) || pipe(report))
		return 2;
	if (with_freezer) {
		snprintf(group_path, sizeof(group_path), "/sys/fs/cgroup/factorybox-compel-%d", getpid());
		if (mkdir(group_path, 0700))
			return 5;
		source_group = open(group_path, O_RDONLY | O_DIRECTORY | O_CLOEXEC | O_NOFOLLOW);
		if (source_group < 0)
			return 6;
	}
	target = fork();
	if (target == 0) {
		if (source_group >= 0) {
			int fd = openat(source_group, "cgroup.procs", O_WRONLY | O_CLOEXEC | O_NOFOLLOW);
			char pid[32];
			int length = snprintf(pid, sizeof(pid), "%d", getpid());
			if (fd < 0 || write(fd, pid, length) != length)
				_exit(7);
			close(fd);
		}
		dup2(input[0], STDIN_FILENO);
		dup2(output[1], STDOUT_FILENO);
		close(input[0]); close(input[1]);
		close(output[0]); close(output[1]);
		close(report[0]); close(report[1]);
		execl(argv[0], argv[0], threaded ? "--actor-threaded" : "--actor", NULL);
		_exit(3);
	}
	if (target < 0)
		return 4;
	close(input[0]); close(output[1]);
	if (ask(input[1], output[0], 0, &before))
		goto cleanup;
	uint64_t original_descriptors = descriptor_digest(target);
	if (!original_descriptors || before.fp != 0x400921fb54442d18ULL || !before.pipe_default_unblocked)
		goto cleanup;
	if (before.thread_errors || (threaded && (!before.background[0] || !before.background[1])))
		goto cleanup;
	trace = fork();
	if (trace == 0) {
		close(report[0]);
		_exit(tracer(target, report[1], with_guardian, threaded, busy));
	}
	if (trace < 0)
		goto cleanup;
	close(report[1]);
	if (receive(report[0], &armed, sizeof(armed)) || armed.tracer != trace ||
	    (with_guardian && armed.guardian <= 1))
		goto cleanup;
	guardian = armed.guardian;
	int maps_before = injected_maps(target);
	if (maps_before <= 0)
		goto cleanup;
	if (kill(trace, SIGKILL))
		goto cleanup;
	pid_t tracer_waited = waitpid(trace, &status, 0);
	if (tracer_waited == trace)
		trace = -1;
	if (tracer_waited <= 0 || !WIFSIGNALED(status) || WTERMSIG(status) != SIGKILL)
		goto cleanup;
	if (with_guardian) {
		pid_t guardian_waited = waitpid(guardian, &status, 0);
		if (guardian_waited == guardian)
			guardian = -1;
		if (guardian_waited <= 0 || !WIFEXITED(status) || WEXITSTATUS(status) != 0) {
			fprintf(stderr, "guardian recovery status=%#x\n", status);
			goto cleanup;
		}
	}
	int held_after_cure = source_group >= 0 && source_is_frozen();
	if (source_group >= 0 && (!held_after_cure || injected_maps(target) != 0 || freezer(0)))
		goto cleanup;
	if (ask(input[1], output[0], 0, &after) ||
	    before.nonce != after.nonce || before.counter != after.counter ||
	    before.checksum != after.checksum || before.fp != after.fp || after.thread_errors ||
	    !after.pipe_default_unblocked ||
	    after.background[0] < before.background[0] || after.background[1] < before.background[1])
		goto cleanup;
	int maps_after = injected_maps(target);
	uint64_t final_descriptors = descriptor_digest(target);
	if (with_guardian && final_descriptors != original_descriptors)
		goto cleanup;
	if (ask(input[1], output[0], 1, &advanced) ||
	    advanced.nonce != before.nonce || advanced.counter != before.counter + 1 ||
	    advanced.checksum == before.checksum || advanced.fp != before.fp || advanced.thread_errors ||
	    !advanced.pipe_default_unblocked)
		goto cleanup;
	if (threaded && (advanced.background[0] <= after.background[0] || advanced.background[1] <= after.background[1]))
		goto cleanup;
	printf("{\"guardian\":%s,\"original_pid\":%d,\"ram_preserved\":true,"
	       "\"fp_preserved\":true,\"descriptors_preserved\":%s,\"useful_work\":true,\"sigpipe_default_unblocked\":true,"
	       "\"injected_maps_before\":%d,\"injected_maps_after\":%d}\n",
	       with_guardian ? "true" : "false", target,
	       final_descriptors == original_descriptors ? "true" : "false", maps_before, maps_after);
	if (with_freezer)
		printf("{\"frozen_guardian\":true,\"held_after_cure\":%s,\"injected_maps_after\":%d,"
		       "\"threads\":%d,\"background_work\":%s,\"busy_rpc\":%s}\n",
		       held_after_cure ? "true" : "false", maps_after, threaded ? 3 : 1,
		       threaded ? "true" : "false", busy ? "true" : "false");
	result = maps_after == 0 ? 0 : 1;
cleanup:
	if (result != 0 && target > 1) {
		siginfo_t exited = {0};
		if (!waitid(P_PID, target, &exited, WEXITED | WNOHANG | WNOWAIT) && exited.si_pid == target)
			fprintf(stderr, "original source exit: code=%d status=%d\n", exited.si_code, exited.si_status);
	}
	if (trace > 1) {
		kill(trace, SIGKILL);
		waitpid(trace, &status, 0);
	}
	if (guardian > 1) {
		kill(guardian, SIGKILL);
		waitpid(guardian, &status, 0);
	}
	if (target > 1) {
		kill(target, SIGKILL);
		waitpid(target, &status, 0);
	}
	if (source_group >= 0) {
		close(source_group);
		if (rmdir(group_path))
			result = 1;
	}
	return result;
}
