package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

var criuRoot = "/experiment"
var criuRuntime = "runc"

// This runner owns only containers created inside its isolated Kubernetes Pod.
// It never changes the node's runtime configuration or accesses its Docker socket.
func criuCommand(timeout time.Duration, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.WaitDelay = 2 * time.Second
	b, err := cmd.CombinedOutput()
	return string(b), err
}

func criuRunc(args ...string) (string, error) {
	return criuCommand(40*time.Second, criuRuntime, append([]string{"--root", criuRoot + "/runtime"}, args...)...)
}

func criuDockerSource() error {
	if err := os.MkdirAll("/workspace", 0755); err != nil {
		return err
	}
	log, err := os.Create("/workspace/docker.log")
	if err != nil {
		return err
	}
	defer log.Close()
	daemonArgs := []string{"--host=unix:///var/run/docker.sock", "--storage-driver=vfs", "--userland-proxy-path=/usr/local/bin/docker-proxy", "--iptables=false", "--ip-forward=false", "--ip-masq=false"}
	network := "none"
	if os.Getenv("CRIU_SYSBOX_NETWORK") == "1" {
		daemonArgs = append(daemonArgs, "--ip6tables=false")
		network = "probe-bridge"
	} else {
		daemonArgs = append(daemonArgs, "--bridge=none")
	}
	daemon := exec.Command("dockerd", daemonArgs...)
	daemon.Stdout, daemon.Stderr = log, log
	if err := daemon.Start(); err != nil {
		return err
	}
	defer func() { _ = daemon.Process.Kill(); _ = daemon.Wait() }()
	deadline := time.Now().Add(30 * time.Second)
	for {
		if _, err = criuCommand(2*time.Second, "docker", "info"); err == nil {
			break
		}
		if time.Now().After(deadline) {
			data, _ := os.ReadFile("/workspace/docker.log")
			return fmt.Errorf("private Docker not ready: %s", data)
		}
		time.Sleep(100 * time.Millisecond)
	}
	if out, err := criuCommand(20*time.Second, "docker", "load", "-i", "/fixture-image.tar"); err != nil {
		return fmt.Errorf("load fixture: %s: %w", out, err)
	}
	if network != "none" {
		if out, err := criuCommand(10*time.Second, "docker", "network", "create", "--driver=bridge", "--subnet=172.29.0.0/24", network); err != nil {
			return fmt.Errorf("create private bridge: %s: %w", out, err)
		}
	}
	for _, name := range []string{"alpha", "beta"} {
		// Ordinary Docker PID/UTS/IPC namespaces and default seccomp remain enabled.
		if out, err := criuCommand(30*time.Second, "docker", "run", "-d", "--name", name, "--network="+network, "--memory=64m", "--pids-limit=64", "criu-probe-inner:4.2.1"); err != nil {
			data, _ := os.ReadFile("/workspace/docker.log")
			if len(data) > 12000 {
				data = data[len(data)-12000:]
			}
			return fmt.Errorf("inner %s: %s: %w\ndaemon tail: %s", name, out, err, data)
		}
	}
	if os.Getenv("CRIU_SYSBOX_TOOLS") == "1" {
		return tools()
	}
	return fixture()
}

func criuBundle(dir, mode string) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	if mode == "sysbox" {
		// Sysbox ID-maps the overlay's backing directories. Keep its lower
		// layer on the owned volume, not on this Pod image's overlay mount.
		copyTimeout := 30 * time.Second
		if os.Getenv("CRIU_SYSBOX_COMPOSE") == "1" {
			copyTimeout = 90 * time.Second
		}
		if os.Getenv("CRIU_SYSBOX_TOOLS") == "1" {
			copyTimeout = 150 * time.Second
		}
		if err := criuUnpackRoot(filepath.Join(dir, "lower"), copyTimeout); err != nil {
			return fmt.Errorf("Sysbox lower unpack: %w", err)
		}
		for _, child := range []string{"rootfs", "upper", "overlay-work"} {
			if err := os.Mkdir(filepath.Join(dir, child), 0755); err != nil {
				return err
			}
		}
		opts := fmt.Sprintf("lowerdir=%s/lower,upperdir=%s/upper,workdir=%s/overlay-work", dir, dir, dir)
		if out, err := criuCommand(10*time.Second, "mount", "-t", "overlay", "overlay", "-o", opts, filepath.Join(dir, "rootfs")); err != nil {
			return fmt.Errorf("Sysbox overlay root: %s: %w", out, err)
		}
	} else {
		if err := criuUnpackRoot(filepath.Join(dir, "rootfs"), 30*time.Second); err != nil {
			return fmt.Errorf("rootfs unpack: %w", err)
		}
	}
	if out, err := criuRunc("spec", "--bundle", dir); err != nil {
		return fmt.Errorf("OCI spec: %s: %w", out, err)
	}
	path := filepath.Join(dir, "config.json")
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var cfg map[string]any
	if err := json.Unmarshal(b, &cfg); err != nil {
		return err
	}
	cfg["root"].(map[string]any)["readonly"] = false
	process := cfg["process"].(map[string]any)
	process["terminal"] = false
	process["args"] = []string{"/bin/sh", "-c", "exec /lab fixture </dev/null >/workspace/stdio.log 2>&1"}
	if mode == "docker" || mode == "sysbox" {
		process["args"] = []string{"/bin/sh", "-c", "exec /lab criu-source </dev/null >/workspace/stdio.log 2>&1"}
	}
	process["cwd"] = "/workspace"
	process["env"] = []string{"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin", "HOME=/root", "DOCKER_HOST=unix:///var/run/docker.sock"}
	if mode == "sysbox" && os.Getenv("CRIU_SYSBOX_NETWORK") == "1" {
		process["env"] = append(process["env"].([]string), "CRIU_SYSBOX_NETWORK=1")
	}
	if mode == "sysbox" && os.Getenv("CRIU_SYSBOX_TOOLS") == "1" {
		process["env"] = append(process["env"].([]string), "CRIU_SYSBOX_TOOLS=1", "UV_USE_IO_URING=0")
	}
	caps := []string{"CAP_CHOWN", "CAP_DAC_OVERRIDE", "CAP_DAC_READ_SEARCH", "CAP_FOWNER", "CAP_FSETID", "CAP_KILL", "CAP_SETGID", "CAP_SETUID", "CAP_SETPCAP", "CAP_LINUX_IMMUTABLE", "CAP_NET_BIND_SERVICE", "CAP_NET_BROADCAST", "CAP_NET_ADMIN", "CAP_NET_RAW", "CAP_IPC_LOCK", "CAP_IPC_OWNER", "CAP_SYS_CHROOT", "CAP_SYS_PTRACE", "CAP_SYS_ADMIN", "CAP_SYS_RESOURCE", "CAP_MKNOD", "CAP_AUDIT_WRITE", "CAP_SETFCAP", "CAP_CHECKPOINT_RESTORE"}
	process["capabilities"] = map[string]any{"bounding": caps, "effective": caps, "permitted": caps}
	linux := cfg["linux"].(map[string]any)
	if mode == "sysbox" && os.Getenv("CRIU_SYSBOX_TOOLS") == "1" {
		arch := "SCMP_ARCH_AARCH64"
		if runtime.GOARCH == "amd64" {
			arch = "SCMP_ARCH_X86_64"
		}
		linux["seccomp"] = map[string]any{
			"defaultAction": "SCMP_ACT_ALLOW", "architectures": []string{arch},
			"syscalls": []map[string]any{{"names": []string{"io_uring_setup", "io_uring_enter", "io_uring_register"}, "action": "SCMP_ACT_ERRNO", "errnoRet": 1}},
		}
	}
	// Stay under this Pod's existing cgroup even when its cgroup namespace is shared.
	data, err := os.ReadFile("/proc/self/cgroup")
	if err != nil {
		return err
	}
	parent := ""
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "0::") {
			parent = strings.TrimPrefix(line, "0::")
		}
	}
	if parent == "" || strings.Contains(parent, "..") {
		return fmt.Errorf("unrecognized cgroup-v2 parent %q", parent)
	}
	linux["cgroupsPath"] = filepath.Join(parent, "criu-probe-"+mode)
	// The probe uses threaded child cgroups. The enclosing Pod owns the memory
	// limit; child cgroups use only the pids controller and device access rules.
	linux["resources"] = map[string]any{"pids": map[string]any{"limit": 512}, "devices": []map[string]any{{"allow": true, "access": "rwm"}}}
	for _, mount := range cfg["mounts"].([]any) {
		m := mount.(map[string]any)
		if m["destination"] == "/sys/fs/cgroup" {
			m["options"] = []string{"nosuid", "noexec", "nodev", "rw"}
		}
	}
	b, err = json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0644)
}

