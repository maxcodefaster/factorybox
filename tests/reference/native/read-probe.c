#define _GNU_SOURCE
#include <assert.h>
#include <dirent.h>
#include <stdio.h>
#include <sys/wait.h>
#include <time.h>
#include "pipe-read.c"

static int descriptor_count(void)
{
	DIR *directory = opendir("/proc/self/fd");
	struct dirent *entry;
	int count = 0;
	assert(directory);
	while ((entry = readdir(directory))) if (entry->d_name[0] != '.') count++;
	assert(!closedir(directory));
	return count;
}

static void verify(struct pipe_page_file *file, uint64_t offset, size_t bytes)
{
	unsigned char buffer[8192];
	assert(bytes <= sizeof(buffer));
	assert(pipe_read_pread(file, buffer, bytes, offset) == (ssize_t)bytes);
	for (size_t index = 0; index < bytes; index++) assert(buffer[index] == (offset + index < 4096 ? 17 : 29));
}

static void verify_batch(struct pipe_page_file *file, uint64_t offset, size_t bytes)
{
	unsigned char buffer[8192];
	assert(bytes <= sizeof(buffer));
	assert(pipe_read_pread(file, buffer, bytes, offset) == (ssize_t)bytes);
	for (size_t index = 0; index < bytes; index++) {
		uint64_t position = offset + index, page = position / 4096, within = position % 4096;
		assert(buffer[index] == (unsigned char)((page * 37 + within * 17 + within / 31) % 251));
	}
}

int main(int argc, char **argv)
{
	struct pipe_page_file *file = NULL;
	assert(argc == 4);
	int original = open(argv[2], O_RDONLY | O_CLOEXEC);
	assert(original >= 0);
	int before = descriptor_count();
	int opened = pipe_read_open_at(argv[1], original, &file);
	if (!strcmp(argv[3], "reject")) {
		assert(opened < 0 && !file && descriptor_count() == before);
		close(original); return 0;
	}
	if (!strcmp(argv[3], "miss")) {
		assert(!opened && !file && descriptor_count() == before);
		close(original); return 0;
	}
	assert(opened == 1 && file);
	if (!strcmp(argv[3], "batch")) {
		assert(file->bytes == 520UL * 4096 && file->nr_spans == 520 && file->nr_fds == 262);
		for (unsigned index = 0; index < 520; index++) verify_batch(file, index * 4096UL, 4096);
		pid_t child = fork();
		assert(child >= 0);
		if (!child) {
			for (unsigned index = 0; index < 128; index++) {
				uint64_t offset = (index * 31337UL) % (file->bytes - 8192);
				verify_batch(file, offset, 8192);
			}
			assert(!pipe_read_close(file)); close(original); _exit(0);
		}
		for (unsigned index = 0; index < 128; index++)
			verify_batch(file, (index * 37139UL) % (file->bytes - 8192), 8192);
		int status;
		assert(waitpid(child, &status, 0) == child && WIFEXITED(status) && !WEXITSTATUS(status));
		assert(!pipe_read_close(file));
		assert(descriptor_count() == before && !close(original));
		puts("{\"status\":\"passed\",\"actual_c_reader\":true,\"metadata_spans\":520,\"transferred_fds\":262,\"all_bytes_correct\":true,\"nonuniform_bytes_and_forked_cross_span_reads\":true}");
		return 0;
	}
	assert(file->bytes == 8192);
	verify(file, 0, 8192);
	if (!strcmp(argv[3], "replaced")) {
		int victim = file->fds[1];
		int foreign = memfd_create("owned-foreign-control", MFD_CLOEXEC);
		assert(foreign >= 0 && dup3(foreign, victim, O_CLOEXEC) == victim);
		unsigned char page[4096];
		assert(pipe_read_pread(file, page, sizeof(page), 0) == -1);
		assert(pipe_read_close(file) == -1);
		struct stat stat;
		assert(!fstat(victim, &stat) && S_ISREG(stat.st_mode));
		assert(!close(victim) && !close(foreign) && !close(original));
		assert(descriptor_count() == before - 1);
		return 0;
	}
	if (!strcmp(argv[3], "drained")) {
		unsigned char page[4096];
		assert(read(file->fds[1], page, sizeof(page)) == sizeof(page));
		assert(pipe_read_pread(file, page, sizeof(page), 0) == -1);
		assert(!pipe_read_close(file));
		assert(descriptor_count() == before);
		close(original); return 0;
	}
	if (!strcmp(argv[3], "die")) _exit(0);
	if (!strcmp(argv[3], "short")) {
		unsigned char page[4096];
		assert(pipe_read_pread(file, page, 17, 8180) == 12);
		assert(pipe_read_pread(file, page, 1, 8192) == -1);
		assert(!pipe_read_close(file));
		assert(descriptor_count() == before);
		close(original); return 0;
	}
	for (unsigned index = 0; index < 1024; index++) {
		uint64_t offset = (index * 31337U) % 8192;
		size_t bytes = 1 + (index * 71U) % 8192;
		if (bytes > 8192 - offset) bytes = 8192 - offset;
		verify(file, offset, bytes);
	}
	unsigned char first[17], second[4096], last[4079];
	struct iovec vector[] = {{first, sizeof(first)}, {second, sizeof(second)}, {last, sizeof(last)}};
	assert(pipe_read_preadv(file, vector, 3, 0) == 8192);
	for (unsigned index = 0; index < sizeof(first); index++) assert(first[index] == 17);
	for (unsigned index = 0; index < sizeof(second); index++) assert(second[index] == (index + 17 < 4096 ? 17 : 29));
	for (unsigned index = 0; index < sizeof(last); index++) assert(last[index] == 29);
	if (!strcmp(argv[3], "fork")) {
		pid_t child = fork();
		assert(child >= 0);
		if (!child) {
			for (unsigned index = 0; index < 64; index++) verify(file, index * 61U, 4096);
			assert(!pipe_read_close(file));
			close(original);
			_exit(0);
		}
		for (unsigned index = 0; index < 64; index++) verify(file, index * 59U, 4096);
		int status;
		assert(waitpid(child, &status, 0) == child && WIFEXITED(status) && !WEXITSTATUS(status));
	}
	for (unsigned index = 1; index < file->nr_fds - 1; index++) {
		int available;
		assert(!ioctl(file->fds[index], FIONREAD, &available) &&
		       available == file->identities[index].available);
	}
	assert(!pipe_read_close(file));
	assert(descriptor_count() == before);
	assert(!close(original));
	printf("{\"status\":\"passed\",\"actual_c_reader\":true,\"mode\":\"%s\","
	       "\"random_reads\":1024,\"mixed_unaligned_and_vector_bytes_correct\":true,"
	       "\"original_pipe_bytes_not_consumed\":true,\"owned_descriptors_actually_closed\":true}\n", argv[3]);
	return 0;
}
