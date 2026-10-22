//go:build linux

package devx_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"time"

	"factorybox/internal/lifecycle"
)

func (f *fixture) harnessReady(phase string, view lifecycle.View) {
	f.t.Helper()
	if os.Getenv("FACTORYBOX_DEVX_OPENCODE") != "1" {
		return
	}
	started := time.Now()
	var anchors []*os.File
	defer func() {
		for _, file := range anchors {
			file.Close()
		}
	}()
	for _, suffix := range []string{"ns/mnt", "root", "ns/pid", "ns/net", "ns/user"} {
		file, err := os.Open(fmt.Sprintf("/proc/%d/%s", view.Runtime.PID, suffix))
		if err != nil {
			f.t.Fatal(err)
		}
		anchors = append(anchors, file)
	}
	var observed map[string]any
	deadline := time.Now().Add(20 * time.Second)
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		command := exec.CommandContext(ctx, "nsenter",
			"--mount=/proc/self/fd/3", "--root=/proc/self/fd/4", "--wd=/proc/self/fd/4",
			"--pid=/proc/self/fd/5", "--net=/proc/self/fd/6", "--user=/proc/self/fd/7",
			"--setuid=0", "--setgid=0", "--",
			"/usr/local/bin/node", "/opt/factorybox/opencode-probe.cjs")
		command.ExtraFiles = anchors
		command.Env = []string{"PATH=/usr/local/bin:/usr/bin:/bin", "HOME=/root"}
		data, err := command.CombinedOutput()
		cancel()
		if err == nil && len(data) <= 16<<10 && json.Unmarshal(data, &observed) == nil {
			break
		}
		if time.Now().After(deadline) {
			f.t.Fatalf("owned optional OpenCode did not serve its authenticated native API: %v: %s", err, data)
		}
		time.Sleep(100 * time.Millisecond)
	}
	if observed["server_version"] != "2.0.27" ||
		observed["unauthorized_status"] != float64(401) || observed["info_status"] != float64(200) ||
		observed["session_status"] != float64(200) {
		f.t.Fatal("OpenCode version or authenticated API differs from the supplied consumer")
	}
	observed["phase"], observed["sandbox"] = phase, view.Record.ID
	observed["runtime_id"] = view.Record.RuntimeID
	observed["probe_seconds"] = time.Since(started).Seconds()
	path := "/output/opencode-observations.json"
	var samples []map[string]any
	if data, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(data, &samples); err != nil {
			f.t.Fatal(err)
		}
	} else if !os.IsNotExist(err) {
		f.t.Fatal(err)
	}
	samples = append(samples, observed)
	data, err := json.MarshalIndent(samples, "", "  ")
	if err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0600); err != nil {
		f.t.Fatal(err)
	}
	f.agentFlowReady(phase, view)
}
