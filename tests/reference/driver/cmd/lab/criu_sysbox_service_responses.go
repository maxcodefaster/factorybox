package main

import (
	"archive/tar"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

type criuServiceExpected struct {
	PID               int
	DockerID          string
	DevContainerID    string
	DevMarker         string
	AppState          map[string]any
	BrowserNonce      string
	OpenCodeServer    map[string]any
	OpenCodeSessionID string
}

var criuServiceDockerID string

// This helper joins only the restored root's network namespace. It stays
// outside captured cgroups and uses the private daemon's actual socket.
func criuSysboxServiceResponses(id, phase string, operationStart time.Time) (resultErr error) {
	if os.Getenv("CRIU_SYSBOX_TOOLS") != "1" {
		return nil
	}
	finishObservation, err := criuBeginServiceObservation(id, phase)
	if err != nil {
		record("sysbox-lazy-observe-start-"+id+"-"+phase, time.Now(), id+"/"+phase, err)
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, finishObservation()) }()
	stateFile := filepath.Join(criuRoot, "runtime", id, "state.json")
	data, err := os.ReadFile(stateFile)
	var state map[string]any
	if err == nil {
		err = json.Unmarshal(data, &state)
	}
	pid, _ := state["init_process_pid"].(float64)
	if err != nil || pid <= 0 {
		return fmt.Errorf("restored runtime PID unavailable: %s: %v", stateFile, err)
	}
	expected := criuServiceExpected{
		PID: int(pid), DockerID: criuServiceDockerID,
		DevContainerID: criuComposeContainerID, DevMarker: criuComposeMarker,
		AppState: criuComposeBefore, BrowserNonce: criuToolsBrowserNonce,
		OpenCodeServer: criuToolsServer, OpenCodeSessionID: criuToolsSessionID,
	}
	out, err := criuCommand(40*time.Second, "nsenter", "--net=/proc/"+strconv.Itoa(expected.PID)+"/ns/net",
		"--", "/lab", "sysbox-service-responses", asJSON(expected))
	if err == nil && criuServiceDockerID == "" {
		var result map[string]map[string]any
		err = json.Unmarshal([]byte(out), &result)
		criuServiceDockerID, _ = result["docker-devcontainer-app"]["docker_id"].(string)
		if err == nil && criuServiceDockerID == "" {
			err = fmt.Errorf("private Docker response did not return daemon identity")
		}
	}
	record("sysbox-"+id+"-"+phase+"-direct-service-responses", operationStart,
		out+"; scope: restore/clone plus direct Docker, Dev Container/app, retained browser RAM and idle OpenCode responses; Theia native file-service editing and fresh Docker lifecycle are checked separately", err)
	if err != nil {
		criuSysboxFailedServiceThreads(id, phase, expected)
		var result map[string]map[string]any
		if json.Unmarshal([]byte(strings.Split(out, "\n")[0]), &result) == nil && result["docker-devcontainer-app"]["error"] != nil {
			start := time.Now()
			diagnostic, diagnosticErr := criuCommand(10*time.Second, criuRuntime, "--root", criuRoot+"/runtime", "exec", id, "docker", "info", "--format", "{{json .}}")
			record("sysbox-"+id+"-"+phase+"-native-docker-response-diagnostic", start, diagnostic, diagnosticErr)
			// Compare the exact same HTTP client inside the sandbox. This is
			// failure diagnosis only; it is never a successful latency sample.
			expected.PID = 1
			start = time.Now()
			diagnostic, diagnosticErr = criuCommand(40*time.Second, criuRuntime, "--root", criuRoot+"/runtime", "exec", id, "/lab", "sysbox-service-responses", asJSON(expected))
			record("sysbox-"+id+"-"+phase+"-in-sandbox-service-response-diagnostic", start, diagnostic, diagnosticErr)
		}
	}
	if err == nil {
		err = criuChildDemandProof(id, phase)
	}
	return err
}

