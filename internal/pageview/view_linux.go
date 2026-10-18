package pageview

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

const Attribute = "user.runtime_fork.deferred_v1"
const magic = 0x31535050

// View borrows immutable descriptors once. Every byte is read locally.
// The caller retains ownership of the original image descriptor.
type View struct {
	mu                                     sync.Mutex
	file                                   *pipeFile
	connection                             *net.UnixConn
	counter                                int
	dataFDs                                int
	position                               int64
	calls, bytes, private, eager, failures uint64
	closed                                 bool
}

func Marked(file *os.File) (bool, error) {
	var marker [1]byte
	n, err := unix.Fgetxattr(int(file.Fd()), Attribute, marker[:])
	if errors.Is(err, unix.ENODATA) {
		return false, nil
	}
	if err != nil || n != 1 || marker[0] != 1 {
		return false, fmt.Errorf("invalid deferred page image marker: %d %v", n, err)
	}
	return true, nil
}

func Captured(file *os.File) (bool, error) {
	var token [16]byte
	n, err := unix.Fgetxattr(int(file.Fd()), "user.runtime_fork.pipe_v1", token[:])
	if errors.Is(err, unix.ENODATA) {
		return false, nil
	}
	if err != nil || n != len(token) {
		return false, fmt.Errorf("invalid retained image token")
	}
	return true, nil
}

