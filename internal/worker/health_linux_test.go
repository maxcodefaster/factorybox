//go:build linux

package worker

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestHealthRequiresLiveVerificationAndDisclosesNoIdentity(t *testing.T) {
	var calls atomic.Int32
	var failed atomic.Bool
	handler := healthHandler(func() error {
		calls.Add(1)
		if failed.Load() {
			return fmt.Errorf("private store/epoch/process identity")
		}
		return nil
	})
	for _, invalid := range []struct {
		method, path string
		status       int
	}{
		{"POST", "/readyz", 405}, {"GET", "/", 404}, {"GET", "/readyz?details=1", 404},
	} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(invalid.method, invalid.path, nil))
		if response.Code != invalid.status || calls.Load() != 0 {
			t.Fatal("invalid request invoked verification", response.Code, calls.Load())
		}
	}
	for _, fail := range []bool{false, true, false} {
		failed.Store(fail)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/readyz", nil))
		expected := http.StatusNoContent
		if fail {
			expected = http.StatusServiceUnavailable
		}
		if response.Code != expected || response.Body.Len() != 0 || response.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("probe accepted stale authority or disclosed private data", response)
		}
	}
	if calls.Load() != 3 {
		t.Fatal("probe cached readiness", calls.Load())
	}
}

func TestHealthListenRequiresExplicitAddress(t *testing.T) {
	for _, value := range []string{"", "127.0.0.1:8080", "0.0.0.0:8080", "[::1]:8080"} {
		if err := validateHealthListen(value); err != nil {
			t.Fatal(value, err)
		}
	}
	for _, value := range []string{":8080", "localhost:8080", "0.0.0.0:0", "127.0.0.1:65536", "127.0.0.1:http"} {
		if err := validateHealthListen(value); err == nil {
			t.Fatal("unsafe listener accepted", value)
		}
	}
	server, err := startHealth("", func() error { t.Fatal("disabled probe verified state"); return nil })
	if err != nil {
		t.Fatal(err)
	}
	if err := server.Close(); err != nil {
		t.Fatal(err)
	}
}
