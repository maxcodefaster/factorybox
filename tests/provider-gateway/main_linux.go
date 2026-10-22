//go:build linux

// Optional native consumer-test gateway. The controller joins only the target
// network namespace; this process stays outside the captured PID/mount tree.
// It must be stopped and its pidfd confirmed exited before any capture.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const readyRoot = "/run/factorybox-consumer-provider"
const address = "127.0.0.1:4097"
const bodyLimit = 2 << 20

var noncePattern = regexp.MustCompile(`^[0-9a-f]{32}$`)
var hashPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

type identity struct {
	Schema     int    `json:"schema"`
	Nonce      string `json:"nonce"`
	PID        int    `json:"pid"`
	Birth      string `json:"birth"`
	Executable string `json:"executable_sha256"`
	Network    string `json:"network_namespace"`
	Address    string `json:"address"`
}

func fileHash(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func birth(pid int) (string, error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return "", err
	}
	end := strings.LastIndexByte(string(data), ')')
	if end < 0 {
		return "", fmt.Errorf("invalid gateway process stat")
	}
	fields := strings.Fields(string(data[end+1:]))
	if len(fields) < 20 || fields[0] == "Z" || fields[0] == "X" {
		return "", fmt.Errorf("gateway is not live")
	}
	return fields[19], nil
}

func observe(pid int, nonce string) (identity, error) {
	value := identity{Schema: 1, Nonce: nonce, PID: pid, Address: address}
	var err error
	value.Birth, err = birth(pid)
	if err == nil {
		value.Executable, err = fileHash(fmt.Sprintf("/proc/%d/exe", pid))
	}
	if err == nil {
		value.Network, err = os.Readlink(fmt.Sprintf("/proc/%d/ns/net", pid))
	}
	return value, err
}

func privateRoot(create bool) error {
	if create {
		if err := os.Mkdir(readyRoot, 0700); err != nil && !errors.Is(err, os.ErrExist) {
			return err
		}
	}
	info, err := os.Lstat(readyRoot)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode().Perm() != 0700 || info.Sys().(*syscall.Stat_t).Uid != 0 {
		return fmt.Errorf("unsafe consumer gateway control directory")
	}
	return nil
}

func publish(value identity) error {
	if err := privateRoot(true); err != nil {
		return err
	}
	path := filepath.Join(readyRoot, value.Nonce+".json")
	temporary := path + ".next"
	file, err := os.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	err = json.NewEncoder(file).Encode(value)
	if err == nil {
		err = file.Sync()
	}
	err = errors.Join(err, file.Close())
	if err != nil {
		return err
	}
	return unix.Renameat2(unix.AT_FDCWD, temporary, unix.AT_FDCWD, path, unix.RENAME_NOREPLACE)
}

func inspect(nonce, expected string, target int) (identity, error) {
	var saved identity
	if err := privateRoot(false); err != nil {
		return saved, err
	}
	file, err := os.OpenFile(filepath.Join(readyRoot, nonce+".json"), os.O_RDONLY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return saved, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return saved, err
	}
	stat := info.Sys().(*syscall.Stat_t)
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || stat.Uid != 0 || stat.Nlink != 1 || info.Size() > 4096 {
		return saved, fmt.Errorf("unsafe gateway identity file")
	}
	decoder := json.NewDecoder(io.LimitReader(file, 4097))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&saved); err != nil {
		return saved, err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return saved, fmt.Errorf("gateway identity contains trailing data")
	}
	if saved.Schema != 1 || saved.Nonce != nonce || saved.PID <= 1 || saved.Executable != expected || saved.Address != address || target <= 1 {
		return saved, fmt.Errorf("gateway identity differs from its exact request")
	}
	actual, err := observe(saved.PID, nonce)
	if err != nil || actual != saved {
		return saved, errors.Join(err, fmt.Errorf("gateway process identity changed"))
	}
	network, err := os.Readlink(fmt.Sprintf("/proc/%d/ns/net", target))
	if err != nil || network != saved.Network {
		return saved, errors.Join(err, fmt.Errorf("gateway joined another network namespace"))
	}
	return saved, nil
}

func stop(value identity) error {
	fd, err := unix.PidfdOpen(value.PID, 0)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	actual, err := observe(value.PID, value.Nonce)
	if err != nil || actual != value {
		return errors.Join(err, fmt.Errorf("refusing to signal a changed gateway identity"))
	}
	if err := unix.PidfdSendSignal(fd, unix.SIGTERM, nil, 0); err != nil {
		return err
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		poll := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
		remaining := max(1, time.Until(deadline).Milliseconds())
		count, err := unix.Poll(poll, int(remaining))
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil {
			return err
		}
		if count == 1 && poll[0].Revents&unix.POLLIN != 0 {
			return nil
		}
	}
	return fmt.Errorf("gateway did not exit within its bounded stop")
}

