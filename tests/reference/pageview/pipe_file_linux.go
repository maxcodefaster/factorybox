package pageview

import (
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"sync"

	"golang.org/x/sys/unix"
)

const (
	maxFileBytes = 1 << 30
	maxPipeBytes = 4 << 20
	readChunk    = 64 << 10
)

type fileIdentity struct{ device, inode uint64 }

func identity(fd int) (fileIdentity, uint32, error) {
	var stat unix.Stat_t
	err := unix.Fstat(fd, &stat)
	return fileIdentity{uint64(stat.Dev), stat.Ino}, stat.Mode & unix.S_IFMT, err
}

type spanSpec struct {
	offset, bytes int64
	pipeFD        int // -1 means the already copied eager file.
	pipeOffset    int64
}

type pipeLease struct {
	fd, available, capacity int
	identity                fileIdentity
}

type pipeScratch struct {
	fds        [3]int
	identities [3]fileIdentity
	capacity   int
}

func newPipeScratch() (_ *pipeScratch, resultErr error) {
	scratch := &pipeScratch{fds: [3]int{-1, -1, -1}}
	if err := unix.Pipe2(scratch.fds[:2], unix.O_CLOEXEC|unix.O_NONBLOCK); err != nil {
		return nil, fmt.Errorf("allocate private scratch pipe: %w", err)
	}
	defer func() {
		if resultErr != nil {
			for _, fd := range scratch.fds {
				if fd >= 0 {
					_ = unix.Close(fd)
				}
			}
		}
	}()
	fd, err := unix.Open("/dev/null", unix.O_WRONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("open private scratch discard descriptor: %w", err)
	}
	scratch.fds[2] = fd
	for index, fd := range scratch.fds {
		key, _, err := identity(fd)
		if err != nil {
			return nil, fmt.Errorf("identify private scratch descriptor: %w", err)
		}
		scratch.identities[index] = key
	}
	scratch.capacity, err = unix.FcntlInt(uintptr(scratch.fds[1]), unix.F_GETPIPE_SZ, 0)
	if err != nil {
		return nil, fmt.Errorf("query private scratch capacity: %w", err)
	}
	return scratch, nil
}

func (scratch *pipeScratch) close() error {
	var result error
	for index, fd := range scratch.fds {
		key, _, err := identity(fd)
		if err != nil {
			result = errors.Join(result, err)
		} else if key != scratch.identities[index] {
			result = errors.Join(result, fmt.Errorf("refusing to close replaced foreign scratch descriptor"))
		} else {
			result = errors.Join(result, unix.Close(fd))
		}
	}
	return result
}

func (scratch *pipeScratch) prepare(capacity int) error {
	for index, fd := range scratch.fds {
		key, _, err := identity(fd)
		if err != nil || key != scratch.identities[index] {
			return fmt.Errorf("scratch descriptor identity changed: %v", err)
		}
	}
	available, err := unix.IoctlGetInt(scratch.fds[0], unix.TIOCINQ)
	if err != nil || available != 0 {
		return fmt.Errorf("scratch pipe is not empty: %d %v", available, err)
	}
	if capacity > scratch.capacity {
		actual, err := unix.FcntlInt(uintptr(scratch.fds[1]), unix.F_SETPIPE_SZ, capacity)
		if err != nil {
			return fmt.Errorf("resize private scratch pipe to %d bytes: %w", capacity, err)
		}
		if actual < capacity {
			return fmt.Errorf("private scratch capacity is short: %d/%d", actual, capacity)
		}
		scratch.capacity = actual
	}
	return nil
}

type fileSpan struct {
	offset, bytes, pipeOffset int64
	pipe                      *pipeLease
}

// The caller provides only fully pinned, CoW-protected private pipe spans.
// Shared/kernel/file exceptions must be copied into the eager file first.
// This object owns duplicated descriptors and never drains the source pipes.
type pipeFile struct {
	mu                          sync.RWMutex
	owner                       int
	size                        int64
	baseFD                      int
	baseIdentity                fileIdentity
	baseMtimeSec, baseMtimeNsec int64
	spans                       []fileSpan
	pipes                       map[fileIdentity]*pipeLease
	credits                     chan struct{}
	scratches                   chan *pipeScratch
	closed                      bool
}

