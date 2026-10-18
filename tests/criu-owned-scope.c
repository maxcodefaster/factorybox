#define _POSIX_C_SOURCE 200809L
#include <stdbool.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "sysbox-owned-scope.h"

struct scope_case {
	const char *name;
	const char *mode;
	const char *path;
	bool accepted;
};

int main(void)
{
	const struct scope_case cases[] = {
		{"legacy-source", "1", "/sys/fs/cgroup/criu-probe-sysbox", true},
		{"legacy-copy", "1", "/sys/fs/cgroup/criu-probe-sysbox-copy", true},
		{"legacy-descendant", "1", "/sys/fs/cgroup/criu-probe-branch-0123456789abcdef0123456789abcdef", true},
		{"factorybox-zero", "1", "/sys/fs/cgroup/fbx-00000000000000000000000000000000", true},
		{"factorybox-hex", "1", "/sys/fs/cgroup/fbx-0123456789abcdef0123456789abcdef", true},
		{"factorybox-ff", "1", "/sys/fs/cgroup/fbx-ffffffffffffffffffffffffffffffff", true},
		{"unset-mode", NULL, "/sys/fs/cgroup/fbx-0123456789abcdef0123456789abcdef", false},
		{"disabled-mode", "0", "/sys/fs/cgroup/fbx-0123456789abcdef0123456789abcdef", false},
		{"text-mode", "true", "/sys/fs/cgroup/fbx-0123456789abcdef0123456789abcdef", false},
		{"extended-mode", "11", "/sys/fs/cgroup/fbx-0123456789abcdef0123456789abcdef", false},
		{"spaced-mode", " 1", "/sys/fs/cgroup/fbx-0123456789abcdef0123456789abcdef", false},
		{"legacy-disabled", NULL, "/sys/fs/cgroup/criu-probe-sysbox", false},
		{"null-path", "1", NULL, false},
		{"empty-path", "1", "", false},
		{"root", "1", "/sys/fs/cgroup", false},
		{"relative", "1", "sys/fs/cgroup/fbx-0123456789abcdef0123456789abcdef", false},
		{"different-mount", "1", "/tmp/cgroup/fbx-0123456789abcdef0123456789abcdef", false},
		{"short-id", "1", "/sys/fs/cgroup/fbx-0123456789abcdef0123456789abcde", false},
		{"long-id", "1", "/sys/fs/cgroup/fbx-0123456789abcdef0123456789abcdef0", false},
		{"uppercase-id", "1", "/sys/fs/cgroup/fbx-0123456789abcdef0123456789abcdeF", false},
		{"invalid-id", "1", "/sys/fs/cgroup/fbx-0123456789abcdef0123456789abcdeg", false},
		{"missing-id", "1", "/sys/fs/cgroup/fbx-", false},
		{"double-separator", "1", "/sys/fs/cgroup//fbx-0123456789abcdef0123456789abcdef", false},
		{"trailing-separator", "1", "/sys/fs/cgroup/fbx-0123456789abcdef0123456789abcdef/", false},
		{"child-is-not-root", "1", "/sys/fs/cgroup/fbx-0123456789abcdef0123456789abcdef/init.scope", false},
		{"parent-traversal", "1", "/sys/fs/cgroup/fbx-0123456789abcdef0123456789abcdef/..", false},
		{"prefix-traversal", "1", "/sys/fs/cgroup/../cgroup/fbx-0123456789abcdef0123456789abcdef", false},
		{"unowned-name", "1", "/sys/fs/cgroup/factorybox", false},
		{"legacy-short-id", "1", "/sys/fs/cgroup/criu-probe-branch-0123456789abcdef", false},
		{"legacy-uppercase", "1", "/sys/fs/cgroup/criu-probe-branch-0123456789abcdef0123456789abcdeF", false},
		{"legacy-child", "1", "/sys/fs/cgroup/criu-probe-sysbox/init.scope", false},
	};
	unsigned int failures = 0;
	const size_t count = sizeof(cases) / sizeof(cases[0]);

	for (size_t i = 0; i < count; i++) {
		const struct scope_case *test = &cases[i];
		if ((test->mode && setenv("SYSBOX_FORK_COLLECT_SCOPE", test->mode, 1)) ||
		    (!test->mode && unsetenv("SYSBOX_FORK_COLLECT_SCOPE"))) {
			perror("set scope mode");
			return 1;
		}
		const char *scope = sysbox_owned_scope_root(test->path);
		if ((scope != NULL) != test->accepted ||
		    (scope && (scope != test->path + strlen("/sys/fs/cgroup") || scope[0] != '/'))) {
			fprintf(stderr, "FAIL %s: accepted=%d expected=%d\n",
				test->name, scope != NULL, test->accepted);
			failures++;
		}
	}
	printf("owned-scope-cases=%zu failures=%u\n", count, failures);
	return failures ? 1 : 0;
}
