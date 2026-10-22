//go:build linux

package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/websocket"
)

// Use real websocket frames through the unchanged authenticated control path.
// A terminal stream error must reach the HTTP caller, rather than waiting for
// its deadline and inflating the first useful-work measurement by ten seconds.
func TestProxyProtocolTerminalFrames(t *testing.T) {
	for _, terminal := range []string{"stream-error", "error", "stream-end"} {
		t.Run(terminal, func(t *testing.T) {
			a := &authority{nonce: "synthetic", parentReady: true, pending: make(map[string]chan message)}
			ready := make(chan struct{})
			server := httptest.NewServer(websocket.Server{
				Handshake: a.handshake,
				Handler: func(connection *websocket.Conn) {
					a.mu.Lock()
					a.child = connection
					a.mu.Unlock()
					close(ready)
					a.control(connection)
				},
			})
			defer server.Close()
			config, err := websocket.NewConfig("ws"+strings.TrimPrefix(server.URL, "http"), server.URL)
			if err != nil {
				t.Fatal(err)
			}
			config.Protocol = []string{protocol}
			config.Header.Set("X-Factory-Connector-Role", "child")
			config.Header.Set("Authorization", "Bearer fixture-child-synthetic")
			config.Header.Set("X-Factory-Runtime-Attempt", "factorybox-example")
			config.Header.Set("X-Factory-Runtime-Generation", "1")
			peer, err := websocket.DialConfig(config)
			if err != nil {
				t.Fatal(err)
			}
			defer peer.Close()
			select {
			case <-ready:
			case <-time.After(time.Second):
				t.Fatal("owned test control peer did not connect")
			}
			sent := make(chan error, 1)
			go func() {
				var request message
				if err := websocket.JSON.Receive(peer, &request); err != nil {
					sent <- err
					return
				}
				if request.Type != "http-request" || request.Channel != "app" || request.StreamID == "" {
					sent <- fmt.Errorf("proxy omitted the original stream request: %+v", request)
					return
				}
				frames := []message{{Type: terminal, StreamID: request.StreamID, Error: "app port not ready"}}
				if terminal == "stream-end" {
					frames = []message{
						{Type: "http-response", StreamID: request.StreamID, Status: 200},
						{Type: "stream-data", StreamID: request.StreamID, Data: base64.StdEncoding.EncodeToString([]byte(`{"ready":true}`))},
						{Type: "stream-end", StreamID: request.StreamID},
					}
				}
				for _, frame := range frames {
					if err := websocket.JSON.Send(peer, frame); err != nil {
						sent <- err
						return
					}
				}
				sent <- nil
			}()
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			body, err := a.proxy(ctx, httptest.NewRequest("GET", "/app", nil))
			if terminal == "stream-end" {
				if err != nil || string(body) != `{"ready":true}` {
					t.Fatalf("successful original stream changed: %s: %v", body, err)
				}
			} else if err == nil || err.Error() != "Connector stream: app port not ready" {
				t.Fatalf("terminal %s was ignored or changed: body=%s err=%v", terminal, body, err)
			}
			if err := <-sent; err != nil {
				t.Fatal(err)
			}
		})
	}
}
