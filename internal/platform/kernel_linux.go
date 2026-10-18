package platform

import "golang.org/x/sys/unix"

func kernelRelease() (string, error) {
	var name unix.Utsname
	if err := unix.Uname(&name); err != nil {
		return "", err
	}
	return unix.ByteSliceToString(name.Release[:]), nil
}
