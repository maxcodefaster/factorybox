package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

const captureMagic = 0x31535050
const captureLimit = 1 << 30
const captureFDLimit = 4096
const abortedCaptureHistoryLimit = 64
const commitReplyHistoryLimit = 64

type abortedCapture struct {
	Image           string         `json:"image,omitempty"`
	Source          sourceIdentity `json:"source"`
	Bytes           int64          `json:"captured_bytes"`
	PipeBytes       int64          `json:"discarded_pipe_bytes"`
	PipeDescriptors int            `json:"discarded_pipe_descriptors"`
	Reason          string         `json:"reason"`
}

type captureAbortError struct {
	record abortedCapture
	cause  error
}

func (err *captureAbortError) Error() string {
	return fmt.Sprintf("capture connection ended before commit: %v", err.cause)
}

func (err *captureAbortError) Unwrap() error { return err.cause }

type committedReplyFailure struct {
	Image  string         `json:"image"`
	Source sourceIdentity `json:"source"`
	Bytes  int64          `json:"captured_bytes"`
	SHA256 string         `json:"sha256"`
	Device uint64         `json:"device,omitempty"`
	Inode  uint64         `json:"inode,omitempty"`
	Reason string         `json:"reason"`
}

type captureReplyError struct {
	record committedReplyFailure
	cause  error
}

func (err *captureReplyError) Error() string {
	return fmt.Sprintf("capture committed but reply failed: %v", err.cause)
}

func (err *captureReplyError) Unwrap() error { return err.cause }

type captureMessage struct {
	op         uint32
	a, b, c, d uint64
	spans      []captureSpan
	fds        []int
}

type captureSpan struct{ offset, bytes, pipeOffset, eager uint64 }
type sourceIdentity struct {
	PID       int    `json:"pid"`
	StartTime string `json:"start_time"`
}
type capturedImage struct {
	mu                  sync.Mutex
	active              int
	retired             bool
	retirement          map[string]any
	name, goldenHash    string
	comm, maps          string
	pageHashes          [][32]byte
	preimages           map[int64][]byte
	preimageBytes       int64
	privateVMAStarts    []int64
	source              sourceIdentity
	golden              *os.File
	inputs              []int
	specs               []spanSpec
	reader              *pipeFile
	bytes, privateBytes int64
	pipeBytes           int64
	buffers, aliases    int
	token               [16]byte
	signature           readImageSignature
	deferredMode        bool
	deferredBytes       int64
	unsharedEagerBytes  int64
	unknownEagerBytes   int64
	guardAuditEnabled   bool
	guardAuditSpecs     []spanSpec
	guardAudit          *pipeFile
	guardAuditBytes     int64
	guardAuditFDs       int
	guardAuditProof     map[string]any
	combinedDigests     bool
	digestLogicalBytes  int64
	digestRetainedBytes int64
	creditsHeld         bool
	eagerPipeBytesFreed int64
	eagerPipeFDsFreed   int
}
type captureStore struct {
	mu                sync.RWMutex
	imageRoot, output string
	images            []*capturedImage
	heldBytes         int64
	heldFDs           int
	readService       *pipeReadService
	rawBase           bool
	preimageBytes     int64
	peakPreimageBytes int64
	auditBytes        int64
	auditFDs          int
	peakAuditBytes    int64
	peakAuditFDs      int
	pending           map[string]*capturedImage
	liveImages        int
	peakLiveImages    int
	registeredBytes   int64
	registeredFDs     int
	registeredImages  uint64
	archiveReady      bool
	archivedImages    uint64
	archivedBytes     int64
	archiveSum        [32]byte
	abortedTotal      uint64
	aborted           []abortedCapture
	commitReplyTotal  uint64
	commitReplies     []committedReplyFailure
}

func (image *capturedImage) usesRawBase() bool {
	return image.reader != nil && image.reader.baseIdentity ==
		(fileIdentity{image.signature.device, image.signature.inode})
}

func sourceAt(pid int) (sourceIdentity, error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return sourceIdentity{}, err
	}
	end := strings.LastIndexByte(string(data), ')')
	if end < 0 {
		return sourceIdentity{}, fmt.Errorf("invalid source stat")
	}
	fields := strings.Fields(string(data[end+1:]))
	if len(fields) < 20 {
		return sourceIdentity{}, fmt.Errorf("source start time absent")
	}
	return sourceIdentity{pid, fields[19]}, nil
}

