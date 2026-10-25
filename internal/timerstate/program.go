package timerstate

import (
	"encoding/binary"
	"fmt"
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
	// A short conditional selects a long exit or skips it. The fixed native
	// kernels support JMP32 JA, so a large owned inventory cannot truncate an
	// exit distance to int16 or turn a refusal into another instruction.
	p.emit(op, dst, src, 1, immediate)
	p.emit(0x05, 0, 0, 1, 0)
	p.exits = append(p.exits, len(p.code))
	p.emit(0x06, 0, 0, 0, 0)
}

func (p *program) onlyPIDs(pids []uint32) error {
	if len(pids) == 0 || len(pids) > 32768 {
		return fmt.Errorf("invalid inline timer PID count")
	}
	for index, pid := range pids {
		if pid == 0 || pid > 0x7fffffff || index > 0 && pid <= pids[index-1] {
			return fmt.Errorf("inline timer PIDs must be sorted unique native identities")
		}
	}
	var matches []int
	for _, pid := range pids {
		p.emit(0x55, 0, 0, 1, int32(pid))
		matches = append(matches, len(p.code))
		p.emit(0x06, 0, 0, 0, 0)
	}
	p.emit(0xb7, 0, 0, 0, 0)
	p.emit(0x95, 0, 0, 0, 0)
	for _, index := range matches {
		p.code[index].immediate = int32(len(p.code) - index - 1)
	}
	return nil
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
		p.code[index].immediate = int32(len(p.code) - index - 1)
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
