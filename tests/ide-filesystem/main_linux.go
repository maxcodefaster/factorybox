//go:build linux

// This short-lived native fixture helper executes after the trusted controller
// has selected the original IDE mount namespace, root and working directory.
package main

import (
	"fmt"
	"io"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

func operation(args []string) (string, error) {
	switch {
	case len(args) == 4 && args[0] == "mkdir" && args[1] == "-p" && args[2] == "--":
		return "", os.MkdirAll(args[3], 0755)
	case len(args) == 4 && args[0] == "mount" && args[1] == "--bind":
		// FD 6 is the source tree cloned before entering the IDE namespace.
		return "", unix.MoveMount(6, "", unix.AT_FDCWD, args[3], unix.MOVE_MOUNT_F_EMPTY_PATH)
	case len(args) == 2 && args[0] == "clone":
		fd, err := unix.OpenTree(unix.AT_FDCWD, args[1], unix.OPEN_TREE_CLONE|unix.OPEN_TREE_CLOEXEC)
		if err != nil {
			return "", err
		}
		defer unix.Close(fd)
		// The original controller owns the other end of this socketpair.
		n, err := unix.SendmsgN(6, []byte{'M'}, unix.UnixRights(fd), nil, 0)
		if err == nil && n != 1 {
			err = fmt.Errorf("incomplete original mount handoff")
		}
		return "", err
	case len(args) == 5 && args[0] == "stat" && args[1] == "-c" && args[2] == "%d:%i" && args[3] == "--":
		info, err := os.Stat(args[4])
		if err != nil {
			return "", err
		}
		stat := info.Sys().(*syscall.Stat_t)
		return fmt.Sprintf("%d:%d", stat.Dev, stat.Ino), nil
	case len(args) == 3 && args[0] == "umount" && args[1] == "--":
		return "", unix.Unmount(args[2], 0)
	case len(args) == 2 && args[0] == "cat":
		file, err := os.Open(args[1])
		if err != nil {
			return "", err
		}
		defer file.Close()
		data, err := io.ReadAll(io.LimitReader(file, 1025))
		if len(data) > 1024 {
			return "", fmt.Errorf("fixture file exceeds its observation bound")
		}
		return string(data), err
	case len(args) == 3 && args[0] == "write":
		if len(args[2]) > 1024 {
			return "", fmt.Errorf("fixture write exceeds its observation bound")
		}
		return "", os.WriteFile(args[1], []byte(args[2]), 0600)
	default:
		return "", fmt.Errorf("unsupported native fixture operation")
	}
}

func main() {
	if os.Getenv("FACTORYBOX_IDE_FILESYSTEM") != "1" {
		fmt.Fprintln(os.Stderr, "requires its original native fixture controller")
		os.Exit(1)
	}
	output, err := operation(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Print(output)
}
