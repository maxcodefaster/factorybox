#ifndef RUNTIME_FORK_PIPE_READ_H
#define RUNTIME_FORK_PIPE_READ_H
#include <sys/types.h>
#include <sys/uio.h>
struct pipe_page_file;
int pipe_read_open(int image_fd, struct pipe_page_file **);
int pipe_read_open_at(const char *, int image_fd, struct pipe_page_file **);
int pipe_read_deferred(int image_fd);
ssize_t pipe_read_pread(struct pipe_page_file *, void *, size_t, off_t);
ssize_t pipe_read_preadv(struct pipe_page_file *, const struct iovec *, int, off_t);
int pipe_read_close(struct pipe_page_file *);
#endif