func criuUnpackRoot(destination string, timeout time.Duration) error {
	if err := os.Mkdir(destination, 0755); err != nil {
		return err
	}
	if handled, err := criuUnpackTemplate(destination, timeout); handled {
		return err
	}
	start := time.Now()
	out, err := criuCommand(timeout, "tar", "--numeric-owner", "--same-owner", "--acls", "--xattrs", "--xattrs-include=*", "-xpf", "/opt/probe-rootfs.tar", "-C", destination)
	if err == nil {
		remaining := timeout - time.Since(start)
		if remaining <= 0 {
			err = fmt.Errorf("rootfs unpack exhausted the preparation deadline")
		} else {
			out, err = criuCommand(remaining, "cp", "-a", "/opt/probe-overrides/.", destination)
		}
	}
	record("rootfs-archive-unpack", start, destination+"\n"+out, err)
	if err != nil {
		return fmt.Errorf("extract %s: %s: %w", destination, out, err)
	}
	return nil
}

func criuCgroup(mode string) error {
	// Start the owned child as threaded even when a fresh OrbStack Pod still
	// reports "domain". runc otherwise encounters "domain invalid" on its first
	// start. This does not change a node cgroup or the enclosing Pod's limits.
	path := filepath.Join("/sys/fs/cgroup", "criu-probe-"+mode)
	if err := os.Mkdir(path, 0755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(path, "cgroup.type"), []byte("threaded"), 0644); err != nil {
		return err
	}
	if mode == "sysbox" || mode == "sysbox-copy" || descendantID.MatchString(mode) {
		// Sysbox places the system container's init in a separate scope.
		// OrbStack requires that owned child to join the threaded subtree.
		scope := filepath.Join(path, "init.scope")
		if err := os.Mkdir(scope, 0755); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(scope, "cgroup.type"), []byte("threaded"), 0644)
	}
	return nil
}

