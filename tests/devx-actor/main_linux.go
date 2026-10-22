//go:build linux

// A closed, test-only Factory Connector authority inside the captured sandbox.
// It exercises the real protocol, not real Factory authority handoff/reconnect.
package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/websocket"
)

const protocol = "factory-runtime-connector-v1"

type message struct {
	Type       string            `json:"type"`
	Role       string            `json:"role,omitempty"`
	Attempt    string            `json:"runtimeAttemptId,omitempty"`
	Generation uint64            `json:"generation,omitempty"`
	Phase      string            `json:"phase,omitempty"`
	Channels   []map[string]any  `json:"channels,omitempty"`
	StreamID   string            `json:"streamId,omitempty"`
	Channel    string            `json:"channel,omitempty"`
	Method     string            `json:"method,omitempty"`
	Path       string            `json:"path,omitempty"`
	Headers    map[string]string `json:"headers,omitempty"`
	Status     int               `json:"status,omitempty"`
	Data       string            `json:"data,omitempty"`
	Error      string            `json:"error,omitempty"`
}

type authority struct {
	mu          sync.Mutex
	write       sync.Mutex
	nonce       string
	parentReady bool
	child       *websocket.Conn
	channels    []map[string]any
	pending     map[string]chan message
	failure     string
}

func (authority *authority) handshake(config *websocket.Config, request *http.Request) error {
	role := request.Header.Get("X-Factory-Connector-Role")
	if (role != "parent" && role != "child") ||
		request.Header.Get("Authorization") != "Bearer fixture-"+role+"-"+authority.nonce ||
		request.Header.Get("X-Factory-Runtime-Attempt") != "factorybox-example" ||
		request.Header.Get("X-Factory-Runtime-Generation") != "1" ||
		!strings.Contains(request.Header.Get("Sec-WebSocket-Protocol"), protocol) {
		return fmt.Errorf("fixture Connector identity rejected")
	}
	config.Protocol = []string{protocol}
	return nil
}

func (authority *authority) control(connection *websocket.Conn) {
	defer connection.Close()
	role := connection.Request().Header.Get("X-Factory-Connector-Role")
	for {
		var event message
		if err := websocket.JSON.Receive(connection, &event); err != nil {
			authority.mu.Lock()
			if authority.child == connection {
				authority.child = nil
			}
			authority.mu.Unlock()
			return
		}
		authority.mu.Lock()
		if event.Type == "hello" {
			if event.Role != role || event.Attempt != "factorybox-example" || event.Generation != 1 {
				authority.failure = "Connector hello differs from authenticated identity"
			} else if role == "child" {
				authority.child, authority.channels = connection, event.Channels
			}
		}
		if role == "parent" && event.Type == "status" && event.Phase == "child-started" {
			authority.parentReady = true
		}
		if event.Type == "status" && event.Phase == "failed" {
			authority.failure = role + ": " + event.Error
		}
		pending := authority.pending[event.StreamID]
		authority.mu.Unlock()
		if role == "child" && pending != nil {
			select {
			case pending <- event:
			case <-time.After(5 * time.Second):
				return
			}
		}
	}
}

