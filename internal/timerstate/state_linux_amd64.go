//go:build linux && amd64

package timerstate

import (
	"bufio"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

type instruction struct {
	opcode, registers byte
	offset            int16
	immediate         int32
}
type program struct {
	code  []instruction
	exits []int
}

func (p *program) emit(op byte, dst, src byte, offset int16, immediate int32) {
	p.code = append(p.code, instruction{op, src<<4 | dst, offset, immediate})
}
func (p *program) jump(op, dst, src byte, immediate int32) {
	p.exits = append(p.exits, len(p.code))
	p.emit(op, dst, src, 0, immediate)
}
func (p *program) mov(dst, src byte)         { p.emit(0xbf, dst, src, 0, 0) }
func (p *program) add(dst byte, value int32) { p.emit(0x07, dst, 0, 0, value) }
func (p *program) read(dst int16, size int32, src byte, offset int32) {
	p.mov(1, 10)
	p.add(1, int32(dst))
	p.emit(0xb7, 2, 0, 0, size)
	p.mov(3, src)
	p.add(3, offset)
	p.emit(0x85, 0, 0, 0, 113)
	p.jump(0x55, 0, 0, 0)
}
func (p *program) finish() []byte {
	for _, index := range p.exits {
		p.code[index].offset = int16(len(p.code) - index - 1)
	}
	p.emit(0xb7, 0, 0, 0, 0)
	p.emit(0x95, 0, 0, 0, 0)
	data := make([]byte, len(p.code)*8)
	for index, row := range p.code {
		offset := index * 8
		data[offset], data[offset+1] = row.opcode, row.registers
		binary.LittleEndian.PutUint16(data[offset+2:], uint16(row.offset))
		binary.LittleEndian.PutUint32(data[offset+4:], uint32(row.immediate))
	}
	return data
}

func bpf(command uintptr, attr []byte) (int, error) {
	var pinned runtime.Pinner
	defer pinned.Unpin()
	result, _, errno := unix.Syscall(unix.SYS_BPF, command, uintptr(address(&pinned, attr)), uintptr(len(attr)))
	runtime.KeepAlive(attr)
	if errno != 0 {
		return -1, errno
	}
	return int(result), nil
}

// Pointers encoded inside bpf_attr are opaque integers to the Go stack mover.
// Pin every referenced allocation until the synchronous syscall returns.
func address(pinned *runtime.Pinner, data []byte) uint64 {
	pinned.Pin(&data[0])
	return uint64(uintptr(unsafe.Pointer(&data[0])))
}

func boundedFile(path string, maximum int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maximum+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maximum {
		return nil, fmt.Errorf("kernel timer input exceeds bound: %s", path)
	}
	return data, nil
}

func clockTicks() (uint64, error) {
	data, err := boundedFile("/proc/self/auxv", 64<<10)
	if err != nil {
		return 0, err
	}
	for len(data) >= 16 {
		key, value := binary.LittleEndian.Uint64(data), binary.LittleEndian.Uint64(data[8:])
		if key == 17 && value > 0 && value <= 1000000 {
			return value, nil
		}
		data = data[16:]
	}
	return 0, fmt.Errorf("native AT_CLKTCK is absent")
}

func timerFunction() (uint64, error) {
	file, err := os.Open("/proc/kallsyms")
	if err != nil {
		return 0, err
	}
	defer file.Close()
	limited := &io.LimitedReader{R: file, N: 32 << 20}
	scanner := bufio.NewScanner(limited)
	var found uint64
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 3 || fields[2] != "hrtimer_nanosleep_restart" {
			continue
		}
		value, err := strconv.ParseUint(fields[0], 16, 64)
		if err != nil || value == 0 || found != 0 {
			return 0, fmt.Errorf("relative timer restart symbol is masked or ambiguous")
		}
		found = value
	}
	if err := scanner.Err(); err != nil {
		return 0, err
	}
	if limited.N == 0 || found == 0 {
		return 0, fmt.Errorf("relative timer restart symbol is absent or exceeds bound")
	}
	return found, nil
}

func objectID(fd int) (uint32, error) {
	var pinned runtime.Pinner
	defer pinned.Unpin()
	info := make([]byte, 128)
	attr := make([]byte, 16)
	binary.LittleEndian.PutUint32(attr, uint32(fd))
	binary.LittleEndian.PutUint32(attr[4:], uint32(len(info)))
	binary.LittleEndian.PutUint64(attr[8:], address(&pinned, info))
	_, err := bpf(15, attr)
	runtime.KeepAlive(info)
	if err != nil {
		return 0, err
	}
	id := binary.LittleEndian.Uint32(info[4:])
	if id == 0 {
		return 0, fmt.Errorf("temporary BPF object has no ID")
	}
	return id, nil
}