func criuState(id, inner, path string) (map[string]any, error) {
	args := []string{"exec", id, "/lab", "get", path}
	if inner != "" {
		args = []string{"exec", id, "docker", "exec", inner, "/lab", "get", path}
	}
	deadline := time.Now().Add(30 * time.Second)
	diagnosed := false
	for {
		out, err := criuRunc(args...)
		if err == nil {
			var state map[string]any
			if err := json.Unmarshal([]byte(out), &state); err != nil {
				return nil, fmt.Errorf("decode state %q: %w", out, err)
			}
			return state, nil
		}
		if !diagnosed && inner != "" && os.Getenv("CRIU_SYSBOX_LAZY") == "1" {
			diagnosed = true
			criuSysboxLazyInnerExecDiagnostic(id, inner)
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("state %s/%s: %s: %w", id, inner, out, err)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func criuRestoredStates(id string, inners []string, before map[string]map[string]any) (map[string]map[string]any, error) {
	restored := map[string]map[string]any{}
	for _, inner := range inners {
		after, err := criuState(id, inner, "/state")
		if err != nil {
			criuSysboxLazyFailureDiagnostic(id, inner)
			return nil, err
		}
		for _, field := range []string{"nonce", "ram_token", "value", "pid", "hostname", "proc_pid", "pid1_command", "proc_fd_pid", "proc_fd_pid1_command", "cgroup_view", "pids_limit", "cgroup_fd_pids_limit", "cpu_max", "cgroup_fd_cpu_max", "cgroup_readonly", "proc_sys_readonly", "proc_keys_masked", "branch"} {
			if after[field] != before[inner][field] {
				return nil, fmt.Errorf("restored %s/%s field %s differs: before=%v after=%v", id, inner, field, before[inner], after)
			}
		}
		oldTicks, oldOK := before[inner]["ticks"].(float64)
		newTicks, newOK := after["ticks"].(float64)
		if !oldOK || !newOK || newTicks < oldTicks {
			return nil, fmt.Errorf("RAM counter regressed for %s/%s", id, inner)
		}
		restored[inner] = after
	}
	return restored, nil
}

func criuSetBranch(id, inner, branch string) error {
	args := []string{"exec", id}
	if inner != "" {
		args = append(args, "docker", "exec", inner)
	}
	args = append(args, "/bin/sh", "-c", `printf '%s' "$1" > /workspace/branch.txt`, "set-branch", branch)
	if out, err := criuRunc(args...); err != nil {
		return fmt.Errorf("set branch %s/%s: %s: %w", id, inner, out, err)
	}
	return nil
}

func criuRuntimeIdentity(id string) (map[string]any, error) {
	out, err := criuRunc("state", id)
	if err != nil {
		return nil, fmt.Errorf("runtime state %s: %s: %w", id, out, err)
	}
	var state struct {
		ID  string `json:"id"`
		PID int    `json:"pid"`
	}
	if err := json.Unmarshal([]byte(out), &state); err != nil || state.ID != id || state.PID <= 0 {
		return nil, fmt.Errorf("invalid restored runtime identity: %s", out)
	}
	identity := map[string]any{"id": id, "init_pid": state.PID}
	cgroup, err := os.ReadFile(fmt.Sprintf("/proc/%d/cgroup", state.PID))
	if err != nil {
		return nil, err
	}
	identity["host_cgroup"] = strings.TrimSpace(string(cgroup))
	if criuRuntime == "sysbox-runc" || criuRuntime == "/usr/local/sbin/sysbox-runc-experiment" {
		expected := "/criu-probe-sysbox/init.scope"
		if id == "copy-sysbox" {
			expected = "/criu-probe-sysbox-copy/init.scope"
		} else if descendantID.MatchString(id) {
			layout, err := descendantOwnedLayout(id)
			if err != nil {
				return nil, err
			}
			expected = strings.TrimPrefix(layout.Cgroup, "/sys/fs/cgroup") + "/init.scope"
		}
		if !strings.HasSuffix(identity["host_cgroup"].(string), expected) {
			return nil, fmt.Errorf("Sysbox %s uses an unexpected host cgroup: %s", id, cgroup)
		}
	}
	namespaces := []string{"pid", "mnt", "net", "uts", "ipc", "cgroup"}
	if criuRuntime == "sysbox-runc" || criuRuntime == "/usr/local/sbin/sysbox-runc-experiment" {
		namespaces = append(namespaces, "user")
	}
	for _, ns := range namespaces {
		link, err := os.Readlink(fmt.Sprintf("/proc/%d/ns/%s", state.PID, ns))
		if err != nil {
			return nil, err
		}
		identity[ns] = link
	}
	return identity, nil
}

func criuSourceThreads(mode string) ([]int, error) {
	seen := map[int]bool{}
	err := filepath.Walk(filepath.Join("/sys/fs/cgroup", "criu-probe-"+mode), func(path string, info os.FileInfo, err error) error {
		if err != nil || !info.IsDir() {
			return err
		}
		data, err := os.ReadFile(filepath.Join(path, "cgroup.threads"))
		if err != nil {
			return err
		}
		for _, field := range strings.Fields(string(data)) {
			pid, err := strconv.Atoi(field)
			if err != nil || pid <= 0 {
				return fmt.Errorf("invalid source thread %q", field)
			}
			seen[pid] = true
		}
		return nil
	})
	threads := make([]int, 0, len(seen))
	for pid := range seen {
		threads = append(threads, pid)
	}
	if err == nil && len(threads) == 0 {
		err = fmt.Errorf("source has no visible threads")
	}
	return threads, err
}

func criuCopyProof(sourceID, copyDir string, inners []string, before map[string]map[string]any) error {
	copyID := "copy-docker"
	defer criuRunc("delete", "--force", copyID)
	start := time.Now()
	images := filepath.Join(copyDir, "images")
	out, err := criuCommand(40*time.Second, "/ramctl", "restore-copy", criuRoot+"/runtime", copyID, images, filepath.Join(copyDir, "rootfs"))
	if err == nil {
		var setup string
		setup, err = criuRunc("exec", copyID, "/lab", "loopback-up")
		out += setup
	}
	if err == nil {
		var restored map[string]map[string]any
		restored, err = criuRestoredStates(copyID, inners, before)
		out += asJSON(restored)
	}
	logData, _ := os.ReadFile(filepath.Join(images, "restore.log"))
	record("docker-copy-restore-ram", start, out+string(logData), err)
	if err != nil {
		return err
	}
	return criuIndependentProof("docker", sourceID, copyID, inners, before)
}

func criuIndependentProof(mode, sourceID, copyID string, inners []string, before map[string]map[string]any) error {
	start := time.Now()
	sourceIdentity, err := criuRuntimeIdentity(sourceID)
	if err != nil {
		return err
	}
	copyIdentity, err := criuRuntimeIdentity(copyID)
	if err != nil {
		return err
	}
	for field, value := range sourceIdentity {
		if copyIdentity[field] == value {
			err = fmt.Errorf("source and copy share runtime identity %s", field)
			break
		}
	}
	record(mode+"-copy-identities", start, asJSON(map[string]any{"source": sourceIdentity, "copy": copyIdentity}), err)
	if err != nil {
		return err
	}
	start = time.Now()
	states := map[string]map[string]map[string]any{}
	for copyIndex, id := range []string{sourceID, copyID} {
		states[id] = map[string]map[string]any{}
		for i, inner := range inners {
			branch := fmt.Sprintf("branch-%d-%d", copyIndex, i)
			value := 111000*(copyIndex+1) + i
			if err := criuSetBranch(id, inner, branch); err != nil {
				return err
			}
			if _, err := criuState(id, inner, fmt.Sprintf("/state?value=%d", value)); err != nil {
				return err
			}
		}
	}
	// Read both branches only after both have changed their RAM and files.
	for copyIndex, id := range []string{sourceID, copyID} {
		for i, inner := range inners {
			after, stateErr := criuState(id, inner, "/state")
			if stateErr != nil || after["nonce"] != before[inner]["nonce"] ||
				after["value"] != float64(111000*(copyIndex+1)+i) || after["branch"] != fmt.Sprintf("branch-%d-%d", copyIndex, i) {
				err = fmt.Errorf("copy divergence failed for %s/%s: %v: %v", id, inner, after, stateErr)
				break
			}
			states[id][inner] = after
		}
		if err != nil {
			break
		}
	}
	record(mode+"-copy-ram-files-diverge", start, asJSON(states), err)
	if err != nil {
		return err
	}
	if mode == "sysbox" {
		for _, id := range []string{sourceID, copyID} {
			if err := criuSysboxNetworkProof(id, "diverged"); err != nil {
				return err
			}
		}
		if err := criuSysboxComposeDivergence(sourceID, copyID); err != nil {
			return err
		}
	}
	start = time.Now()
	out, err := criuRunc("exec", sourceID, "docker", "rm", "--force", "beta")
	if err == nil {
		var beta map[string]any
		beta, err = criuState(copyID, "beta", "/state")
		out += asJSON(beta)
		if err == nil && (beta["nonce"] != before["beta"]["nonce"] || beta["value"] != float64(222002) || beta["branch"] != "branch-1-2") {
			err = fmt.Errorf("removing source beta changed the copy: %v", beta)
		}
	}
	if err == nil {
		var fresh string
		fresh, err = criuRunc("exec", copyID, "docker", "run", "--rm", "--name", "after-fork", "--network=none", "--pids-limit=64", "--entrypoint=/bin/sh", "criu-probe-inner:4.2.1", "-c", "test -x /lab")
		out += fresh
	}
	record(mode+"-copy-independent-lifecycle", start, out, err)
	return err
}

func criuCase(mode string) (resultErr error) {
	id := "source-" + mode
	dir := filepath.Join(criuRoot, mode)
	start := time.Now()
	if err := criuCgroup(mode); err != nil {
		return err
	}
	if err := criuBundle(dir, mode); err != nil {
		record(mode+"-prepare", start, "", err)
		return err
	}
	defer criuRunc("delete", "--force", id)
	stdio := filepath.Join(dir, "rootfs", "workspace", "stdio.log")
	log, err := os.Create(stdio)
	if err != nil {
		return err
	}
	defer log.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, criuRuntime, "--root", criuRoot+"/runtime", "run", "--detach", "--bundle", dir, id)
	cmd.Stdout, cmd.Stderr = log, log
	if err := cmd.Run(); err != nil {
		data, _ := os.ReadFile(stdio)
		record(mode+"-start", start, string(data), err)
		return err
	}
	if out, err := criuRunc("exec", id, "/lab", "loopback-up"); err != nil {
		data, _ := os.ReadFile(stdio)
		err = fmt.Errorf("loopback: %s: %w", out, err)
		record(mode+"-start", start, string(data), err)
		return err
	}
	before := map[string]map[string]any{}
	inners := []string{""}
	if mode == "docker" || mode == "sysbox" {
		inners = append(inners, "alpha", "beta")
	}
	for i, inner := range inners {
		state, err := criuState(id, inner, fmt.Sprintf("/state?value=%d", 91827364+i))
		if err != nil {
			data, _ := os.ReadFile(stdio)
			record(mode+"-ready", start, string(data), err)
			return err
		}
		if nonce, ok := state["nonce"].(string); !ok || len(nonce) != 32 {
			return fmt.Errorf("missing RAM nonce for %s: %v", inner, state)
		}
		if state["proc_pid"] != state["pid"] || state["proc_fd_pid"] != state["pid"] {
			return fmt.Errorf("proc PID view differs for %s: %v", inner, state)
		}
		if mode != "sysbox" && (state["proc_sys_readonly"] != true || state["proc_keys_masked"] != true) {
			return fmt.Errorf("source proc protections missing for %s: %v", inner, state)
		}
		if mode != "sysbox" {
			expected := "512"
			if inner != "" {
				expected = "64"
			}
			if state["pids_limit"] != expected {
				return fmt.Errorf("source PID limit missing for %s: %v", inner, state)
			}
		}
		if state["cgroup_fd_pids_limit"] != state["pids_limit"] {
			return fmt.Errorf("source cgroup FD sees a different PID limit for %s: %v", inner, state)
		}
		if inner != "" && state["cgroup_readonly"] != true {
			return fmt.Errorf("source inner cgroup mount is writable for %s: %v", inner, state)
		}
		if inner != "" && (state["cpu_max"] != "max 100000" || state["cgroup_fd_cpu_max"] != state["cpu_max"]) {
			return fmt.Errorf("source inner default CPU view missing for %s: %v", inner, state)
		}
		before[inner] = state
	}
	b, _ := json.Marshal(before)
	record(mode+"-ready", start, string(b), nil)
	if mode == "sysbox" {
		if err := criuSysboxNetworkProof(id, "source"); err != nil {
			return err
		}
		if err := criuSysboxComposePrepare(id); err != nil {
			return err
		}
		if err := criuSysboxTheiaReady([]string{id}, "source", time.Now()); err != nil {
			return err
		}
		if err := criuSysboxProcProtection(id, "source"); err != nil {
			return err
		}
	}
	stateJSON, err := criuRunc("state", id)
	if err != nil {
		return fmt.Errorf("source runtime state: %s: %w", stateJSON, err)
	}
	var source struct {
		PID int `json:"pid"`
	}
	if err := json.Unmarshal([]byte(stateJSON), &source); err != nil || source.PID <= 0 {
		return fmt.Errorf("invalid source PID: %s", stateJSON)
	}
	sourceThreads, err := criuSourceThreads(mode)
	if err != nil {
		return err
	}
	if mode == "sysbox" && os.Getenv("CRIU_SYSBOX_TOOLS") == "1" {
		if err := criuSysboxIOPreflight(sourceThreads); err != nil {
			return err
		}
	}
	images := filepath.Join(dir, "images")
	if err := os.MkdirAll(images, 0755); err != nil {
		return err
	}
	if mode == "sysbox" {
		if err := criuSysboxPrepareRAMCapture(images); err != nil {
			return err
		}
	}
	start = time.Now()
	operation := "checkpoint"
	if mode == "sysbox" {
		operation = "checkpoint-only"
		mounts, mountErr := criuRunc("exec", id, "cat", "/proc/self/mountinfo")
		record("sysbox-source-mounts", start, mounts, mountErr)
		if mountErr != nil {
			return mountErr
		}
	}
	var out string
	if mode == "sysbox" {
		out, err = criuSysboxProfileCommand("capture", images, "/ramctl", operation, criuRoot+"/runtime", id, images)
	} else {
		out, err = criuCommand(40*time.Second, "/ramctl", operation, criuRoot+"/runtime", id, images)
	}
	dumpLog, _ := os.ReadFile(filepath.Join(images, "dump.log"))
	record(mode+"-checkpoint", start, out+string(dumpLog), err)
	if err != nil {
		return err
	}
	if mode == "sysbox" {
		if err := criuSysboxCgroupPrefixProof(images); err != nil {
			return err
		}
		if err := criuSysboxRAMCaptureProof(images); err != nil {
			return err
		}
	}
	if mode == "sysbox" {
		start = time.Now()
		for _, pid := range append(sourceThreads, source.PID) {
			status, statErr := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
			if statErr == nil && !strings.Contains(string(status), "State:\tZ") {
				return fmt.Errorf("Sysbox source thread %d still exists after checkpoint", pid)
			}
			if statErr != nil && !os.IsNotExist(statErr) {
				return statErr
			}
		}
		record("sysbox-source-terminated", start, fmt.Sprintf("original PID %d and %d captured threads are absent or zero-RSS zombies; retained manager/fs state stays outside the workload", source.PID, len(sourceThreads)), nil)
		if err := criuSysboxReleaseRAMCapture(images); err != nil {
			return err
		}
		// Compare the controller's overlay and backing-dir views after Sysbox
		// ID-mapping and the terminating snapshot, before restore mutates them.
		paths := []string{filepath.Join(dir, "rootfs", "var", "lib"), filepath.Join(dir, "lower", "var", "lib"), filepath.Join(dir, "upper", "var", "lib"), "/experiment/sysbox-mgr/buildkit/" + id}
		for _, path := range paths {
			diagnostic, diagErr := criuCommand(2*time.Second, "ls", "-lna", path)
			record("sysbox-rootfs-view", time.Now(), path+"\n"+diagnostic, diagErr)
		}
		data, diagErr := os.ReadFile(filepath.Join(criuRoot, "runtime", id, "state.json"))
		if diagErr == nil {
			var state map[string]any
			diagErr = json.Unmarshal(data, &state)
			if diagErr == nil {
				config, _ := state["config"].(map[string]any)
				data, diagErr = json.Marshal(map[string]any{"rootfs": config["rootfs"], "mounts": config["mounts"], "uid_mappings": config["uid_mappings"], "gid_mappings": config["gid_mappings"]})
			}
		}
		record("sysbox-restore-mount-metadata", time.Now(), string(data), diagErr)
		mounts, mountErr := os.ReadFile("/proc/self/mountinfo")
		for _, line := range strings.Split(string(mounts), "\n") {
			if strings.Contains(line, "/experiment/") {
				record("sysbox-controller-mount", time.Now(), line, mountErr)
			}
		}
		start = time.Now()
		out, err := criuSysboxRestore(images, "unshare", "--mount", "--propagation", "private", "/usr/local/sbin/sysbox-runc-experiment",
			"--root", criuRoot+"/runtime", "experiment-restore-existing", "--image-path", images, id)
		if err == nil {
			err = criuSysboxServiceResponses(id, "first-resume", start)
		}
		if err == nil {
			err = criuSysboxTheiaReady([]string{id}, "first-resume", start)
		}
		if err == nil {
			err = criuSysboxRememberHotTrace(images)
		}
		record("sysbox-first-resume-service-and-ide-readiness", start, "restore request through direct application responses and warm native IDE read/edit; browser UI and active inference are not asserted", err)
		var warm *criuPreparation
		if err == nil {
			warm, err = criuSysboxStartWarmPreparation(id)
			if warm != nil {
				// This defer precedes source/service teardown even if an
				// overlapping native assertion fails before the fork.
				defer func() { resultErr = errors.Join(resultErr, warm.wait()) }()
			}
		}
		if err == nil {
			var restored map[string]map[string]any
			restored, err = criuRestoredStates(id, inners, before)
			out += asJSON(restored)
		}
		logData, _ := os.ReadFile(filepath.Join(images, "restore.log"))
		record("sysbox-restore-ram", start, out+string(logData), err)
		if err != nil {
			return err
		}
		if err := criuSysboxNetworkProof(id, "first-resume"); err != nil {
			return err
		}
		if err := criuSysboxComposeProof(id, "first-resume"); err != nil {
			return err
		}
		record("sysbox-first-resume-complete-runtime-assertions", start,
			"restore request, retained RAM assertions, private Docker bridge/DNS, Compose/Dev Containers and browser/OpenCode; separate Theia editing and new Docker lifecycle follow", nil)
		start = time.Now()
		out, err = criuRunc("exec", id, "docker", "run", "--rm", "--name", "after-restore", "--network=none", "--pids-limit=64", "--entrypoint=/bin/sh", "criu-probe-inner:4.2.1", "-c", "test -x /lab")
		record("sysbox-retained-docker-lifecycle", start, out, err)
		if err != nil {
			return err
		}
		if err := criuSysboxProcProtection(id, "restored"); err != nil {
			return err
		}
		if err := criuSysboxSecondResume(id, dir, inners, warm); err != nil {
			return err
		}
		return nil
	}
	// runc normally removes runtime state after a successful terminating checkpoint.
	if _, err := os.Stat(filepath.Join(criuRoot, "runtime", id)); err == nil {
		if out, err := criuRunc("delete", id); err != nil {
			return fmt.Errorf("release stopped source: %s: %w", out, err)
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	for _, pid := range append(sourceThreads, source.PID) {
		status, statErr := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
		if statErr == nil && !strings.Contains(string(status), "State:\tZ") {
			return fmt.Errorf("source thread %d still exists after checkpoint", pid)
		}
		if statErr != nil && !os.IsNotExist(statErr) {
			return statErr
		}
	}
	if _, err := os.Stat(filepath.Join("/sys/fs/cgroup", "criu-probe-"+mode)); !os.IsNotExist(err) {
		return fmt.Errorf("source cgroup still exists after checkpoint: %v", err)
	}
	record(mode+"-source-terminated", start, fmt.Sprintf("original PID %d and %d captured threads are absent or zero-RSS zombies; owned cgroup subtree is gone", source.PID, len(sourceThreads)), nil)
	copyDir := filepath.Join(criuRoot, "docker-copy")
	if mode == "docker" {
		start = time.Now()
		if err := os.Mkdir(copyDir, 0700); err != nil {
			return err
		}
		// Both process trees are stopped while RAM images and all writable
		// files are cloned. Reflinks are used when supported; no shared writes.
		out, err := criuCommand(40*time.Second, "cp", "-a", "--reflink=auto", filepath.Join(dir, "rootfs"), images, copyDir)
		record("docker-copy-filesystem", start, out, err)
		if err != nil {
			return err
		}
	}
	if err := criuCgroup(mode); err != nil {
		return err
	}
	start = time.Now()
	out, err = criuCommand(40*time.Second, "/ramctl", "restore", criuRoot+"/runtime", id, images)
	if err == nil {
		// runc delegates network setup to its caller. Rebind the owned loopback
		// interface before testing the restored application, just as on first start.
		setup, setupErr := criuRunc("exec", id, "/lab", "loopback-up")
		if setupErr != nil {
			err = fmt.Errorf("restored loopback: %s: %w", setup, setupErr)
		}
	}
	if err == nil {
		var restored map[string]map[string]any
		restored, err = criuRestoredStates(id, inners, before)
		b, _ := json.Marshal(restored)
		out += string(b)
	}
	logData, _ := os.ReadFile(filepath.Join(images, "restore.log"))
	record(mode+"-restore-ram", start, out+string(logData), err)
	if err == nil && mode == "docker" {
		return criuCopyProof(id, copyDir, inners, before)
	}
	return err
}

func criuSysboxProcProtection(id, phase string) error {
	start := time.Now()
	out, err := criuRunc("exec", id, "/bin/sh", "-c", "awk '$5==\"/proc/bus\" && $6 ~ /(^|,)ro(,|$)/ {found=1} END {exit !found}' /proc/self/mountinfo && for kind in none proc sysfs; do if mount -t \"$kind\" -o remount,bind,rw none /proc/bus; then echo \"readonly proc remount unexpectedly allowed for $kind\"; exit 1; fi; done; result=$?; test $result -eq 0 && echo 'readonly proc remount rejected for none, proc and sysfs'; exit $result")
	record("sysbox-"+phase+"-proc-remount-denied", start, out, err)
	return err
}

// Checkpoint the resumed tree after new Docker activity and RAM/file writes.
// This exercises listener lifetimes and fs re-registration across generations.
func criuSysboxSecondResume(id, dir string, inners []string, warm *criuPreparation) (resultErr error) {
	materializationAndPreparationStart := time.Now()
	if criuSysboxInheritEnabled() {
		if err := criuSysboxPrepareInheritParent(id); err != nil {
			return err
		}
	} else if warm != nil {
		ready := warm.ready()
		err := warm.wait()
		record("sysbox-warm-preparation-joined", materializationAndPreparationStart, asJSON(map[string]any{
			"preparation_ms": warm.duration().Milliseconds(), "ready_before_request": ready,
			"scope": "join includes any remaining preparation; failed base is never consumed; preparation remains separately charged in performance comparison"}), err)
		if err != nil {
			return err
		}
	} else {
		if err := criuSysboxMaterializeSource(id); err != nil {
			return err
		}
		if err := criuSysboxPreDumpSource(id); err != nil {
			return err
		}
	}
	liveExecs, err := criuSysboxStartLiveExecs(id)
	if err != nil {
		return err
	}
	before := map[string]map[string]any{}
	for i, inner := range inners {
		path := fmt.Sprintf("/state?value=%d", 773000+i)
		if criuSysboxUsesParent() {
			// A dedicated mmap page changes once after the base. It is then
			// read-only through both dumps, so unrelated Go allocations cannot
			// mask a broken reset by making that page dirty again.
			path += fmt.Sprintf("&ram_token=%032x", 884000+i)
		}
		state, err := criuState(id, inner, path)
		if err != nil {
			return err
		}
		if criuSysboxUsesParent() && state["ram_token"] != fmt.Sprintf("%032x", 884000+i) {
			return fmt.Errorf("dedicated RAM token mutation did not succeed for %s: %v", inner, state)
		}
		before[inner] = state
	}
	out, err := criuRunc("exec", id, "/bin/sh", "-c", "printf retained-second-resume > /workspace/post-restore.txt")
	if err != nil {
		return fmt.Errorf("resumed file mutation: %s: %w", out, err)
	}
	outerPanic, err := os.ReadFile("/proc/sys/kernel/panic")
	if err != nil {
		return err
	}
	out, err = criuRunc("exec", id, "/bin/sh", "-c", "printf '17\\n' > /proc/sys/kernel/panic; test \"$(cat /proc/sys/kernel/panic)\" = 17")
	record("sysbox-source-virtual-file-write", time.Now(), out, err)
	if err != nil {
		return err
	}
	threads, err := criuSourceThreads("sysbox")
	if err != nil {
		return err
	}
	images := filepath.Join(dir, "images-again")
	start := time.Now()
	liveForkStart := start
	retainSource := os.Getenv("CRIU_SYSBOX_RETAIN_SOURCE") == "1"
	var sourceIdentity map[string]any
	var retainedThreads map[int]string
	sourceFrozen := false
	defer func() {
		if sourceFrozen {
			cleanupStart := time.Now()
			cleanupErr := criuSysboxSetSourceFrozen(id, false)
			record("sysbox-retained-source-error-thaw", cleanupStart, "direct owned cgroup thaw; manager/file ownership unchanged", cleanupErr)
			resultErr = errors.Join(resultErr, cleanupErr)
		}
	}()
	operation := "checkpoint-only"
	if retainSource {
		sourceIdentity, err = criuRuntimeIdentity(id)
		if err != nil {
			return err
		}
		sourceFrozen = true // A failed confirmation may already have written freeze=1.
		err = criuSysboxSetSourceFrozen(id, true)
		if err == nil {
			retainedThreads, err = criuSysboxFrozenThreads()
		}
		record("sysbox-retained-source-frozen-before-capture", start, "direct kernel freeze; manager/file ownership unchanged; "+asJSON(sourceIdentity), err)
		if err != nil {
			return err
		}
		operation = "checkpoint-retain-source"
	}
	var pipeline *criuCopyPipeline
	if criuCopyPipelineEnabled() {
		pipeline, err = criuSysboxStartCopyPipeline(id)
		if err != nil {
			return err
		}
		defer func() { resultErr = errors.Join(resultErr, pipeline.close()) }()
	}
	if err := criuSysboxPrepareRAMCapture(images); err != nil {
		return err
	}
	if err := criuSysboxParentGuardProof(images); err != nil {
		return err
	}
	if criuSysboxInheritEnabled() {
		daemon, daemonErr := criuInheritSourceDaemon()
		if daemonErr != nil {
			return daemonErr
		}
		lazyWP := "0"
		if criuLazyWPEnabled() {
			lazyWP = "1"
		}
		if pipeline != nil {
			pipeline.captureStart = time.Now()
		}
		out, err = criuSysboxProfileCommand("capture", images, "env",
			"RUNTIME_FORK_LAZY_INHERIT_SOCKET="+daemon.inheritSocket,
			fmt.Sprintf("RUNTIME_FORK_LAZY_INHERIT_DAEMON_PID=%d", daemon.command.Process.Pid),
			"RUNTIME_FORK_LAZY_INHERIT_FRESH=1",
			"RUNTIME_FORK_LAZY_WP="+lazyWP,
			"/ramctl", operation, criuRoot+"/runtime", id, images)
		if pipeline != nil {
			pipeline.captureEnd = time.Now()
		}
	} else {
		out, err = criuSysboxProfileCommand("capture", images, "/ramctl", operation, criuRoot+"/runtime", id, images)
	}
	// Keep the full diagnostic file on disk, but defer its stream transfer so
	// both branches can become available immediately after capture/restore.
	defer criuLazyLogRecords("sysbox-second-checkpoint-diagnostic", time.Now(), filepath.Join(images, "dump.log"), nil)
	record("sysbox-second-checkpoint", start, out+"; full dump log retained at "+filepath.Join(images, "dump.log"), err)
	if err != nil {
		return err
	}
	if err := criuSysboxCgroupPrefixProof(images); err != nil {
		return err
	}
	if err := criuSysboxRAMCaptureProof(images); err != nil {
		return err
	}
	if os.Getenv("CRIU_SYSBOX_REPEAT_DELTA") == "1" {
		// Capture twice against the same parent with no source writes
		// between dumps, then actually restore the later child image.
		// A reset after the first dump would lose the dedicated token above.
		images = filepath.Join(dir, "images-repeat")
		if err := criuSysboxPrepareRAMCapture(images); err != nil {
			return err
		}
		repeatStart := time.Now()
		out, err = criuSysboxProfileCommand("capture", images, "/ramctl", "checkpoint-retain-source", criuRoot+"/runtime", id, images)
		record("sysbox-repeated-fixed-base-checkpoint", repeatStart, out+"; no source thaw or RAM writes between the two child dumps", err)
		defer criuLazyLogRecords("sysbox-repeat-checkpoint-diagnostic", time.Now(), filepath.Join(images, "dump.log"), nil)
		if err != nil {
			return err
		}
		if err := criuSysboxRAMCaptureProof(images); err != nil {
			return err
		}
	}
	if retainSource {
		if err := criuSysboxFrozenSourceProof(retainedThreads, "after-capture"); err != nil {
			return err
		}
	} else {
		for _, pid := range threads {
			status, statErr := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
			if statErr == nil && !strings.Contains(string(status), "State:\tZ") {
				return fmt.Errorf("resumed source thread %d still exists", pid)
			}
			if statErr != nil && !os.IsNotExist(statErr) {
				return statErr
			}
		}
		record("sysbox-second-source-terminated", time.Now(), fmt.Sprintf("%d captured resumed threads absent or zero-RSS zombies", len(threads)), nil)
	}
	if pipeline != nil && os.Getenv("CRIU_SYSBOX_COPY_PIPELINE_ABORT") == "1" {
		if err := criuSysboxPipelineAbortProof(pipeline, id, images, retainedThreads, sourceIdentity, inners, before); err != nil {
			return err
		}
		sourceFrozen = false
		return nil
	}
	defer criuRunc("delete", "--force", "copy-sysbox")
	var copy criuSysboxCopy
	if pipeline != nil {
		copy, err = pipeline.finish(images)
	} else {
		if os.Getenv("RUNTIME_FORK_RICH_EPOCH_CHAIN") == "1" {
			copy, images, err = criuEpochChainCopies(id, images, inners, before, retainedThreads)
		} else {
			copy, err = criuSysboxCopyRestore(id, images)
		}
	}
	if err != nil {
		record("sysbox-live-fork-full-readiness", liveForkStart, "child restore failed: "+copy.Output, err)
		return err
	}
	if criuEpochPipelineCancelled {
		sourceFrozen = false // The abort proof already thawed and verified the source.
		return nil
	}
	if criuEpochResumedSourceIdentity != nil {
		sourceIdentity = criuEpochResumedSourceIdentity
	}
	start = time.Now()
	if retainSource {
		err = criuSysboxFrozenSourceProof(retainedThreads, "after-child-restore")
		if err == nil {
			err = criuSysboxSetSourceFrozen(id, false)
			out = "direct owned cgroup thaw; manager/file ownership unchanged; "
			if err == nil {
				sourceFrozen = false
				var after map[string]any
				after, err = criuRuntimeIdentity(id)
				if err == nil && asJSON(after) != asJSON(sourceIdentity) {
					err = fmt.Errorf("source runtime/PID/namespace identity changed after thaw")
				}
				out += asJSON(after)
			}
		}
		record("sysbox-retained-source-thaw-and-identity", start, out, err)
	} else {
		out, err = criuSysboxRestore(images, "unshare", "--mount", "--propagation", "private", "/usr/local/sbin/sysbox-runc-experiment",
			"--root", criuRoot+"/runtime", "experiment-restore-existing", "--image-path", images, id)
	}
	if err == nil {
		// Both restore commands have finished. The native service helpers
		// join separate network namespaces and do not mutate CRIU config.
		errors := make(chan error, 2)
		go func() { errors <- criuSysboxServiceResponses(id, "second-resume", start) }()
		go func() { errors <- criuSysboxServiceResponses("copy-sysbox", "fork-restored", copy.Start) }()
		for range 2 {
			if responseErr := <-errors; responseErr != nil {
				err = responseErr
			}
		}
	}
	if err == nil {
		err = criuSysboxTheiaReady([]string{id, "copy-sysbox"}, "live-fork", liveForkStart)
	}
	recovery := "serial child and source restores"
	if retainSource {
		recovery = "child restore and direct thaw of the original source PIDs"
	}
	if !criuEpochSecondForkStart.IsZero() {
		secondElapsed := time.Since(criuEpochSecondForkStart)
		record("sysbox-epoch-second-fork-cost", criuEpochSecondForkStart, asJSON(map[string]any{
			"rebase":                         os.Getenv("RUNTIME_FORK_EPOCH_REBASE") == "1",
			"second_copy_pipeline":           os.Getenv("RUNTIME_FORK_EPOCH_PIPELINE") == "1",
			"parallel_parent_verification":   os.Getenv("RUNTIME_FORK_VERIFY_OVERLAP") == "1",
			"lazy_copied_page_tracking":      criuLazyWPEnabled(),
			"request_us":                     secondElapsed.Microseconds(),
			"prior_epoch_preparation_us":     criuEpochPreparation.Microseconds(),
			"prior_committed_preparation_us": criuCommittedPreparation.Microseconds(),
			"preparation_plus_request_us":    (secondElapsed + criuEpochPreparation + criuCommittedPreparation).Microseconds(),
			"scope":                          "second complete live fork after actual source execution; prior epoch publication charged; P1/P2 snapshot RAM remains charged until separate verified persistence/release at teardown; common first fork and fixture checks are not second-fork work",
		}), err)
		record("sysbox-epoch-second-fork-full-readiness", criuEpochSecondForkStart,
			"second actual source freeze, complete capture, private filesystem/images clone, child restore, original source thaw, Docker/Dev Container/browser/OpenCode and native Theia on both branches; prior parent persistence/rebase reported separately and must be charged for overall cost", err)
	}
	record("sysbox-live-fork-full-readiness", liveForkStart,
		"running-source capture, private filesystem/images clone, "+recovery+", concurrent direct Docker/Dev Container/app/browser/OpenCode responses, then warm native Theia read/edit on both branches; browser UI and active inference are not asserted", err)
	record("sysbox-live-fork-readiness-including-materialization-and-preparation", materializationAndPreparationStart,
		"source page-daemon completion, preparatory baseline/tracking and fixture mutations, then complete live-fork service/IDE readiness; this actual span includes preparation excluded by the legacy capture-only readiness record", err)
	requestTime := time.Since(materializationAndPreparationStart)
	chargedTime := requestTime
	var preparationTime time.Duration
	if warm != nil {
		preparationTime = warm.duration()
		chargedTime += preparationTime
	}
	chargedTime += criuParentProofRegistrationTime
	chargedTime += criuHotTraceRegistrationTime
	record("sysbox-live-fork-preparation-cost", materializationAndPreparationStart, asJSON(map[string]any{
		"warm": warm != nil, "request_ms": requestTime.Milliseconds(),
		"background_preparation_ms":               preparationTime.Milliseconds(),
		"prior_verified_manifest_registration_ms": criuParentProofRegistrationTime.Milliseconds(),
		"prior_hot_trace_registration_us":         criuHotTraceRegistrationTime.Microseconds(),
		"preparation_plus_request_ms":             chargedTime.Milliseconds(),
		"scope":                                   "separate actual warm-request latency and conservatively charged preparation plus request; overlapping join time may be counted twice, never subtracted from cold-equivalent cost"}), err)
	if err != nil {
		return err
	}
	if err := criuSysboxRAMLiveAccounting(images); err != nil {
		return err
	}
	if err := criuSysboxCopyProof(copy, inners, before); err != nil {
		return err
	}
	if err := criuSysboxParentManifestProof(); err != nil {
		return err
	}
	if err == nil {
		var restored map[string]map[string]any
		restored, err = criuRestoredStates(id, inners, before)
		out += asJSON(restored)
	}
	if retainSource {
		record("sysbox-second-retained-source-ram", start, out, err)
	} else {
		logData, _ := os.ReadFile(filepath.Join(images, "restore.log"))
		record("sysbox-second-restore-ram", start, out+string(logData), err)
	}
	if err != nil {
		return err
	}
	if err := criuSysboxNetworkProof(id, "second-resume"); err != nil {
		return err
	}
	if err := criuSysboxComposeProof(id, "second-resume"); err != nil {
		return err
	}
	record("sysbox-second-resume-complete-runtime-assertions", start,
		recovery+", retained RAM assertions, private Docker bridge/DNS, Compose/Dev Containers and browser/OpenCode; separate Theia editing and new Docker lifecycle follow", nil)
	start = time.Now()
	out, err = criuRunc("exec", id, "/bin/sh", "-c", "test \"$(cat /workspace/post-restore.txt)\" = retained-second-resume && docker run --rm --name after-second-restore --network=none --pids-limit=64 --entrypoint=/bin/sh criu-probe-inner:4.2.1 -c 'test -x /lab'")
	record("sysbox-second-docker-files-lifecycle", start, out, err)
	if err != nil {
		return err
	}
	if err := criuSysboxProcProtection(id, "second-restored"); err != nil {
		return err
	}
	if err := criuSysboxVirtualProof(id, "copy-sysbox", string(outerPanic)); err != nil {
		return err
	}
	if err := criuSysboxLiveExecProof(id, "copy-sysbox", liveExecs); err != nil {
		return err
	}
	if err := criuSysboxResourceProof(id, "copy-sysbox"); err != nil {
		return err
	}
	if err := criuIndependentProof("sysbox", id, "copy-sysbox", inners, before); err != nil {
		return err
	}
	if os.Getenv("RUNTIME_FORK_EPOCH_BACKED_RESUME") == "1" {
		return criuEpochBackedImagesUnchanged()
	}
	if os.Getenv("RUNTIME_FORK_DESCENDANT") == "1" {
		return criuDescendantForkProof()
	}
	return nil
}

func criuProbe() (resultErr error) {
	if _, err := committedSnapshotMode(); err != nil {
		return err
	}
	defer func() {
		criuSysboxLazyCleanup()
		resultErr = errors.Join(resultErr, criuSysboxDirtyKeeperCleanup())
		resultErr = errors.Join(resultErr, criuSysboxRAMCaptureCleanup())
		resultErr = errors.Join(resultErr, criuSysboxParentCleanup())
	}()
	if root := os.Getenv("CRIU_PROBE_ROOT"); root != "" {
		if !strings.HasPrefix(root, "/experiment/") || strings.Contains(root, "..") {
			return fmt.Errorf("invalid probe evidence path %q", root)
		}
		criuRoot = root
	}
	if err := criuInheritGuard(criuRoot, "source-sysbox"); err != nil {
		return err
	}
	if err := os.MkdirAll(criuRoot, 0755); err != nil {
		return err
	}
	if os.Getenv("CRIU_SYSBOX_PROBE") == "1" {
		if err := criuSysboxDirtyKeeperStart(); err != nil {
			return err
		}
		if err := criuSysboxProfilePreflight(); err != nil {
			return err
		}
		criuRuntime = "sysbox-runc"
		const configPath = "/etc/criu/runc.conf"
		config, err := os.ReadFile(configPath)
		if err != nil || strings.TrimSpace(string(config)) != "nested-ns" {
			return fmt.Errorf("unexpected owned CRIU configuration: %q %v", config, err)
		}
		if err := os.WriteFile(configPath, append(config, []byte("sysbox-oom-floor\n")...), 0644); err != nil {
			return err
		}
		if os.Getenv("CRIU_SYSBOX_TOOLS") == "1" {
			criuRuntime = "/usr/local/sbin/sysbox-runc-experiment"
			// Standard CRIU scan hints resolve overlay-backed application
			// watches by path when their kernel handles cannot be reopened.
			const path = "/etc/criu/runc.conf"
			base, err := os.ReadFile(path)
			if err != nil || strings.TrimSpace(string(base)) != "nested-ns\nsysbox-oom-floor" {
				return fmt.Errorf("unexpected owned CRIU configuration: %q %v", base, err)
			}
			hints := "irmap-scan-path /workspace\nirmap-scan-path /root\nirmap-scan-path /tmp\nirmap-scan-path /usr/share/fonts\nirmap-scan-path /usr/local/share/fonts\nirmap-scan-path /var/cache/fontconfig\nirmap-scan-path /usr/share/zoneinfo\n"
			if err := os.WriteFile(path, append(base, []byte(hints)...), 0644); err != nil {
				return err
			}
			record("sysbox-criu-inotify-scan-hints", time.Now(), hints, nil)
		}
		if os.Getenv("CRIU_SYSBOX_DUMP_LOW_LOG") == "1" {
			const path = "/etc/criu/runc.conf"
			base, err := os.ReadFile(path)
			if err != nil || strings.Contains(string(base), "sysbox-dump-low-log") {
				return fmt.Errorf("unexpected owned dump logging configuration: %q %v", base, err)
			}
			if err := os.WriteFile(path, append(base, []byte("sysbox-dump-low-log\n")...), 0644); err != nil {
				return err
			}
			record("sysbox-dump-warnings-and-errors", time.Now(), "Explicit owned CRIU RPC dump cap: warnings/errors retained, native counters and complete state checks unchanged; restore logging unaffected", nil)
		}
		for _, binary := range []string{"sysbox-runc", "/usr/local/sbin/sysbox-runc-experiment"} {
			start := time.Now()
			out, err := criuCommand(5*time.Second, binary, "--version")
			record(filepath.Base(binary)+"-version", start, out, err)
			if err != nil {
				return err
			}
		}
		if err := criuSysboxServices(); err != nil {
			return err
		}
		if err := criuWorkerCanary("before-case"); err != nil {
			return err
		}
		err = criuCase("sysbox")
		err = errors.Join(err, criuWorkerCanary("after-case"))
		for _, name := range []string{"sysbox-mgr", "sysbox-fs"} {
			data, _ := os.ReadFile(filepath.Join(criuRoot, name+".log"))
			record(name+"-diagnostic", time.Now(), string(data), nil)
		}
		return err
	}
	for _, file := range []string{"cgroup.type", "cgroup.controllers"} {
		start := time.Now()
		data, err := os.ReadFile(filepath.Join("/sys/fs/cgroup", file))
		record(file, start, string(data), err)
	}
	for _, name := range []string{"criu", "runc"} {
		start := time.Now()
		out, err := criuCommand(5*time.Second, name, "--version")
		record(name+"-version", start, out, err)
	}
	start := time.Now()
	out, err := criuCommand(20*time.Second, "criu", "check")
	record("criu-kernel-check", start, out, err)
	controlErr := criuCase("control")
	dockerErr := criuCase("docker")
	if controlErr != nil || dockerErr != nil {
		return fmt.Errorf("checkpoint proof incomplete: control=%v, nested Docker=%v", controlErr, dockerErr)
	}
	return nil
}

func criuSysboxServices() error {
	for _, args := range [][]string{
		{"sysbox-mgr", "--disable-shiftfs", "--data-root", "/experiment/sysbox-mgr", "--log", "/experiment/sysbox-mgr.log"},
		{"sysbox-fs", "--mountpoint", "/experiment/sysboxfs", "--log", "/experiment/sysbox-fs.log"},
	} {
		start := time.Now()
		cmd := exec.Command(args[0], args[1:]...)
		log, err := os.Create(filepath.Join(criuRoot, args[0]+"-stdio.log"))
		if err != nil {
			return err
		}
		cmd.Stdout, cmd.Stderr = log, log
		if err := cmd.Start(); err != nil {
			log.Close()
			return err
		}
		log.Close()
		go cmd.Wait()
		record(args[0]+"-started", start, fmt.Sprintf("PID %d inside owned Pod", cmd.Process.Pid), nil)
	}
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		mgr, _ := os.ReadFile("/experiment/sysbox-mgr.log")
		if _, err := os.Stat("/run/sysbox/sysfs-seccomp.sock"); err == nil && strings.Contains(string(mgr), "Listening") {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	mgr, _ := os.ReadFile("/experiment/sysbox-mgr.log")
	fs, _ := os.ReadFile("/experiment/sysbox-fs.log")
	err := fmt.Errorf("Sysbox services not ready")
	record("sysbox-services-ready", time.Now(), string(mgr)+string(fs), err)
	return err
}
