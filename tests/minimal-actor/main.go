// Test-only workload for the unchanged minimal Dev Container example.
package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log"
	"net"
	"net/http"
	"os"
	"sync"
	"time"
)

func main() {
	payload := make([]byte, 32<<20)
	if _, err := rand.Read(payload); err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile("/workspace/marker", []byte("source"), 0600); err != nil {
		log.Fatal(err)
	}
	listener, err := net.Listen("unix", "/workspace/.minimal.sock")
	if err != nil {
		log.Fatal(err)
	}
	var mutex sync.Mutex
	counter := 0
	server := &http.Server{ReadHeaderTimeout: 5 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mutex.Lock()
		defer mutex.Unlock()
		if r.Method == http.MethodPost {
			var change struct {
				Marker string `json:"marker"`
			}
			if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&change) != nil || change.Marker == "" {
				http.Error(w, "invalid mutation", http.StatusBadRequest)
				return
			}
			if err := os.WriteFile("/workspace/marker", []byte(change.Marker), 0600); err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			counter++
			payload[counter%len(payload)] ^= byte(counter)
		}
		marker, err := os.ReadFile("/workspace/marker")
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		digest := sha256.Sum256(payload)
		if err := json.NewEncoder(w).Encode(map[string]any{"counter": counter, "marker": string(marker), "ram_sha256": hex.EncodeToString(digest[:]), "ram_bytes": len(payload)}); err != nil {
			log.Print(err)
		}
	})}
	log.Fatal(server.Serve(listener))
}