func receiveCapture(connection *net.UnixConn) (captureMessage, error) {
	var message captureMessage
	packet := make([]byte, 40+2048*32)
	control := make([]byte, unix.CmsgSpace(4*4))
	n, oob, flags, _, err := connection.ReadMsgUnix(packet, control)
	if err != nil {
		return message, err
	}
	defer func() {
		if err != nil {
			for _, fd := range message.fds {
				_ = unix.Close(fd)
			}
		}
	}()
	messages, parseErr := unix.ParseSocketControlMessage(control[:oob])
	if parseErr != nil {
		return message, parseErr
	}
	for _, item := range messages {
		fds, rightsErr := unix.ParseUnixRights(&item)
		if rightsErr != nil {
			err = rightsErr
			return message, err
		}
		for _, fd := range fds {
			unix.CloseOnExec(fd)
		}
		message.fds = append(message.fds, fds...)
	}
	if flags&(unix.MSG_TRUNC|unix.MSG_CTRUNC) != 0 || n < 40 || binary.LittleEndian.Uint32(packet) != captureMagic {
		err = fmt.Errorf("truncated or foreign capture packet")
		return message, err
	}
	message.op = binary.LittleEndian.Uint32(packet[4:])
	fields := []*uint64{&message.a, &message.b, &message.c, &message.d}
	for index, field := range fields {
		*field = binary.LittleEndian.Uint64(packet[8+index*8:])
	}
	if message.op == 2 {
		if message.c == 0 || message.c > 2048 || n != 40+int(message.c)*32 {
			err = fmt.Errorf("invalid capture span count")
			return message, err
		}
		for offset := 40; offset < n; offset += 32 {
			message.spans = append(message.spans, captureSpan{
				binary.LittleEndian.Uint64(packet[offset:]),
				binary.LittleEndian.Uint64(packet[offset+8:]),
				binary.LittleEndian.Uint64(packet[offset+16:]),
				binary.LittleEndian.Uint64(packet[offset+24:]),
			})
		}
	} else if n != 40 {
		err = fmt.Errorf("unexpected capture packet payload")
		return message, err
	}
	return message, nil
}

func replyCapture(connection *net.UnixConn, op uint32, result error) error {
	var packet [16]byte
	binary.LittleEndian.PutUint32(packet[:], captureMagic)
	binary.LittleEndian.PutUint32(packet[4:], op)
	if result != nil {
		binary.LittleEndian.PutUint64(packet[8:], ^uint64(0))
	}
	count, err := connection.Write(packet[:])
	if err == nil && count != len(packet) {
		err = io.ErrShortWrite
	}
	return err
}

func peerRoot(connection *net.UnixConn) error {
	raw, err := connection.SyscallConn()
	if err != nil {
		return err
	}
	var credential *unix.Ucred
	var credentialErr error
	if err := raw.Control(func(fd uintptr) {
		credential, credentialErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	}); err != nil {
		return err
	}
	if credentialErr != nil {
		return credentialErr
	}
	if credential.Uid != 0 {
		return fmt.Errorf("capture peer is not the owned root controller")
	}
	return nil
}

var pageName = regexp.MustCompile(`^pages-[1-9][0-9]*\.img$`)

func (store *captureStore) begin(message captureMessage) (*capturedImage, error) {
	if len(message.fds) != 1 || message.a == 0 || message.b == 0 || message.b > 1<<31 ||
		message.c > 1 || message.d != 0 {
		return nil, fmt.Errorf("invalid image begin or image bound")
	}
	fd := message.fds[0]
	name, err := os.Readlink(fmt.Sprintf("/proc/self/fd/%d", fd))
	if err != nil {
		return nil, err
	}
	base := filepath.Base(name)
	parent := filepath.Dir(name)
	cgroup, owned := captureParentCgroup(store.imageRoot, parent)
	if !pageName.MatchString(base) || base != fmt.Sprintf("pages-%d.img", message.a) ||
		!owned {
		return nil, fmt.Errorf("image descriptor is outside the owned captures: %q", name)
	}
	key, kind, err := identity(fd)
	if err != nil || kind != unix.S_IFREG {
		return nil, fmt.Errorf("image descriptor is not regular")
	}
	golden, err := os.Open(fmt.Sprintf("/proc/self/fd/%d", fd))
	if err != nil {
		return nil, err
	}
	readKey, _, err := identity(int(golden.Fd()))
	if err != nil || readKey != key {
		_ = golden.Close()
		return nil, fmt.Errorf("golden image identity changed")
	}
	source, err := sourceAt(int(message.b))
	if err != nil {
		_ = golden.Close()
		return nil, err
	}
	if cgroup != "" {
		if err := captureSourceCgroup(source, cgroup); err != nil {
			_ = golden.Close()
			return nil, err
		}
	}
	comm, _ := os.ReadFile(fmt.Sprintf("/proc/%d/comm", source.PID))
	image := &capturedImage{name: name, golden: golden, source: source, comm: strings.TrimSpace(string(comm)),
		deferredMode: message.c == 1, guardAuditEnabled: os.Getenv("RUNTIME_FORK_PIPE_GUARD_AUDIT") == "1"}
	if err := store.admit(image); err != nil {
		_ = golden.Close()
		return nil, err
	}
	return image, nil
}

