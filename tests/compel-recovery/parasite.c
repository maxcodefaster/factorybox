#include <errno.h>
/* Deliberately new fixture payload, not a replacement for an upstream test. */
#include <time.h>
#include <compel/plugins/std.h>
#include <infect-rpc.h>

int parasite_trap_cmd(int command, void *args)
{
	(void)command;
	(void)args;
	return 0;
}

void parasite_cleanup(void)
{
}

int parasite_daemon_cmd(int command, void *args)
{
	if (command != PARASITE_USER_CMDS)
		return -EINVAL;
	*(volatile int *)args = 1;
	struct timespec delay = {.tv_sec = 0, .tv_nsec = 250000000};
	sys_nanosleep(&delay, NULL);
	return 0;
}