func newPipeFile(baseFD int, size int64, specs []spanSpec) (_ *pipeFile, resultErr error) {
	if size <= 0 || size > maxFileBytes || size%4096 != 0 || len(specs) == 0 || len(specs) > 8192 {
		return nil, fmt.Errorf("page file exceeds fixed bounds")
	}
	ordered := append([]spanSpec(nil), specs...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].offset < ordered[j].offset })
	var position int64
	for _, span := range ordered {
		if span.offset != position || span.bytes <= 0 || span.bytes%4096 != 0 ||
			span.bytes > size-position || span.pipeOffset < 0 || span.pipeOffset%4096 != 0 ||
			span.pipeFD < -1 {
			return nil, fmt.Errorf("page file span set has a gap, overlap or invalid range")
		}
		position += span.bytes
	}
	if position != size {
		return nil, fmt.Errorf("page file span set is incomplete")
	}
	base, kind, err := identity(baseFD)
	if err != nil || kind != unix.S_IFREG {
		return nil, fmt.Errorf("eager backing is not a regular file: %w", err)
	}
	var stat unix.Stat_t
	if err := unix.Fstat(baseFD, &stat); err != nil || stat.Size != size {
		return nil, fmt.Errorf("eager backing has wrong logical length: %w", err)
	}
	baseFlags, err := unix.FcntlInt(uintptr(baseFD), unix.F_GETFL, 0)
	if err != nil || baseFlags&unix.O_ACCMODE == unix.O_WRONLY {
		return nil, fmt.Errorf("eager backing is not readable: flags=%d error=%v", baseFlags, err)
	}
	duplicate, err := unix.FcntlInt(uintptr(baseFD), unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	file := &pipeFile{
		owner: os.Getpid(), size: size, baseFD: duplicate, baseIdentity: base,
		baseMtimeSec: stat.Mtim.Sec, baseMtimeNsec: stat.Mtim.Nsec,
		pipes: make(map[fileIdentity]*pipeLease), credits: make(chan struct{}, 4),
		scratches: make(chan *pipeScratch, 4),
	}
	defer func() {
		if resultErr != nil {
			_ = file.Close()
		}
	}()
	for _, spec := range ordered {
		span := fileSpan{offset: spec.offset, bytes: spec.bytes, pipeOffset: spec.pipeOffset}
		if spec.pipeFD >= 0 {
			key, kind, err := identity(spec.pipeFD)
			if err != nil || kind != unix.S_IFIFO {
				return nil, fmt.Errorf("saved page source is not a pipe: %w", err)
			}
			flags, err := unix.FcntlInt(uintptr(spec.pipeFD), unix.F_GETFL, 0)
			if err != nil || flags&unix.O_ACCMODE != unix.O_RDONLY {
				return nil, fmt.Errorf("saved page descriptor is not read-only: flags=%d error=%v", flags, err)
			}
			lease := file.pipes[key]
			if lease == nil {
				available, err := unix.IoctlGetInt(spec.pipeFD, unix.TIOCINQ)
				if err != nil || available <= 0 || available > maxPipeBytes || available%4096 != 0 {
					return nil, fmt.Errorf("saved pipe payload exceeds its bound: %w", err)
				}
				capacity, err := unix.FcntlInt(uintptr(spec.pipeFD), unix.F_GETPIPE_SZ, 0)
				if err != nil || capacity < available || capacity > maxPipeBytes {
					return nil, fmt.Errorf("saved pipe capacity exceeds its bound: %w", err)
				}
				fd, err := unix.FcntlInt(uintptr(spec.pipeFD), unix.F_DUPFD_CLOEXEC, 0)
				if err != nil {
					return nil, err
				}
				lease = &pipeLease{fd: fd, identity: key, available: available, capacity: capacity}
				file.pipes[key] = lease
			}
			if spec.pipeOffset > int64(lease.available) || spec.bytes > int64(lease.available)-spec.pipeOffset {
				return nil, fmt.Errorf("saved span exceeds actual pipe bytes")
			}
			span.pipe = lease
		}
		file.spans = append(file.spans, span)
	}
	return file, nil
}

