//go:build linux

package main

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

func closedWritableMapping(path string) ([]byte, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := file.Truncate(32768); err != nil {
		file.Close()
		return nil, err
	}
	mapping, err := unix.Mmap(int(file.Fd()), 0, 32768, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
	closeErr := file.Close()
	if err != nil {
		return nil, err
	}
	if closeErr != nil {
		unix.Munmap(mapping)
		return nil, closeErr
	}
	for index := 0; index < 16; index++ {
		mapping[index] = byte(index)
	}
	return mapping, unix.Msync(mapping, unix.MS_SYNC)
}

func writableMappingInode(mapping []byte) (uint64, error) {
	address := uint64(uintptr(unsafe.Pointer(&mapping[0])))
	data, err := os.ReadFile("/proc/self/maps")
	if err != nil {
		return 0, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}
		rangeParts := strings.SplitN(fields[0], "-", 2)
		if len(rangeParts) != 2 {
			continue
		}
		start, startErr := strconv.ParseUint(rangeParts[0], 16, 64)
		end, endErr := strconv.ParseUint(rangeParts[1], 16, 64)
		if startErr == nil && endErr == nil && start <= address && address < end {
			if fields[1] != "rw-s" {
				return 0, fmt.Errorf("mapping is not writable and shared")
			}
			return strconv.ParseUint(fields[4], 10, 64)
		}
	}
	return 0, fmt.Errorf("original mapping is absent")
}

func init() {
	if os.Getenv("FACTORYBOX_SHARED_SOURCE_WRITABLE_MMAP") != "1" ||
		len(os.Args) == 2 && os.Args[1] == "--docker-parent" {
		return
	}
	project := os.Getenv("FACTORYBOX_SHARED_SOURCE_ROOT")
	if project == "" {
		project = "/home/coder/project"
	}
	mapping, err := closedWritableMapping(project + "/.factorybox-mapped-shm")
	if err != nil {
		log.Fatal(err)
	}
	listener, err := net.Listen("unix", "/workspace/.writable-mapping.sock")
	if err != nil {
		log.Fatal(err)
	}
	var mutex sync.Mutex
	counter := 0
	server := &http.Server{ReadHeaderTimeout: 5 * time.Second,
		Handler: http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			mutex.Lock()
			defer mutex.Unlock()
			inode, err := writableMappingInode(mapping)
			if err != nil || inode == 0 {
				http.Error(writer, "original writable mapping was lost", 500)
				return
			}
			if request.Method == http.MethodPost {
				counter++
				mapping[0]++
				if err := unix.Msync(mapping, unix.MS_SYNC); err != nil {
					http.Error(writer, "original writable mapping cannot sync", 500)
					return
				}
			}
			if err := json.NewEncoder(writer).Encode(map[string]any{
				"inode": inode, "counter": counter, "writable_shared": true,
				"mapped_hex": hex.EncodeToString(mapping[:16]),
			}); err != nil {
				log.Print(err)
			}
		}),
	}
	go func() { log.Fatal(server.Serve(listener)) }()
}