func released(command uintptr, id uint32) error {
	attr := make([]byte, 12)
	binary.LittleEndian.PutUint32(attr, id)
	for deadline := time.Now().Add(500 * time.Millisecond); ; {
		fd, err := bpf(command, attr)
		if errors.Is(err, unix.ENOENT) {
			return nil
		}
		if err != nil {
			return err
		}
		unix.Close(fd)
		if time.Now().After(deadline) {
			return fmt.Errorf("own temporary BPF object remains after close")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// Read emits only the requested tasks in the caller's PID namespace. Every
// thread is pinned before the iterator runs; kernel birth ticks must match the
// caller's frozen ownership inventory. The caller owns the freezer boundary.
func Read(tasks []Task) (observation Observation, resultErr error) {
	if len(tasks) == 0 || len(tasks) > 32768 {
		return observation, fmt.Errorf("invalid timer task count")
	}
	wanted := map[uint32]Task{}
	var pidfds []int
	defer func() {
		for _, fd := range pidfds {
			unix.Close(fd)
		}
	}()
	for _, task := range tasks {
		if task.PID < 1 || uint64(task.PID) > 0x7fffffff || task.StartTicks == 0 {
			return observation, fmt.Errorf("invalid exact timer task identity")
		}
		if _, exists := wanted[uint32(task.PID)]; exists {
			return observation, fmt.Errorf("duplicate timer task identity")
		}
		fd, err := unix.PidfdOpen(task.PID, unix.O_EXCL) // PIDFD_THREAD, including non-leader TIDs.
		if err != nil {
			return observation, err
		}
		pidfds = append(pidfds, fd)
		wanted[uint32(task.PID)] = task
	}
	checkLive := func() error {
		for _, fd := range pidfds {
			poll := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
			for {
				count, err := unix.Poll(poll, 0)
				if errors.Is(err, unix.EINTR) {
					continue
				}
				if err != nil || count != 0 {
					return fmt.Errorf("pinned timer task exited or became invalid: %v", err)
				}
				break
			}
		}
		return nil
	}
	if err := checkLive(); err != nil {
		return observation, err
	}
	raw, err := boundedFile("/sys/kernel/btf/vmlinux", 32<<20)
	if err != nil {
		return observation, err
	}
	observation.KernelBTFHash = fmt.Sprintf("%x", sha256.Sum256(raw))
	btf, err := parseBTF(raw)
	if err != nil {
		return observation, err
	}
	ids := map[string]uint32{}
	for _, name := range []string{"bpf_iter__task", "task_struct", "pid", "upid", "bpf_iter_meta"} {
		id, err := btf.named(name, 4)
		if err != nil {
			return observation, err
		}
		ids[name] = id
	}
	attach, err := btf.named("bpf_iter_task", 12)
	if err != nil {
		return observation, err
	}
	member := func(id uint32, name string) (int32, error) {
		_, offset, err := btf.member(id, name)
		return offset, err
	}
	fields := map[string]int32{}
	for _, row := range []struct{ key, owner, member string }{
		{"ctxTask", "bpf_iter__task", "task"}, {"ctxMeta", "bpf_iter__task", "meta"},
		{"threadPID", "task_struct", "thread_pid"}, {"birth", "task_struct", "start_boottime"},
		{"level", "pid", "level"}, {"numbers", "pid", "numbers"},
		{"ns", "upid", "ns"}, {"nr", "upid", "nr"}, {"seq", "bpf_iter_meta", "seq"},
	} {
		value, err := member(ids[row.owner], row.member)
		if err != nil {
			return observation, err
		}
		if value < 0 || value > 32767 {
			return observation, fmt.Errorf("unsupported kernel task member offset")
		}
		fields[row.key] = value
	}
	block, blockOffset, err := btf.member(ids["task_struct"], "restart_block")
	if err != nil {
		return observation, err
	}
	if block == 0 || uint64(block) >= uint64(len(btf.types)) {
		return observation, fmt.Errorf("invalid kernel restart block type")
	}
	blockSize := int(btf.types[block].value)
	if blockSize < 16 || blockSize > 128 || btf.types[ids["upid"]].value != 16 {
		return observation, fmt.Errorf("unsupported kernel task restart layout")
	}
	fnOffset, err := member(block, "fn")
	if err != nil {
		return observation, err
	}
	sleep, sleepOffset, err := btf.member(block, "nanosleep")
	if err != nil {
		return observation, err
	}
	sleepFields := map[string]int{}
	for name, size := range map[string]int{"clockid": 4, "type": 4, "rmtp": 8, "expires": 8} {
		offset, err := member(sleep, name)
		if err != nil {
			return observation, err
		}
		value := int(sleepOffset + offset)
		if value < 0 || value+size > blockSize {
			return observation, fmt.Errorf("kernel timer value exceeds restart block")
		}
		sleepFields[name] = value
	}
	if fnOffset < 0 || int(fnOffset)+8 > blockSize {
		return observation, fmt.Errorf("kernel restart function exceeds block")
	}
	function, err := timerFunction()
	if err != nil {
		return observation, err
	}
	ticks, err := clockTicks()
	if err != nil {
		return observation, err
	}
	var descriptors []int
	defer func() {
		for index := len(descriptors) - 1; index >= 0; index-- {
			resultErr = errors.Join(resultErr, unix.Close(descriptors[index]))
		}
		if observation.ProgramID != 0 {
			err := released(13, observation.ProgramID)
			observation.ProgramReleased = err == nil
			resultErr = errors.Join(resultErr, err)
		}
		if observation.MapID != 0 {
			err := released(14, observation.MapID)
			observation.MapReleased = err == nil
			resultErr = errors.Join(resultErr, err)
		}
	}()
	mapAttr := make([]byte, 72)
	binary.LittleEndian.PutUint32(mapAttr, 1)
	binary.LittleEndian.PutUint32(mapAttr[4:], 4)
	binary.LittleEndian.PutUint32(mapAttr[8:], 8)
	binary.LittleEndian.PutUint32(mapAttr[12:], uint32(len(tasks)))
	mapFD, err := bpf(0, mapAttr)
	if err != nil {
		return observation, err
	}
	descriptors = append(descriptors, mapFD)
	observation.MapID, err = objectID(mapFD)
	if err != nil {
		return observation, err
	}
	for pid, task := range wanted {
		var pinned runtime.Pinner
		key, value, attr := make([]byte, 4), make([]byte, 8), make([]byte, 32)
		binary.LittleEndian.PutUint32(key, pid)
		binary.LittleEndian.PutUint64(value, task.StartTicks)
		binary.LittleEndian.PutUint32(attr, uint32(mapFD))
		binary.LittleEndian.PutUint64(attr[8:], address(&pinned, key))
		binary.LittleEndian.PutUint64(attr[16:], address(&pinned, value))
		_, err = bpf(2, attr)
		pinned.Unpin()
		runtime.KeepAlive(key)
		runtime.KeepAlive(value)
		if err != nil {
			return observation, err
		}
	}
	freeze := make([]byte, 4)
	binary.LittleEndian.PutUint32(freeze, uint32(mapFD))
	if _, err := bpf(22, freeze); err != nil {
		return observation, err
	}
	p := program{}
	p.mov(6, 1)
	p.emit(0x79, 7, 6, int16(fields["ctxTask"]), 0)
	p.jump(0x15, 7, 0, 0)
	p.emit(0x85, 0, 0, 0, 158)
	p.read(-16, 8, 0, fields["threadPID"])
	p.emit(0x79, 8, 10, -16, 0)
	p.read(-24, 4, 8, fields["level"])
	p.emit(0x61, 9, 10, -24, 0)
	p.jump(0x25, 9, 0, 32)
	p.emit(0x7b, 10, 9, -40, 0)
	p.emit(0x67, 9, 0, 0, 4)
	p.emit(0x0f, 8, 9, 0, 0)
	p.read(-32, 8, 8, fields["numbers"]+fields["ns"])
	p.read(-16, 8, 7, fields["threadPID"])
	p.emit(0x79, 8, 10, -16, 0)
	p.read(-24, 4, 8, fields["level"])
	p.emit(0x61, 0, 10, -24, 0)
	p.emit(0x79, 9, 10, -40, 0)
	p.jump(0x2d, 9, 0, 0)
	p.emit(0x67, 9, 0, 0, 4)
	p.emit(0x0f, 8, 9, 0, 0)
	p.add(8, fields["numbers"])
	p.read(-48, 4, 8, fields["nr"])
	p.emit(0x61, 0, 10, -48, 0)
	p.emit(0x63, 10, 0, -256, 0)
	p.emit(0x62, 10, 0, -252, 0)
	p.read(-56, 8, 8, fields["ns"])
	p.emit(0x79, 0, 10, -56, 0)
	p.emit(0x79, 9, 10, -32, 0)
	p.jump(0x5d, 0, 9, 0)
	p.emit(0x18, 1, 1, 0, int32(mapFD))
	p.emit(0, 0, 0, 0, 0)
	p.mov(2, 10)
	p.add(2, -48)
	p.emit(0x85, 0, 0, 0, 1)
	p.jump(0x15, 0, 0, 0)
	p.read(-240, int32(blockSize), 7, blockOffset)
	p.read(-248, 8, 7, fields["birth"])
	p.emit(0x79, 1, 6, int16(fields["ctxMeta"]), 0)
	p.emit(0x79, 1, 1, int16(fields["seq"]), 0)
	p.mov(2, 10)
	p.add(2, -256)
	p.emit(0xb7, 3, 0, 0, int32(blockSize+16))
	p.emit(0x85, 0, 0, 0, 127)
	code := p.finish()
	observation.ProgramHash = fmt.Sprintf("%x", sha256.Sum256(code))
	license, verifier, attr := []byte("GPL\x00"), make([]byte, 65536), make([]byte, 144)
	var pinned runtime.Pinner
	defer pinned.Unpin()
	binary.LittleEndian.PutUint32(attr, 26)
	binary.LittleEndian.PutUint32(attr[4:], uint32(len(code)/8))
	binary.LittleEndian.PutUint64(attr[8:], address(&pinned, code))
	binary.LittleEndian.PutUint64(attr[16:], address(&pinned, license))
	binary.LittleEndian.PutUint32(attr[24:], 1)
	binary.LittleEndian.PutUint32(attr[28:], uint32(len(verifier)))
	binary.LittleEndian.PutUint64(attr[32:], address(&pinned, verifier))
	binary.LittleEndian.PutUint32(attr[68:], 28)
	binary.LittleEndian.PutUint32(attr[108:], attach)
	progFD, err := bpf(5, attr)
	runtime.KeepAlive(code)
	runtime.KeepAlive(license)
	runtime.KeepAlive(verifier)
	if err != nil {
		return observation, fmt.Errorf("load bounded task timer reader: %w: %s", err, strings.TrimRight(string(verifier), "\x00"))
	}
	descriptors = append(descriptors, progFD)
	observation.ProgramID, err = objectID(progFD)
	if err != nil {
		return observation, err
	}
	linkAttr := make([]byte, 32)
	binary.LittleEndian.PutUint32(linkAttr, uint32(progFD))
	binary.LittleEndian.PutUint32(linkAttr[8:], 28)
	linkFD, err := bpf(28, linkAttr)
	if err != nil {
		return observation, err
	}
	descriptors = append(descriptors, linkFD)
	iterAttr := make([]byte, 8)
	binary.LittleEndian.PutUint32(iterAttr, uint32(linkFD))
	iterFD, err := bpf(33, iterAttr)
	if err != nil {
		return observation, err
	}
	reader := os.NewFile(uintptr(iterFD), "owned task timer iterator")
	maximum := len(tasks) * (blockSize + 16)
	data, readErr := io.ReadAll(io.LimitReader(reader, int64(maximum)+1))
	closeErr := reader.Close()
	if err := errors.Join(readErr, closeErr); err != nil {
		return observation, err
	}
	if len(data) != maximum {
		return observation, fmt.Errorf("timer iterator did not return one record per exact requested task")
	}
	seen := map[uint32]bool{}
	for len(data) > 0 {
		pid, birth := binary.LittleEndian.Uint32(data), binary.LittleEndian.Uint64(data[8:])
		task, exists := wanted[pid]
		if !exists || seen[pid] || birth/1000000000*ticks+birth%1000000000*ticks/1000000000 != task.StartTicks {
			return observation, fmt.Errorf("kernel timer task differs from pinned PID namespace/birth inventory")
		}
		seen[pid] = true
		block := data[16 : blockSize+16]
		state := State{Task: task, BirthNS: birth, RelativeTimer: binary.LittleEndian.Uint64(block[fnOffset:]) == function}
		if state.RelativeTimer {
			state.Clock = int32(binary.LittleEndian.Uint32(block[sleepFields["clockid"]:]))
			state.TimespecType = int32(binary.LittleEndian.Uint32(block[sleepFields["type"]:]))
			state.Remainder = binary.LittleEndian.Uint64(block[sleepFields["rmtp"]:])
			state.ExpiresNS = int64(binary.LittleEndian.Uint64(block[sleepFields["expires"]:]))
		}
		observation.States = append(observation.States, state)
		data = data[blockSize+16:]
	}
	if err := checkLive(); err != nil {
		return observation, err
	}
	sort.Slice(observation.States, func(i, j int) bool { return observation.States[i].PID < observation.States[j].PID })
	// Close all descriptors before publishing successful release observations.
	for index := len(descriptors) - 1; index >= 0; index-- {
		if err := unix.Close(descriptors[index]); err != nil {
			return observation, err
		}
	}
	descriptors = nil
	if err := released(13, observation.ProgramID); err != nil {
		return observation, err
	}
	observation.ProgramReleased = true
	if err := released(14, observation.MapID); err != nil {
		return observation, err
	}
	observation.MapReleased = true
	return observation, nil
}