func (authority *authority) proxy(ctx context.Context, request *http.Request) (json.RawMessage, error) {
	authority.mu.Lock()
	child := authority.child
	ready, failure := authority.parentReady, authority.failure
	stream := rand.Text()
	pending := make(chan message, 16)
	authority.pending[stream] = pending
	authority.mu.Unlock()
	defer func() {
		authority.mu.Lock()
		delete(authority.pending, stream)
		authority.mu.Unlock()
	}()
	if child == nil || !ready || failure != "" {
		return nil, fmt.Errorf("Connector not ready: %s", failure)
	}
	body, err := io.ReadAll(io.LimitReader(request.Body, 1025))
	if err != nil || len(body) > 1024 {
		return nil, fmt.Errorf("invalid app request body")
	}
	authority.write.Lock()
	err = websocket.JSON.Send(child, message{
		Type: "http-request", StreamID: stream, Channel: "app", Method: request.Method,
		Path: "/state", Headers: map[string]string{"content-type": "application/json"},
		Data: base64.StdEncoding.EncodeToString(body),
	})
	authority.write.Unlock()
	if err != nil {
		return nil, err
	}
	var response []byte
	status := 0
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case event := <-pending:
			switch event.Type {
			case "http-response":
				status = event.Status
			case "stream-data":
				data, err := base64.StdEncoding.DecodeString(event.Data)
				if err != nil || len(response)+len(data) > 64<<10 {
					return nil, fmt.Errorf("invalid or oversized Connector response")
				}
				response = append(response, data...)
			case "stream-end":
				if status != http.StatusOK || !json.Valid(response) {
					return nil, fmt.Errorf("app status %d: %s", status, response)
				}
				return response, nil
			case "error", "stream-error":
				return nil, fmt.Errorf("Connector stream: %s", event.Error)
			}
		}
	}
}

func (authority *authority) observe(response http.ResponseWriter, request *http.Request) {
	if request.URL.Path != "/app" || (request.Method != "GET" && request.Method != "POST") {
		http.NotFound(response, request)
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), 10*time.Second)
	defer cancel()
	app, err := authority.proxy(ctx, request)
	if err != nil {
		http.Error(response, err.Error(), http.StatusServiceUnavailable)
		return
	}
	authority.mu.Lock()
	defer authority.mu.Unlock()
	response.Header().Set("Content-Type", "application/json")
	json.NewEncoder(response).Encode(map[string]any{
		"authority_nonce": authority.nonce, "parent_ready": authority.parentReady,
		"channels": authority.channels, "app": app,
	})
}

func main() {
	if os.Getenv("FACTORYBOX_DEVX_SANDBOX") != "1" {
		log.Fatal("requires the owned native developer sandbox")
	}
	authority := &authority{nonce: rand.Text(), pending: make(map[string]chan message)}
	mux := http.NewServeMux()
	mux.Handle("/control", websocket.Server{
		Handshake: authority.handshake, Handler: authority.control,
	})
	listener, err := net.Listen("tcp4", "0.0.0.0:4700")
	if err != nil {
		log.Fatal(err)
	}
	go func() { log.Fatal(http.Serve(listener, mux)) }()
	observer, err := net.Listen("unix", "/run/factorybox-devx.sock")
	if err != nil {
		log.Fatal(err)
	}
	if err := os.Chmod("/run/factorybox-devx.sock", 0600); err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile("/run/fixture-child-token", []byte("fixture-child-"+authority.nonce), 0600); err != nil {
		log.Fatal(err)
	}
	parent := exec.Command("/usr/local/bin/factory-runtime-connector", "parent",
		"--assignment", "/opt/factorybox/example/assignment.json",
		"--child-token-file", "/run/fixture-child-token")
	parent.Env = append(os.Environ(), "FACTORY_CONNECTOR_TOKEN=fixture-parent-"+authority.nonce)
	output, err := os.OpenFile("/var/log/factorybox-connector.log", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		log.Fatal(err)
	}
	parent.Stdout, parent.Stderr = output, output
	if err := parent.Start(); err != nil {
		log.Fatal(err)
	}
	go func() {
		err := parent.Wait()
		data, readErr := os.ReadFile("/var/log/factorybox-connector.log")
		if readErr != nil {
			data = []byte("parent log unavailable: " + readErr.Error())
		}
		if len(data) > 8192 {
			data = data[len(data)-8192:]
		}
		authority.mu.Lock()
		authority.failure = "Connector parent exited: " + fmt.Sprint(err) + ": " + string(data)
		authority.mu.Unlock()
		log.Print("Connector parent exited; pid=", strconv.Itoa(parent.Process.Pid))
	}()
	log.Fatal(http.Serve(observer, http.HandlerFunc(authority.observe)))
}
