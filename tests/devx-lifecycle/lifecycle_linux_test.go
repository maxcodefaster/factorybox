//go:build linux

package devx_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"factorybox/internal/lifecycle"
	"factorybox/internal/sysboxcapture"
	"golang.org/x/sys/unix"
)

type appState struct {
	Instance   string `json:"instance"`
	Counter    int    `json:"counter"`
	Marker     string `json:"marker"`
	Hook       string `json:"hook"`
	Dependency string `json:"dependency"`
}

type observedState struct {
	Authority string           `json:"authority_nonce"`
	Parent    bool             `json:"parent_ready"`
	Channels  []map[string]any `json:"channels"`
	App       appState         `json:"app"`
}

type observation struct {
	Phase           string            `json:"phase"`
	Millis          float64           `json:"milliseconds"`
	State           observedState     `json:"state"`
	WorkerResources map[string]string `json:"worker_resources"`
}

type fixture struct {
	t       *testing.T
	root    string
	index   int
	samples []observation
}

// These counters belong to the complete bounded fixture, including its
// controller and Sysbox services. They are not isolated application charges.
func (fixture *fixture) resources() map[string]string {
	values := map[string]string{"sampled_at": time.Now().UTC().Format(time.RFC3339Nano)}
	for _, name := range []string{
		"cpu.stat", "cpu.pressure", "memory.current", "memory.peak",
		"memory.stat", "memory.pressure", "io.stat", "io.pressure",
	} {
		data, err := os.ReadFile("/sys/fs/cgroup/" + name)
		if err != nil {
			values[name] = "unavailable: " + err.Error()
		} else {
			values[name] = strings.TrimSpace(string(data))
		}
	}
	return values
}

func (fixture *fixture) command(args ...string) []byte {
	fixture.t.Helper()
	fixture.index++
	timeout := 90 * time.Second
	if len(args) != 0 && args[0] == "prepare-base" {
		timeout = 180 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	prefix := []string{
		"--root", fixture.root, "--runtime", "/out/sysbox-runc",
		"--runtime-sha256", os.Getenv("FACTORYBOX_RUNTIME_SHA256"),
		"--criu", "/out/criu", "--criu-sha256", os.Getenv("FACTORYBOX_CRIU_SHA256"),
		"--manager-root", "/var/lib/sysbox/manager",
	}
	command := exec.CommandContext(ctx, "/factorybox", append(prefix, args...)...)
	var output, errors bytes.Buffer
	command.Stdout, command.Stderr = &output, &errors
	resourcesBefore := fixture.resources()
	start := time.Now()
	err := command.Run()
	elapsed := time.Since(start)
	resourcesAfter := fixture.resources()
	path := fmt.Sprintf("/output/cli-%02d-%s", fixture.index, args[0])
	if writeErr := os.WriteFile(path+".json", output.Bytes(), 0600); writeErr != nil {
		fixture.t.Fatal(writeErr)
	}
	if writeErr := os.WriteFile(path+".stderr", errors.Bytes(), 0600); writeErr != nil {
		fixture.t.Fatal(writeErr)
	}
	timing, writeErr := json.Marshal(map[string]any{
		"arguments": args, "seconds": elapsed.Seconds(),
		"worker_resources_before": resourcesBefore, "worker_resources_after": resourcesAfter,
	})
	if writeErr != nil {
		fixture.t.Fatal(writeErr)
	}
	if writeErr := os.WriteFile(path+".timing.json", append(timing, '\n'), 0600); writeErr != nil {
		fixture.t.Fatal(writeErr)
	}
	if err != nil {
		text := errors.String()
		if len(text) > 8192 {
			text = text[len(text)-8192:]
		}
		fixture.t.Fatalf("public %v failed: %v: %s", args, err, text)
	}
	return output.Bytes()
}

func (fixture *fixture) view(args ...string) lifecycle.View {
	fixture.t.Helper()
	var view lifecycle.View
	if err := json.Unmarshal(fixture.command(args...), &view); err != nil {
		fixture.t.Fatal(err)
	}
	return view
}

func readApp(pid int, body string) (observedState, error) {
	var observed observedState
	transport := &http.Transport{
		DisableKeepAlives: true,
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", fmt.Sprintf("/proc/%d/root/run/factorybox-devx.sock", pid))
		},
	}
	defer transport.CloseIdleConnections()
	client := http.Client{Transport: transport, Timeout: 12 * time.Second}
	method := http.MethodGet
	if body != "" {
		method = http.MethodPost
	}
	request, err := http.NewRequest(method, "http://fixture/app", strings.NewReader(body))
	if err != nil {
		return observed, err
	}
	response, err := client.Do(request)
	if err != nil {
		return observed, err
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 64<<10))
	if err != nil {
		return observed, err
	}
	if response.StatusCode != http.StatusOK {
		return observed, fmt.Errorf("fixture authority HTTP %d: %s", response.StatusCode, data)
	}
	if err := json.Unmarshal(data, &observed); err != nil {
		return observed, err
	}
	if observed.Authority == "" || !observed.Parent || observed.App.Instance == "" ||
		observed.App.Hook != "ready" || observed.App.Dependency != "ready" ||
		len(observed.Channels) != 2 || observed.Channels[0]["name"] != "app" ||
		observed.Channels[1]["name"] != "ide" {
		return observed, fmt.Errorf("real Connector/Dev Container response is incomplete: %+v", observed)
	}
	return observed, nil
}

