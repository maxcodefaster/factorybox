#define _GNU_SOURCE
#include <grp.h>
#include <sched.h>
#include <stdio.h>
#include <stdlib.h>
#include <sys/mount.h>
#include <unistd.h>

/* Single-threaded fixture setup. The workload runs as guest user 1000 in
 * a nested mount namespace, using the path used by Factory Dev Containers. */
static void fail(const char *operation)
{
    perror(operation);
    _exit(1);
}

int main(void)
{
    if (getuid() != 0 || unshare(CLONE_NEWNS)) fail("private alias namespace");
    if (mount(NULL, "/", NULL, MS_PRIVATE | MS_REC, NULL)) fail("private propagation");
    if (mount("/home/coder/project", "/workspace/project", NULL, MS_BIND | MS_REC, NULL)) fail("project alias");
    if (chdir("/workspace/project")) fail("project cwd");
    if (setgroups(0, NULL) || setgid(1000) || setuid(1000)) fail("runtime user");
    if (setenv("FACTORYBOX_SHARED_SOURCE_ROOT", "/workspace/project", 1)) fail("workload path");
    execl("/opt/factorybox-example-actor", "/opt/factorybox-example-actor", (char *)NULL);
    fail("workload exec");
}
