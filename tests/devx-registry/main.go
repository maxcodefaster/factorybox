// Test-only offline Docker Hub mirror. It serves the exact OCI bytes retained
// by Docker save so pulls preserve RepoDigests. It is never a runtime dependency.
package main

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"
)

var digestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

type pin struct {
	Image     string `json:"image"`
	Canonical string `json:"canonical_image"`
}

type mirror struct {
	blobs map[string][]byte
	repos map[string]bool
}

func load(archive io.Reader, pins []pin) (*mirror, error) {
	result := &mirror{blobs: make(map[string][]byte), repos: make(map[string]bool)}
	reader := tar.NewReader(archive)
	var total int64
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if header.Typeflag != tar.TypeReg || !strings.HasPrefix(header.Name, "blobs/sha256/") {
			continue
		}
		digest := "sha256:" + strings.TrimPrefix(header.Name, "blobs/sha256/")
		if !digestPattern.MatchString(digest) || header.Size < 0 || header.Size > 128<<20 {
			return nil, fmt.Errorf("invalid archive blob")
		}
		total += header.Size
		if total > 256<<20 {
			return nil, fmt.Errorf("example image archive exceeds bounded fixture")
		}
		data, err := io.ReadAll(reader)
		if err != nil || fmt.Sprintf("sha256:%x", sha256.Sum256(data)) != digest {
			return nil, fmt.Errorf("archive blob checksum differs: %s", digest)
		}
		if _, exists := result.blobs[digest]; exists {
			return nil, fmt.Errorf("duplicate archive blob: %s", digest)
		}
		result.blobs[digest] = data
	}
	if len(pins) < 1 || len(pins) > 2 {
		return nil, fmt.Errorf("requires the exact bounded example images")
	}
	for _, pin := range pins {
		repo, digest, ok := strings.Cut(pin.Canonical, "@")
		if !ok || !digestPattern.MatchString(digest) || len(result.blobs[digest]) == 0 ||
			strings.ContainsAny(repo, "/.:") {
			return nil, fmt.Errorf("requires exact Docker Hub official-image archive roots")
		}
		result.repos["library/"+repo] = true
	}
	return result, nil
}

func (mirror *mirror) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	if request.Method != "GET" && request.Method != "HEAD" {
		response.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	response.Header().Set("Docker-Distribution-Api-Version", "registry/2.0")
	if request.URL.Path == "/v2/" {
		response.WriteHeader(http.StatusOK)
		return
	}
	path := strings.TrimPrefix(request.URL.Path, "/v2/")
	for _, kind := range []string{"manifests", "blobs"} {
		repo, digest, ok := strings.Cut(path, "/"+kind+"/")
		if !ok || !mirror.repos[repo] || !digestPattern.MatchString(digest) {
			continue
		}
		data, exists := mirror.blobs[digest]
		if !exists {
			break
		}
		contentType := "application/octet-stream"
		if kind == "manifests" {
			var manifest struct {
				MediaType string `json:"mediaType"`
			}
			if json.Unmarshal(data, &manifest) != nil || manifest.MediaType == "" {
				break
			}
			contentType = manifest.MediaType
		}
		response.Header().Set("Content-Type", contentType)
		response.Header().Set("Docker-Content-Digest", digest)
		http.ServeContent(response, request, digest, time.Time{}, bytes.NewReader(data))
		return
	}
	http.NotFound(response, request)
}

func main() {
	archive := flag.String("archive", "", "Verified Docker save OCI archive")
	pinsPath := flag.String("pins", "", "Exact example-image roots")
	flag.Parse()
	data, err := os.ReadFile(*pinsPath)
	if err != nil {
		log.Fatal(err)
	}
	var pins []pin
	if err := json.Unmarshal(data, &pins); err != nil {
		log.Fatal(err)
	}
	file, err := os.Open(*archive)
	if err != nil {
		log.Fatal(err)
	}
	mirror, err := load(file, pins)
	file.Close()
	if err != nil {
		log.Fatal(err)
	}
	server := &http.Server{Addr: "127.0.0.1:5000", Handler: mirror, ReadHeaderTimeout: 5 * time.Second}
	log.Print("verified offline registry ready on loopback")
	log.Fatal(server.ListenAndServe())
}
