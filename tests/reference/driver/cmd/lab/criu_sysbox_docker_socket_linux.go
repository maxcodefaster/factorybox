//go:build linux

package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"strconv"

	pathrs "github.com/cyphar/filepath-securejoin/pathrs-lite"
)

func criuServiceSocketDial(ctx context.Context, root string) (net.Conn, error) {
	// An absolute /var/run -> /run link must resolve inside the sandbox.
	// Keep the resolved socket pinned through connect, rather than reopening
	// a pathname that could change or escape the selected root.
	handle, err := pathrs.OpenInRoot(root, "var/run/docker.sock")
	if err != nil {
		return nil, err
	}
	defer handle.Close()
	info, err := handle.Stat()
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSocket == 0 {
		return nil, fmt.Errorf("selected private Docker endpoint is not a socket")
	}
	return (&net.Dialer{}).DialContext(ctx, "unix", "/proc/self/fd/"+strconv.Itoa(int(handle.Fd())))
}