func readPipeWindow(lease *pipeLease, destination []byte, offset int64, scratch *pipeScratch) error {
	current, kind, err := identity(lease.fd)
	if err != nil || kind != unix.S_IFIFO || current != lease.identity {
		return fmt.Errorf("saved pipe identity changed: %w", err)
	}
	available, err := unix.IoctlGetInt(lease.fd, unix.TIOCINQ)
	if err != nil || available != lease.available || offset < 0 ||
		offset > int64(available) || len(destination) > available-int(offset) || len(destination) > readChunk {
		return fmt.Errorf("saved pipe content length/range changed: %w", err)
	}
	if len(destination) == 0 {
		return nil
	}
	if err := scratch.prepare(lease.capacity); err != nil {
		return err
	}
	wanted := int(offset) + len(destination)
	var copied int64
	for {
		copied, err = unix.Tee(lease.fd, scratch.fds[1], wanted, unix.SPLICE_F_NONBLOCK)
		if !errors.Is(err, unix.EINTR) {
			break
		}
	}
	// Repeating a short tee would duplicate the same prefix. Fail closed.
	if err != nil || copied != int64(wanted) {
		return fmt.Errorf("incomplete immutable pipe window: bytes=%d wanted=%d: %w", copied, wanted, err)
	}
	if offset > 0 {
		remaining := int(offset)
		for remaining > 0 {
			count, err := unix.Splice(scratch.fds[0], nil, scratch.fds[2], nil, remaining, unix.SPLICE_F_MOVE)
			if errors.Is(err, unix.EINTR) {
				continue
			}
			if err != nil || count <= 0 {
				return fmt.Errorf("cannot discard private scratch prefix: %w", err)
			}
			remaining -= int(count)
		}
	}
	position := 0
	for position < len(destination) {
		count, err := unix.Read(scratch.fds[0], destination[position:])
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil || count <= 0 {
			return fmt.Errorf("cannot read complete private scratch window: %w", err)
		}
		position += count
	}
	return nil
}

func (file *pipeFile) ReadAt(destination []byte, offset int64) (result int, resultErr error) {
	file.mu.RLock()
	defer file.mu.RUnlock()
	if file.owner != os.Getpid() || file.closed {
		return 0, os.ErrClosed
	}
	if offset < 0 {
		return 0, fmt.Errorf("negative page file offset")
	}
	if len(destination) == 0 {
		return 0, nil
	}
	if offset >= file.size {
		return 0, io.EOF
	}
	file.credits <- struct{}{}
	var scratch *pipeScratch
	defer func() {
		if resultErr != nil && !errors.Is(resultErr, io.EOF) && scratch != nil {
			resultErr = errors.Join(resultErr, scratch.close())
			scratch = nil
		}
		if scratch != nil {
			file.scratches <- scratch
		}
		<-file.credits
	}()
	wanted := len(destination)
	if int64(wanted) > file.size-offset {
		wanted = int(file.size - offset)
	}
	position := 0
	for position < wanted {
		cursor := offset + int64(position)
		index := sort.Search(len(file.spans), func(index int) bool {
			return file.spans[index].offset+file.spans[index].bytes > cursor
		})
		if index == len(file.spans) {
			return position, fmt.Errorf("published page file span disappeared")
		}
		span := file.spans[index]
		count := min(wanted-position, readChunk, int(span.offset+span.bytes-cursor))
		part := destination[position : position+count]
		if span.pipe == nil {
			var stat unix.Stat_t
			err := unix.Fstat(file.baseFD, &stat)
			if err != nil || stat.Mode&unix.S_IFMT != unix.S_IFREG ||
				(fileIdentity{uint64(stat.Dev), stat.Ino}) != file.baseIdentity ||
				stat.Size != file.size || stat.Mtim.Sec != file.baseMtimeSec || stat.Mtim.Nsec != file.baseMtimeNsec {
				return position, fmt.Errorf("eager backing identity changed: %w", err)
			}
			var read int
			for {
				read, err = unix.Pread(file.baseFD, part, cursor)
				if !errors.Is(err, unix.EINTR) {
					break
				}
			}
			if err != nil || read != count {
				return position, fmt.Errorf("eager backing truncated or unreadable: %w", err)
			}
		} else {
			if scratch == nil {
				select {
				case scratch = <-file.scratches:
				default:
					var err error
					scratch, err = newPipeScratch()
					if err != nil {
						return position, err
					}
				}
			}
			if err := readPipeWindow(span.pipe, part, span.pipeOffset+cursor-span.offset, scratch); err != nil {
				return position, err
			}
		}
		position += count
	}
	if position != len(destination) {
		return position, io.EOF
	}
	return position, nil
}

func (file *pipeFile) Close() error {
	file.mu.Lock()
	defer file.mu.Unlock()
	if file.owner != os.Getpid() {
		return fmt.Errorf("page file descriptors belong to another controller")
	}
	if file.closed {
		return nil
	}
	file.closed = true
	var result error
	closeOwned := func(fd int, expected fileIdentity) {
		current, _, err := identity(fd)
		if err != nil {
			result = errors.Join(result, err)
		} else if current != expected {
			result = errors.Join(result, fmt.Errorf("refusing to close replaced foreign descriptor"))
		} else {
			result = errors.Join(result, unix.Close(fd))
		}
	}
	for _, pipe := range file.pipes {
		closeOwned(pipe.fd, pipe.identity)
	}
	closeOwned(file.baseFD, file.baseIdentity)
	for len(file.scratches) > 0 {
		result = errors.Join(result, (<-file.scratches).close())
	}
	return result
}
