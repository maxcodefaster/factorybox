//go:build linux

package main

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/unix"
)

func ioPolicyCheck() error {
	var parameters [120]byte
	for _, call := range []uintptr{unix.SYS_IO_URING_SETUP, unix.SYS_IO_URING_ENTER, unix.SYS_IO_URING_REGISTER} {
		args := [3]uintptr{^uintptr(0), 0, 0}
		if call == unix.SYS_IO_URING_SETUP {
			args = [3]uintptr{1, uintptr(unsafe.Pointer(&parameters)), 0}
		}
		result, _, errno := unix.Syscall6(call, args[0], args[1], args[2], 0, 0, 0)
		if errno == 0 && call == unix.SYS_IO_URING_SETUP {
			unix.Close(int(result))
		}
		if errno != unix.EPERM {
			return fmt.Errorf("io_uring syscall %d should return EPERM, got %v", call, errno)
		}
	}
	fmt.Println("io_uring setup, enter and register denied with EPERM")
	return nil
}
