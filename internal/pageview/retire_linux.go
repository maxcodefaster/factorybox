package pageview

import (
	"fmt"
	"net"
	"os"
	"time"

	"golang.org/x/sys/unix"
)

func RetireAt(socket string, original, backing *os.File) error {
	captured, err := Captured(original)
	if err != nil || !captured {
		return err
	}
	var stat unix.Stat_t
	if err := unix.Fstat(int(original.Fd()), &stat); err != nil {
		return err
	}
	for attempt := 0; attempt < 100; attempt++ {
		busy, err := retireOnce(socket, original, backing, stat)
		if err != nil || !busy {
			return err
		}
		time.Sleep(5 * time.Millisecond)
	}
	return fmt.Errorf("logical readers remain active at retirement")
}
func retireOnce(socket string, original, backing *os.File, stat unix.Stat_t) (bool, error) {
	connection, err := net.DialUnix("unixpacket", nil, &net.UnixAddr{Name: socket, Net: "unixpacket"})
	if err != nil {
		return false, err
	}
	defer connection.Close()
	if err := connection.SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
		return false, err
	}
	query := encode(15, [4]uint64{uint64(stat.Dev), stat.Ino, uint64(stat.Size), 2})
	n, _, err := connection.WriteMsgUnix(query, unix.UnixRights(int(original.Fd()), int(backing.Fd())), nil)
	if err != nil || n != len(query) {
		return false, fmt.Errorf("retirement query: %d %v", n, err)
	}
	op, fields, payload, fds, err := receive(connection)
	for _, fd := range fds {
		_ = unix.Close(fd)
	}
	if err != nil {
		return false, err
	}
	if op != 15 || len(fds) != 0 || len(payload) != 0 || fields[0] > 1 || fields[1] != 0 || fields[2] != 0 || fields[3] != 0 {
		return false, fmt.Errorf("invalid retirement acknowledgement")
	}
	return fields[0] == 1, nil
}
