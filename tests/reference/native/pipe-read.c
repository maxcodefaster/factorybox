#ifndef _GNU_SOURCE
#define _GNU_SOURCE
#endif
#include <errno.h>
#include <fcntl.h>
#include <limits.h>
#include <stdbool.h>
#include <stdint.h>
#include <stdlib.h>
#include <string.h>
#include <sys/ioctl.h>
#include <sys/mman.h>
#include <sys/socket.h>
#include <sys/stat.h>
#include <sys/statvfs.h>
#include <sys/vfs.h>
#include <sys/un.h>
#include <sys/xattr.h>
#include <unistd.h>
#include "pipe-read.h"

#define READ_MAGIC 0x31535050U
#define READ_MAX_BYTES (1ULL << 30)
#define READ_MAX_SPANS 8192U
#define READ_MAX_FDS 4098U
#define READ_PACKET_FDS 253U
#define READ_SPANS_PACKET 128U
#define READ_PIPE_LIMIT (4U << 20)
#define READ_CHUNK (64U << 10)
struct read_wire { uint32_t magic, op; uint64_t a, b, c, d; };
struct read_span { uint64_t offset, bytes, fd_index, pipe_offset; };
struct read_identity { dev_t device; ino_t inode; mode_t kind; int available, capacity; };
struct read_counters {
	uint64_t calls, bytes, private_bytes, eager_bytes, errors;
	uint64_t explicit_closes, released_data_fds, scratch_fork_resets;
};
struct pipe_page_file {
	uint64_t bytes, lease;
	unsigned nr_spans, nr_fds;
	struct read_span *spans;
	int *fds, socket_fd, scratch[2], null_fd, scratch_capacity;
	struct read_identity *identities;
	struct read_identity scratch_ids[3];
	struct read_counters *counters;
	pid_t scratch_owner;
};
struct read_packet {
	struct read_wire header;
	struct read_span spans[READ_SPANS_PACKET];
};

static int read_fd_identity(int fd, struct read_identity *identity)
{
	struct stat st;
	if (fstat(fd, &st)) return -1;
	identity->device = st.st_dev; identity->inode = st.st_ino;
	identity->kind = st.st_mode & S_IFMT;
	return 0;
}

static bool same_identity(int fd, const struct read_identity *expected)
{
	struct read_identity current;
	return !read_fd_identity(fd, &current) && current.device == expected->device &&
		current.inode == expected->inode && current.kind == expected->kind;
}

static int receive_packet(int fd, struct read_packet *packet, size_t *bytes,
			  int *received, unsigned *nr_received)
{
	union { struct cmsghdr align; char bytes[CMSG_SPACE(sizeof(int) * READ_PACKET_FDS)]; } control;
	struct iovec iov = {packet, sizeof(*packet)};
	struct msghdr message = {.msg_iov = &iov, .msg_iovlen = 1,
		.msg_control = control.bytes, .msg_controllen = sizeof(control.bytes)};
	struct cmsghdr *header;
	ssize_t count;
	bool invalid = false;
	memset(&control, 0, sizeof(control));
	*nr_received = 0;
	do { count = recvmsg(fd, &message, MSG_CMSG_CLOEXEC | MSG_TRUNC); } while (count < 0 && errno == EINTR);
	if (count <= 0) { errno = EPROTO; return -1; }
	for (header = CMSG_FIRSTHDR(&message); header; header = CMSG_NXTHDR(&message, header)) {
		unsigned descriptors, index;
		int *rights;
		if (header->cmsg_level != SOL_SOCKET || header->cmsg_type != SCM_RIGHTS ||
		    header->cmsg_len < CMSG_LEN(0) ||
		    (header->cmsg_len - CMSG_LEN(0)) % sizeof(int)) { invalid = true; continue; }
		descriptors = (header->cmsg_len - CMSG_LEN(0)) / sizeof(int);
		rights = (int *)CMSG_DATA(header);
		for (index = 0; index < descriptors; index++) {
			if (*nr_received == READ_PACKET_FDS) { close(rights[index]); invalid = true; }
			else received[(*nr_received)++] = rights[index];
		}
	}
	if (invalid || message.msg_flags & (MSG_TRUNC | MSG_CTRUNC) ||
	    count < (ssize_t)sizeof(packet->header) || count > (ssize_t)sizeof(*packet) ||
	    packet->header.magic != READ_MAGIC) {
		unsigned index;
		for (index = 0; index < *nr_received; index++) close(received[index]);
		*nr_received = 0; errno = EPROTO; return -1;
	}
	*bytes = count;
	return 0;
}

