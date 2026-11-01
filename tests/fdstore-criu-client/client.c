#define _GNU_SOURCE
#include <dirent.h>
#include <errno.h>
#include <fcntl.h>
#include <limits.h>
#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/resource.h>
#include <sys/socket.h>
#include <sys/stat.h>
#include <sys/time.h>
#include <sys/un.h>
#include <unistd.h>

#define FD_TYPES__REG 1
#define FD_TYPES__INOTIFY 2
#define FD_TYPES__FANOTIFY 3
#define xmalloc malloc
#define xrealloc realloc
#define xstrdup strdup
#define xfree free
#define pr_info(...) fprintf(stderr, __VA_ARGS__)
#define pr_err(...) fprintf(stderr, __VA_ARGS__)

static struct {
	char *factorybox_fdstore;
} opts;
struct file_operations {
	int type;
};
struct file_desc {
	unsigned int id;
	struct file_operations *ops;
};
static struct {
	int fd;
	char *name;
} inherited[1024];
static unsigned int inherited_count;

static int inherit_fd_add(int fd, char *name)
{
	if (inherited_count >= 1024 || fcntl(fd, F_GETFD) < 0)
		return -1;
	inherited[inherited_count].fd = fd;
	inherited[inherited_count].name = strdup(name);
	if (!inherited[inherited_count].name)
		return -1;
	inherited_count++;
	return 0;
}

static int inherit_fd_lookup_id(char *name)
{
	unsigned int index;
	for (index = 0; index < inherited_count; index++)
		if (!strcmp(inherited[index].name, name))
			return dup(inherited[index].fd);
	return -1;
}

/* @CRIU_SHARED_DESCRIPTION_CLIENT@ */

static int open_descriptions(void)
{
	DIR *directory = opendir("/proc/self/fd");
	struct dirent *entry;
	int count = 0;
	if (!directory)
		return -1;
	while ((entry = readdir(directory)))
		if (strcmp(entry->d_name, ".") && strcmp(entry->d_name, ".."))
			count++;
	closedir(directory);
	return count;
}

int main(int argc, char **argv)
{
	struct file_operations regular = { .type = FD_TYPES__REG };
	unsigned int expected, index;
	int result = 1, before, loaded;
	if (argc != 6 || asprintf(&opts.factorybox_fdstore, "%s|%s", argv[1], argv[2]) < 0)
		return 2;
	expected = strtoul(argv[4], NULL, 10);
	before = open_descriptions();
	loaded = factorybox_load_fds();
	if (!strcmp(argv[3], "reject")) {
		if (loaded && !inherited_count && !factorybox_directory_count &&
		    before >= 0 && open_descriptions() == before) {
			puts("criu-rejected-generation-before-publication=0 leaked=0");
			result = 0;
		}
		goto out;
	}
	if (!expected || expected >= 1024 || loaded || inherited_count != expected)
		goto out;
	for (index = 0; index < inherited_count; index++) {
		struct stat metadata;
		char name[64], bytes[32] = { 0 };
		struct file_desc description = { .id = index + 1, .ops = &regular };
		int duplicate = -1;
		snprintf(name, sizeof(name), "factorybox-reg:%u", index + 1);
		if (strcmp(inherited[index].name, name) || fstat(inherited[index].fd, &metadata) ||
		    !(fcntl(inherited[index].fd, F_GETFD) & FD_CLOEXEC) ||
		    !factorybox_inherited_fd(&description, &duplicate))
			goto out;
		close(duplicate);
		if (!strcmp(argv[3], "directories")) {
			const char *path = factorybox_directory_path(&description);
			if (!S_ISDIR(metadata.st_mode) || !path || strcmp(path, argv[5]))
				goto out;
		} else if (!S_ISREG(metadata.st_mode) ||
			   pread(inherited[index].fd, bytes, 18, 0) != 18 ||
			   memcmp(bytes, "pinned description", 18)) {
			goto out;
		}
	}
	printf("criu-shared-description-client=%u mode=%s\n", inherited_count, argv[3]);
	result = 0;
out:
	for (index = 0; index < inherited_count; index++) {
		close(inherited[index].fd);
		free(inherited[index].name);
	}
	free(opts.factorybox_fdstore);
	return result;
}
