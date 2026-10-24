/* A shim-like parent outside the PID namespace of its waitable child. */
#define _GNU_SOURCE
#include <errno.h>
#include <fcntl.h>
#include <sched.h>
#include <signal.h>
#include <stdio.h>
#include <stdlib.h>
#include <sys/stat.h>
#include <sys/types.h>
#include <sys/wait.h>
#include <unistd.h>

static int control[2], ready[2];
static volatile sig_atomic_t stopped;
static void stop(int number) { (void)number; stopped = 1; }

static int inner_init(void *unused)
{
	(void)unused;
	char byte = 'R';
	close(control[1]);
	close(ready[0]);
	if (getpid() != 1 || write(ready[1], &byte, 1) != 1)
		return 1;
	close(ready[1]);
	return read(control[0], &byte, 1) == 1 && byte == 'X' ? 0 : 1;
}

static unsigned long long namespace_inode(pid_t pid)
{
	char path[80];
	struct stat st;
	snprintf(path, sizeof(path), "/proc/%d/ns/pid", (int)pid);
	if (stat(path, &st)) { perror("stat namespace"); exit(1); }
	return (unsigned long long)st.st_ino;
}

int main(int argc, char **argv)
{
	if (argc != 3 || pipe(control) || pipe(ready))
		return 1;
	if (signal(SIGTERM, stop) == SIG_ERR)
		return 1;
	void *stack = malloc(1 << 20);
	if (!stack)
		return 1;
	pid_t init = clone(inner_init, (char *)stack + (1 << 20), CLONE_NEWPID | SIGCHLD, NULL);
	if (init < 0) { perror("clone init"); return 1; }
	close(control[0]);
	close(ready[1]);
	char byte;
	if (read(ready[0], &byte, 1) != 1 || byte != 'R')
		return 1;
	close(ready[0]);
	int own = open("/proc/self/ns/pid", O_RDONLY | O_CLOEXEC);
	char path[80];
	snprintf(path, sizeof(path), "/proc/%d/ns/pid", (int)init);
	int target = open(path, O_RDONLY | O_CLOEXEC), report[2];
	if (own < 0 || target < 0 || pipe(report) || setns(target, CLONE_NEWPID)) {
		perror("enter PID namespace for children"); return 1;
	}
	pid_t zombie = fork();
	if (zombie < 0) { perror("fork zombie"); return 1; }
	if (!zombie) {
		close(report[0]);
		pid_t inner = getpid();
		if (write(report[1], &inner, sizeof(inner)) != sizeof(inner))
			_exit(99);
		_exit(47);
	}
	if (setns(own, CLONE_NEWPID)) { perror("reset PID namespace for children"); return 1; }
	close(own);
	close(target);
	close(report[1]);
	pid_t child_own;
	if (read(report[0], &child_own, sizeof(child_own)) != sizeof(child_own) || child_own != 2)
		return 1;
	close(report[0]);
	siginfo_t info;
	if (waitid(P_PID, zombie, &info, WEXITED | WNOWAIT) || info.si_code != CLD_EXITED || info.si_status != 47)
		return 1;
	unsigned long long parent_ns = namespace_inode(getpid()), child_ns = namespace_inode(zombie);
	if (parent_ns == child_ns || child_ns != namespace_inode(init))
		return 1;
	FILE *out = fopen(argv[1], "wx");
	if (!out)
		return 1;
	fprintf(out, "{\"parent\":%d,\"init\":%d,\"zombie\":%d,\"child_own_pid\":%d,"
		"\"parent_namespace\":%llu,\"child_namespace\":%llu}\n",
		(int)getpid(), (int)init, (int)zombie, (int)child_own, parent_ns, child_ns);
	if (fclose(out))
		return 1;
	while (!stopped)
		pause();
	/* After restore, the exact original parent must still own and reap this
	 * child by its original outer PID and receive its original exit status. */
	if (namespace_inode(zombie) != namespace_inode(init) || namespace_inode(zombie) == namespace_inode(getpid()))
		return 1;
	int status;
	if (waitpid(zombie, &status, 0) != zombie || !WIFEXITED(status) || WEXITSTATUS(status) != 47)
		return 1;
	if (write(control[1], "X", 1) != 1 || waitpid(init, &status, 0) != init || !WIFEXITED(status) || WEXITSTATUS(status))
		return 1;
	close(control[1]);
	out = fopen(argv[2], "wx");
	if (!out)
		return 1;
	fprintf(out, "{\"passed\":true,\"waitable_child_pid\":%d,\"exit_status\":47}\n", (int)zombie);
	return fclose(out) ? 1 : 0;
}
