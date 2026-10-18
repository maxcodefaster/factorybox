#ifndef _GNU_SOURCE
#define _GNU_SOURCE
#endif
#include <errno.h>
#include <fcntl.h>
#include <stdint.h>
#include <stdlib.h>
#include <string.h>
#include <sys/ioctl.h>
#include <sys/socket.h>
#include <sys/stat.h>
#include <sys/un.h>
#include <sys/xattr.h>
#include <unistd.h>

#include "types.h"
#include "cr_options.h"
#include "image.h"
#include "page-xfer.h"
#include "page-pipe.h"
#include "page.h"
#include "vma.h"
#include "kerndat.h"
#include "mman.h"
#include "pstree.h"
#include "cow-capture.h"
#include "cow-pin-guard.h"
#include "pipe-capture.h"
#include "rseq-pages.h"
#include "sysbox-owned-scope.h"
#include "criu-log.h"

#define PIPE_CAPTURE_MAGIC 0x31535050U
#define PIPE_CAPTURE_SPANS 2048
#define PIPE_CAPTURE_MAX (4UL << 20)
struct capture_header { uint32_t magic, op; uint64_t a, b, c, d; };
struct capture_span { uint64_t offset, bytes, pipe_offset, eager; };
struct capture_packet {
	struct capture_header header;
	struct capture_span spans[PIPE_CAPTURE_SPANS];
};
static unsigned guard_logs;

static int capture_send(int socket_fd, void *data, size_t bytes, int fd)
{
	union { struct cmsghdr align; char bytes[CMSG_SPACE(sizeof(int))]; } control;
	struct iovec iov = {data, bytes};
	struct msghdr message = {.msg_iov = &iov, .msg_iovlen = 1};
	struct { uint32_t magic, op; int64_t status; } reply;
	ssize_t result;
	memset(&control, 0, sizeof(control));
	if (fd >= 0) {
		struct cmsghdr *rights;
		message.msg_control = control.bytes;
		message.msg_controllen = sizeof(control.bytes);
		rights = CMSG_FIRSTHDR(&message);
		rights->cmsg_level = SOL_SOCKET;
		rights->cmsg_type = SCM_RIGHTS;
		rights->cmsg_len = CMSG_LEN(sizeof(int));
		memcpy(CMSG_DATA(rights), &fd, sizeof(fd));
	}
	do { result = sendmsg(socket_fd, &message, MSG_NOSIGNAL); } while (result < 0 && errno == EINTR);
	if (result != (ssize_t)bytes) return -1;
	do { result = recv(socket_fd, &reply, sizeof(reply), MSG_TRUNC); } while (result < 0 && errno == EINTR);
	if (result != (ssize_t)sizeof(reply) || reply.magic != PIPE_CAPTURE_MAGIC ||
	    reply.op != ((struct capture_header *)data)->op || reply.status)
		return -1;
	return 0;
}

