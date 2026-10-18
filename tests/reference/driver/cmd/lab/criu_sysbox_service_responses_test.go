package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
)

func TestDirectOpenCodeRejectsDifferentRetainedState(t *testing.T) {
	for _, change := range []string{"none", "pid", "session", "active", "missing-active"} {
		t.Run(change, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				user, password, ok := r.BasicAuth()
				if !ok || user != "opencode" || password != "local-experiment-only" {
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/api/info":
					pid := 73
					if change == "pid" {
						pid = 74
					}
					fmt.Fprintf(w, "{\"version\":\"2.0.27\",\"pid\":%d}", pid)
				case "/api/session/saved-session":
					id := "saved-session"
					if change == "session" {
						id = "restarted-session"
					}
					fmt.Fprintf(w, "{\"data\":{\"id\":%q,\"title\":\"captured-before-snapshot\"}}", id)
				case "/api/session/active":
					switch change {
					case "active":
						fmt.Fprint(w, "{\"data\":{\"saved-session\":\"running\"}}")
					case "missing-active":
						fmt.Fprint(w, "{}")
					default:
						fmt.Fprint(w, "{\"data\":{}}")
					}
				default:
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer server.Close()
			expected := criuServiceExpected{OpenCodeServer: map[string]any{"version": "2.0.27", "pid": float64(73)}, OpenCodeSessionID: "saved-session"}
			_, err := criuServiceOpenCode(context.Background(), server.Client(), server.URL, expected)
			if (err == nil) != (change == "none") {
				t.Fatalf("HTTP 200 is insufficient for retained state: change=%s err=%v", change, err)
			}
		})
	}
}

func TestDirectServiceRejectsHTMLHealthShell(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, "<html>running</html>")
	}))
	defer server.Close()
	var state map[string]any
	err := criuServiceJSON(context.Background(), server.Client(), server.URL, false, &state)
	if err == nil || !strings.Contains(err.Error(), "not JSON") {
		t.Fatalf("HTML shell counted as service response: %v", err)
	}
}

func TestDirectDockerRejectsAnotherDaemon(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, "{\"ID\":\"another-daemon\",\"Driver\":\"vfs\"}")
	}))
	defer server.Close()
	transport := server.Client().Transport
	client := &http.Client{Transport: roundTripRewrite{transport, server.URL}}
	_, err := criuServiceDocker(context.Background(), client, server.Client(), criuServiceExpected{DockerID: "saved-private-daemon"})
	if err == nil || !strings.Contains(err.Error(), "identity/driver changed") {
		t.Fatalf("another daemon counted as restored private Docker: %v", err)
	}
}

func TestDirectBrowserPreservesOriginChecksAndSavedRAM(t *testing.T) {
	for _, change := range []string{"none", "nonce", "error", "exception"} {
		t.Run(change, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if _, present := r.Header["Origin"]; present {
					t.Error("native debugger client sent Origin; Chromium would reject it")
					w.WriteHeader(http.StatusForbidden)
					return
				}
				conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
				if err != nil {
					t.Error(err)
					return
				}
				defer conn.Close()
				var request map[string]any
				if err := conn.ReadJSON(&request); err != nil {
					t.Error(err)
					return
				}
				params, _ := request["params"].(map[string]any)
				if request["method"] != "Runtime.evaluate" || params["expression"] != "globalThis.runtimeForkNonce" {
					t.Errorf("RAM check sent another request: %v", request)
				}
				value := "saved-nonce"
				if change == "nonce" {
					value = "restarted-browser"
				}
				result := map[string]any{"result": map[string]any{"value": value}}
				response := map[string]any{"id": 1, "result": result}
				if change == "error" {
					response["error"] = map[string]any{"message": "context lost"}
				}
				if change == "exception" {
					result["exceptionDetails"] = map[string]any{"text": "context lost"}
				}
				if err := conn.WriteJSON(response); err != nil {
					t.Error(err)
				}
			}))
			defer server.Close()
			_, err := criuServiceBrowserRAM(context.Background(), "ws"+strings.TrimPrefix(server.URL, "http"), "saved-nonce")
			if (err == nil) != (change == "none") {
				t.Fatalf("debugger connection is insufficient for saved RAM: change=%s err=%v", change, err)
			}
		})
	}
}

type roundTripRewrite struct {
	transport http.RoundTripper
	base      string
}

func (r roundTripRewrite) RoundTrip(request *http.Request) (*http.Response, error) {
	base, err := url.Parse(r.base)
	if err != nil {
		return nil, err
	}
	replacement := request.Clone(request.Context())
	replacement.URL.Scheme, replacement.URL.Host = base.Scheme, base.Host
	return r.transport.RoundTrip(replacement)
}

func TestFreshDockerExecRequiresCommandOutputAndSelectedContainer(t *testing.T) {
	for _, change := range []string{"none", "marker", "stderr", "truncated", "stream", "running", "exit", "missing-exit", "container"} {
		t.Run(change, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/containers/saved-container/exec":
					var input struct {
						AttachStdout, AttachStderr, Tty, Privileged bool
						Cmd                                         []string
					}
					if r.Method != http.MethodPost || json.NewDecoder(r.Body).Decode(&input) != nil ||
						!input.AttachStdout || !input.AttachStderr || input.Tty || input.Privileged ||
						len(input.Cmd) != 2 || input.Cmd[0] != "/bin/cat" || input.Cmd[1] != "/workspace/marker.txt" {
						t.Error("exec request did not execute saved-state read")
					}
					w.WriteHeader(http.StatusCreated)
					fmt.Fprint(w, `{"Id":"fresh-exec"}`)
				case "/exec/fresh-exec/start":
					if r.Method != http.MethodPost {
						t.Error("fresh exec was not started")
					}
					payload := []byte("saved-marker\n")
					if change == "marker" {
						payload = []byte("recreated-state\n")
					}
					var header [8]byte
					header[0] = 1
					if change == "stderr" {
						header[0] = 2
					}
					if change == "stream" {
						header[0] = 3
					}
					binary.BigEndian.PutUint32(header[4:], uint32(len(payload)))
					w.Write(header[:])
					if change == "truncated" {
						payload = payload[:2]
					}
					w.Write(payload)
				case "/exec/fresh-exec/json":
					state := map[string]any{"ContainerID": "saved-container", "Running": false, "ExitCode": 0}
					switch change {
					case "running":
						state["Running"] = true
					case "exit":
						state["ExitCode"] = 1
					case "missing-exit":
						delete(state, "ExitCode")
					case "container":
						state["ContainerID"] = "other-container"
					}
					json.NewEncoder(w).Encode(state)
				default:
					t.Errorf("unexpected exec endpoint %s", r.URL)
				}
			}))
			defer server.Close()
			client := &http.Client{Transport: roundTripRewrite{server.Client().Transport, server.URL}}
			_, err := criuServiceDockerExec(context.Background(), client, "saved-container", "saved-marker\n")
			if (err == nil) != (change == "none") {
				t.Fatalf("fresh command state not checked: change=%s err=%v", change, err)
			}
			if change == "none" && calls != 3 {
				t.Fatalf("fresh command was not created, started and inspected: %d calls", calls)
			}
		})
	}
}

func TestDockerExecStreamRejectsTruncatedHeaderAndOversizedFrames(t *testing.T) {
	for _, data := range [][]byte{{1, 0}, {1, 0, 0, 0, 0, 0, 16, 1}, {1, 1, 0, 0, 0, 0, 0, 0}} {
		if _, _, err := criuDockerExecStreams(bytes.NewReader(data)); err == nil {
			t.Fatalf("invalid Docker stream accepted: %v", data)
		}
	}
}
