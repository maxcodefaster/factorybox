package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

const readImageAttribute = "user.runtime_fork.pipe_v1"
const deferredImageAttribute = "user.runtime_fork.deferred_v1"

type readImageSignature struct {
	device, inode uint64
	size          int64
	mtimeSec      int64
	mtimeNsec     int64
}

func signatureForFD(fd int) (readImageSignature, error) {
	var stat unix.Stat_t
	err := unix.Fstat(fd, &stat)
	if err != nil || stat.Mode&unix.S_IFMT != unix.S_IFREG {
		return readImageSignature{}, fmt.Errorf("retained image signature is not regular: %v", err)
	}
	return readImageSignature{uint64(stat.Dev), stat.Ino, stat.Size, stat.Mtim.Sec, stat.Mtim.Nsec}, nil
}

type pipeReadService struct {
	store        *captureStore
	listener     *net.UnixListener
	acceptDone   chan struct{}
	wg           sync.WaitGroup
	mu           sync.Mutex
	leases       []map[string]any
	failures     []string
	active, peak int
	activeFDs    int
	peakFDs      int
	nextID       uint64
	misses       uint64
	requireReads bool
	completed    uint64
	totalCalls   uint64
	totalBytes   uint64
	totalPrivate uint64
	totalEager   uint64
}

func sendReadPacket(connection *net.UnixConn, op uint32, fields [4]uint64, spans [][4]uint64, fds []int) error {
	packet := make([]byte, 40+len(spans)*32)
	binary.LittleEndian.PutUint32(packet, captureMagic)
	binary.LittleEndian.PutUint32(packet[4:], op)
	for index, field := range fields {
		binary.LittleEndian.PutUint64(packet[8+8*index:], field)
	}
	for index, span := range spans {
		for column, value := range span {
			binary.LittleEndian.PutUint64(packet[40+32*index+8*column:], value)
		}
	}
	var control []byte
	if len(fds) != 0 {
		control = unix.UnixRights(fds...)
	}
	n, _, err := connection.WriteMsgUnix(packet, control, nil)
	if err == nil && n != len(packet) {
		err = io.ErrShortWrite
	}
	return err
}

func peerIdentity(connection *net.UnixConn) (sourceIdentity, error) {
	raw, err := connection.SyscallConn()
	if err != nil {
		return sourceIdentity{}, err
	}
	var credentials *unix.Ucred
	var credentialErr error
	if err := raw.Control(func(fd uintptr) {
		credentials, credentialErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	}); err != nil {
		return sourceIdentity{}, err
	}
	if credentialErr != nil {
		return sourceIdentity{}, credentialErr
	}
	if credentials.Uid != 0 {
		return sourceIdentity{}, fmt.Errorf("retained reader peer is not the owned root process")
	}
	return sourceAt(int(credentials.Pid))
}

func peerRetired(identity sourceIdentity) bool {
	current, err := sourceAt(identity.PID)
	if os.IsNotExist(err) || (err == nil && current != identity) {
		return true
	}
	if err != nil {
		return false
	}
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", identity.PID))
	if err != nil {
		return os.IsNotExist(err)
	}
	end := strings.LastIndexByte(string(data), ')')
	fields := strings.Fields(string(data[end+1:]))
	return len(fields) > 0 && (fields[0] == "Z" || fields[0] == "X")
}

func (service *pipeReadService) find(message captureMessage) (*capturedImage, error) {
	if message.op != 11 || len(message.fds) != 1 || (message.d != 1 && message.d != 2) {
		return nil, fmt.Errorf("invalid retained reader query")
	}
	fd := message.fds[0]
	signature, err := signatureForFD(fd)
	if err != nil {
		return nil, err
	}
	if signature.device != message.a || signature.inode != message.b || uint64(signature.size) != message.c {
		return nil, fmt.Errorf("reader query does not match its actual descriptor")
	}
	var marker [1]byte
	marked, markerErr := unix.Fgetxattr(fd, deferredImageAttribute, marker[:])
	deferred := markerErr == nil && marked == 1 && marker[0] == 1
	if markerErr != nil && !errors.Is(markerErr, unix.ENODATA) || markerErr == nil && !deferred {
		return nil, fmt.Errorf("invalid mandatory deferred marker")
	}
	var token [16]byte
	n, err := unix.Fgetxattr(fd, readImageAttribute, token[:])
	if errors.Is(err, unix.ENODATA) {
		if deferred {
			return nil, fmt.Errorf("deferred image has no committed token")
		}
		return nil, nil
	}
	if err != nil || n != len(token) {
		return nil, fmt.Errorf("invalid retained image token: %d %v", n, err)
	}
	service.store.mu.RLock()
	defer service.store.mu.RUnlock()
	for _, image := range service.store.images {
		if image.reader != nil && image.signature == signature && bytes.Equal(image.token[:], token[:]) {
			if message.d == 1 && image.usesRawBase() {
				var filesystem unix.Statfs_t
				if err := unix.Fstatfs(image.reader.baseFD, &filesystem); err != nil || filesystem.Flags&unix.ST_RDONLY == 0 {
					return nil, fmt.Errorf("CRIU raw image lease requires kernel readonly publication")
				}
			}
			return image, nil
		}
	}
	if deferred {
		return nil, fmt.Errorf("deferred image has no matching live logical backing")
	}
	return nil, nil
}