static int send_query(int socket_fd, int image_fd)
{
	struct stat st;
	struct read_wire query;
	union { struct cmsghdr align; char bytes[CMSG_SPACE(sizeof(int))]; } control;
	struct cmsghdr *header;
	struct iovec iov;
	struct msghdr message;
	ssize_t count;
	if (fstat(image_fd, &st) || !S_ISREG(st.st_mode) || st.st_size < 0 ||
	    (uint64_t)st.st_size > READ_MAX_BYTES) return -1;
	query = (struct read_wire){READ_MAGIC, 11, st.st_dev, st.st_ino, st.st_size, 1};
	iov = (struct iovec){&query, sizeof(query)};
	memset(&control, 0, sizeof(control)); memset(&message, 0, sizeof(message));
	message.msg_iov = &iov; message.msg_iovlen = 1;
	message.msg_control = control.bytes; message.msg_controllen = sizeof(control.bytes);
	header = CMSG_FIRSTHDR(&message); header->cmsg_level = SOL_SOCKET;
	header->cmsg_type = SCM_RIGHTS; header->cmsg_len = CMSG_LEN(sizeof(int));
	memcpy(CMSG_DATA(header), &image_fd, sizeof(image_fd));
	do { count = sendmsg(socket_fd, &message, MSG_NOSIGNAL); } while (count < 0 && errno == EINTR);
	return count == (ssize_t)sizeof(query) ? 0 : -1;
}

static void close_unpublished(struct pipe_page_file *file)
{
	unsigned index;
	if (!file) return;
	if (file->counters && file->counters != MAP_FAILED) munmap(file->counters, 4096);
	if (file->fds) for (index = 0; index < file->nr_fds; index++)
		if (file->fds[index] >= 0) close(file->fds[index]);
	if (file->socket_fd >= 0) close(file->socket_fd);
	free(file->fds); free(file->spans); free(file->identities); free(file);
}

int pipe_read_deferred(int image_fd)
{
	char marker;
	ssize_t n;
	if (image_fd < 0) return 0; /* Parent-only pagemaps can have no local payload. */
	n = fgetxattr(image_fd, "user.runtime_fork.deferred_v1", &marker, 1);
	if (n < 0 && errno == ENODATA) return 0;
	if (n != 1 || marker != 1) { errno = EPROTO; return -1; }
	return 1;
}

