package timerstate

import (
	"encoding/binary"
	"testing"
)

func executePIDFilter(t *testing.T, raw []byte, pid uint64) uint64 {
	t.Helper()
	r := [11]uint64{pid}
	for pc, steps := int64(0), 0; pc >= 0 && pc < int64(len(raw)/8); steps++ {
		if steps > len(raw)/8 {
			t.Fatal("PID filter has a loop")
		}
		row := raw[pc*8 : pc*8+8]
		dst := row[1] & 15
		value := int64(int32(binary.LittleEndian.Uint32(row[4:])))
		pc++
		switch row[0] {
		case 0x15, 0x55:
			matched := r[dst] == uint64(value)
			if matched == (row[0] == 0x15) {
				pc += int64(int16(binary.LittleEndian.Uint16(row[2:])))
			}
		case 0x05:
			pc += int64(int16(binary.LittleEndian.Uint16(row[2:])))
		case 0x06:
			pc += value
		case 0xb7:
			r[dst] = uint64(value)
		case 0x95:
			return r[0]
		default:
			t.Fatalf("unexpected PID filter instruction %#x", row[0])
		}
	}
	t.Fatal("PID filter escaped its instruction bound")
	return 0
}

func TestMaplessPIDFilterPreservesExactMembershipAndLongExitBounds(t *testing.T) {
	for _, count := range []int{1, 16, 32768} {
		pids := make([]uint32, count)
		for index := range pids {
			pids[index] = uint32(index*2 + 1)
		}
		p := program{}
		p.jump(0x15, 0, 0, 0)
		if err := p.onlyPIDs(pids); err != nil {
			t.Fatal(err)
		}
		p.emit(0xb7, 0, 0, 0, 1)
		p.emit(0x95, 0, 0, 0, 0)
		raw := p.finish()
		for _, pid := range []uint64{0, 1, uint64(pids[len(pids)-1]), 2, uint64(pids[len(pids)-1]) + 1, 0xffffffff} {
			want := uint64(0)
			if pid > 0 && pid%2 == 1 && pid <= uint64(pids[len(pids)-1]) {
				want = 1
			}
			if actual := executePIDFilter(t, raw, pid); actual != want {
				t.Fatal("PID filter changed its exact owned inventory", count, pid, actual, want)
			}
		}
		if count == 32768 && int32(binary.LittleEndian.Uint32(raw[20:])) <= 32767 {
			t.Fatal("large filter truncated its conditional exit to a short jump")
		}
	}
	for _, pids := range [][]uint32{nil, {0}, {2, 1}, {1, 1}, {0x80000000}, make([]uint32, 32769)} {
		p := program{}
		if err := p.onlyPIDs(pids); err == nil {
			t.Fatal("invalid PID filter was admitted", len(pids))
		}
	}
}
