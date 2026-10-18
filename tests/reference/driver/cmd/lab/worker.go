package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

var caps = []string{"CAP_AUDIT_WRITE", "CAP_CHOWN", "CAP_DAC_OVERRIDE", "CAP_FOWNER", "CAP_FSETID", "CAP_KILL", "CAP_MKNOD", "CAP_NET_BIND_SERVICE", "CAP_NET_ADMIN", "CAP_NET_RAW", "CAP_SETFCAP", "CAP_SETGID", "CAP_SETPCAP", "CAP_SETUID", "CAP_SYS_ADMIN", "CAP_SYS_CHROOT", "CAP_SYS_PTRACE"}

func sandbox() error {
	log, err := os.Create("/tmp/dockerd.log")
	if err != nil {
		return err
	}
	d := exec.Command("dockerd", "--host=unix:///var/run/docker.sock", "--iptables=false", "--ip6tables=false", "--storage-driver=vfs", "--feature=containerd-snapshotter=false")
	d.Stdout = log
	d.Stderr = log
	if err = d.Start(); err != nil {
		return err
	}
	defer func() { d.Process.Kill(); d.Wait(); log.Close() }()
	return tools()
}

func tools() error {
	os.MkdirAll("/workspace", 0755)
	os.MkdirAll("/workspace/fixture", 0755)
	if _, err := os.Stat("/opt/browser.mjs"); err == nil {
		for _, process := range [][]string{{"/opt/opencode/bun", "/opt/opencode/server.mjs"}, {"node", "/opt/browser.mjs"}} {
			if os.Getenv("CRIU_SYSBOX_TOOLS") == "1" {
				// Finish the cold Docker build before launching browser/agent
				// processes. They still run unchanged before the full snapshot.
				process = append([]string{"/bin/sh", "-c", "while ! test -f /workspace/tools-start; do sleep 0.1; done; exec \"$@\"", "tool-start"}, process...)
			}
			f, err := os.CreateTemp("/tmp", "tool-log-")
			if err != nil {
				return err
			}
			p := exec.Command(process[0], process[1:]...)
			p.Dir = "/workspace/fixture"
			p.Stdout = f
			p.Stderr = f
			if err = p.Start(); err != nil {
				return err
			}
			defer func() { p.Process.Kill(); p.Wait(); f.Close() }()
		}
	}
	return fixture()
}

func spec(id, mode string) (string, error) {
	dir := filepath.Join("/experiment", id)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	processMode := mode
	if mode == "full" || mode == "compose" {
		processMode = "sandbox"
	}
	if mode == "files" {
		processMode = "fixture"
	}
	s := map[string]any{
		"ociVersion": "1.0.2",
		"root":       map[string]any{"path": "/rootfs", "readonly": false},
		"process":    map[string]any{"terminal": false, "user": map[string]any{"uid": 0, "gid": 0}, "args": []string{"/lab", processMode}, "cwd": "/", "env": []string{"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin", "HOME=/root", "DOCKER_TLS_CERTDIR="}, "capabilities": map[string]any{"bounding": caps, "effective": caps, "permitted": caps}},
		"mounts": []any{
			map[string]any{"destination": "/proc", "type": "proc", "source": "proc"},
			map[string]any{"destination": "/dev", "type": "tmpfs", "source": "tmpfs", "options": []string{"nosuid", "strictatime", "mode=755", "size=65536k"}},
			map[string]any{"destination": "/dev/pts", "type": "devpts", "source": "devpts", "options": []string{"nosuid", "noexec", "newinstance", "ptmxmode=0666", "mode=0620", "gid=5"}},
			map[string]any{"destination": "/dev/shm", "type": "tmpfs", "source": "shm", "options": []string{"nosuid", "noexec", "nodev", "mode=1777", "size=65536k"}},
			map[string]any{"destination": "/sys", "type": "sysfs", "source": "sysfs", "options": []string{"nosuid", "noexec", "nodev", "ro"}},
			map[string]any{"destination": "/sys/fs/cgroup", "type": "cgroup", "source": "cgroup", "options": []string{"nosuid", "noexec", "nodev", "rw"}},
			map[string]any{"destination": "/tmp", "type": "tmpfs", "source": "tmpfs", "options": []string{"mode=1777"}},
			map[string]any{"destination": "/run", "type": "tmpfs", "source": "tmpfs"},
			map[string]any{"destination": "/var/lib/docker", "type": "tmpfs", "source": "tmpfs", "options": []string{"size=512m"}},
			map[string]any{"destination": "/workspace", "type": "tmpfs", "source": "tmpfs"},
			map[string]any{"destination": "/lab", "type": "bind", "source": "/lab", "options": []string{"bind", "ro"}},
			map[string]any{"destination": "/inner-image.tar", "type": "bind", "source": "/inner-image.tar", "options": []string{"bind", "ro"}},
		},
		"linux": map[string]any{"namespaces": []any{map[string]any{"type": "pid"}, map[string]any{"type": "mount"}, map[string]any{"type": "ipc"}, map[string]any{"type": "uts"}, map[string]any{"type": "network"}}},
	}
	if mode == "full" || mode == "files" || mode == "tools" {
		workspace := filepath.Join("/experiment", "workspaces", id)
		if err := os.MkdirAll(workspace, 0777); err != nil {
			return "", err
		}
		os.Chmod(workspace, 0777)
		mounts := s["mounts"].([]any)
		for _, m := range mounts {
			m := m.(map[string]any)
			if m["destination"] == "/workspace" {
				m["type"] = "bind"
				m["source"] = workspace
				m["options"] = []string{"bind", "rw"}
			}
		}
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return "", err
	}
	return dir, os.WriteFile(filepath.Join(dir, "config.json"), b, 0644)
}