int pipe_read_open_at(const char *path, int image_fd, struct pipe_page_file **result)
{
	struct sockaddr_un address = {.sun_family = AF_UNIX};
	struct timeval timeout = {.tv_sec = 10};
	struct read_packet packet;
	struct pipe_page_file *file = NULL;
	int received[READ_PACKET_FDS], socket_fd = -1;
	unsigned nr_received, spans = 0, fds = 0, index;
	size_t bytes;
	uint64_t cursor = 0, retained = 0;
	bool done = false;
	*result = NULL;
	if (!path || strlen(path) >= sizeof(address.sun_path)) { errno = EINVAL; return -1; }
	strcpy(address.sun_path, path);
	socket_fd = socket(AF_UNIX, SOCK_SEQPACKET | SOCK_CLOEXEC, 0);
	if (socket_fd < 0) return -1;
	if (setsockopt(socket_fd, SOL_SOCKET, SO_RCVTIMEO, &timeout, sizeof(timeout)) ||
	    setsockopt(socket_fd, SOL_SOCKET, SO_SNDTIMEO, &timeout, sizeof(timeout)) ||
	    connect(socket_fd, (struct sockaddr *)&address, sizeof(address)) ||
	    send_query(socket_fd, image_fd) ||
	    receive_packet(socket_fd, &packet, &bytes, received, &nr_received)) goto error;
	if (nr_received) {
		for (index = 0; index < nr_received; index++) close(received[index]);
		goto error;
	}
	if (bytes != sizeof(packet.header) || packet.header.op != 11) goto error;
	if (!packet.header.a) {
		if (packet.header.b || packet.header.c || packet.header.d) goto error;
		if (pipe_read_deferred(image_fd) != 0) goto error;
		close(socket_fd); return 0;
	}
	if (packet.header.a > READ_MAX_BYTES || packet.header.a % 4096 ||
	    !packet.header.b || packet.header.b > READ_MAX_SPANS ||
	    packet.header.c < 2 || packet.header.c > READ_MAX_FDS || !packet.header.d) goto error;
	file = calloc(1, sizeof(*file));
	if (!file) goto error;
	file->socket_fd = socket_fd; socket_fd = -1;
	file->scratch[0] = file->scratch[1] = file->null_fd = -1;
	file->bytes = packet.header.a; file->nr_spans = packet.header.b;
	file->nr_fds = packet.header.c; file->lease = packet.header.d;
	file->spans = calloc(file->nr_spans, sizeof(*file->spans));
	file->fds = malloc(file->nr_fds * sizeof(*file->fds));
	if (file->fds) for (index = 0; index < file->nr_fds; index++) file->fds[index] = -1;
	file->identities = calloc(file->nr_fds, sizeof(*file->identities));
	if (!file->spans || !file->fds || !file->identities) goto error;
	while (!done) {
		if (receive_packet(file->socket_fd, &packet, &bytes, received, &nr_received)) goto error;
		if (packet.header.op == 12 && !nr_received &&
		    packet.header.a == spans && packet.header.b > 0 &&
		    packet.header.b <= READ_SPANS_PACKET && packet.header.b <= file->nr_spans - spans &&
		    !packet.header.c && !packet.header.d &&
		    bytes == sizeof(packet.header) + packet.header.b * sizeof(struct read_span)) {
			memcpy(&file->spans[spans], packet.spans, packet.header.b * sizeof(struct read_span));
			spans += packet.header.b;
		} else if (packet.header.op == 13 && bytes == sizeof(packet.header) &&
			   packet.header.a == fds && nr_received > 0 &&
			   packet.header.b == nr_received && nr_received <= file->nr_fds - fds &&
			   !packet.header.c && !packet.header.d) {
			for (index = 0; index < nr_received; index++) file->fds[fds++] = received[index];
		} else if (packet.header.op == 14 && bytes == sizeof(packet.header) &&
			   !nr_received && packet.header.a == file->bytes &&
			   packet.header.b == file->nr_spans && packet.header.c == file->nr_fds &&
			   packet.header.d == file->lease && spans == file->nr_spans && fds == file->nr_fds) {
			done = true;
		} else {
			for (index = 0; index < nr_received; index++) close(received[index]);
			goto error;
		}
	}
	for (index = 0; index < file->nr_fds; index++) {
		struct stat st;
		int flags = fcntl(file->fds[index], F_GETFL, 0), seals;
		if (read_fd_identity(file->fds[index], &file->identities[index]) || flags < 0) goto error;
		if (index == 0 || index == file->nr_fds - 1) {
			int required = F_SEAL_GROW | F_SEAL_SHRINK;
			if (index == 0) required |= F_SEAL_WRITE;
			seals = fcntl(file->fds[index], F_GET_SEALS);
			if (file->identities[index].kind != S_IFREG || (flags & O_ACCMODE) == O_WRONLY ||
			    fstat(file->fds[index], &st) || st.st_size != (index ? 4096 : (off_t)file->bytes)) goto error;
			if (seals < 0 || (seals & required) != required) {
				struct stat original;
				struct statfs filesystem;
				/* Only the exact original image can replace a sealed
				 * eager memfd, and CRIU must see a kernel readonly mount.
				 * The broker separately enforces this for role1 leases.
				 */
				if (index || (flags & O_ACCMODE) != O_RDONLY ||
				    fstat(image_fd, &original) || original.st_dev != st.st_dev ||
				    original.st_ino != st.st_ino || original.st_size != st.st_size ||
				    fstatfs(file->fds[index], &filesystem) || !(filesystem.f_flags & ST_RDONLY))
					goto error;
			}
		} else {
			struct read_identity *identity = &file->identities[index];
			if (identity->kind != S_IFIFO || (flags & O_ACCMODE) != O_RDONLY ||
			    ioctl(file->fds[index], FIONREAD, &identity->available)) goto error;
			identity->capacity = fcntl(file->fds[index], F_GETPIPE_SZ);
			if (identity->available <= 0 || identity->available % 4096 ||
			    (unsigned)identity->available > READ_PIPE_LIMIT || identity->capacity < identity->available ||
			    (unsigned)identity->capacity > READ_PIPE_LIMIT) goto error;
			retained += identity->available;
			if (retained > READ_MAX_BYTES) goto error;
		}
	}
	for (index = 0; index < file->nr_spans; index++) {
		struct read_span *span = &file->spans[index];
		if (span->offset != cursor || !span->bytes || span->bytes % 4096 ||
		    span->bytes > file->bytes - cursor || span->fd_index >= file->nr_fds - 1 ||
		    span->pipe_offset % 4096) goto error;
		if (span->fd_index) {
			uint64_t available = file->identities[span->fd_index].available;
			if (span->pipe_offset > available || span->bytes > available - span->pipe_offset) goto error;
		}
		cursor += span->bytes;
	}
	if (cursor != file->bytes) goto error;
	file->counters = mmap(NULL, 4096, PROT_READ | PROT_WRITE, MAP_SHARED, file->fds[file->nr_fds - 1], 0);
	if (file->counters == MAP_FAILED) { file->counters = NULL; goto error; }
	*result = file;
	return 1;
error:
	if (socket_fd >= 0) close(socket_fd);
	close_unpublished(file); errno = EPROTO; return -1;
}

