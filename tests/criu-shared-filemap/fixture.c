#define _GNU_SOURCE
#include <dirent.h>
#include <errno.h>
#include <fcntl.h>
#include <linux/openat2.h>
#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/mman.h>
#include <sys/stat.h>
#include <sys/syscall.h>
#include <unistd.h>

#define container_of(ptr, type, member) ((type *)((char *)(ptr) - offsetof(type, member)))
#define pr_err(...) fprintf(stderr, __VA_ARGS__)
#define pr_perror(...) do { fprintf(stderr, __VA_ARGS__); fprintf(stderr, ": %s\n", strerror(errno)); } while (0)
typedef uint32_t u32;

struct file_desc { int id; };
struct reg_file_entry {
	int flags;
	int mnt_id;
	uint64_t pos;
	void *fown;
};
struct reg_file_info {
	struct file_desc d;
	struct reg_file_entry *rfe;
};
static int pinned_fd = -1;

static bool factorybox_inherited_fd(struct file_desc *d, int *fd)
{
	(void)d;
	*fd = dup(pinned_fd);
	return *fd >= 0;
}

static const char *factorybox_directory_path(struct file_desc *d)
{
	(void)d;
	return NULL;
}

static int mntns_get_root_by_mnt_id(int id)
{
	(void)id;
	return -1;
}

static int sys_openat2(int fd, const char *path, struct open_how *how, size_t size)
{
	return syscall(SYS_openat2, fd, path, how, size);
}

static int restore_fown(int fd, void *owner)
{
	(void)owner;
	return fcntl(fd, F_GETFD) < 0 ? -1 : 0;
}

static int do_open_reg_noseek_flags(int root, struct reg_file_info *rfi, void *arg)
{
	(void)root;
	(void)rfi;
	(void)arg;
	abort(); /* A pinned regular object must not fall back to a pathname. */
}

static int normal_description(int root, struct reg_file_info *rfi, void *arg)
{
	(void)root;
	(void)rfi;
	(void)arg;
	abort();
}

static int restored_open_path(struct file_desc *d,
			     int (*open_cb)(int, struct reg_file_info *, void *), void *arg)
{
	int tmp = -1, mntns_root;
	struct reg_file_info *rfi;
	(void)open_cb;
	(void)arg;
	/* @CRIU_INHERITED_OPEN_BRANCH@ */
	errno = ENOENT;
	return -1;
}

static int descriptions(void)
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

#define REQUIRE(condition, message) do { \
	if (!(condition)) { fprintf(stderr, "%s: errno=%d\n", message, errno); return 1; } \
} while (0)

int main(void)
{
	char path[] = "/tmp/factorybox-mapping-XXXXXX";
	struct reg_file_entry entry = { .flags = O_RDONLY, .pos = 17 };
	struct reg_file_info info = { .d = { .id = 261 }, .rfe = &entry };
	struct stat original, restored;
	unsigned char *mapping;
	unsigned char value;
	int writer = mkstemp(path), fd, flags, before;
	REQUIRE(writer >= 0 && !ftruncate(writer, 32768), "create owned mapping");
	value = 0x5a;
	REQUIRE(pwrite(writer, &value, 1, 0) == 1, "write original bytes");
	pinned_fd = open(path, O_RDONLY | O_CLOEXEC);
	REQUIRE(pinned_fd >= 0 && lseek(pinned_fd, 17, SEEK_SET) == 17, "pin read-only original");
	REQUIRE(!fstat(pinned_fd, &original), "original kernel identity");
	before = descriptions();

	fd = restored_open_path(&info.d, normal_description, NULL);
	REQUIRE(fd >= 0 && (fcntl(fd, F_GETFL) & O_ACCMODE) == O_RDONLY,
		"normal descriptor must retain read-only access");
	REQUIRE(lseek(fd, 0, SEEK_CUR) == 17, "normal descriptor must retain its open description");
	close(fd);

	REQUIRE(!unlink(path), "unlink original name while keeping its kernel object");
	fd = open(path, O_CREAT | O_EXCL | O_RDWR | O_CLOEXEC, 0600);
	REQUIRE(fd >= 0 && write(fd, "replacement", 11) == 11, "create independent replacement");
	close(fd);
	flags = O_RDWR;
	fd = restored_open_path(&info.d, do_open_reg_noseek_flags, &flags);
	REQUIRE(fd >= 0, "open pinned writable mapping");
	REQUIRE(!fstat(fd, &restored) && original.st_dev == restored.st_dev &&
		original.st_ino == restored.st_ino, "mapping must not adopt a replacement pathname");
	mapping = mmap(NULL, 32768, PROT_READ | PROT_WRITE, MAP_SHARED, fd, 0);
	REQUIRE(mapping != MAP_FAILED, "restore writable shared mapping from read-only pinned object");
	REQUIRE(mapping[0] == 0x5a, "mapped original bytes");
	mapping[0] = 0x6b;
	REQUIRE(!msync(mapping, 32768, MS_SYNC) && !munmap(mapping, 32768), "write through original mapping");
	close(fd);
	REQUIRE(pread(writer, &value, 1, 0) == 1 && value == 0x6b, "write reached original inode");
	REQUIRE((fcntl(pinned_fd, F_GETFL) & O_ACCMODE) == O_RDONLY &&
		lseek(pinned_fd, 0, SEEK_CUR) == 17, "mapping must not upgrade or seek original description");

	flags = O_RDONLY;
	fd = restored_open_path(&info.d, do_open_reg_noseek_flags, &flags);
	REQUIRE(fd >= 0 && (fcntl(fd, F_GETFL) & O_ACCMODE) == O_RDONLY,
		"read-only mapping must not gain write access");
	close(fd);
	REQUIRE(descriptions() == before, "mapping must not leak received or reopened descriptors");
	fd = open(path, O_RDONLY | O_CLOEXEC);
	REQUIRE(fd >= 0 && pread(fd, &value, 1, 0) == 1 && value == 'r',
		"replacement pathname must stay unchanged");
	close(fd);
	close(pinned_fd);
	close(writer);
	REQUIRE(!unlink(path), "remove only owned replacement");
	puts("shared-filemap writable=1 original-inode=1 normal-readonly=1 original-offset=1 replacement-unchanged=1 leaks=0");
	return 0;
}
