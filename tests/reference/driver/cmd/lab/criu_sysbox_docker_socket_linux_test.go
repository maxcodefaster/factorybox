//go:build linux

package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func criuTestSocketRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, name := range []string{"run", "var"} {
		if err := os.Mkdir(filepath.Join(root, name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink("/run", filepath.Join(root, "var", "run")); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestPrivateDockerSocketAbsoluteSymlinkStaysInRoot(t *testing.T) {
	root := criuTestSocketRoot(t)
	listener, err := net.Listen("unix", filepath.Join(root, "run", "docker.sock"))
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/info" {
			t.Errorf("unexpected private API request: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"ID":"owned-listener","Driver":"vfs"}`)
	})}
	go server.Serve(listener)
	defer server.Close()
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return criuServiceSocketDial(ctx, root)
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: time.Second}
	var state map[string]any
	if err := criuServiceJSON(context.Background(), client, "http://private-docker/info", false, &state); err != nil {
		t.Fatal(err)
	}
	if state["ID"] != "owned-listener" {
		t.Fatalf("absolute symlink reached another endpoint: %v", state)
	}
}

func TestPrivateDockerSocketRejectsNonSocket(t *testing.T) {
	root := criuTestSocketRoot(t)
	if err := os.WriteFile(filepath.Join(root, "run", "docker.sock"), []byte("cached health"), 0600); err != nil {
		t.Fatal(err)
	}
	conn, err := criuServiceSocketDial(context.Background(), root)
	if conn != nil {
		conn.Close()
	}
	if err == nil || !strings.Contains(err.Error(), "not a socket") {
		t.Fatalf("regular file accepted as private Docker: %v", err)
	}
}

func TestPrivateDockerSocketRejectsSymlinkOutsideRoot(t *testing.T) {
	root := criuTestSocketRoot(t)
	outside := filepath.Join(t.TempDir(), "other.sock")
	listener, err := net.Listen("unix", outside)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if err := os.Symlink(outside, filepath.Join(root, "run", "docker.sock")); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	conn, err := criuServiceSocketDial(ctx, root)
	if conn != nil {
		conn.Close()
	}
	if err == nil {
		t.Fatal("private Docker lookup connected outside the sandbox root")
	}
}
