//go:build linux

// Package providerfixture is optional test-controller IPC. It has no role in
// the lifecycle runtime. The host peer must independently verify the worker,
// target ownership, gateway executable/birth/netns and confirmed pidfd exit.
package providerfixture

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

var token = regexp.MustCompile(`^[0-9a-f]{32}$`)
var sandbox = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)
var hash = regexp.MustCompile(`^[0-9a-f]{64}$`)

type Target struct {
	RunOwner string `json:"run_owner"`
	Sandbox  string `json:"sandbox"`
	Owner    string `json:"owner"`
	Runtime  string `json:"runtime_id"`
	PID      int    `json:"pid"`
	Created  string `json:"runtime_created"`
}

type Request struct {
	Schema    int    `json:"schema"`
	Nonce     string `json:"nonce"`
	Phase     string `json:"phase"`
	Operation string `json:"operation"`
	Target    Target `json:"target"`
}

type Reply struct {
	Request
	Accepted bool   `json:"accepted"`
	Stopped  bool   `json:"stopped"`
	PID      int    `json:"gateway_pid"`
	Birth    string `json:"gateway_birth"`
	SHA256   string `json:"gateway_sha256"`
	Network  string `json:"gateway_network"`
	Error    string `json:"error,omitempty"`
}

type Lease struct {
	directory string
	request   Request
	attached  Reply
	closed    bool
}

func fileName(prefix string, value Request) string {
	return prefix + "-" + value.Nonce + "-" + value.Phase + ".json"
}

func Exchange(ctx context.Context, directory string, request Request) (Reply, error) {
	var reply Reply
	if !filepath.IsAbs(directory) || filepath.Clean(directory) != directory {
		return reply, fmt.Errorf("gateway IPC requires an absolute output directory")
	}
	resolved, err := filepath.EvalSymlinks(directory)
	if err != nil || resolved != directory {
		return reply, errors.Join(err, fmt.Errorf("gateway IPC refuses an aliased output directory"))
	}
	if request.Schema != 1 || !token.MatchString(request.Nonce) ||
		(request.Phase != "attach" && request.Phase != "release") ||
		(request.Operation != "arm" && request.Operation != "finish") ||
		!token.MatchString(request.Target.RunOwner) || !token.MatchString(request.Target.Owner) ||
		!sandbox.MatchString(request.Target.Sandbox) || request.Target.PID <= 1 ||
		request.Target.Runtime == "" || request.Target.Created == "" {
		return reply, fmt.Errorf("gateway request lacks its exact target identity")
	}
	output, err := os.Stat(directory)
	if err != nil {
		return reply, err
	}
	owner := output.Sys().(*syscall.Stat_t)
	body, err := json.Marshal(request)
	if err != nil {
		return reply, err
	}
	path := filepath.Join(directory, fileName("provider-request", request))
	partial := path + ".partial"
	file, err := os.OpenFile(partial, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return reply, err
	}
	_, err = file.Write(body)
	if err == nil {
		err = file.Chown(int(owner.Uid), int(owner.Gid))
	}
	if err == nil {
		err = file.Sync()
	}
	err = errors.Join(err, file.Close())
	if err != nil {
		return reply, err
	}
	if err := unix.Renameat2(unix.AT_FDCWD, partial, unix.AT_FDCWD, path, unix.RENAME_NOREPLACE); err != nil {
		return reply, err
	}
	response := filepath.Join(directory, fileName("provider-reply", request))
	for {
		file, err := os.OpenFile(response, os.O_RDONLY|unix.O_NOFOLLOW, 0)
		if err == nil {
			info, statErr := file.Stat()
			if statErr != nil {
				file.Close()
				return reply, statErr
			}
			stat := info.Sys().(*syscall.Stat_t)
			if !info.Mode().IsRegular() || info.Size() > 8192 || info.Mode().Perm() != 0600 || stat.Nlink != 1 || stat.Uid != owner.Uid {
				file.Close()
				return reply, fmt.Errorf("gateway reply is not the exact private host response")
			}
			decoder := json.NewDecoder(io.LimitReader(file, 8193))
			decoder.DisallowUnknownFields()
			decodeErr := decoder.Decode(&reply)
			var extra any
			trailing := decoder.Decode(&extra)
			file.Close()
			if decodeErr != nil || !errors.Is(trailing, io.EOF) {
				return reply, errors.Join(decodeErr, fmt.Errorf("invalid bounded gateway response"))
			}
			if reply.Request != request || !reply.Accepted || reply.Error != "" || reply.PID <= 1 || reply.Birth == "" ||
				!hash.MatchString(reply.SHA256) || reply.Network == "" || reply.Stopped != (request.Phase == "release") {
				return reply, fmt.Errorf("gateway reply differs from its request: %+v", reply)
			}
			return reply, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return reply, err
		}
		select {
		case <-ctx.Done():
			return reply, ctx.Err()
		case <-time.After(25 * time.Millisecond):
		}
	}
}

func Acquire(ctx context.Context, directory, operation string, target Target) (*Lease, error) {
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, err
	}
	request := Request{Schema: 1, Nonce: hex.EncodeToString(nonce[:]), Phase: "attach", Operation: operation, Target: target}
	reply, err := Exchange(ctx, directory, request)
	if err != nil {
		return nil, err
	}
	return &Lease{directory: directory, request: request, attached: reply}, nil
}

func (lease *Lease) Close(ctx context.Context) error {
	if lease.closed {
		return nil
	}
	request := lease.request
	request.Phase = "release"
	reply, err := Exchange(ctx, lease.directory, request)
	if err != nil {
		return err
	}
	if reply.PID != lease.attached.PID || reply.Birth != lease.attached.Birth || reply.SHA256 != lease.attached.SHA256 || reply.Network != lease.attached.Network {
		return fmt.Errorf("gateway release refers to another process")
	}
	lease.closed = true
	return nil
}
