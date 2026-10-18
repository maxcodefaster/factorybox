#ifndef RUNTIME_FORK_PIPE_CAPTURE_H
#define RUNTIME_FORK_PIPE_CAPTURE_H
struct page_xfer;
struct page_pipe_buf;
int pipe_capture_begin(struct page_xfer *, int, unsigned long);
int pipe_capture_buffer(struct page_xfer *, struct page_pipe_buf *);
int pipe_capture_finish(struct page_xfer *);
void pipe_capture_close(struct page_xfer *);
#endif