func (fixture *fixture) useful(phase string, view lifecycle.View, start time.Time, body string) observedState {
	fixture.t.Helper()
	if view.Runtime == nil || view.Runtime.PID <= 1 || view.Record.Start.CompletedAt == "" {
		fixture.t.Fatal("public operation lacks a completed live identity")
	}
	readiness := 60 * time.Second
	if phase == "prepared-start-first-use" {
		// Cold preparation includes image acquisition and Dev Containers setup.
		// The outer fixture still enforces its 420-second lifecycle deadline.
		readiness = 180 * time.Second
	}
	deadline := time.Now().Add(readiness)
	var state observedState
	var err error
	for {
		state, err = readApp(view.Runtime.PID, body)
		if err == nil {
			break
		}
		// Never replay a mutation after an uncertain response.
		if body != "" || strings.Contains(err.Error(), "Connector parent exited:") || time.Now().After(deadline) {
			fixture.t.Fatal("first useful Connector app request: ", err)
		}
		time.Sleep(100 * time.Millisecond)
	}
	elapsed := float64(time.Since(start)) / float64(time.Millisecond)
	fixture.samples = append(fixture.samples, observation{
		Phase: phase, Millis: elapsed, State: state, WorkerResources: fixture.resources(),
	})
	data, err := json.MarshalIndent(fixture.samples, "", "  ")
	if err != nil {
		fixture.t.Fatal(err)
	}
	if err := os.WriteFile("/output/devx-observations.json", append(data, '\n'), 0600); err != nil {
		fixture.t.Fatal(err)
	}
	fixture.t.Logf("%s through real Connector app response: %.3fms; individual shared-worker observation", phase, elapsed)
	return state
}

func (fixture *fixture) cleanup(id string) {
	fixture.t.Helper()
	fixture.t.Cleanup(func() {
		view := fixture.view("state", id)
		root := view.Record.Rootfs
		if view.Runtime != nil && view.Runtime.PID > 1 {
			root = fmt.Sprintf("/proc/%d/root", view.Runtime.PID)
		}
		for _, name := range []string{"factorybox-devx.log", "factorybox-connector.log",
			"factorybox-devcontainer.log", "factorybox-dockerd.log", "factorybox-registry.log",
			"factorybox-docker-info.txt"} {
			if data, err := os.ReadFile(root + "/var/log/" + name); err == nil {
				if err := os.WriteFile("/output/"+id+"-"+name, data, 0600); err != nil {
					fixture.t.Error(err)
				}
			}
		}
		fixture.view("delete", id)
	})
}

