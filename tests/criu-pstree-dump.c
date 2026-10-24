/* Exercise the selected CRIU dump_pstree() body, extracted without edits.
 * The image writer and namespace query are controlled at their real call seam.
 * A later validation/allocation failure must never inherit a previous write's
 * success and publish a truncated process tree as a successful checkpoint.
 */
#include <stdbool.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <sys/types.h>

enum { TASK_ALIVE = 1, TASK_DEAD = 2, CR_FD_PSTREE = 1, O_DUMP = 1, PB_PSTREE = 1 };
struct pid { int real, state; struct { int virt; } ns[1]; };
struct pstree_item {
	struct pid *pid, *threads;
	struct pstree_item *parent, *next;
	int sid, pgid, nr_threads;
};
typedef struct {
	uint32_t pid, ppid, pgid, sid, own_pid, *threads;
	int n_threads;
	bool has_own_pid, has_zombie_parent_pidns, zombie_parent_pidns;
} PstreeEntry;
struct cr_img { int unused; };
static struct { bool shell_job; } opts = { true };
static struct pstree_item *root_item;
static int namespace_result, write_failure, allocation_failure, writes, visits, errors;
static bool nested;
#define PSTREE_ENTRY__INIT { 0 }
#define for_each_pstree_item(item) for ((item) = root_item; (item); (item) = (item)->next)
#define OPT_SHELL_JOB "shell-job"
#define pr_info(...) ((void)0)
#define pr_err(...) ((void)++errors)
#define xfree free
static int vpid(struct pstree_item *item) { return item->pid->ns[0].virt; }
static bool nested_ns_enabled(void) { return nested; }
static int nested_ns_dump_zombie_parent(struct pstree_item *item)
{
	if (!item || item->pid->state != TASK_DEAD)
		abort();
	return namespace_result;
}
static int nested_ns_dump_own_pid(int real, int fallback)
{
	if (real <= 0 || fallback <= 0)
		abort();
	return fallback;
}
static struct cr_img *open_image(int type, int mode)
{
	static struct cr_img image;
	if (type != CR_FD_PSTREE || mode != O_DUMP)
		abort();
	return &image;
}
static void close_image(struct cr_img *image) { if (!image) abort(); }
static void *xmalloc(size_t size)
{
	visits++;
	return allocation_failure == visits ? NULL : malloc(size);
}
static int pb_write_one(struct cr_img *image, PstreeEntry *entry, int type)
{
	if (!image || type != PB_PSTREE || !entry->threads || entry->n_threads != 1 ||
	    entry->pid != entry->threads[0])
		abort();
	if (write_failure == writes + 1)
		return -1;
	writes++;
	return 0;
}

#include "dump-pstree.inc"

int main(void)
{
	struct pid pids[4] = {
		{ .real = 101, .state = TASK_ALIVE, .ns = {{1}} },
		{ .real = 102, .state = TASK_ALIVE, .ns = {{2}} },
		{ .real = 103, .state = TASK_DEAD, .ns = {{3}} },
		{ .real = 104, .state = TASK_ALIVE, .ns = {{4}} },
	};
	struct pstree_item items[4];
	for (int i = 0; i < 4; i++)
		items[i] = (struct pstree_item) {
			.pid = &pids[i], .threads = &pids[i], .sid = 1, .pgid = 1, .nr_threads = 1,
			.parent = i ? &items[0] : NULL, .next = i < 3 ? &items[i + 1] : NULL,
		};
	const struct {
		const char *name;
		int ns, fail_write, fail_allocate, start, records;
		bool nested, success;
	} cases[] = {
		{ "complete-tree", 1, 0, 0, 0, 4, true, true },
		{ "zombie-namespace-mismatch-after-written-parents", 0, 0, 0, 0, 2, true, false },
		{ "zombie-namespace-read-error-after-written-parents", -1, 0, 0, 0, 2, true, false },
		{ "first-zombie-namespace-mismatch", 0, 0, 0, 2, 0, true, false },
		{ "allocation-error-after-written-parent", 1, 0, 2, 0, 1, true, false },
		{ "image-write-error-after-written-parent", 1, 2, 0, 0, 1, true, false },
		{ "ordinary-nonnested-tree", -1, 0, 0, 0, 4, false, true },
	};
	unsigned int failures = 0;
	for (size_t i = 0; i < sizeof(cases) / sizeof(cases[0]); i++) {
		root_item = &items[cases[i].start];
		namespace_result = cases[i].ns;
		write_failure = cases[i].fail_write;
		allocation_failure = cases[i].fail_allocate;
		nested = cases[i].nested;
		writes = visits = errors = 0;
		int ret = dump_pstree(root_item);
		bool passed = (ret == 0) == cases[i].success && writes == cases[i].records;
		printf("%s %s return=%d records=%d expected_success=%d errors=%d\n",
		       passed ? "PASS" : "FAIL", cases[i].name, ret, writes, cases[i].success, errors);
		failures += !passed;
	}
	printf("pstree-cases=%zu failures=%u\n", sizeof(cases) / sizeof(cases[0]), failures);
	return failures ? 1 : 0;
}
