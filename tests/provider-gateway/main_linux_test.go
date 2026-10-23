//go:build linux

package main

import (
	"crypto/rand"
	"encoding/hex"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestGatewayForwardsBoundedBodyAndClosesConnection(t *testing.T) {
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		t.Fatal(err)
	}
	// An owned abstract Unix socket avoids filesystem path-length limits under
	// the deliberately long artifact-backed temporary directory. No provider
	// or external network is involved in this protocol unit test.
	socket := "\x00factorybox-gateway-test-" + hex.EncodeToString(nonce[:])
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	type receipt struct {
		body   string
		header http.Header
		length int64
	}
	received := make(chan receipt, 1)
	upstream := &http.Server{Handler: http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		data, _ := io.ReadAll(request.Body)
		received <- receipt{string(data), request.Header.Clone(), request.ContentLength}
		writer.Header().Set("Content-Type", "application/json")
		io.WriteString(writer, `{"synthetic":true}`)
	})}
	go upstream.Serve(listener)
	defer upstream.Close()
	front := httptest.NewServer(handler(socket))
	defer front.Close()
	body := `{"model":"unit-test-free","messages":[{"role":"user","content":"synthetic"}]}`
	request, err := http.NewRequest("POST", front.URL+"/v1/chat/completions", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.ContentLength = -1
	request.TransferEncoding = []string{"chunked"}
	request.Header.Set("User-Agent", "synthetic-unit-client")
	request.Header.Set("Authorization", "Bearer public")
	response, err := front.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	actual, err := io.ReadAll(response.Body)
	if err != nil || response.StatusCode != 200 || string(actual) != `{"synthetic":true}` || !response.Close {
		t.Fatalf("gateway response changed or retained a connection: %q: %v: %+v", actual, err, response)
	}
	select {
	case value := <-received:
		if value.body != body || value.length != int64(len(body)) || value.header.Get("User-Agent") != "synthetic-unit-client" || value.header.Get("Authorization") != "Bearer public" {
			t.Fatalf("synthetic request was changed: %+v", value)
		}
	case <-time.After(time.Second):
		t.Fatal("owned test relay received no request")
	}
}

func TestGatewayRejectsPathsAndOversizeBeforeDial(t *testing.T) {
	for _, test := range []struct{ method, path, body string }{
		{"GET", "/v1/chat/completions", ""},
		{"POST", "/other", "{}"},
		{"POST", "/v1/chat/completions?other=1", "{}"},
		{"POST", "/v1/chat/completions", ""},
		{"POST", "/v1/chat/completions", strings.Repeat("x", bodyLimit+1)},
	} {
		t.Run(test.method+test.path+"/"+strconv.Itoa(len(test.body)), func(t *testing.T) {
			request := httptest.NewRequest(test.method, test.path, strings.NewReader(test.body))
			writer := httptest.NewRecorder()
			handler("/missing-unit-socket").ServeHTTP(writer, request)
			if writer.Code != 400 {
				t.Fatalf("rejected request reached the dial path: HTTP %d", writer.Code)
			}
		})
	}
}

func TestGatewayObservationUsesActualBirthExecutableAndNamespace(t *testing.T) {
	value, err := observe(os.Getpid(), strings.Repeat("a", 32))
	if err != nil {
		t.Fatal(err)
	}
	expected, err := fileHash("/proc/self/exe")
	if err != nil || value.Executable != expected || value.Birth == "" || value.PID != os.Getpid() || !strings.HasPrefix(value.Network, "net:[") {
		t.Fatalf("gateway identity did not reflect the real process: %+v: %v", value, err)
	}
}

func TestEffectEndpointCannotAdmitInferenceOrEscapeItsTwoPaths(t *testing.T) {
	for _, test := range []struct{ method, path string }{
		{"POST", "/v1/chat/completions"}, {"GET", "/fixture/effects/status"},
		{"POST", "/fixture/effects/status?other=1"}, {"POST", "/fixture/effects/other"},
	} {
		request := httptest.NewRequest(test.method, test.path, strings.NewReader(`{"operation":"synthetic"}`))
		writer := httptest.NewRecorder()
		endpointHandler("/missing-unit-socket", true).ServeHTTP(writer, request)
		if writer.Code != 400 {
			t.Fatalf("effect endpoint admitted an unrelated path: %s %s: %d", test.method, test.path, writer.Code)
		}
	}
	for _, path := range []string{"/fixture/effects/apply", "/fixture/effects/status"} {
		writer := httptest.NewRecorder()
		handler("/missing-unit-socket").ServeHTTP(writer, httptest.NewRequest("POST", path, strings.NewReader("{}")))
		if writer.Code != 400 {
			t.Fatal("ordinary inference endpoint admitted an effect", path, writer.Code)
		}
	}
}