func (store *captureStore) buffer(image *capturedImage, message captureMessage) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	if len(message.fds) != 1 || message.a != uint64(image.bytes) || message.b == 0 ||
		message.b > maxPipeBytes || message.b%4096 != 0 || message.a > captureLimit-message.b ||
		message.d > captureLimit || message.d%4096 != 0 ||
		len(image.specs)+len(message.spans) > 8192 {
		return fmt.Errorf("pipe range or ownership credit exceeds its bound")
	}
	fd := message.fds[0]
	_, kind, err := identity(fd)
	if err != nil || kind != unix.S_IFIFO {
		return fmt.Errorf("capture payload is not a pipe")
	}
	flags, err := unix.FcntlInt(uintptr(fd), unix.F_GETFL, 0)
	if err != nil || flags&unix.O_ACCMODE != unix.O_RDONLY {
		return fmt.Errorf("capture pipe descriptor is not read-only")
	}
	available, err := unix.IoctlGetInt(fd, unix.TIOCINQ)
	if err != nil || uint64(available) != message.b {
		return fmt.Errorf("actual retained pipe length differs")
	}
	capacity, err := unix.FcntlInt(uintptr(fd), unix.F_GETPIPE_SZ, 0)
	if err != nil || capacity < available || capacity > maxPipeBytes {
		return fmt.Errorf("actual retained pipe capacity differs")
	}
	position, pipeOffset := message.a, uint64(0)
	var specs []spanSpec
	var private, deferred int64
	auditNeedsPipe := false
	for _, span := range message.spans {
		lane := span.eager & 3
		guard := span.eager & 24
		if span.offset != position || span.bytes == 0 || span.bytes%4096 != 0 ||
			span.bytes > message.a+message.b-position || span.pipeOffset != pipeOffset ||
			span.eager&^uint64(31) != 0 || lane > 2 || (span.eager&4 != 0 && lane == 1) ||
			guard == 24 || (guard != 0 && lane != 1) ||
			(lane == 2 && !image.deferredMode) || (lane == 0 && image.deferredMode) {
			return fmt.Errorf("capture spans are incomplete, overlapping or unaligned")
		}
		sourceFD := fd
		if lane == 1 {
			sourceFD = -1
			if guard == 8 {
				image.unsharedEagerBytes += int64(span.bytes)
			} else if guard == 16 {
				image.unknownEagerBytes += int64(span.bytes)
			}
		} else {
			private += int64(span.bytes)
			if lane == 2 {
				deferred += int64(span.bytes)
			}
			if span.eager&4 != 0 {
				image.privateVMAStarts = append(image.privateVMAStarts, int64(span.offset))
			}
		}
		specs = append(specs, spanSpec{int64(span.offset), int64(span.bytes), sourceFD, int64(pipeOffset)})
		if image.guardAuditEnabled {
			auditFD := -1
			if guard != 0 {
				auditFD = fd
				auditNeedsPipe = true
			}
			image.guardAuditSpecs = append(image.guardAuditSpecs, spanSpec{int64(span.offset), int64(span.bytes), auditFD, int64(pipeOffset)})
		}
		position += span.bytes
		pipeOffset += span.bytes
	}
	if position != message.a+message.b {
		return fmt.Errorf("capture span coverage is incomplete")
	}
	retain := private != 0 || auditNeedsPipe
	if retain && (store.heldFDs >= captureFDLimit || store.heldBytes > captureLimit-int64(message.b)) {
		return fmt.Errorf("retained capture credit limit: held_bytes=%d incoming_bytes=%d byte_limit=%d held_fds=%d fd_limit=%d",
			store.heldBytes, message.b, captureLimit, store.heldFDs, captureFDLimit)
	}
	if retain {
		image.inputs = append(image.inputs, fd)
		image.pipeBytes += int64(message.b)
		image.buffers++
		store.heldBytes += int64(message.b)
		store.heldFDs++
	} else {
		// All spans read the independent complete writer. This duplicate pipe
		// supplies no retained private/audit bytes and must own no credits.
		if err := unix.Close(fd); err != nil {
			return err
		}
		image.eagerPipeBytesFreed += int64(message.b)
		image.eagerPipeFDsFreed++
	}
	image.specs = append(image.specs, specs...)
	image.bytes += int64(message.b)
	image.privateBytes += private
	image.deferredBytes += deferred
	if message.d > 0 {
		image.aliases++
	}
	return nil
}

