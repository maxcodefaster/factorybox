//go:build linux

package main

import (
	"bytes"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestExperimentCopyPipelineChild(t *testing.T) {
	mode := os.Getenv("EXPERIMENT_PIPELINE_TEST_CHILD")
	if mode == "" {
		return
	}
	timeout := time.Second
	if mode == "timeout" {
		timeout = 40 * time.Millisecond
	}
	err := experimentCopyPipelineRegistered(timeout)
	if err == nil {
		err = experimentCopyPipelineBarrier(timeout)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(23)
	}
	fmt.Print("restored")
	os.Exit(0)
}

func TestExperimentCopyPipeline(t *testing.T) {
	for _, mode := range []string{"release", "close", "invalid", "truncated", "timeout", "ungated-control", "stream"} {
		t.Run(mode, func(t *testing.T) {
			kind := unix.SOCK_SEQPACKET
			if mode == "stream" {
				kind = unix.SOCK_STREAM
			}
			sockets, err := unix.Socketpair(unix.AF_UNIX, kind|unix.SOCK_CLOEXEC, 0)
			if err != nil {
				t.Fatal(err)
			}
			parent := os.NewFile(uintptr(sockets[0]), "test-pipeline-parent")
			child := os.NewFile(uintptr(sockets[1]), "test-pipeline-child")
			connection, err := net.FileConn(parent)
			parent.Close()
			if err != nil {
				child.Close()
				t.Fatal(err)
			}
			defer connection.Close()
			connection.SetDeadline(time.Now().Add(2 * time.Second))
			var output bytes.Buffer
			command := exec.Command(os.Args[0], "-test.run=^TestExperimentCopyPipelineChild$")
			command.Env = append(os.Environ(), "EXPERIMENT_PIPELINE_TEST_CHILD="+mode,
				"CRIU_SYSBOX_COPY_PIPELINE=1", "CRIU_SYSBOX_RETAIN_SOURCE=1", "RUNTIME_FORK_COPY_PIPELINE=1")
			if mode == "ungated-control" {
				command.Env = append(command.Env, "RUNTIME_FORK_COPY_PIPELINE=0")
			}
			command.ExtraFiles = []*os.File{child}
			command.Stdout, command.Stderr = &output, &output
			if err := command.Start(); err != nil {
				child.Close()
				t.Fatal(err)
			}
			child.Close()
			done := make(chan error, 1)
			go func() { done <- command.Wait() }()
			reaped := false
			defer func() {
				if !reaped {
					command.Process.Kill()
					<-done
				}
			}()
			var ready [8]byte
			count, err := connection.Read(ready[:])
			if mode == "ungated-control" || mode == "stream" {
				if err == nil || count != 0 {
					t.Fatalf("unsafe control advertised readiness: %q %v", ready[:count], err)
				}
			} else {
				if err != nil || count != 1 || ready[0] != 'F' {
					t.Fatalf("registration not acknowledged: %q %v", ready[:count], err)
				}
				count, err = connection.Read(ready[:])
				if err != nil || count != 1 || ready[0] != 'P' {
					t.Fatalf("actual gate not ready: %q %v", ready[:count], err)
				}
				if mode != "timeout" {
					select {
					case err := <-done:
						reaped = true
						t.Fatalf("restore escaped its barrier: %v %s", err, output.String())
					case <-time.After(15 * time.Millisecond):
					}
				}
				switch mode {
				case "release":
					connection.Write([]byte("R"))
				case "invalid":
					connection.Write([]byte("A"))
				case "truncated":
					connection.Write([]byte("RRRRRRRRRRRR"))
				case "close":
					connection.Close()
				}
			}
			select {
			case err := <-done:
				reaped = true
				success := mode == "release" || mode == "ungated-control"
				if (err == nil) != success || strings.Contains(output.String(), "restored") != success {
					t.Fatalf("mode=%s err=%v output=%s", mode, err, output.String())
				}
			case <-time.After(2 * time.Second):
				t.Fatal("owned gate child did not finish")
			}
		})
	}
}
