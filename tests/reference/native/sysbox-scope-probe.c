#define _GNU_SOURCE
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include "sysbox-owned-scope.h"

int main(void)
{
	const char *valid[] = {
		"/sys/fs/cgroup/criu-probe-sysbox",
		"/sys/fs/cgroup/criu-probe-sysbox-copy",
		"/sys/fs/cgroup/criu-probe-branch-0123456789abcdef0123456789abcdef",
	};
	const char *invalid[] = {
		NULL, "", "/", "/sys/fs/cgroup", "/sys/fs/cgroup/",
		"/sys/fs/cgroup/criu-probe-sysbox-copy-foreign",
		"/sys/fs/cgroup/criu-probe-sysbox/init.scope",
		"/sys/fs/cgroup/criu-probe-sysbox/../host",
		"/sys/fs/cgroup/criu-probe-branch-0123456789abcdef0123456789abcde",
		"/sys/fs/cgroup/criu-probe-branch-0123456789abcdef0123456789abcdef0",
		"/sys/fs/cgroup/criu-probe-branch-0123456789abcdef0123456789abcdeg",
		"/sys/fs/cgroup/criu-probe-branch-0123456789abcdef0123456789abcdeF",
		"/sys/fs/cgroup/criu-probe-branch-0123456789abcdef0123456789abcdef/child",
		"/sys/fs/cgroup/criu-probe-branch-0123456789abcdef0123456789abcdef/../host",
	};
	size_t i, cases = 0;
	if (unsetenv("SYSBOX_FORK_COLLECT_SCOPE") || sysbox_owned_scope_root(valid[0]))
		return 1;
	cases++;
	if (setenv("SYSBOX_FORK_COLLECT_SCOPE", "0", 1) || sysbox_owned_scope_root(valid[0]))
		return 2;
	cases++;
	if (setenv("SYSBOX_FORK_COLLECT_SCOPE", "1", 1))
		return 3;
	for (i = 0; i < sizeof(valid) / sizeof(valid[0]); i++) {
		const char *root = sysbox_owned_scope_root(valid[i]);
		if (!root || strcmp(root, valid[i] + strlen("/sys/fs/cgroup")))
			return 4;
		cases++;
	}
	for (i = 0; i < sizeof(invalid) / sizeof(invalid[0]); i++) {
		if (sysbox_owned_scope_root(invalid[i]))
			return 5;
		cases++;
	}
	printf("native_owned_scope_cases=%zu failures=0\n", cases);
	return 0;
}