func hashReader(reader io.ReaderAt, bytes int64) (string, error) {
	hash := sha256.New()
	count, err := io.Copy(hash, io.NewSectionReader(reader, 0, bytes))
	if err != nil || count != bytes {
		return "", fmt.Errorf("complete image hash failed after %d bytes: %v", count, err)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func (store *captureStore) commit(image *capturedImage, message captureMessage) error {
	if len(message.fds) != 0 || message.a != uint64(image.bytes) || message.b != 0 ||
		message.c != 0 || message.d != 0 {
		return fmt.Errorf("incomplete capture finish")
	}
	info, err := image.golden.Stat()
	if err != nil || info.Size() != image.bytes {
		return fmt.Errorf("original writer length differs from retained spans")
	}
	if image.bytes == 0 {
		_ = image.golden.Close()
		image.golden = nil
		image.goldenHash = hex.EncodeToString(sha256.New().Sum(nil))
		image.retirement = map[string]any{
			"original_image": image.name, "source": image.source, "logical_bytes": int64(0),
			"empty_image": true, "sha256_logical_after_capture": image.goldenHash,
		}
		store.publish(image)
		store.completeRetirement(image)
		return nil
	}
	image.reader, err = store.pageBacking(image)
	if err != nil {
		return err
	}
	logical := io.ReaderAt(image.golden)
	if image.deferredBytes > 0 {
		var marker [1]byte
		n, markerErr := unix.Fgetxattr(int(image.golden.Fd()), deferredImageAttribute, marker[:])
		if markerErr != nil || n != 1 || marker[0] != 1 {
			return fmt.Errorf("omitted bytes lack mandatory deferred marker")
		}
		logical = image.reader
	}
	image.combinedDigests = os.Getenv("RUNTIME_FORK_PIPE_COMBINED_DIGEST") == "1"
	digests, err := captureDigests(logical, image.reader, image.bytes, image.deferredBytes > 0, image.combinedDigests)
	image.goldenHash, image.pageHashes = digests.whole, digests.pages
	image.digestLogicalBytes, image.digestRetainedBytes = digests.logicalBytes, digests.retainedBytes
	if digests.complete {
		if maps, mapErr := os.ReadFile(fmt.Sprintf("/proc/%d/maps", image.source.PID)); mapErr == nil && len(maps) <= 1<<20 {
			image.maps = string(maps)
		}
	}
	if err != nil {
		if digests.complete && digests.different {
			if diagnosticErr := store.mismatch(image, false); diagnosticErr != nil {
				return fmt.Errorf("retained image differs; mismatch evidence failed: %s: %v", image.name, diagnosticErr)
			}
		}
		return fmt.Errorf("capture digest failed for %s: %w", image.name, err)
	}
	if err := store.capturePreimages(image); err != nil {
		return err
	}
	if _, err := rand.Read(image.token[:]); err != nil {
		return err
	}
	if err := store.openGuardAudit(image); err != nil {
		return err
	}
	if err := unix.Fsetxattr(int(image.golden.Fd()), readImageAttribute, image.token[:], unix.XATTR_CREATE); err != nil {
		return fmt.Errorf("publish retained image identity: %w", err)
	}
	image.signature, err = signatureForFD(int(image.golden.Fd()))
	if err != nil {
		return err
	}
	// The optional raw backend deliberately retains its exact readonly
	// image anchor. Verified quiescent retirement closes it before unmount ACK.
	if err := image.golden.Close(); err != nil {
		return err
	}
	image.golden = nil
	for _, fd := range image.inputs {
		if err := unix.Close(fd); err != nil {
			return err
		}
	}
	image.inputs = nil
	store.publish(image)
	return nil
}

func (store *captureStore) pageBacking(image *capturedImage) (*pipeFile, error) {
	if store.rawBase {
		return newPipeFile(int(image.golden.Fd()), image.bytes, image.specs)
	}
	fd, err := unix.MemfdCreate("owned-capture-eager-exceptions", unix.MFD_CLOEXEC|unix.MFD_ALLOW_SEALING)
	if err != nil {
		return nil, err
	}
	eager := os.NewFile(uintptr(fd), "eager-exceptions")
	defer eager.Close()
	if err := unix.Ftruncate(fd, image.bytes); err != nil {
		return nil, err
	}
	buffer := make([]byte, readChunk)
	for _, span := range image.specs {
		if span.pipeFD >= 0 {
			continue
		}
		for position := int64(0); position < span.bytes; {
			count := int(min(int64(len(buffer)), span.bytes-position))
			if n, err := image.golden.ReadAt(buffer[:count], span.offset+position); err != nil || n != count {
				return nil, fmt.Errorf("original eager exception read failed: %d %v", n, err)
			}
			if n, err := eager.WriteAt(buffer[:count], span.offset+position); err != nil || n != count {
				return nil, fmt.Errorf("eager exception copy failed: %d %v", n, err)
			}
			position += int64(count)
		}
	}
	if _, err := unix.FcntlInt(uintptr(fd), unix.F_ADD_SEALS,
		unix.F_SEAL_WRITE|unix.F_SEAL_GROW|unix.F_SEAL_SHRINK|unix.F_SEAL_SEAL); err != nil {
		return nil, err
	}
	return newPipeFile(fd, image.bytes, image.specs)
}

func (store *captureStore) mismatch(image *capturedImage, late bool) error {
	var original, retained [4096]byte
	golden := image.golden
	if late {
		golden, _ = os.Open(image.name)
		if golden != nil {
			defer golden.Close()
		}
	}
	for offset := int64(0); offset < image.bytes; offset += int64(len(original)) {
		if _, err := image.reader.ReadAt(retained[:], offset); err != nil {
			return err
		}
		expected := image.pageHashes[offset/4096]
		if sha256.Sum256(retained[:]) == expected {
			continue
		}
		originalAvailable := false
		if golden != nil {
			if n, err := golden.ReadAt(original[:], offset); err == nil && n == len(original) {
				originalAvailable = sha256.Sum256(original[:]) == expected
			}
		}
		var changed []int
		if originalAvailable {
			for index := range original {
				if original[index] != retained[index] {
					changed = append(changed, index)
				}
			}
		}
		evidence := map[string]any{
			"image": image.name, "source": image.source, "logical_bytes": image.bytes,
			"comm": image.comm, "after_source_exit": late, "original_page_available": originalAvailable,
			"original_page_sha256": hex.EncodeToString(expected[:]),
			"page_offset":          offset, "changed_byte_offsets_in_page": changed,
			"scope": "First actual retained/golden image mismatch. Only byte positions and owned raw diagnostic pages are captured; no page contents emitted to logs.",
		}
		for _, span := range image.specs {
			if offset >= span.offset && offset < span.offset+span.bytes {
				evidence["span"] = map[string]any{
					"offset": span.offset, "bytes": span.bytes, "eager": span.pipeFD < 0,
					"pipe_offset": span.pipeOffset, "offset_within_span": offset - span.offset,
				}
				break
			}
		}
		stem := fmt.Sprintf("mismatch-%03d-%s", len(store.images), filepath.Base(image.name))
		pages := map[string][]byte{"-retained.bin": retained[:]}
		if originalAvailable {
			pages["-original.bin"] = original[:]
		}
		for suffix, data := range pages {
			file, err := os.OpenFile(filepath.Join(store.output, stem+suffix), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
			if err != nil {
				return err
			}
			_, writeErr := file.Write(data)
			if err := errors.Join(writeErr, file.Sync(), file.Close()); err != nil {
				return err
			}
		}
		if image.maps != "" {
			if err := os.WriteFile(filepath.Join(store.output, stem+"-maps.txt"), []byte(image.maps), 0600); err != nil {
				return err
			}
		}
		return writeJSON(filepath.Join(store.output, stem+".json"), evidence)
	}
	return fmt.Errorf("whole-image hashes differed but a second full read found no stable mismatching page")
}

func (store *captureStore) discard(image *capturedImage) error {
	if image == nil {
		return nil
	}
	var result error
	store.releasePreimages(image)
	result = errors.Join(result, store.closeGuardAudit(image))
	if image.golden != nil {
		result = errors.Join(result, image.golden.Close())
		image.golden = nil
	}
	for _, fd := range image.inputs {
		result = errors.Join(result, unix.Close(fd))
	}
	image.inputs = nil
	if image.reader != nil {
		result = errors.Join(result, image.reader.Close())
	}
	store.releaseCaptureCredits(image)
	return result
}

func (store *captureStore) noteAbort(record abortedCapture) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.abortedTotal++
	if len(store.aborted) == abortedCaptureHistoryLimit {
		copy(store.aborted, store.aborted[1:])
		store.aborted[len(store.aborted)-1] = record
	} else {
		store.aborted = append(store.aborted, record)
	}
}

func (store *captureStore) noteCommitReplyFailure(record committedReplyFailure) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.commitReplyTotal++
	if len(store.commitReplies) == commitReplyHistoryLimit {
		copy(store.commitReplies, store.commitReplies[1:])
		store.commitReplies[len(store.commitReplies)-1] = record
	} else {
		store.commitReplies = append(store.commitReplies, record)
	}
}

func (store *captureStore) connection(connection *net.UnixConn) (resultErr error) {
	defer connection.Close()
	if err := peerRoot(connection); err != nil {
		return err
	}
	var image *capturedImage
	committed := false
	defer func() {
		if !committed {
			if err := store.discard(image); err != nil {
				resultErr = errors.Join(resultErr, fmt.Errorf("capture discard ownership failed: %w", err))
			}
		}
	}()
	for {
		if err := connection.SetDeadline(time.Now().Add(15 * time.Second)); err != nil {
			return err
		}
		message, err := receiveCapture(connection)
		if err != nil {
			if errors.Is(err, io.EOF) {
				record := abortedCapture{Reason: "producer_eof_before_commit"}
				if image != nil {
					record.Image, record.Source, record.Bytes = image.name, image.source, image.bytes
					record.PipeBytes, record.PipeDescriptors = image.pipeBytes, image.buffers
				}
				return &captureAbortError{record: record, cause: err}
			}
			return fmt.Errorf("capture connection ended before commit: %w", err)
		}
		transferred := false
		switch message.op {
		case 1:
			if image != nil {
				err = fmt.Errorf("duplicate image begin")
			} else {
				image, err = store.begin(message)
			}
		case 2:
			if image == nil {
				err = fmt.Errorf("pipe before image begin")
			} else {
				err = store.buffer(image, message)
				transferred = err == nil
			}
		case 3:
			if image == nil {
				err = fmt.Errorf("finish before image begin")
			} else {
				err = store.commit(image, message)
				committed = err == nil
			}
		default:
			err = fmt.Errorf("unknown capture command")
		}
		if !transferred {
			for _, fd := range message.fds {
				_ = unix.Close(fd)
			}
		}
		replyErr := replyCapture(connection, message.op, err)
		if committed && err == nil && replyErr != nil {
			return &captureReplyError{
				record: committedReplyFailure{
					Image: image.name, Source: image.source, Bytes: image.bytes,
					SHA256: image.goldenHash, Device: image.signature.device, Inode: image.signature.inode,
					Reason: "committed_reply_failed",
				},
				cause: replyErr,
			}
		}
		if err != nil || replyErr != nil {
			return errors.Join(err, replyErr)
		}
		if committed {
			return nil
		}
	}
}

func memorySample() (map[string]any, error) {
	result := map[string]any{}
	for _, name := range []string{"memory.current", "memory.stat", "memory.events"} {
		data, err := os.ReadFile(filepath.Join("/sys/fs/cgroup", name))
		if err != nil {
			return nil, err
		}
		if name == "memory.current" {
			current, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
			if err != nil {
				return nil, err
			}
			result[name] = current
		} else {
			values := map[string]int64{}
			for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
				fields := strings.Fields(line)
				if len(fields) != 2 {
					return nil, fmt.Errorf("invalid memory counter")
				}
				value, err := strconv.ParseInt(fields[1], 10, 64)
				if err != nil {
					return nil, err
				}
				values[fields[0]] = value
			}
			result[name] = values
		}
	}
	return result, nil
}