int pipe_read_open(int image_fd, struct pipe_page_file **result)
{
	const char *mode = getenv("RUNTIME_FORK_PIPE_READ");
	const char *owned = getenv("RUNTIME_FORK_LAZY_CAPTURE_CONTROL");
	*result = NULL;
	if (!mode || !strcmp(mode, "0")) return 0;
	if (strcmp(mode, "lazy")) { errno = EINVAL; return -1; }
	if (!owned || strcmp(owned, "1")) return 0;
	if (image_fd < 0) return 0;
	if (geteuid()) { errno = EPERM; return -1; }
	return pipe_read_open_at("/experiment/pipe-store/read.sock", image_fd, result);
}

static int reset_scratch(struct pipe_page_file *file)
{
	unsigned index;
	if (file->scratch_owner == getpid()) return 0;
	if (file->scratch_owner) {
		int old[] = {file->scratch[0], file->scratch[1], file->null_fd};
		for (index = 0; index < 3; index++) {
			if (!same_identity(old[index], &file->scratch_ids[index]) || close(old[index])) return -1;
		}
		__atomic_fetch_add(&file->counters->scratch_fork_resets, 1, __ATOMIC_RELAXED);
	}
	file->scratch[0] = file->scratch[1] = file->null_fd = -1;
	file->scratch_owner = 0;
	if (pipe2(file->scratch, O_CLOEXEC | O_NONBLOCK)) return -1;
	if (read_fd_identity(file->scratch[0], &file->scratch_ids[0]) ||
	    read_fd_identity(file->scratch[1], &file->scratch_ids[1])) return -1;
	file->null_fd = open("/dev/null", O_WRONLY | O_CLOEXEC);
	if (file->null_fd < 0) return -1;
	if (read_fd_identity(file->null_fd, &file->scratch_ids[2])) return -1;
	file->scratch_capacity = fcntl(file->scratch[0], F_GETPIPE_SZ);
	if (file->scratch_capacity < 0) return -1;
	file->scratch_owner = getpid();
	return 0;
}

static int read_window(struct pipe_page_file *file, unsigned index, void *buffer, size_t bytes, uint64_t offset)
{
	struct read_identity *identity = &file->identities[index];
	int available;
	ssize_t count;
	size_t position = 0;
	if (!same_identity(file->fds[index], identity) ||
	    ioctl(file->fds[index], FIONREAD, &available) || available != identity->available ||
	    bytes > READ_CHUNK || offset > (uint64_t)available || bytes > (uint64_t)available - offset)
		return -1;
	if (reset_scratch(file)) return -1;
	if (identity->capacity > file->scratch_capacity) {
		int capacity = fcntl(file->scratch[1], F_SETPIPE_SZ, identity->capacity);
		if (capacity < identity->capacity) return -1;
		file->scratch_capacity = capacity;
	}
	if (ioctl(file->scratch[0], FIONREAD, &available) || available) return -1;
	do { count = tee(file->fds[index], file->scratch[1], offset + bytes, SPLICE_F_NONBLOCK); }
	while (count < 0 && errno == EINTR);
	/* Never retry a short tee from the same prefix. */
	if (count != (ssize_t)(offset + bytes)) return -1;
	while (offset) {
		count = splice(file->scratch[0], NULL, file->null_fd, NULL, offset, SPLICE_F_MOVE);
		if (count < 0 && errno == EINTR) continue;
		if (count <= 0) return -1;
		offset -= count;
	}
	while (position < bytes) {
		count = read(file->scratch[0], (char *)buffer + position, bytes - position);
		if (count < 0 && errno == EINTR) continue;
		if (count <= 0) return -1;
		position += count;
	}
	return 0;
}