int pipe_capture_begin(struct page_xfer *xfer, int type, unsigned long pages_id)
{
	const char *enabled = getenv("RUNTIME_FORK_PIPE_CAPTURE");
	pid_t source;
	struct sockaddr_un address = {.sun_family = AF_UNIX};
	struct timeval timeout = {.tv_sec = 10};
	struct capture_header header;
	int fd;
	if (!enabled || !strcmp(enabled, "0")) return 0;
	if (strcmp(enabled, "observe") && strcmp(enabled, "defer-lazy")) return -1;
	if (opts.final_state != TASK_ALIVE || type != CR_FD_PAGEMAP) return 0;
	source = cow_capture_source();
	if (!source || opts.lazy_pages || opts.stream || opts.auto_dedup ||
	    !opts.nested_ns || !opts.freeze_cgroup ||
	    !sysbox_owned_scope_root(opts.freeze_cgroup))
		return -1;
	strcpy(address.sun_path, "/experiment/pipe-store/control.sock");
	fd = socket(AF_UNIX, SOCK_SEQPACKET | SOCK_CLOEXEC, 0);
	if (fd < 0) return -1;
	if (setsockopt(fd, SOL_SOCKET, SO_RCVTIMEO, &timeout, sizeof(timeout)) ||
	    setsockopt(fd, SOL_SOCKET, SO_SNDTIMEO, &timeout, sizeof(timeout)) ||
	    connect(fd, (struct sockaddr *)&address, sizeof(address))) goto error;
	header = (struct capture_header){
		.magic = PIPE_CAPTURE_MAGIC, .op = 1, .a = pages_id, .b = source,
		.c = !strcmp(enabled, "defer-lazy"),
	};
	if (capture_send(fd, &header, sizeof(header), img_raw_fd(xfer->pi))) goto error;
	xfer->pipe_capture_fd = fd;
	guard_logs = 0;
	return 0;
error:
	close(fd);
	pr_err("PIPE_CAPTURE_BEGIN failed source=%d pages=%lu\n", source, pages_id);
	return -1;
}

static int append_spans(struct page_xfer *xfer, struct capture_packet *packet,
		       uint64_t address, uint64_t bytes, uint64_t *pipe_offset, unsigned *count,
		       bool deferred, bool lazy)
{
	while (bytes) {
		struct vma_area *vma, *found = NULL;
		uint64_t length;
		uint64_t guard_flags = 0;
		bool private;
		list_for_each_entry(vma, &xfer->pipe_capture_vmas->h, list)
			if (address >= vma->e->start && address < vma->e->end) { found = vma; break; }
		if (!found || *count == PIPE_CAPTURE_SPANS || address % PAGE_SIZE || bytes % PAGE_SIZE)
			return -1;
		length = found->e->end - address;
		if (length > bytes) length = bytes;
		/* Be conservative about special/kernel/file mappings. Only ordinary
		 * anonymous private pages use retained pins in this first composition.
		 * Actual original eager output supplies every exception at finish.
		 */
		private = vma_area_is(found, VMA_AREA_REGULAR) &&
			vma_area_is(found, VMA_ANON_PRIVATE) &&
			(found->e->flags & MAP_PRIVATE) && (found->e->flags & MAP_ANONYMOUS) &&
			!(found->e->flags & (MAP_HUGETLB | MAP_DROPPABLE)) &&
			!vma_area_is(found, VMA_AREA_AIORING) && !vma_area_is(found, VMA_AREA_VVAR) &&
			!vma_area_is(found, VMA_AREA_VDSO) && !vma_area_is(found, VMA_AREA_GUARD);
		if (deferred && !lazy) private = false;
		if (private) {
			unsigned thread;
			if (!xfer->pipe_capture_item) return -1;
			for (thread = 0; thread < xfer->pipe_capture_item->nr_threads; thread++) {
				CoreEntry *core = xfer->pipe_capture_item->core[thread];
				RseqEntry *rseq = core->thread_core->rseq_entry;
				if (rseq && pipe_capture_clip_rseq(address, &length, rseq->rseq_abi_pointer,
						rseq->rseq_abi_size, PAGE_SIZE, &private))
					return -1;
			}
		}
		if (private) {
			size_t shared_length;
			int state;
			if (cow_capture_shared_prefix(address, length, &shared_length, &state))
				return -1;
			length = shared_length;
			if (state != COW_PIN_SHARED) {
				private = false;
				guard_flags = state == COW_PIN_UNAVAILABLE ? 16 : 8;
				if (guard_logs++ < 4)
					pr_warn("COW_PIN_GUARD_COPY source=%d address=%llx image_offset=%llu bytes=%llu state=%d\n",
						cow_capture_source(), (unsigned long long)address,
						(unsigned long long)(xfer->pipe_capture_offset + *pipe_offset),
						(unsigned long long)length, state);
			}
		}
		packet->spans[(*count)++] = (struct capture_span){
			.offset = xfer->pipe_capture_offset + *pipe_offset, .bytes = length,
			.pipe_offset = *pipe_offset,
			/* Low bits retain the existing lane; bit 2 identifies the
			 * exact first page of an ordinary private VMA for bounded
			 * diagnostic preimages in the owning keeper. */
			.eager = (private ? (deferred ? 2 : 0) : 1) | guard_flags |
				(private && address == found->e->start ? 4 : 0),
		};
		*pipe_offset += length;
		address += length; bytes -= length;
	}
	return 0;
}