// OpenAt returns nil only for a complete ordinary image. A marked image
// cannot fall back to the raw file, including on connection or identity loss.
func OpenAt(socket string, image *os.File) (_ *View, resultErr error) {
	marked, err := Marked(image)
	if err != nil || !marked {
		return nil, err
	}
	var stat unix.Stat_t
	if err := unix.Fstat(int(image.Fd()), &stat); err != nil {
		return nil, err
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Size <= 0 || stat.Size > maxFileBytes ||
		stat.Size%4096 != 0 {
		return nil, fmt.Errorf("invalid deferred image size")
	}
	connection, err := net.DialUnix("unixpacket", nil, &net.UnixAddr{Name: socket, Net: "unixpacket"})
	if err != nil {
		return nil, err
	}
	success := false
	var received []int
	defer func() {
		for _, fd := range received {
			_ = unix.Close(fd)
		}
		if !success {
			_ = connection.Close()
		}
	}()
	if err := connection.SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
		return nil, err
	}
	query := encode(11, [4]uint64{uint64(stat.Dev), stat.Ino, uint64(stat.Size), 2})
	n, _, err := connection.WriteMsgUnix(query, unix.UnixRights(int(image.Fd())), nil)
	if err != nil || n != len(query) {
		return nil, fmt.Errorf("logical query failed: %d %v", n, err)
	}
	op, fields, payload, fds, err := receive(connection)
	received = append(received, fds...)
	if err != nil {
		return nil, err
	}
	if op != 11 || len(payload) != 0 || len(fds) != 0 || fields[0] != uint64(stat.Size) ||
		fields[1] == 0 || fields[1] > 8192 || fields[2] < 2 || fields[2] > 4098 || fields[3] == 0 {
		return nil, fmt.Errorf("missing or invalid deferred logical view")
	}
	header := fields
	var spans [][4]uint64
	for {
		op, fields, payload, fds, err = receive(connection)
		startFD := len(received)
		received = append(received, fds...)
		if err != nil {
			return nil, err
		}
		switch op {
		case 12:
			if len(fds) != 0 || fields[0] != uint64(len(spans)) || fields[1] == 0 || fields[1] > 128 ||
				fields[1] > header[1]-uint64(len(spans)) || fields[2] != 0 || fields[3] != 0 ||
				len(payload) != int(fields[1])*32 {
				return nil, fmt.Errorf("invalid logical span packet")
			}
			for offset := 0; offset < len(payload); offset += 32 {
				var span [4]uint64
				for i := range span {
					span[i] = binary.LittleEndian.Uint64(payload[offset+8*i:])
				}
				spans = append(spans, span)
			}
		case 13:
			if len(payload) != 0 || fields[0] != uint64(startFD) || len(fds) == 0 || len(fds) > 253 ||
				fields[1] != uint64(len(fds)) || uint64(len(received)) > header[2] ||
				fields[2] != 0 || fields[3] != 0 {
				return nil, fmt.Errorf("invalid logical FD packet")
			}
		case 14:
			if len(payload) != 0 || len(fds) != 0 || fields != header || uint64(len(spans)) != header[1] ||
				uint64(len(received)) != header[2] {
				return nil, fmt.Errorf("incomplete logical view")
			}
			goto complete
		default:
			return nil, fmt.Errorf("unknown logical view packet")
		}
	}
complete:
	for _, i := range []int{0, len(received) - 1} {
		seals, err := unix.FcntlInt(uintptr(received[i]), unix.F_GET_SEALS, 0)
		required := unix.F_SEAL_GROW | unix.F_SEAL_SHRINK
		if i == 0 {
			required |= unix.F_SEAL_WRITE
		}
		var st unix.Stat_t
		size := int64(4096)
		if i == 0 {
			size = stat.Size
		}
		if unix.Fstat(received[i], &st) != nil || st.Size != size || st.Mode&unix.S_IFMT != unix.S_IFREG {
			return nil, fmt.Errorf("unsealed logical backing")
		}
		if err != nil || seals&required != required {
			// Role2 is the trusted root controller's construction/hash/export
			// path, including the step before globally readonly publication.
			// It can only borrow the exact original image as an unsealed base.
			flags, flagErr := unix.FcntlInt(uintptr(received[i]), unix.F_GETFL, 0)
			if i != 0 || st.Dev != stat.Dev || st.Ino != stat.Ino ||
				st.Mtim.Sec != stat.Mtim.Sec || st.Mtim.Nsec != stat.Mtim.Nsec ||
				flagErr != nil || flags&unix.O_ACCMODE != unix.O_RDONLY {
				return nil, fmt.Errorf("unsealed logical backing is not the exact read-only original descriptor")
			}
		}
	}
	specs := make([]spanSpec, 0, len(spans))
	for _, span := range spans {
		if span[2] >= uint64(len(received)-1) || span[3] > maxFileBytes || span[0] > maxFileBytes ||
			span[1] > maxFileBytes {
			return nil, fmt.Errorf("logical span exceeds descriptor bounds")
		}
		fd := -1
		if span[2] > 0 {
			fd = received[span[2]]
		}
		specs = append(specs, spanSpec{int64(span[0]), int64(span[1]), fd, int64(span[3])})
	}
	file, err := newPipeFile(received[0], stat.Size, specs)
	if err != nil {
		return nil, err
	}
	counter, err := unix.FcntlInt(uintptr(received[len(received)-1]), unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	if err := connection.SetDeadline(time.Time{}); err != nil {
		_ = file.Close()
		_ = unix.Close(counter)
		return nil, err
	}
	success = true
	return &View{file: file, connection: connection, counter: counter, dataFDs: len(received) - 1}, nil
}

func encode(op uint32, fields [4]uint64) []byte {
	packet := make([]byte, 40)
	binary.LittleEndian.PutUint32(packet, magic)
	binary.LittleEndian.PutUint32(packet[4:], op)
	for i, value := range fields {
		binary.LittleEndian.PutUint64(packet[8+8*i:], value)
	}
	return packet
}

func receive(connection *net.UnixConn) (op uint32, fields [4]uint64, payload []byte, fds []int, resultErr error) {
	packet := make([]byte, 40+128*32)
	control := make([]byte, unix.CmsgSpace(253*4))
	n, oob, flags, _, err := connection.ReadMsgUnix(packet, control)
	if err != nil {
		return 0, fields, nil, nil, err
	}
	messages, err := unix.ParseSocketControlMessage(control[:oob])
	if err != nil {
		return 0, fields, nil, nil, err
	}
	for _, message := range messages {
		rights, err := unix.ParseUnixRights(&message)
		if err != nil {
			resultErr = err
			continue
		}
		for _, fd := range rights {
			unix.CloseOnExec(fd)
		}
		fds = append(fds, rights...)
	}
	if resultErr != nil || flags&(unix.MSG_TRUNC|unix.MSG_CTRUNC) != 0 || n < 40 ||
		binary.LittleEndian.Uint32(packet) != magic {
		return 0, fields, nil, fds, fmt.Errorf("invalid logical response")
	}
	op = binary.LittleEndian.Uint32(packet[4:])
	for i := range fields {
		fields[i] = binary.LittleEndian.Uint64(packet[8+8*i:])
	}
	return op, fields, packet[40:n], fds, nil
}

func (view *View) readAt(data []byte, offset int64) (int, error) {
	if view.closed {
		return 0, os.ErrClosed
	}
	n, err := view.file.ReadAt(data, offset)
	view.calls++
	view.bytes += uint64(n)
	for _, span := range view.file.spans {
		start := max(offset, span.offset)
		end := min(offset+int64(n), span.offset+span.bytes)
		if end > start {
			if span.pipe == nil {
				view.eager += uint64(end - start)
			} else {
				view.private += uint64(end - start)
			}
		}
	}
	if err != nil && !errors.Is(err, io.EOF) {
		view.failures++
	}
	return n, err
}
func (view *View) ReadAt(data []byte, offset int64) (int, error) {
	view.mu.Lock()
	defer view.mu.Unlock()
	return view.readAt(data, offset)
}
func (view *View) Read(data []byte) (int, error) {
	view.mu.Lock()
	defer view.mu.Unlock()
	n, err := view.readAt(data, view.position)
	view.position += int64(n)
	return n, err
}
func (view *View) Close() error {
	view.mu.Lock()
	defer view.mu.Unlock()
	if view.closed {
		return nil
	}
	view.closed = true
	err := view.file.Close()
	if err != nil {
		view.failures++
	}
	values := [8]uint64{view.calls, view.bytes, view.private, view.eager, view.failures, 1, uint64(view.dataFDs), 0}
	var raw [64]byte
	for i, value := range values {
		binary.LittleEndian.PutUint64(raw[8*i:], value)
	}
	n, writeErr := unix.Pwrite(view.counter, raw[:], 0)
	if n != len(raw) {
		writeErr = errors.Join(writeErr, io.ErrShortWrite)
	}
	return errors.Join(err, writeErr, unix.Close(view.counter), view.connection.Close())
}
