#ifndef SYSBOX_OWNED_SCOPE_H
#define SYSBOX_OWNED_SCOPE_H

#include <stddef.h>
#include <stdlib.h>
#include <string.h>

/* Only canonical system-container roots inside the private worker namespace
 * are accepted. The caller still validates PID namespace ownership and
 * complete frozen cgroup membership before collecting processes.
 */
static inline const char *sysbox_owned_scope_root(const char *freezer)
{
	const char *mode = getenv("SYSBOX_FORK_COLLECT_SCOPE");
	const char *prefix = "/sys/fs/cgroup";
	const char *branch = "/sys/fs/cgroup/criu-probe-branch-";
	const char *id;
	size_t i;

	if (!mode || strcmp(mode, "1") || !freezer)
		return NULL;
	if (!strcmp(freezer, "/sys/fs/cgroup/criu-probe-sysbox") ||
	    !strcmp(freezer, "/sys/fs/cgroup/criu-probe-sysbox-copy"))
		return freezer + strlen(prefix);
	if (strncmp(freezer, branch, strlen(branch)))
		return NULL;
	id = freezer + strlen(branch);
	if (strlen(id) != 32)
		return NULL;
	for (i = 0; i < 32; i++) {
		if (!((id[i] >= '0' && id[i] <= '9') || (id[i] >= 'a' && id[i] <= 'f')))
			return NULL;
	}
	return freezer + strlen(prefix);
}

#endif