func handler(socket string) http.Handler {
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", socket)
		},
		DisableKeepAlives: true, ResponseHeaderTimeout: 50 * time.Second,
	}
	proxy := &httputil.ReverseProxy{
		Director: func(request *http.Request) {
			request.URL.Scheme, request.URL.Host, request.Host = "http", "provider.invalid", "provider.invalid"
		},
		Transport: transport,
		ModifyResponse: func(response *http.Response) error {
			response.Close = true
			response.Header.Set("Connection", "close")
			return nil
		},
		ErrorHandler: func(writer http.ResponseWriter, request *http.Request, _ error) {
			http.Error(writer, "bounded consumer provider relay failed", http.StatusBadGateway)
		},
	}
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		request.Close = true
		writer.Header().Set("Connection", "close")
		if request.Method != "POST" || request.URL.Path != "/v1/chat/completions" || request.URL.RawQuery != "" {
			http.Error(writer, "consumer inference path refused", http.StatusBadRequest)
			return
		}
		body, err := io.ReadAll(io.LimitReader(request.Body, bodyLimit+1))
		request.Body.Close()
		if err != nil || len(body) == 0 || len(body) > bodyLimit {
			http.Error(writer, "consumer inference body exceeds its bound", http.StatusBadRequest)
			return
		}
		request.Body = io.NopCloser(bytes.NewReader(body))
		request.ContentLength = int64(len(body))
		request.TransferEncoding = nil
		proxy.ServeHTTP(writer, request)
	})
}

func serve(nonce, expected, socket string, lifetime time.Duration) error {
	if !filepath.IsAbs(socket) || filepath.Clean(socket) != socket {
		return fmt.Errorf("requires a canonical consumer relay socket")
	}
	info, err := os.Lstat(socket)
	if err != nil || info.Mode()&os.ModeSocket == 0 {
		return errors.Join(err, fmt.Errorf("consumer relay socket is missing"))
	}
	value, err := observe(os.Getpid(), nonce)
	if err != nil || value.Executable != expected {
		return errors.Join(err, fmt.Errorf("gateway executable differs from its frozen input"))
	}
	listener, err := net.Listen("tcp4", address)
	if err != nil {
		return err
	}
	defer listener.Close()
	if err := publish(value); err != nil {
		return err
	}
	if err := json.NewEncoder(os.Stdout).Encode(value); err != nil {
		return err
	}
	signals, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()
	ctx, expire := context.WithTimeout(signals, lifetime)
	defer expire()
	server := &http.Server{Handler: handler(socket), ReadHeaderTimeout: 3 * time.Second,
		ReadTimeout: 5 * time.Second, WriteTimeout: 60 * time.Second, IdleTimeout: 2 * time.Second, MaxHeaderBytes: 32 << 10}
	closed := make(chan error, 1)
	go func() {
		<-ctx.Done()
		shutdown, release := context.WithTimeout(context.Background(), 2*time.Second)
		defer release()
		err := server.Shutdown(shutdown)
		if err != nil {
			server.Close()
		}
		closed <- err
	}()
	if err := server.Serve(listener); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	if err := <-closed; err != nil {
		return err
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return fmt.Errorf("consumer gateway exceeded its lifetime")
	}
	return nil
}

func run() error {
	operation := flag.String("operation", "serve", "serve, inspect or stop one exact optional consumer gateway")
	nonce := flag.String("nonce", "", "exact 32-character gateway request nonce")
	expected := flag.String("sha256", "", "exact immutable helper executable SHA-256")
	socket := flag.String("socket", "/external-provider/provider.sock", "owned host relay socket")
	target := flag.Int("namespace-of", 0, "verified live sandbox PID, for inspection and stop")
	expectedPID := flag.Int("expected-pid", 0, "previously verified gateway PID, required for stop")
	expectedBirth := flag.String("expected-birth", "", "previously verified gateway birth, required for stop")
	lifetime := flag.Int("lifetime-seconds", 110, "maximum gateway lifetime, at most 120 seconds")
	flag.Parse()
	if flag.NArg() != 0 || os.Geteuid() != 0 || !noncePattern.MatchString(*nonce) || !hashPattern.MatchString(*expected) || *lifetime < 1 || *lifetime > 120 {
		return fmt.Errorf("requires worker root and exact bounded gateway inputs")
	}
	if *operation == "serve" {
		return serve(*nonce, *expected, *socket, time.Duration(*lifetime)*time.Second)
	}
	if *operation != "inspect" && *operation != "stop" {
		return fmt.Errorf("unknown gateway operation: %s", *operation)
	}
	value, err := inspect(*nonce, *expected, *target)
	if err != nil {
		return err
	}
	if *operation == "stop" {
		if *expectedPID <= 1 || *expectedBirth == "" || value.PID != *expectedPID || value.Birth != *expectedBirth {
			return fmt.Errorf("refusing to signal a gateway other than the attached process")
		}
		if err := stop(value); err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(map[string]any{"nonce": *nonce, "pid": value.PID, "stopped": true})
	}
	return json.NewEncoder(os.Stdout).Encode(value)
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