func criuServiceJSON(ctx context.Context, client *http.Client, address string, auth bool, dst any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return err
	}
	if auth {
		req.SetBasicAuth("opencode", "local-experiment-only")
	}
	var connected, written, firstByte atomic.Bool
	req = req.WithContext(httptrace.WithClientTrace(req.Context(), &httptrace.ClientTrace{
		ConnectDone:          func(_, _ string, err error) { connected.Store(err == nil) },
		GotConn:              func(httptrace.GotConnInfo) { connected.Store(true) },
		WroteRequest:         func(info httptrace.WroteRequestInfo) { written.Store(info.Err == nil) },
		GotFirstResponseByte: func() { firstByte.Store(true) },
	}))
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("%w; connected=%t request_written=%t first_response_byte=%t", err, connected.Load(), written.Load(), firstByte.Load())
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: HTTP %d", address, resp.StatusCode)
	}
	if !strings.HasPrefix(resp.Header.Get("Content-Type"), "application/json") {
		return fmt.Errorf("%s: response is not JSON", address)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(dst)
}

func criuServiceDocker(ctx context.Context, client, networkClient *http.Client, expected criuServiceExpected) (map[string]any, error) {
	var info map[string]any
	if err := criuServiceJSON(ctx, client, "http://private-docker/info", false, &info); err != nil {
		return nil, err
	}
	dockerID, _ := info["ID"].(string)
	if dockerID == "" || info["Driver"] != "vfs" || (expected.DockerID != "" && dockerID != expected.DockerID) {
		return nil, fmt.Errorf("private Docker identity/driver changed: %v", info)
	}
	var container struct {
		ID              string
		State           struct{ Running bool }
		NetworkSettings struct {
			Networks map[string]struct{ IPAddress string }
		}
	}
	if err := criuServiceJSON(ctx, client, "http://private-docker/containers/"+url.PathEscape(expected.DevContainerID)+"/json", false, &container); err != nil {
		return nil, err
	}
	if container.ID != expected.DevContainerID || !container.State.Running {
		return nil, fmt.Errorf("saved Dev Container is not running: %v", container)
	}
	appIP := ""
	for _, network := range container.NetworkSettings.Networks {
		if net.ParseIP(network.IPAddress) != nil {
			appIP = network.IPAddress
			break
		}
	}
	if appIP == "" {
		return nil, fmt.Errorf("saved Dev Container has no bridge address")
	}
	var app map[string]any
	if err := criuServiceJSON(ctx, networkClient, "http://"+net.JoinHostPort(appIP, "8080")+"/state", false, &app); err != nil {
		return nil, err
	}
	for _, field := range []string{"nonce", "value", "pid", "hostname", "branch", "pids_limit"} {
		if app[field] == nil || app[field] != expected.AppState[field] {
			return nil, fmt.Errorf("Dev Container RAM field %s changed: %v", field, app)
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"http://private-docker/containers/"+url.PathEscape(expected.DevContainerID)+"/archive?path="+url.QueryEscape("/workspace/marker.txt"), nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Dev Container marker archive: HTTP %d", resp.StatusCode)
	}
	reader := tar.NewReader(io.LimitReader(resp.Body, 1<<20))
	header, err := reader.Next()
	if err != nil || header.Typeflag != tar.TypeReg || filepath.Base(header.Name) != "marker.txt" || header.Size > 4096 {
		return nil, fmt.Errorf("Dev Container marker archive differs: %v %v", header, err)
	}
	marker, err := io.ReadAll(reader)
	if err != nil || string(marker) != expected.DevMarker {
		return nil, fmt.Errorf("Dev Container file state changed: %q %v", marker, err)
	}
	execution, err := criuServiceDockerExec(ctx, client, expected.DevContainerID, expected.DevMarker)
	if err != nil {
		return nil, err
	}
	return map[string]any{"docker_id": dockerID, "dev_container_id": container.ID, "app": app, "marker": string(marker), "fresh_exec": execution}, nil
}

// An inspect response can come from saved daemon metadata. A fresh command
// must execute inside the retained Dev Container and return its saved file.
func criuServiceDockerExec(ctx context.Context, client *http.Client, containerID, marker string) (map[string]any, error) {
	post := func(path string, payload any) (*http.Response, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://private-docker"+path, strings.NewReader(asJSON(payload)))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		return client.Do(req)
	}
	response, err := post("/containers/"+url.PathEscape(containerID)+"/exec", map[string]any{
		"AttachStdout": true, "AttachStderr": true, "Tty": false, "Privileged": false, "Cmd": []string{"/bin/cat", "/workspace/marker.txt"},
	})
	if err != nil {
		return nil, err
	}
	var created struct{ ID string }
	err = json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&created)
	response.Body.Close()
	if response.StatusCode != http.StatusCreated || err != nil || created.ID == "" {
		return nil, fmt.Errorf("fresh Docker exec create: HTTP %d id=%q error=%v", response.StatusCode, created.ID, err)
	}
	response, err = post("/exec/"+url.PathEscape(created.ID)+"/start", map[string]any{"Detach": false, "Tty": false})
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fresh Docker exec start: HTTP %d", response.StatusCode)
	}
	stdout, stderr, err := criuDockerExecStreams(io.LimitReader(response.Body, 65537))
	if err != nil || stdout != marker || stderr != "" {
		return nil, fmt.Errorf("fresh Docker exec output differs: stdout=%q stderr=%q error=%v", stdout, stderr, err)
	}
	var state struct {
		ContainerID string
		Running     *bool
		ExitCode    *int
	}
	if err := criuServiceJSON(ctx, client, "http://private-docker/exec/"+url.PathEscape(created.ID)+"/json", false, &state); err != nil {
		return nil, err
	}
	if state.ContainerID != containerID || state.Running == nil || *state.Running || state.ExitCode == nil || *state.ExitCode != 0 {
		return nil, fmt.Errorf("fresh Docker exec did not complete in selected Dev Container: %v", state)
	}
	return map[string]any{"id": created.ID, "container_id": state.ContainerID, "stdout": stdout, "exit_code": *state.ExitCode}, nil
}

