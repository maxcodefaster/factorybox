package main

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func archive(t *testing.T, named map[string][]byte) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := tar.NewWriter(&buffer)
	for name, data := range named {
		if err := writer.WriteHeader(&tar.Header{Name: name, Mode: 0600, Size: int64(len(data))}); err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func TestMirrorServesOriginalManifestBytesAndDigest(t *testing.T) {
	data := []byte(`{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json"}`)
	hash := fmt.Sprintf("%x", sha256.Sum256(data))
	pins := []pin{{Canonical: "node@sha256:" + hash}, {Canonical: "alpine@sha256:" + hash}}
	mirror, err := load(bytes.NewReader(archive(t, map[string][]byte{"blobs/sha256/" + hash: data})), pins)
	if err != nil {
		t.Fatal(err)
	}
	path := "/v2/library/node/manifests/sha256:" + hash
	for _, method := range []string{"GET", "HEAD"} {
		response := httptest.NewRecorder()
		mirror.ServeHTTP(response, httptest.NewRequest(method, path, nil))
		if response.Code != http.StatusOK || response.Header().Get("Docker-Content-Digest") != "sha256:"+hash ||
			response.Header().Get("Content-Type") != "application/vnd.oci.image.manifest.v1+json" ||
			(method == "GET" && !bytes.Equal(response.Body.Bytes(), data)) ||
			(method == "HEAD" && response.Body.Len() != 0) {
			t.Fatalf("exact offline manifest changed: %s %v", method, response)
		}
	}
	for _, path := range []string{"/v2/foreign/manifests/sha256:" + hash, "/v2/library/node/manifests/latest",
		"/v2/library/node/blobs/sha256:" + string(bytes.Repeat([]byte("f"), 64))} {
		response := httptest.NewRecorder()
		mirror.ServeHTTP(response, httptest.NewRequest("GET", path, nil))
		if response.Code != http.StatusNotFound {
			t.Fatal("served an unadmitted repository, tag or unavailable blob")
		}
	}
}

func TestMirrorRejectsChangedContentBeforeServing(t *testing.T) {
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte("original")))
	pins := []pin{{Canonical: "node@sha256:" + hash}, {Canonical: "alpine@sha256:" + hash}}
	for _, named := range []map[string][]byte{
		{"blobs/sha256/" + hash: []byte("substituted")},
		{"blobs/sha256/../../escape": []byte("bad path")},
		{},
	} {
		if _, err := load(bytes.NewReader(archive(t, named)), pins); err == nil {
			t.Fatal("unverified image archive accepted")
		}
	}
}

func TestMirrorServesOnlyReferrersPresentInTheVerifiedArchive(t *testing.T) {
	manifest := []byte(`{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json"}`)
	hash := fmt.Sprintf("%x", sha256.Sum256(manifest))
	referrer := []byte(fmt.Sprintf(`{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json","artifactType":"application/vnd.test","subject":{"digest":"sha256:%s"}}`, hash))
	refHash := fmt.Sprintf("%x", sha256.Sum256(referrer))
	mirror, err := load(bytes.NewReader(archive(t, map[string][]byte{"blobs/sha256/" + hash: manifest, "blobs/sha256/" + refHash: referrer})), []pin{{Canonical: "node@sha256:" + hash}})
	if err != nil {
		t.Fatal(err)
	}
	for digest, count := range map[string]int{hash: 1, refHash: 0} {
		response := httptest.NewRecorder()
		mirror.ServeHTTP(response, httptest.NewRequest("GET", "/v2/library/node/referrers/sha256:"+digest, nil))
		var index struct {
			Manifests []struct {
				Digest string `json:"digest"`
			} `json:"manifests"`
		}
		if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &index) != nil || len(index.Manifests) != count || (count == 1 && index.Manifests[0].Digest != "sha256:"+refHash) {
			t.Fatal("offline referrers differ", response)
		}
	}
	for _, repo := range []string{"library/node", "foreign"} {
		response := httptest.NewRecorder()
		mirror.ServeHTTP(response, httptest.NewRequest("GET", "/v2/"+repo+"/referrers/sha256:"+fmt.Sprintf("%064x", 0), nil))
		if response.Code != 404 {
			t.Fatal("served unadmitted or absent referrers")
		}
	}
}
