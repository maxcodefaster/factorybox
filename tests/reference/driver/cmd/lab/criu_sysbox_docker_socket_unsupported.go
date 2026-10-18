//go:build !linux

package main

import (
	"context"
	"fmt"
	"net"
)

func criuServiceSocketDial(context.Context, string) (net.Conn, error) {
	return nil, fmt.Errorf("the private Docker socket oracle requires Linux")
}