func runsc(args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	base := []string{"--root=/experiment/runsc", "--platform=systrap", "--network=none", "--ignore-cgroups=true", "--net-raw=true", "--allow-packet-socket-write=true", "--overlay2=root:memory", "--log=/experiment/runsc.log"}
	// Detached sentry/guest processes inherit stdio. Files avoid a pipe reader
	// waiting forever after runsc itself has exited, and remain for restore.
	f, err := os.CreateTemp("/experiment", "runsc-command-")
	if err != nil {
		return "", err
	}
	c := exec.CommandContext(ctx, "/opt/gvisor/runsc", append(base, args...)...)
	c.Stdout = f
	c.Stderr = f
	err = c.Run()
	f.Close()
	b, readErr := os.ReadFile(f.Name())
	if err == nil {
		err = readErr
	}
	return string(b), err
}

func guest(id string, args ...string) (string, error) {
	return runsc(append([]string{"exec", id}, args...)...)
}

func state(id string) (map[string]any, error) {
	out, err := guest(id, "/lab", "get", "/state")
	if err != nil {
		return nil, fmt.Errorf("%s: %w", out, err)
	}
	var s map[string]any
	err = json.Unmarshal([]byte(out), &s)
	return s, err
}

func ready(id string) (map[string]any, error) {
	var last error
	for range 25 {
		s, err := state(id)
		if err == nil {
			return s, nil
		}
		last = err
		time.Sleep(100 * time.Millisecond)
	}
	return nil, last
}