/* Only the original transfer pipe is drained. The keeper owns the complete
 * immutable clone before this function can replace private lazy writes by
 * holes. Set the mandatory marker before the first omitted byte. */
static int write_deferred_buffer(struct page_xfer *xfer, struct page_pipe_buf *buffer,
				 struct capture_packet *packet, unsigned count)
{
	int image = img_raw_fd(xfer->pi), null_fd = -1, result = -1;
	unsigned index;
	off_t position = lseek(image, 0, SEEK_CUR);
	if (position < 0 || (uint64_t)position != xfer->pipe_capture_offset) return -1;
	for (index = 0; index < count; index++) {
		struct capture_span *span = &packet->spans[index];
		uint64_t remaining = span->bytes;
		bool skip = (span->eager & 3) == 2;
		if (skip) {
			char marker = 1;
			if (fsetxattr(image, "user.runtime_fork.deferred_v1", &marker, 1, 0)) goto out;
			if (null_fd < 0) null_fd = open("/dev/null", O_WRONLY | O_CLOEXEC);
			if (null_fd < 0) goto out;
		}
		while (remaining) {
			ssize_t n = splice(buffer->p[0], NULL, skip ? null_fd : image, NULL,
					   remaining, SPLICE_F_MOVE);
			if (n < 0 && errno == EINTR) continue;
			if (n <= 0) goto out;
			remaining -= n;
		}
		if (skip && lseek(image, span->bytes, SEEK_CUR) < 0) goto out;
	}
	if (ftruncate(image, xfer->pipe_capture_offset + packet->header.b)) goto out;
	result = 1;
out:
	if (null_fd >= 0) close(null_fd);
	return result;
}