func TestNativeDeveloperLifecycle(t *testing.T) {
	if os.Getenv("FACTORYBOX_DEVX_LIFECYCLE") != "1" {
		t.Skip("requires its bounded native Sysbox developer fixture")
	}
	bundle := "/var/lib/sysbox/devx/bundle"
	fixture := &fixture{t: t, root: "/var/lib/sysbox/devx/controller"}
	preparationStart := time.Now()
	var base lifecycle.Base
	if err := json.Unmarshal(fixture.command("prepare-base", "--source", bundle+"/lower", "developer-base"), &base); err != nil {
		t.Fatal(err)
	}
	if base.Tree.Immutable == nil || base.PreparedAt == "" {
		t.Fatal("public base preparation omitted its complete verified backing")
	}
	if err := os.Rename(bundle+"/lower", bundle+"/lower-input"); err != nil {
		t.Fatal(err)
	}
	alias, err := sysboxcapture.BindFilesystemBase(base.Tree, bundle+"/lower")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := sysboxcapture.RetireImmutableAlias(bundle+"/lower", alias); err != nil {
			t.Error("retire only the developer fixture's original base alias", err)
		}
	})
	for _, child := range []string{"rootfs", "upper", "work"} {
		if err := os.Mkdir(filepath.Join(bundle, child), 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := unix.Mount("overlay", bundle+"/rootfs", "overlay", 0,
		"lowerdir="+bundle+"/lower,upperdir="+bundle+"/upper,workdir="+bundle+"/work"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := unix.Unmount(bundle+"/rootfs", 0); err != nil {
			t.Error(err)
		}
	})
	if output, err := exec.Command("/out/sysbox-runc", "spec", "--bundle", bundle).CombinedOutput(); err != nil {
		t.Fatalf("native OCI spec: %v: %s", err, output)
	}
	data, err := os.ReadFile(bundle + "/config.json")
	if err != nil {
		t.Fatal(err)
	}
	var spec map[string]any
	if err := json.Unmarshal(data, &spec); err != nil {
		t.Fatal(err)
	}
	process := spec["process"].(map[string]any)
	process["terminal"], process["cwd"] = false, "/"
	process["args"] = []string{"/bin/sh", "-c", "exec /usr/local/bin/docker-init -- /bin/sh /opt/factorybox/sandbox.sh </dev/null >/var/log/factorybox-devx.log 2>&1"}
	process["env"] = []string{
		"PATH=/opt/factorybox/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		"HOME=/root", "DOCKER_HOST=unix:///var/run/docker.sock",
		"FACTORYBOX_DEVX_SANDBOX=1", "UV_USE_IO_URING=0",
	}
	capabilities := process["capabilities"].(map[string]any)
	for key, list := range capabilities {
		var retained []string
		for _, value := range list.([]any) {
			capability := value.(string)
			if capability != "CAP_SYS_MODULE" && capability != "CAP_SYS_RAWIO" &&
				capability != "CAP_SYS_BOOT" && capability != "CAP_SYS_TIME" && capability != "CAP_SYSLOG" {
				retained = append(retained, capability)
			}
		}
		capabilities[key] = retained
	}
	spec["root"].(map[string]any)["readonly"] = false
	annotations, _ := spec["annotations"].(map[string]any)
	if annotations == nil {
		annotations = map[string]any{}
	}
	annotations["io.factorybox.base"] = base.ID
	spec["annotations"] = annotations
	linux := spec["linux"].(map[string]any)
	linux["resources"] = map[string]any{
		"pids": map[string]any{"limit": 384}, "devices": []map[string]any{{"allow": true, "access": "rwm"}},
	}
	linux["seccomp"] = map[string]any{
		"defaultAction": "SCMP_ACT_ALLOW", "architectures": []string{"SCMP_ARCH_AARCH64"},
		"syscalls": []map[string]any{{
			"names":  []string{"io_uring_setup", "io_uring_enter", "io_uring_register"},
			"action": "SCMP_ACT_ERRNO", "errnoRet": 1,
		}},
	}
	for _, value := range spec["mounts"].([]any) {
		mount := value.(map[string]any)
		if mount["destination"] == "/sys/fs/cgroup" {
			mount["options"] = []string{"nosuid", "noexec", "nodev", "rw"}
		}
	}
	data, err = json.MarshalIndent(spec, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bundle+"/config.json", data, 0600); err != nil {
		t.Fatal(err)
	}
	preparationBytes, err := os.ReadFile("/output/preparation.json")
	if err != nil {
		t.Fatal(err)
	}
	var preparation map[string]any
	if err := json.Unmarshal(preparationBytes, &preparation); err != nil {
		t.Fatal(err)
	}
	originalSeconds, ok := preparation["seconds"].(float64)
	if !ok {
		t.Fatal("original rootfs preparation has no charged time")
	}
	baseSeconds := time.Since(preparationStart).Seconds()
	preparation["frozen_rootfs_seconds"] = originalSeconds
	preparation["immutable_base_and_oci_seconds"] = baseSeconds
	preparation["seconds"] = originalSeconds + baseSeconds
	preparation["scope"] = "Unpack the frozen rootfs, inject test inputs, start Sysbox daemons, " +
		"import and completely verify the owned immutable base, bind its read-only alias and prepare OCI. " +
		"This precedes the prepared-start first-use observation."
	preparationBytes, err = json.MarshalIndent(preparation, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("/output/preparation.json", append(preparationBytes, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	source := fixture.view("start", "--bundle", bundle, "source")
	fixture.cleanup("source")
	initial := fixture.useful("prepared-start-first-use", source, start, "")
	if initial.App.Counter != 0 || initial.App.Marker != "source" {
		t.Fatal("source did not initialize its fresh RAM and workspace")
	}
	snapshot := strings.Repeat("a", 32)
	fixture.command("checkpoint", "--snapshot-id", snapshot, "source")
	start = time.Now()
	branch := fixture.view("fork", "--source", "source", "--snapshot-id", snapshot, "branch")
	fixture.cleanup("branch")
	cloned := fixture.useful("fork-first-use", branch, start, "")
	if !reflect.DeepEqual(initial, cloned) || branch.Runtime.ID == source.Runtime.ID {
		t.Fatal("fork did not retain full captured app/authority RAM with independent runtime identity")
	}
	sourceAfter := fixture.useful("source-advance", source, time.Now(), `{"increment":1,"marker":"source-after"}`)
	branchAfter := fixture.useful("branch-advance", branch, time.Now(), `{"increment":1,"marker":"branch-after"}`)
	if sourceAfter.App.Counter != 1 || branchAfter.App.Counter != 1 ||
		sourceAfter.App.Marker != "source-after" || branchAfter.App.Marker != "branch-after" {
		t.Fatal("source and fork did not diverge independently")
	}
	sourceAgain := fixture.useful("source-independent", source, time.Now(), "")
	if !reflect.DeepEqual(sourceAfter, sourceAgain) {
		t.Fatal("branch mutation changed the retained source")
	}
	fixture.view("delete", "source")
	descendantSnapshot := strings.Repeat("b", 32)
	fixture.command("checkpoint", "--snapshot-id", descendantSnapshot, "branch")
	start = time.Now()
	descendant := fixture.view("fork", "--source", "branch", "--snapshot-id", descendantSnapshot, "descendant")
	fixture.cleanup("descendant")
	descendantState := fixture.useful("fork-of-fork-first-use", descendant, start, "")
	if !reflect.DeepEqual(branchAfter, descendantState) {
		t.Fatal("fork of a fork changed its saved app/authority state")
	}
	fixture.view("delete", "descendant")
	pauseSnapshot := strings.Repeat("c", 32)
	start = time.Now()
	paused := fixture.view("pause", "--snapshot-id", pauseSnapshot, "branch")
	if paused.Record.Phase != "paused" || paused.Record.Pause == nil || paused.Record.Pause.CompletedAt == "" ||
		paused.Record.Pause.After.Anon > 1<<20 ||
		paused.Record.Pause.Before.Anon <= paused.Record.Pause.After.Anon ||
		!paused.Record.Pause.Snapshot.FileBacked || paused.Record.Pause.Snapshot.ResidentBytesAfter != 0 {
		t.Fatal("public pause lacks its completed stopped state")
	}
	t.Logf("developer pause request completed in %s", time.Since(start))
	start = time.Now()
	resumed := fixture.view("resume", "--snapshot-id", pauseSnapshot, "branch")
	resumedState := fixture.useful("resume-first-use", resumed, start, "")
	if !reflect.DeepEqual(branchAfter, resumedState) {
		t.Fatal("resume lost app RAM, Compose dependency, Connector state or writable workspace")
	}
	resumedSnapshot := strings.Repeat("d", 32)
	fixture.command("checkpoint", "--snapshot-id", resumedSnapshot, "branch")
	start = time.Now()
	afterResume := fixture.view("fork", "--source", "branch", "--snapshot-id", resumedSnapshot, "after-resume")
	fixture.cleanup("after-resume")
	afterResumeState := fixture.useful("fork-of-resumed-first-use", afterResume, start, "")
	if !reflect.DeepEqual(branchAfter, afterResumeState) {
		t.Fatal("fork of the resumed developer sandbox changed complete state")
	}
}
