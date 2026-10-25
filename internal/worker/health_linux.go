//go:build linux

package worker

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"time"
)

func validateHealthListen(address string) error {
	if address == "" {
		return nil
	}
	host, port, err := net.SplitHostPort(address)
	number, portErr := strconv.Atoi(port)
	if err != nil || net.ParseIP(host) == nil || portErr != nil || number < 1 || number > 65535 {
		return fmt.Errorf("health listener requires a numeric IP and explicit port from 1 to 65535")
	}
	return nil
}

func healthHandler(verify func() error) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if r.URL.Path != "/readyz" || r.URL.RawQuery != "" {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if err := verify(); err != nil {
			// Do not expose process identities, paths, epochs or service errors.
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

type noHealth struct{}

func (noHealth) Close() error { return nil }

func startHealth(address string, verify func() error) (io.Closer, error) {
	if err := validateHealthListen(address); err != nil {
		return nil, err
	}
	if address == "" {
		return noHealth{}, nil
	}
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return nil, fmt.Errorf("worker health listener: %w", err)
	}
	server := &http.Server{
		Handler: healthHandler(verify), ReadHeaderTimeout: 2 * time.Second,
		ReadTimeout: 2 * time.Second, WriteTimeout: 5 * time.Second,
		IdleTimeout: 5 * time.Second, MaxHeaderBytes: 4096,
	}
	go func() { _ = server.Serve(listener) }()
	return server, nil
}