func (store *captureStore) finish() (map[string]any, error) {
	proof := map[string]any{"scope": "Actual CRIU page-pipe transfer observed alongside original synchronous writer. Complete retained bytes after original source processes exit; synced file export/readback and owned descriptor release. Additional retained private RAM is charged during this diagnostic. No deferred capture or full-stack latency sample."}
	store.mu.RLock()
	proof["aborted_captures_total"] = store.abortedTotal
	proof["aborted_capture_history_limit"] = abortedCaptureHistoryLimit
	proof["aborted_captures"] = append([]abortedCapture(nil), store.aborted...)
	proof["commit_reply_failures_total"] = store.commitReplyTotal
	proof["commit_reply_history_limit"] = commitReplyHistoryLimit
	proof["commit_reply_failures"] = append([]committedReplyFailure(nil), store.commitReplies...)
	store.mu.RUnlock()
	if store.readService != nil {
		readProof, err := store.readService.stop()
		proof["direct_criu_readers"] = readProof
		if err != nil {
			return proof, err
		}
	}
	if len(store.images) == 0 {
		return proof, fmt.Errorf("no actual images captured")
	}
	archived, err := store.verifyArchivedRetirements()
	proof["archived_retirements"] = archived
	if err != nil {
		return proof, err
	}
	var records []map[string]any
	sources := map[sourceIdentity]bool{}
	var owned []int
	for index, image := range store.images {
		current, err := sourceAt(image.source.PID)
		if err == nil && current == image.source {
			return proof, fmt.Errorf("original captured source still alive: %d", current.PID)
		}
		if err != nil && !os.IsNotExist(err) {
			return proof, err
		}
		sources[image.source] = true
		if image.bytes == 0 {
			continue
		}
		if image.retired {
			records = append(records, image.retirement)
			continue
		}
		got, err := hashReader(image.reader, image.bytes)
		if err != nil || got != image.goldenHash {
			if diagnosticErr := store.mismatch(image, true); diagnosticErr != nil {
				return proof, fmt.Errorf("post-source mismatch evidence failed: %s %v", image.name, diagnosticErr)
			}
			return proof, fmt.Errorf("retained payload changed after source exit: %s %v", image.name, err)
		}
		name := fmt.Sprintf("capture-%03d-%s", index, filepath.Base(image.name))
		export, err := os.OpenFile(filepath.Join(store.output, name), os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
		if err != nil {
			return proof, err
		}
		var backing unix.Statfs_t
		if err := unix.Fstatfs(int(export.Fd()), &backing); err != nil || backing.Type == unix.TMPFS_MAGIC {
			_ = export.Close()
			return proof, fmt.Errorf("export is not file backed")
		}
		_, copyErr := io.Copy(export, io.NewSectionReader(image.reader, 0, image.bytes))
		syncErr := export.Sync()
		exportHash, hashErr := hashReader(export, image.bytes)
		evictErr := unix.Fadvise(int(export.Fd()), 0, 0, unix.FADV_DONTNEED)
		closeErr := export.Close()
		if err := errors.Join(copyErr, syncErr, hashErr, evictErr, closeErr); err != nil || exportHash != got {
			return proof, fmt.Errorf("synced retained export differs: %s %v", image.name, err)
		}
		owned = append(owned, image.reader.baseFD)
		for _, lease := range image.reader.pipes {
			owned = append(owned, lease.fd)
		}
		owned = append(owned, image.reader.scratchDescriptors()...)
		records = append(records, map[string]any{
			"original_image": image.name, "source": image.source, "logical_bytes": image.bytes,
			"private_pipe_bytes": image.privateBytes, "eager_exception_bytes": image.bytes - image.privateBytes,
			"actual_pipe_buffers": image.buffers, "nonzero_original_pipe_offsets": image.aliases,
			"sha256_logical_after_capture": image.goldenHash, "sha256_after_source_exit": got,
			"deferred_private_lazy_bytes":   image.deferredBytes,
			"cow_unshared_eager_bytes":      image.unsharedEagerBytes,
			"cow_unknown_eager_bytes":       image.unknownEagerBytes,
			"combined_capture_digests":      image.combinedDigests,
			"capture_digest_logical_bytes":  image.digestLogicalBytes,
			"capture_digest_retained_bytes": image.digestRetainedBytes,
			"raw_eager_base":                image.usesRawBase(),
			"synced_export":                 name, "synced_export_sha256": exportHash,
		})
		audit, err := store.inspectGuardAudit(image)
		if err != nil {
			return proof, err
		}
		for key, value := range audit {
			records[len(records)-1][key] = value
		}
	}
	runtime.GC()
	debug.FreeOSMemory()
	before, err := memorySample()
	if err != nil {
		return proof, err
	}
	for _, image := range store.images {
		if image.reader != nil && !image.retired {
			if err := image.reader.Close(); err != nil {
				return proof, err
			}
		}
		store.releasePreimages(image)
		store.releaseCaptureCredits(image)
	}
	for _, fd := range owned {
		var stat unix.Stat_t
		if err := unix.Fstat(fd, &stat); !errors.Is(err, unix.EBADF) {
			return proof, fmt.Errorf("owned page descriptor remained open")
		}
	}
	runtime.GC()
	debug.FreeOSMemory()
	after, err := memorySample()
	if err != nil {
		return proof, err
	}
	proof["images"] = records
	proof["peak_diagnostic_preimage_bytes"] = store.peakPreimageBytes
	proof["remaining_diagnostic_preimage_bytes"] = store.preimageBytes
	proof["peak_guard_audit_pipe_bytes"] = store.peakAuditBytes
	proof["peak_guard_audit_fds"] = store.peakAuditFDs
	proof["remaining_guard_audit_pipe_bytes"] = store.auditBytes
	proof["remaining_guard_audit_fds"] = store.auditFDs
	proof["all_source_identities_retired"] = len(sources)
	proof["all_owned_descriptor_references_closed"] = len(owned)
	proof["registered_pipe_bytes"] = store.registeredBytes
	proof["registered_pipe_descriptors"] = store.registeredFDs
	proof["registered_images"] = store.registeredImages
	proof["active_capture_images"] = store.liveImages
	proof["active_capture_credit_bytes"] = store.heldBytes
	proof["active_capture_credit_descriptors"] = store.heldFDs
	proof["peak_active_capture_images"] = store.peakLiveImages
	proof["retained_catalog_images"] = len(store.images)
	proof["live_capture_image_limit"] = liveCaptureImageLimit
	proof["retired_capture_history_limit"] = retiredCaptureHistoryLimit
	proof["memory_before_release"] = before
	proof["memory_after_release"] = after
	proof["memory_current_drop"] = before["memory.current"].(int64) - after["memory.current"].(int64)
	proof["performance_accepted"], proof["p95_accepted"] = false, false
	return proof, nil
}

func writeJSON(path string, data any) error {
	encoded, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(append(encoded, '\n'))
	syncErr := file.Sync()
	return errors.Join(writeErr, syncErr, file.Close())
}

func keeperMain() error {
	directory := "/experiment/pipe-store"
	if err := os.Mkdir(directory, 0700); err != nil {
		return err
	}
	output := filepath.Join(directory, "exports")
	if err := os.Mkdir(output, 0700); err != nil {
		return err
	}
	var err error
	store := &captureStore{imageRoot: "/experiment/sysbox", output: output}
	rawMode := os.Getenv("RUNTIME_FORK_PIPE_RAW_BASE")
	if rawMode != "" && rawMode != "0" && rawMode != "1" {
		return fmt.Errorf("unknown eager backing mode")
	}
	store.rawBase = rawMode == "1"
	auditMode := os.Getenv("RUNTIME_FORK_PIPE_GUARD_AUDIT")
	if auditMode != "" && auditMode != "0" && auditMode != "1" {
		return fmt.Errorf("unknown guard audit mode")
	}
	digestMode := os.Getenv("RUNTIME_FORK_PIPE_COMBINED_DIGEST")
	if digestMode != "" && digestMode != "0" && digestMode != "1" {
		return fmt.Errorf("unknown capture digest mode")
	}
	mode := os.Getenv("RUNTIME_FORK_PIPE_READ")
	if mode != "" && mode != "0" && mode != "lazy" {
		return fmt.Errorf("unknown retained reader mode")
	}
	if mode == "lazy" {
		store.readService, err = startReadService(store, filepath.Join(directory, "read.sock"), true)
		if err != nil {
			return err
		}
	}
	listener, err := net.ListenUnix("unixpacket", &net.UnixAddr{Name: filepath.Join(directory, "control.sock"), Net: "unixpacket"})
	if err != nil {
		return err
	}
	defer listener.Close()
	if err := os.Chmod(listener.Addr().String(), 0600); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(directory, "ready.json"), map[string]any{
		"pid": os.Getpid(), "mode": "observe", "max_pipe_bytes": captureLimit, "max_pipe_descriptors": captureFDLimit,
		"raw_eager_base": store.rawBase,
	}); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() {
		for {
			connection, err := listener.AcceptUnix()
			if err != nil {
				if errors.Is(err, net.ErrClosed) {
					err = nil
				}
				done <- err
				return
			}
			if err := store.connection(connection); err != nil {
				// These exact outcomes are local to one connection. Failed
				// cleanup, integrity checks and protocol validation stay fatal.
				if aborted, ok := err.(*captureAbortError); ok {
					store.noteAbort(aborted.record)
					continue
				}
				if committed, ok := err.(*captureReplyError); ok {
					store.noteCommitReplyFailure(committed.record)
					continue
				}
				done <- err
				return
			}
		}
	}()
	deadline := time.NewTimer(390 * time.Second)
	defer deadline.Stop()
	var serverErr error
	finished := false
	for !finished {
		select {
		case serverErr = <-done:
			finished = true
		case <-deadline.C:
			serverErr = fmt.Errorf("bounded keeper deadline exceeded")
			_ = listener.Close()
			finished = true
		case <-time.After(25 * time.Millisecond):
			if _, err := os.Stat(filepath.Join(directory, "finish")); err == nil {
				_ = listener.Close()
				serverErr = <-done
				finished = true
			} else if !os.IsNotExist(err) {
				return err
			}
		}
	}
	proof, finishErr := store.finish()
	resultErr := errors.Join(serverErr, finishErr)
	proof["status"] = "passed"
	if resultErr != nil {
		proof["status"], proof["error"] = "failed", resultErr.Error()
	}
	writeErr := writeJSON(filepath.Join(directory, "result.json"), proof)
	return errors.Join(resultErr, writeErr)
}

func main() {
	if err := keeperMain(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