// Docker non-TTY output uses an eight-byte header and separate stdout/stderr
// frames. Refuse truncation, unknown streams and unbounded payloads.
func criuDockerExecStreams(reader io.Reader) (string, string, error) {
	var stdout, stderr strings.Builder
	for {
		var header [8]byte
		_, err := io.ReadFull(reader, header[:])
		if err == io.EOF {
			return stdout.String(), stderr.String(), nil
		}
		if err != nil {
			return "", "", err
		}
		size := binary.BigEndian.Uint32(header[4:])
		if (header[0] != 1 && header[0] != 2) || header[1] != 0 || header[2] != 0 || header[3] != 0 ||
			size > 4096 || stdout.Len()+stderr.Len()+int(size) > 4096 {
			return "", "", fmt.Errorf("invalid or oversized Docker exec frame")
		}
		payload := make([]byte, int(size))
		if _, err := io.ReadFull(reader, payload); err != nil {
			return "", "", err
		}
		if header[0] == 1 {
			stdout.Write(payload)
		} else {
			stderr.Write(payload)
		}
	}
}

func criuServiceOpenCode(ctx context.Context, client *http.Client, base string, expected criuServiceExpected) (map[string]any, error) {
	var server map[string]any
	if err := criuServiceJSON(ctx, client, base+"/api/info", true, &server); err != nil {
		return nil, err
	}
	if server["version"] != expected.OpenCodeServer["version"] || server["pid"] != expected.OpenCodeServer["pid"] {
		return nil, fmt.Errorf("retained OpenCode process changed: %v", server)
	}
	var session, active struct{ Data map[string]any }
	if err := criuServiceJSON(ctx, client, base+"/api/session/"+url.PathEscape(expected.OpenCodeSessionID), true, &session); err != nil {
		return nil, err
	}
	if session.Data["id"] != expected.OpenCodeSessionID || session.Data["title"] != "captured-before-snapshot" {
		return nil, fmt.Errorf("retained OpenCode session changed: %v", session.Data)
	}
	if err := criuServiceJSON(ctx, client, base+"/api/session/active", true, &active); err != nil {
		return nil, err
	}
	if active.Data == nil || len(active.Data) != 0 {
		return nil, fmt.Errorf("restored idle OpenCode is not idle: %v", active.Data)
	}
	return map[string]any{"server": server, "session": session.Data, "active": active.Data}, nil
}

func criuServiceBrowser(ctx context.Context, client *http.Client, expected string) (map[string]any, error) {
	var pages []struct{ Type, WebSocketDebuggerURL string }
	if err := criuServiceJSON(ctx, client, "http://127.0.0.1:9222/json/list", false, &pages); err != nil {
		return nil, err
	}
	for _, page := range pages {
		if page.Type != "page" {
			continue
		}
		address, err := url.Parse(page.WebSocketDebuggerURL)
		if err != nil || address.Scheme != "ws" || address.Hostname() != "127.0.0.1" || address.Port() != "9222" {
			return nil, fmt.Errorf("unexpected private Chromium debugger address")
		}
		return criuServiceBrowserRAM(ctx, page.WebSocketDebuggerURL, expected)
	}
	return nil, fmt.Errorf("Chromium has no saved page")
}

