//go:build linux

// Test-only workload for canonical source edits while execution is paused.
package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

func main() {
	logPath := "/workspace/shared-source.log"
	if len(os.Args) == 2 && os.Args[1] == "--docker-parent" {
		logPath = "/workspace/docker-parent.log"
	}
	output, err := os.OpenFile(logPath, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0600)
	if err != nil {
		log.Fatal(err)
	}
	input, err := os.Open("/dev/null")
	if err != nil {
		log.Fatal(err)
	}
	if err := unix.Dup2(int(input.Fd()), 0); err != nil {
		log.Fatal(err)
	}
	if err := unix.Dup2(int(output.Fd()), 1); err != nil {
		log.Fatal(err)
	}
	if err := unix.Dup2(int(output.Fd()), 2); err != nil {
		log.Fatal(err)
	}
	input.Close()
	output.Close()
	if len(os.Args) == 2 && os.Args[1] == "--docker-parent" {
		if err := dockerParent(); err != nil {
			log.Fatal(err)
		}
		return
	}
	payload := make([]byte, 1<<20)
	if _, err := rand.Read(payload); err != nil {
		log.Fatal(err)
	}
	project := os.Getenv("FACTORYBOX_SHARED_SOURCE_ROOT")
	if project == "" {
		project = "/home/coder/project"
	}
	workingDirectory := project
	relativeName := "current.txt"
	if os.Getenv("FACTORYBOX_SHARED_SOURCE_SUBDIRECTORY") == "1" {
		workingDirectory += "/folder"
		relativeName = "../current.txt"
	}
	if err := os.Chdir(workingDirectory); err != nil {
		log.Fatal(err)
	}
	// Keep a real project FD and mapping across an editor's atomic save.
	held, err := os.Open(project + "/held.txt")
	if err != nil {
		log.Fatal(err)
	}
	mapping, err := unix.Mmap(int(held.Fd()), 0, 4096, unix.PROT_READ, unix.MAP_PRIVATE)
	if err != nil {
		log.Fatal(err)
	}
	directory, err := os.Open(workingDirectory)
	if err != nil {
		log.Fatal(err)
	}
	watch, err := unix.InotifyInit1(unix.IN_NONBLOCK | unix.IN_CLOEXEC)
	if err != nil {
		log.Fatal(err)
	}
	if _, err := unix.InotifyAddWatch(watch, project, unix.IN_CREATE|unix.IN_MOVED_TO|unix.IN_MODIFY); err != nil {
		log.Fatal(err)
	}
	listener, err := net.Listen("unix", "/workspace/.shared-source.sock")
	if err != nil {
		log.Fatal(err)
	}
	var mutex sync.Mutex
	counter := 0
	requests := 0
	networkClient := &http.Client{Timeout: 3 * time.Second, Transport: &http.Transport{DisableKeepAlives: true}}
	server := &http.Server{ReadHeaderTimeout: 5 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		mutex.Lock()
		defer mutex.Unlock()
		requests++
		networkValue := ""
		if address := os.Getenv("FACTORYBOX_NETWORK_URL"); address != "" {
			response, err := networkClient.Get(address)
			if err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			body, err := io.ReadAll(io.LimitReader(response.Body, 256))
			response.Body.Close()
			if err != nil || response.StatusCode != 200 {
				http.Error(w, "network response differs", 500)
				return
			}
			networkValue = string(body)
		}
		if request.Method == http.MethodPost {
			if err := os.WriteFile(project+"/runtime-write", []byte("runtime user 1000"), 0644); err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			counter++
			payload[counter%len(payload)] ^= byte(counter)
		}
		current, err := os.ReadFile(project + "/current.txt")
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		cwd, err := os.Getwd()
		if err != nil {
			http.Error(w, "project working directory lost", 500)
			return
		}
		var cwdStat, originalDirectory unix.Stat_t
		if unix.Stat(".", &cwdStat) != nil || unix.Fstat(int(directory.Fd()), &originalDirectory) != nil ||
			cwdStat.Dev != originalDirectory.Dev || cwdStat.Ino != originalDirectory.Ino {
			http.Error(w, "working directory inode differs from retained directory", 500)
			return
		}
		relative, err := os.ReadFile(relativeName)
		if err != nil || string(relative) != string(current) {
			http.Error(w, "relative source access differs", 500)
			return
		}
		fd, err := unix.Openat(int(directory.Fd()), relativeName, unix.O_RDONLY|unix.O_CLOEXEC, 0)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		file := os.NewFile(uintptr(fd), "held project directory read")
		throughDirectory, readErr := io.ReadAll(file)
		file.Close()
		if readErr != nil || string(throughDirectory) != string(current) {
			http.Error(w, "retained directory source access differs", 500)
			return
		}
		old := make([]byte, 16)
		if _, err := held.ReadAt(old, 0); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		events := make([]byte, 64<<10)
		n, err := unix.Read(watch, events)
		if err != nil && err != unix.EAGAIN {
			http.Error(w, err.Error(), 500)
			return
		}
		digest := sha256.Sum256(payload)
		if err := json.NewEncoder(w).Encode(map[string]any{"counter": counter, "requests": requests, "cwd": cwd, "current": string(current), "network": networkValue,
			"held": string(old), "mapped": string(mapping[:16]), "watch_bytes": max(n, 0),
			"ram_sha256": hex.EncodeToString(digest[:]), "ram_bytes": len(payload)}); err != nil {
			log.Print(err)
		}
	})}
	log.Fatal(server.Serve(listener))
}