int pipe_capture_buffer(struct page_xfer *xfer, struct page_pipe_buf *buffer)
{
	size_t bytes;
	int available, capacity;
	struct capture_packet *packet;
	uint64_t offset = 0;
	unsigned count = 0;
	int pipes[2] = {-1, -1}, result = -1, actual_capacity = -1, failure_errno = 0;
	ssize_t copied = -1;
	const char *stage = "spans";
	bool eager_fallback = false;
	bool deferred = !strcmp(getenv("RUNTIME_FORK_PIPE_CAPTURE") ?: "", "defer-lazy");
	if (xfer->pipe_capture_fd < 0) return 0;
	if (!buffer->pages_in) return 0;
	if (!xfer->pipe_capture_vmas || !xfer->transfer_lazy) return -1;
	bytes = buffer->pages_in * PAGE_SIZE;
	if (bytes > PIPE_CAPTURE_MAX || xfer->pipe_capture_offset > (1UL << 30) - bytes) return -1;
	capacity = fcntl(buffer->p[0], F_GETPIPE_SZ);
	if (capacity <= 0 || capacity > PIPE_CAPTURE_MAX ||
	    ioctl(buffer->p[0], FIONREAD, &available) || available < (int)bytes) return -1;
	packet = calloc(1, sizeof(*packet));
	if (!packet) return -1;
	for (unsigned index = 0; index < buffer->nr_segs; index++)
		if (append_spans(xfer, packet, (uintptr_t)buffer->iov[index].iov_base,
				 buffer->iov[index].iov_len, &offset, &count,
				 deferred, buffer->flags & PPB_LAZY)) goto out;
	stage = "span-length";
	if (offset != bytes) goto out;
	stage = "allocate-pipe";
	if (pipe2(pipes, O_CLOEXEC | O_NONBLOCK)) {
		failure_errno = errno;
		goto out;
	}
	stage = "resize-pipe";
	actual_capacity = fcntl(pipes[1], F_SETPIPE_SZ, capacity);
	if (actual_capacity != capacity) {
		failure_errno = actual_capacity < 0 ? errno : 0;
		/* The original writer already owns all bytes. If duplicating its
		 * pipe ring runs out of memory, publish this whole buffer as eager
		 * instead. The keeper validates and closes the transferred original
		 * read descriptor before acknowledging; it retains no pipe span.
		 * Only an exact whole buffer can take this path. No short tee or
		 * uncertain private page is ever retried or published.
		 */
		if (failure_errno == ENOMEM && available == (int)bytes) {
			for (unsigned index = 0; index < count; index++)
				packet->spans[index].eager = 1;
			eager_fallback = true;
			pr_warn("PIPE_CAPTURE_EAGER_FALLBACK errno=%d offset=%llu bytes=%zu\n",
				failure_errno, (unsigned long long)xfer->pipe_capture_offset, bytes);
			goto send;
		}
		goto out;
	}
	stage = "tee";
	do { copied = tee(buffer->p[0], pipes[1], bytes, SPLICE_F_NONBLOCK); } while (copied < 0 && errno == EINTR);
	/* A short tee cannot be retried from the same original prefix. */
	if (copied != (ssize_t)bytes) {
		failure_errno = copied < 0 ? errno : 0;
		goto out;
	}
send:
	packet->header = (struct capture_header){
		.magic = PIPE_CAPTURE_MAGIC, .op = 2, .a = xfer->pipe_capture_offset,
		.b = bytes, .c = count, .d = buffer->pipe_off * PAGE_SIZE,
	};
	stage = "send";
	if (capture_send(xfer->pipe_capture_fd, packet,
			 sizeof(packet->header) + count * sizeof(packet->spans[0]),
			 eager_fallback ? buffer->p[0] : pipes[0])) {
		failure_errno = errno;
		goto out;
	}
	stage = "write-deferred";
	result = deferred ? write_deferred_buffer(xfer, buffer, packet, count) : 0;
	if (result < 0) {
		failure_errno = errno;
		goto out;
	}
	xfer->pipe_capture_offset += bytes;
out:
	if (pipes[0] >= 0) close(pipes[0]);
	if (pipes[1] >= 0) close(pipes[1]);
	free(packet);
	if (result < 0) pr_err("PIPE_CAPTURE_BUFFER failed stage=%s errno=%d offset=%llu bytes=%zu "
		"available=%d capacity=%d actual_capacity=%d copied=%zd spans=%u span_bytes=%llu\n",
		stage, failure_errno, (unsigned long long)xfer->pipe_capture_offset, bytes,
		available, capacity, actual_capacity, copied, count, (unsigned long long)offset);
	return result;
}

int pipe_capture_finish(struct page_xfer *xfer)
{
	struct capture_header header = {
		.magic = PIPE_CAPTURE_MAGIC, .op = 3, .a = xfer->pipe_capture_offset,
	};
	int result;
	if (xfer->pipe_capture_fd < 0) return 0;
	result = capture_send(xfer->pipe_capture_fd, &header, sizeof(header), -1);
	pr_warn("PIPE_CAPTURE_FINISH bytes=%llu result=%d\n",
		(unsigned long long)xfer->pipe_capture_offset, result);
	pipe_capture_close(xfer);
	return result;
}

void pipe_capture_close(struct page_xfer *xfer)
{
	if (xfer->pipe_capture_fd >= 0) {
		close(xfer->pipe_capture_fd);
		xfer->pipe_capture_fd = -1;
	}
}