func criuServiceBrowserRAM(ctx context.Context, address, expected string) (map[string]any, error) {
	// Native CDP clients omit Origin. Keep Chromium's origin checks enabled.
	conn, response, err := (&websocket.Dialer{HandshakeTimeout: 2 * time.Second}).DialContext(ctx, address, nil)
	if response != nil {
		response.Body.Close()
	}
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	deadline := time.Now().Add(2 * time.Second)
	conn.SetWriteDeadline(deadline)
	conn.SetReadDeadline(deadline)
	if err := conn.WriteJSON(map[string]any{"id": 1, "method": "Runtime.evaluate", "params": map[string]any{"expression": "globalThis.runtimeForkNonce", "returnByValue": true}}); err != nil {
		return nil, err
	}
	var result struct {
		ID     int
		Error  any
		Result struct {
			ExceptionDetails any
			Result           struct{ Value string }
		}
	}
	if err := conn.ReadJSON(&result); err != nil {
		return nil, err
	}
	if result.ID != 1 || result.Error != nil || result.Result.ExceptionDetails != nil || result.Result.Result.Value != expected || expected == "" {
		return nil, fmt.Errorf("retained Chromium RAM differs: %v", result)
	}
	return map[string]any{"browser_nonce": result.Result.Result.Value}, nil
}

func criuSysboxServiceResponseHelper() error {
	if len(os.Args) != 3 {
		return fmt.Errorf("expected private service response input")
	}
	var expected criuServiceExpected
	if err := json.Unmarshal([]byte(os.Args[2]), &expected); err != nil {
		return err
	}
	if expected.PID <= 0 || expected.DevContainerID == "" || expected.BrowserNonce == "" || expected.OpenCodeSessionID == "" {
		return fmt.Errorf("incomplete private service response input")
	}
	// This is the correctness observation limit. The performance target
	// remains <1s and the recorder includes all actual waiting time.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	dockerTransport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return criuServiceSocketDial(ctx, "/proc/"+strconv.Itoa(expected.PID)+"/root")
	}}
	defer dockerTransport.CloseIdleConnections()
	dockerClient := &http.Client{Transport: dockerTransport, Timeout: 2 * time.Second}
	networkTransport := &http.Transport{}
	defer networkTransport.CloseIdleConnections()
	networkClient := &http.Client{Transport: networkTransport, Timeout: 2 * time.Second}
	checks := map[string]func() (map[string]any, error){
		"docker-devcontainer-app": func() (map[string]any, error) { return criuServiceDocker(ctx, dockerClient, networkClient, expected) },
		"browser":                 func() (map[string]any, error) { return criuServiceBrowser(ctx, networkClient, expected.BrowserNonce) },
		"opencode": func() (map[string]any, error) {
			return criuServiceOpenCode(ctx, networkClient, "http://127.0.0.1:4096", expected)
		},
	}
	type response struct {
		name  string
		state map[string]any
		err   error
	}
	results := make(chan response, len(checks))
	for name, check := range checks {
		go func(name string, check func() (map[string]any, error)) {
			start := time.Now()
			var state map[string]any
			var err error
			var firstError string
			attempts := 0
			for {
				if ctx.Err() != nil {
					err = ctx.Err()
					break
				}
				attempts++
				state, err = check()
				if err != nil && firstError == "" {
					firstError = err.Error()
				}
				if err == nil || ctx.Err() != nil {
					break
				}
				select {
				case <-ctx.Done():
				case <-time.After(25 * time.Millisecond):
				}
			}
			if state == nil {
				state = map[string]any{}
			}
			state["duration_ms"] = time.Since(start).Milliseconds()
			state["attempts"] = attempts
			if err != nil {
				state["error"] = err.Error()
				state["first_error"] = firstError
			}
			results <- response{name, state, err}
		}(name, check)
	}
	states := map[string]map[string]any{}
	var resultErr error
	for range checks {
		result := <-results
		states[result.name] = result.state
		if result.err != nil {
			resultErr = fmt.Errorf("%s: %w", result.name, result.err)
		}
	}
	fmt.Println(asJSON(states))
	return resultErr
}