func worker() error {
	os.MkdirAll("/experiment", 0755)
	ver, err := command(context.Background(), "/opt/gvisor/runsc", "--version")
	record("gvisor-version", time.Now(), ver, err)
	if err != nil {
		return err
	}
	// Small control separates runtime snapshot failure from nested-Docker failure.
	modes := []string{"fixture", "sandbox"}
	if v := os.Getenv("LAB_WORKLOADS"); v != "" {
		modes = strings.Split(v, ",")
	}
	for _, mode := range modes {
		coldStart := time.Now()
		var browserNonce string
		id := "original-" + mode
		bundle, err := spec(id, mode)
		if err != nil {
			return err
		}
		if mode == "tools" {
			target := filepath.Join("/experiment", "workspaces", id, "fixture")
			if err := os.MkdirAll(target, 0755); err != nil {
				return err
			}
			if out, err := command(context.Background(), "cp", "-a", "/rootfs/fixture/.", target); err != nil {
				return fmt.Errorf("prepare tools workspace %s: %w", out, err)
			}
		}
		start := time.Now()
		out, err := runsc("run", "--detach", "--bundle", bundle, id)
		if err != nil {
			record(mode+"-start", start, out, err)
			continue
		}
		defer runsc("delete", "--force", id)
		before, err := ready(id)
		record(mode+"-ready", start, asJSON(before), err)
		if err != nil {
			continue
		}
		if mode == "sandbox" || mode == "full" || mode == "compose" {
			start = time.Now()
			for range 30 {
				out, err = guest(id, "docker", "info", "--format", "{{json .ServerVersion}}")
				if err == nil {
					break
				}
				time.Sleep(200 * time.Millisecond)
			}
			record("gvisor-private-docker", start, out, err)
			if err != nil {
				log, _ := guest(id, "cat", "/tmp/dockerd.log")
				record("gvisor-docker-diagnostic", time.Now(), log, err)
				continue
			}
			out, err = guest(id, "docker", "load", "-i", "/inner-image.tar")
			record("gvisor-docker-load", time.Now(), out, err)
			if err != nil {
				continue
			}
			start = time.Now()
			out, err = guest(id, "docker", "run", "--name", "nested-counter", "-d", "runtime-experiment-inner:eval", "sh", "-c", "i=0; while true; do i=$((i+1)); echo $i >/counter; sleep 0.1; done")
			record("gvisor-nested-container", start, out, err)
			if err != nil {
				continue
			}
			if mode == "full" || mode == "compose" {
				commands := [][]string{
					{"sh", "-c", "cp -a /fixture/. /workspace/fixture; cp /lab /workspace/fixture/lab; docker build --network=none -t experiment-build /workspace/fixture"},
					{"docker", "compose", "-f", "/workspace/fixture/compose.yaml", "up", "-d"},
					{"docker", "compose", "-f", "/workspace/fixture/compose.yaml", "exec", "-T", "client", "wget", "-q", "-O", "-", "http://app:8080/state"},
					{"devcontainer", "up", "--workspace-folder", "/workspace/fixture", "--log-level", "info"},
					{"/opt/opencode/bun", "/opt/opencode/health.mjs"},
					{"node", "/opt/browser-state.mjs", "set"},
				}
				if mode == "compose" {
					commands = commands[:3]
				}
				for i, c := range commands {
					start = time.Now()
					out, err = guest(id, c...)
					record(fmt.Sprintf("%s-preparation-%d", mode, i), start, out, err)
					if err != nil {
						log, _ := guest(id, "sh", "-c", "tail -n 15 /tmp/tool-log-* /tmp/dockerd.log")
						record("full-diagnostic", time.Now(), log, err)
						break
					}
					if mode == "full" && i == len(commands)-1 {
						browserNonce = strings.TrimSpace(out)
					}
				}
				if err != nil {
					continue
				}
				record(mode+"-cold-complete-readiness", coldStart, "Docker build and Compose connectivity; full mode additionally checks Dev Container, OpenCode and browser RAM", nil)
			}
		}
		if mode == "tools" {
			for _, c := range [][]string{{"/opt/opencode/bun", "/opt/opencode/health.mjs"}, {"node", "/opt/browser-state.mjs", "set"}} {
				start = time.Now()
				out, err = guest(id, c...)
				record("tools-preparation", start, out, err)
				if err != nil {
					break
				}
				browserNonce = strings.TrimSpace(out)
			}
			if err != nil {
				log, _ := guest(id, "sh", "-c", "tail -n 20 /tmp/tool-log-*")
				record("tools-diagnostic", time.Now(), log, err)
				continue
			}
			record("tools-cold-complete-readiness", coldStart, "OpenCode 2 and Chromium without nested Docker", nil)
		}
		cp, err := os.MkdirTemp("/experiment", "snapshot-"+mode+"-")
		if err != nil {
			return err
		}
		start = time.Now()
		out, err = runsc("checkpoint", "--image-path", cp, id)
		record(mode+"-checkpoint", start, cp+"\n"+out, err)
		if err != nil {
			continue
		}
		// Delete/recreate the runtime: merely freezing a live cgroup cannot pass.
		out, err = runsc("delete", "--force", id)
		if err != nil {
			record(mode+"-delete", time.Now(), out, err)
			continue
		}
		restored := true
		for n := range 2 {
			copyID := fmt.Sprintf("%s-copy-%d", mode, n)
			bundle, err = spec(copyID, mode)
			if err != nil {
				return err
			}
			start = time.Now()
			if mode == "full" || mode == "files" || mode == "tools" {
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				out, err = command(ctx, "cp", "-a", filepath.Join("/experiment", "workspaces", id)+"/.", filepath.Join("/experiment", "workspaces", copyID))
				cancel()
				if err != nil {
					return fmt.Errorf("filesystem clone %s: %w", out, err)
				}
				record(fmt.Sprintf("%s-filesystem-copy-%d", mode, n), start, "independent workspace copy", nil)
			}
			out, err = runsc("restore", "--detach", "--bundle", bundle, "--image-path", cp, copyID)
			if err == nil {
				var after map[string]any
				after, err = ready(copyID)
				out = asJSON(after)
				if err == nil && after["nonce"] != before["nonce"] {
					err = fmt.Errorf("RAM nonce changed: process restarted")
				}
				if err == nil {
					_, err = guest(copyID, "/lab", "get", fmt.Sprintf("/state?value=%d", n+10))
				}
				if err == nil && (mode == "sandbox" || mode == "full" || mode == "compose") {
					nested, e := guest(copyID, "docker", "exec", "nested-counter", "cat", "/counter")
					out += " nested-counter=" + nested
					err = e
				}
				if err == nil && (mode == "full" || mode == "files") {
					_, err = guest(copyID, "sh", "-c", fmt.Sprintf("echo copy-%d >/workspace/branch.txt; chmod 666 /workspace/branch.txt", n))
				}
				if err == nil && (mode == "full" || mode == "tools") {
					if err == nil {
						s, e := guest(copyID, "node", "/opt/browser-state.mjs", "get")
						out += " browser-nonce=" + s
						err = e
						if err == nil && strings.TrimSpace(s) != browserNonce {
							err = fmt.Errorf("browser RAM nonce changed")
						}
					}
					if err == nil {
						checks := [][]string{
							{"/opt/opencode/bun", "/opt/opencode/health.mjs"},
							{"docker", "compose", "-f", "/workspace/fixture/compose.yaml", "exec", "-T", "client", "wget", "-q", "-O", "-", "http://app:8080/state"},
							{"devcontainer", "exec", "--workspace-folder", "/workspace/fixture", "cat", "/workspace/marker.txt"},
						}
						if mode == "tools" {
							checks = checks[:1]
						}
						for _, check := range checks {
							s, e := guest(copyID, check...)
							out += " restored-service=" + s
							if e != nil {
								err = e
								break
							}
						}
					}
				}
			}
			record(mode+"-restore-"+fmt.Sprint(n), start, out, err)
			defer runsc("delete", "--force", copyID)
			if err != nil {
				restored = false
				break
			}
		}
		if !restored {
			continue
		}
		first, e1 := state(mode + "-copy-0")
		second, e2 := state(mode + "-copy-1")
		var diverge error
		if e1 != nil || e2 != nil || first["value"] != float64(10) || second["value"] != float64(11) {
			diverge = fmt.Errorf("independent values not demonstrated: %v / %v", e1, e2)
		}
		if (mode == "full" || mode == "files") && diverge == nil && (first["branch"] != "copy-0" || second["branch"] != "copy-1") {
			diverge = fmt.Errorf("filesystem copies did not diverge")
		}
		record(mode+"-independent-copies", time.Now(), asJSON([]any{first, second}), diverge)
		mem, _ := os.ReadFile("/sys/fs/cgroup/memory.current")
		record(mode+"-worker-memory-bytes", time.Now(), strings.TrimSpace(string(mem)), nil)
		if (mode == "full" || mode == "files" || mode == "tools") && os.Getenv("LAB_KEEP_COPIES") == "1" && diverge == nil {
			record("theia-attachment-ready", time.Now(), "/workspaces/"+mode+"-copy-0 and /workspaces/"+mode+"-copy-1; copies retained for IDE proof", nil)
			stop, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
			<-stop.Done()
			cancel()
		}
		// Release both copies before advancing to the next workload.
		for n := range 2 {
			runsc("delete", "--force", fmt.Sprintf("%s-copy-%d", mode, n))
		}
	}
	return nil
}

func asJSON(v any) string { b, _ := json.Marshal(v); return string(b) }
