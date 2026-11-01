//go:build linux

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"time"
)

var retainedDescriptions []*os.File

func init() {
	value := os.Getenv("FACTORYBOX_SHARED_SOURCE_RETAINED_DESCRIPTIONS")
	if value == "" {
		return
	}
	count, err := strconv.Atoi(value)
	if err != nil || count < 1 {
		log.Fatal("invalid owned retained-description count")
	}
	project := os.Getenv("FACTORYBOX_SHARED_SOURCE_ROOT")
	if project == "" {
		project = "/home/coder/project"
	}
	for index := 0; index < count; index++ {
		file, err := os.Open(project + "/held.txt")
		if err != nil {
			log.Fatal(err)
		}
		if _, err := file.Seek(int64(index), io.SeekStart); err != nil {
			log.Fatal(err)
		}
		retainedDescriptions = append(retainedDescriptions, file)
	}
	listener, err := net.Listen("unix", "/workspace/.large-descriptions.sock")
	if err != nil {
		log.Fatal(err)
	}
	server := &http.Server{
		ReadHeaderTimeout: 5 * time.Second,
		Handler: http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			digest := sha256.New()
			for index, file := range retainedDescriptions {
				offset, err := file.Seek(0, io.SeekCurrent)
				if err != nil || offset != int64(index) {
					http.Error(writer, "independent file position changed", 500)
					return
				}
				prefix := make([]byte, 16)
				if _, err := file.ReadAt(prefix, 0); err != nil {
					http.Error(writer, "retained source read failed", 500)
					return
				}
				fmt.Fprintf(digest, "%d:%d:%x\n", index, offset, prefix)
			}
			if err := json.NewEncoder(writer).Encode(map[string]any{
				"count":  len(retainedDescriptions),
				"sha256": hex.EncodeToString(digest.Sum(nil)),
			}); err != nil {
				log.Print(err)
			}
		}),
	}
	go func() {
		log.Fatal(server.Serve(listener))
	}()
}