func (service *pipeReadService) connection(connection *net.UnixConn) (resultErr error) {
	defer connection.Close()
	identity, err := peerIdentity(connection)
	if err != nil {
		return err
	}
	if err := connection.SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
		return err
	}
	message, err := receiveCapture(connection)
	if err != nil {
		return err
	}
	if message.op == 15 {
		err = service.retire(connection, message)
		return err
	}
	image, err := service.find(message)
	for _, fd := range message.fds {
		_ = unix.Close(fd)
	}
	if err != nil {
		return err
	}
	if image == nil {
		service.mu.Lock()
		service.misses++
		service.mu.Unlock()
		return sendReadPacket(connection, 11, [4]uint64{}, nil, nil)
	}
	image.mu.Lock()
	if image.retired {
		image.mu.Unlock()
		return fmt.Errorf("retired logical image cannot lease RAM descriptors")
	}
	image.active++
	image.mu.Unlock()
	defer func() { image.mu.Lock(); image.active--; image.mu.Unlock() }()
	leases := make([]*pipeLease, 0, len(image.reader.pipes))
	for _, lease := range image.reader.pipes {
		leases = append(leases, lease)
	}
	sort.Slice(leases, func(a, b int) bool {
		if leases[a].identity.device != leases[b].identity.device {
			return leases[a].identity.device < leases[b].identity.device
		}
		return leases[a].identity.inode < leases[b].identity.inode
	})
	fds := []int{image.reader.baseFD}
	indices := map[*pipeLease]uint64{}
	for _, lease := range leases {
		indices[lease] = uint64(len(fds))
		fds = append(fds, lease.fd)
	}
	statsFD, err := unix.MemfdCreate("owned-criu-reader-counters", unix.MFD_CLOEXEC|unix.MFD_ALLOW_SEALING)
	if err != nil {
		return err
	}
	defer unix.Close(statsFD)
	if err := unix.Ftruncate(statsFD, 4096); err != nil {
		return err
	}
	if _, err := unix.FcntlInt(uintptr(statsFD), unix.F_ADD_SEALS,
		unix.F_SEAL_GROW|unix.F_SEAL_SHRINK|unix.F_SEAL_SEAL); err != nil {
		return err
	}
	dataFDs := len(fds)
	fds = append(fds, statsFD)
	service.mu.Lock()
	if service.active >= 128 || service.activeFDs+len(fds) > 8192 {
		service.mu.Unlock()
		return fmt.Errorf("retained client leases exceed fixed credits")
	}
	service.nextID++
	id := service.nextID
	service.active++
	service.activeFDs += len(fds)
	service.peak = max(service.peak, service.active)
	service.peakFDs = max(service.peakFDs, service.activeFDs)
	service.mu.Unlock()
	defer func() {
		service.mu.Lock()
		service.active--
		service.activeFDs -= len(fds)
		service.mu.Unlock()
	}()
	if err := sendReadPacket(connection, 11,
		[4]uint64{uint64(image.bytes), uint64(len(image.reader.spans)), uint64(len(fds)), id}, nil, nil); err != nil {
		return err
	}
	for start := 0; start < len(image.reader.spans); {
		count := min(128, len(image.reader.spans)-start)
		var spans [][4]uint64
		for _, span := range image.reader.spans[start : start+count] {
			spans = append(spans, [4]uint64{
				uint64(span.offset), uint64(span.bytes), indices[span.pipe], uint64(span.pipeOffset),
			})
		}
		if err := sendReadPacket(connection, 12, [4]uint64{uint64(start), uint64(count)}, spans, nil); err != nil {
			return err
		}
		start += count
	}
	for start := 0; start < len(fds); {
		count := min(253, len(fds)-start)
		if err := sendReadPacket(connection, 13, [4]uint64{uint64(start), uint64(count)}, nil, fds[start:start+count]); err != nil {
			return err
		}
		start += count
	}
	if err := sendReadPacket(connection, 14,
		[4]uint64{uint64(image.bytes), uint64(len(image.reader.spans)), uint64(len(fds)), id}, nil, nil); err != nil {
		return err
	}
	if err := connection.SetDeadline(time.Now().Add(390 * time.Second)); err != nil {
		return err
	}
	var unexpected [1]byte
	n, err := connection.Read(unexpected[:])
	if err != io.EOF || n != 0 {
		return fmt.Errorf("client lease did not close normally: %d %v", n, err)
	}
	var raw [64]byte
	if n, err := unix.Pread(statsFD, raw[:], 0); err != nil || n != len(raw) {
		return fmt.Errorf("reader counters unavailable: %d %v", n, err)
	}
	values := [8]uint64{}
	for index := range values {
		values[index] = binary.LittleEndian.Uint64(raw[index*8:])
	}
	calls, readBytes, private, eager, failures, closes, released, forks := values[0], values[1], values[2], values[3], values[4], values[5], values[6], values[7]
	retired := false
	if closes == 0 {
		for attempt := 0; attempt < 20; attempt++ {
			if peerRetired(identity) {
				retired = true
				break
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
	record := map[string]any{
		"lease": id, "client": identity, "image": image.name, "calls": calls,
		"bytes": readBytes, "private_pipe_bytes": private, "eager_bytes": eager,
		"errors": failures, "explicit_closes": closes, "released_data_fds": released,
		"leased_data_fds": dataFDs, "scratch_fork_resets": forks,
		"span_count": len(image.reader.spans),
		"client_process_retired_without_explicit_close": retired,
		"role": message.d,
	}
	service.mu.Lock()
	service.leases = append(service.leases, record)
	if len(service.leases) > completedReaderHistoryLimit {
		copy(service.leases, service.leases[1:])
		service.leases[len(service.leases)-1] = nil
		service.leases = service.leases[:len(service.leases)-1]
	}
	service.completed++
	service.totalCalls += calls
	service.totalBytes += readBytes
	service.totalPrivate += private
	service.totalEager += eager
	service.mu.Unlock()
	if failures != 0 || readBytes != private+eager || (closes == 0 && !retired) ||
		(closes > 0 && released != uint64(dataFDs)*closes) {
		return fmt.Errorf("retained reader failed or left owned references unaccounted for: lease=%d", id)
	}
	return nil
}

func startReadService(store *captureStore, path string, required bool) (*pipeReadService, error) {
	listener, err := net.ListenUnix("unixpacket", &net.UnixAddr{Name: path, Net: "unixpacket"})
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0600); err != nil {
		_ = listener.Close()
		return nil, err
	}
	service := &pipeReadService{store: store, listener: listener, acceptDone: make(chan struct{}), requireReads: required}
	go func() {
		defer close(service.acceptDone)
		for {
			connection, err := listener.AcceptUnix()
			if err != nil {
				if !errors.Is(err, net.ErrClosed) {
					service.mu.Lock()
					if len(service.failures) < 16 {
						service.failures = append(service.failures, err.Error())
					}
					service.mu.Unlock()
				}
				return
			}
			service.wg.Add(1)
			go func() {
				defer service.wg.Done()
				if err := service.connection(connection); err != nil {
					service.mu.Lock()
					if len(service.failures) < 16 {
						service.failures = append(service.failures, err.Error())
					}
					service.mu.Unlock()
				}
			}()
		}
	}()
	return service, nil
}

func (service *pipeReadService) stop() (map[string]any, error) {
	_ = service.listener.Close()
	<-service.acceptDone
	done := make(chan struct{})
	go func() { service.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		return nil, fmt.Errorf("retained page clients still active at store release")
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	calls, bytes, private, eager := service.totalCalls, service.totalBytes, service.totalPrivate, service.totalEager
	proof := map[string]any{
		"leases": service.leases, "misses": service.misses, "failures": service.failures,
		"peak_client_leases": service.peak, "peak_client_descriptors": service.peakFDs,
		"active_client_leases": service.active, "active_client_descriptors": service.activeFDs,
		"calls": calls, "bytes": bytes, "private_pipe_bytes": private, "eager_bytes": eager,
		"required": service.requireReads, "socket": filepath.Base(service.listener.Addr().String()),
		"completed_client_leases":          service.completed,
		"retained_completed_client_leases": len(service.leases),
		"omitted_completed_client_leases":  service.completed - uint64(len(service.leases)),
		"completed_reader_history_limit":   completedReaderHistoryLimit,
		"scope":                            "Actual central CRIU reader counters from size-sealed shared memfds after every leased connection ends. Reads use the transferred immutable pipe/eager descriptors locally, with no Go RPC per fault. Normal close verifies data descriptors; killed lazy daemons require kernel-observed original process retirement.",
	}
	if len(service.failures) != 0 || service.active != 0 || service.activeFDs != 0 ||
		(service.requireReads && (calls == 0 || private == 0)) {
		return proof, fmt.Errorf("direct retained reader evidence failed: %v", service.failures)
	}
	return proof, nil
}
