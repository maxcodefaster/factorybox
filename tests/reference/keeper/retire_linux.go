package main

import (
	"errors"
	"fmt"
	"net"
	"os"

	"golang.org/x/sys/unix"
)

func (service *pipeReadService) retire(connection *net.UnixConn, message captureMessage) error {
	// Retirement owns the received SCM_RIGHTS references. In particular,
	// the old image FD must be closed before any ACK can permit unmount.
	release := func() error {
		var err error
		for _, fd := range message.fds {
			err = errors.Join(err, unix.Close(fd))
		}
		message.fds = nil
		return err
	}
	defer release()
	reply := func(busy uint64) error {
		if err := release(); err != nil {
			return err
		}
		return sendReadPacket(connection, 15, [4]uint64{busy}, nil, nil)
	}
	if len(message.fds) != 2 || message.d != 2 {
		return fmt.Errorf("invalid logical retirement request")
	}
	query := message
	query.op = 11
	query.fds = message.fds[:1]
	image, err := service.find(query)
	if err != nil || image == nil {
		return fmt.Errorf("retirement has no exact committed image: %v", err)
	}
	image.mu.Lock()
	defer image.mu.Unlock()
	if image.retired {
		return reply(0)
	}
	if image.active != 0 {
		return reply(1)
	}
	fd := message.fds[1]
	var st unix.Stat_t
	var fs unix.Statfs_t
	if unix.Fstat(fd, &st) != nil || st.Mode&unix.S_IFMT != unix.S_IFREG || st.Size != image.bytes ||
		unix.Fstatfs(fd, &fs) != nil || fs.Type == unix.TMPFS_MAGIC {
		return fmt.Errorf("retirement requires complete file backing")
	}
	backing, err := os.Open(fmt.Sprintf("/proc/self/fd/%d", fd))
	if err != nil {
		return err
	}
	defer backing.Close()
	var marker [1]byte
	if _, err := unix.Fgetxattr(fd, deferredImageAttribute, marker[:]); err != unix.ENODATA {
		return fmt.Errorf("retirement backing retained deferred marker")
	}
	got, err := hashReader(backing, image.bytes)
	if err != nil || got != image.goldenHash {
		diagnosticErr := service.store.retirementMismatch(image, backing, got)
		return fmt.Errorf("retirement backing logical hash differs for %s: read=%v diagnostic=%v", image.name, err, diagnosticErr)
	}
	// Observe mode preserves the original complete writer as an independent
	// oracle. Correct disk backing must not hide changed retained pipe bytes.
	// Deferred mode already exports those retained bytes into the checked
	// backing, so this additional diagnostic traversal is unnecessary there.
	if !image.deferredMode {
		logicalHash, err := hashReader(image.reader, image.bytes)
		if err != nil || logicalHash != image.goldenHash {
			diagnosticErr := service.store.retirementMismatch(image, backing, got)
			return fmt.Errorf("retirement observed retained hash differs for %s: read=%v diagnostic=%v", image.name, err, diagnosticErr)
		}
	}
	if err := backing.Sync(); err != nil {
		return err
	}
	if err := unix.Fadvise(fd, 0, 0, unix.FADV_DONTNEED); err != nil {
		return err
	}
	audit, err := service.store.inspectGuardAudit(image)
	if err != nil {
		return err
	}
	if err := image.reader.Close(); err != nil {
		return err
	}
	image.retirement = map[string]any{
		"original_image": image.name, "source": image.source, "logical_bytes": image.bytes,
		"private_pipe_bytes": image.privateBytes, "eager_exception_bytes": image.bytes - image.privateBytes,
		"deferred_private_lazy_bytes": image.deferredBytes, "actual_pipe_buffers": image.buffers,
		"eager_only_pipe_bytes_released_before_commit": image.eagerPipeBytesFreed,
		"eager_only_pipe_fds_released_before_commit":   image.eagerPipeFDsFreed,
		"cow_unshared_eager_bytes":                     image.unsharedEagerBytes,
		"cow_unknown_eager_bytes":                      image.unknownEagerBytes,
		"combined_capture_digests":                     image.combinedDigests,
		"capture_digest_logical_bytes":                 image.digestLogicalBytes,
		"capture_digest_retained_bytes":                image.digestRetainedBytes,
		"sha256_logical_after_capture":                 image.goldenHash, "synced_export_sha256": got,
		"retired_after_verified_backing": true, "active_clients_at_retirement": image.active,
		"raw_eager_base":                    image.usesRawBase(),
		"diagnostic_capture_preimage_bytes": image.preimageBytes,
		"eager_duplicate_bytes_avoided": func() int64 {
			if image.usesRawBase() {
				return image.bytes - image.privateBytes
			}
			return 0
		}(),
	}
	for key, value := range audit {
		image.retirement[key] = value
	}
	service.store.releasePreimages(image)
	service.store.completeRetirement(image)
	return reply(0)
}