static ssize_t read_bytes(struct pipe_page_file *file, void *buffer, size_t bytes, off_t offset)
{
	size_t position = 0;
	if (offset < 0 || (uint64_t)offset >= file->bytes) { errno = EIO; return -1; }
	if (bytes > file->bytes - (uint64_t)offset) bytes = file->bytes - (uint64_t)offset;
	while (position < bytes) {
		uint64_t cursor = (uint64_t)offset + position;
		unsigned low = 0, high = file->nr_spans;
		struct read_span *span;
		size_t count;
		while (low < high) {
			unsigned middle = low + (high - low) / 2;
			if (file->spans[middle].offset + file->spans[middle].bytes <= cursor) low = middle + 1;
			else high = middle;
		}
		if (low == file->nr_spans) return -1;
		span = &file->spans[low];
		count = span->offset + span->bytes - cursor;
		if (count > bytes - position) count = bytes - position;
		if (count > READ_CHUNK) count = READ_CHUNK;
		if (!span->fd_index) {
			ssize_t read;
			if (!same_identity(file->fds[0], &file->identities[0])) return -1;
			do { read = pread(file->fds[0], (char *)buffer + position, count, cursor); }
			while (read < 0 && errno == EINTR);
			if (read != (ssize_t)count) return -1;
			__atomic_fetch_add(&file->counters->eager_bytes, count, __ATOMIC_RELAXED);
		} else {
			if (read_window(file, span->fd_index, (char *)buffer + position, count,
					span->pipe_offset + cursor - span->offset)) return -1;
			__atomic_fetch_add(&file->counters->private_bytes, count, __ATOMIC_RELAXED);
		}
		position += count;
	}
	return position;
}

ssize_t pipe_read_preadv(struct pipe_page_file *file, const struct iovec *iov, int nr, off_t offset)
{
	ssize_t total = 0;
	int index;
	__atomic_fetch_add(&file->counters->calls, 1, __ATOMIC_RELAXED);
	if (!iov || nr <= 0 || nr > IOV_MAX) goto error;
	for (index = 0; index < nr; index++) {
		ssize_t count;
		if (!iov[index].iov_len) continue;
		if (iov[index].iov_len > READ_MAX_BYTES || (uint64_t)total > READ_MAX_BYTES - iov[index].iov_len) goto error;
		count = read_bytes(file, iov[index].iov_base, iov[index].iov_len, offset + total);
		if (count < 0) goto error;
		total += count;
		if ((size_t)count != iov[index].iov_len) break;
	}
	if (!total) goto error;
	__atomic_fetch_add(&file->counters->bytes, total, __ATOMIC_RELAXED);
	return total;
error:
	__atomic_fetch_add(&file->counters->errors, 1, __ATOMIC_RELAXED);
	errno = EIO; return -1;
}

ssize_t pipe_read_pread(struct pipe_page_file *file, void *buffer, size_t bytes, off_t offset)
{
	struct iovec iov = {buffer, bytes};
	return pipe_read_preadv(file, &iov, 1, offset);
}

int pipe_read_close(struct pipe_page_file *file)
{
	unsigned index, released = 0;
	int result = 0;
	if (!file) return 0;
	for (index = 0; index < file->nr_fds - 1; index++) {
		if (!same_identity(file->fds[index], &file->identities[index]) || close(file->fds[index]))
			result = -1;
		else { file->fds[index] = -1; released++; }
	}
	{
		int scratch[] = {file->scratch[0], file->scratch[1], file->null_fd};
		for (index = 0; index < 3; index++)
			if (scratch[index] >= 0 &&
			    (!same_identity(scratch[index], &file->scratch_ids[index]) || close(scratch[index]))) result = -1;
	}
	if (result) __atomic_fetch_add(&file->counters->errors, 1, __ATOMIC_RELAXED);
	__atomic_fetch_add(&file->counters->released_data_fds, released, __ATOMIC_RELAXED);
	__atomic_fetch_add(&file->counters->explicit_closes, 1, __ATOMIC_RELAXED);
	if (munmap(file->counters, 4096)) result = -1;
	index = file->nr_fds - 1;
	if (!same_identity(file->fds[index], &file->identities[index]) || close(file->fds[index])) result = -1;
	if (close(file->socket_fd)) result = -1;
	free(file->fds); free(file->identities); free(file->spans); free(file);
	return result;
}
