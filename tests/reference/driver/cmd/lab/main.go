package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

const namespace = "runtime-fork-experiment"

type result struct {
	Name       string `json:"name"`
	Status     string `json:"status"`
	DurationMS int64  `json:"duration_ms"`
	Output     string `json:"output,omitempty"`
}

func command(ctx context.Context, name string, args ...string) (string, error) {
	b, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	return string(b), err
}

func record(name string, start time.Time, output string, err error) {
	status := "passed"
	if err != nil {
		status = "failed"
		output += "\n" + err.Error()
	}
	b, _ := json.Marshal(result{name, status, time.Since(start).Milliseconds(), output})
	fmt.Println(string(b))
}

func cluster(args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	return command(ctx, "kubectl", append([]string{"--context", "orbstack"}, args...)...)
}

func nativeProbe() error {
	start := time.Now()
	if out, err := cluster("apply", "-f", "deploy/namespace.yaml"); err != nil {
		return fmt.Errorf("namespace: %s: %w", out, err)
	}
	if out, err := cluster("apply", "-f", "deploy/native-probe.yaml"); err != nil {
		return fmt.Errorf("probe: %s: %w", out, err)
	}
	defer cluster("delete", "pod", "native-gvisor-probe", "-n", namespace, "--wait=false")
	for range 12 {
		out, err := cluster("get", "pod", "native-gvisor-probe", "-n", namespace, "-o", "json")
		if err != nil {
			return err
		}
		var p struct {
			Status struct {
				Phase string `json:"phase"`
			} `json:"status"`
		}
		if err := json.Unmarshal([]byte(out), &p); err != nil {
			return err
		}
		if p.Status.Phase == "Succeeded" {
			log, err := cluster("logs", "native-gvisor-probe", "-n", namespace)
			record("native-runtimeclass", start, log, err)
			return err
		}
		events, _ := cluster("get", "events", "-n", namespace, "--field-selector", "involvedObject.name=native-gvisor-probe", "-o", "json")
		if strings.Contains(events, "no runtime for") || strings.Contains(events, "not configured") || strings.Contains(events, "unknown or invalid runtime") || strings.Contains(events, "RuntimeHandler") && strings.Contains(events, "not supported") {
			record("native-runtimeclass", start, events, fmt.Errorf("registered RuntimeClass has no usable node runtime"))
			return nil // A measured blocker is a valid experiment outcome.
		}
		time.Sleep(time.Second)
	}
	out, _ := cluster("describe", "pod", "native-gvisor-probe", "-n", namespace)
	record("native-runtimeclass", start, out, fmt.Errorf("probe did not finish within bounded observation"))
	return nil
}

// The fixture's nonce exists only in RAM. A restart cannot pass a restore test.
func fixture() error {
	port, err := fixturePort()
	if err != nil {
		return err
	}
	procDir, err := os.Open("/proc")
	if err != nil {
		return err
	}
	defer procDir.Close()
	cgroupDir, err := os.Open("/sys/fs/cgroup")
	if err != nil {
		return err
	}
	defer cgroupDir.Close()
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return err
	}
	nonce := hex.EncodeToString(b)
	ramToken, err := newFixtureRAMToken()
	if err != nil {
		return err
	}
	defer ramToken.close()
	var mu sync.Mutex
	var ticks, value uint64
	go func() {
		for {
			time.Sleep(100 * time.Millisecond)
			mu.Lock()
			ticks++
			mu.Unlock()
		}
	}()
	http.HandleFunc("/state", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if s := r.URL.Query().Get("ram_token"); s != "" {
			if err := ramToken.set(s); err != nil {
				http.Error(w, err.Error(), 400)
				return
			}
		}
		if s := r.URL.Query().Get("value"); s != "" {
			n, err := strconv.ParseUint(s, 10, 64)
			if err != nil {
				http.Error(w, "invalid value", 400)
				return
			}
			value = n
		}
		branch, _ := os.ReadFile("/workspace/branch.txt")
		hostname, _ := os.Hostname()
		procStatus, _ := os.ReadFile("/proc/self/status")
		procPID := statusPID(procStatus)
		pid1Command, _ := os.ReadFile("/proc/1/cmdline")
		fdRoot := fmt.Sprintf("/proc/self/fd/%d", procDir.Fd())
		fdStatus, _ := os.ReadFile(fdRoot + "/self/status")
		fdPID1, _ := os.ReadFile(fdRoot + "/1/cmdline")
		cgroup, _ := os.ReadFile("/proc/self/cgroup")
		pidsLimit, _ := os.ReadFile("/sys/fs/cgroup/pids.max")
		fdPidsLimit, _ := os.ReadFile(fmt.Sprintf("/proc/self/fd/%d/pids.max", cgroupDir.Fd()))
		cpuMax, _ := os.ReadFile("/sys/fs/cgroup/cpu.max")
		fdCpuMax, _ := os.ReadFile(fmt.Sprintf("/proc/self/fd/%d/cpu.max", cgroupDir.Fd()))
		mountinfo, _ := os.ReadFile("/proc/self/mountinfo")
		procSysReadonly := false
		cgroupReadonly := false
		for _, line := range strings.Split(string(mountinfo), "\n") {
			fields := strings.Fields(line)
			if len(fields) > 5 && fields[4] == "/proc/sys" {
				procSysReadonly = strings.Contains(","+fields[5]+",", ",ro,")
			}
			if len(fields) > 5 && fields[4] == "/sys/fs/cgroup" {
				cgroupReadonly = strings.Contains(","+fields[5]+",", ",ro,")
			}
		}
		keys, keysErr := os.Stat("/proc/keys")
		keysMasked := keysErr == nil && keys.Mode()&os.ModeCharDevice != 0
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"nonce": nonce, "ram_token": ramToken.get(), "ticks": ticks, "value": value, "branch": strings.TrimSpace(string(branch)), "pid": os.Getpid(), "hostname": hostname, "proc_pid": procPID, "pid1_command": strings.TrimSpace(strings.ReplaceAll(string(pid1Command), "\x00", " ")), "proc_fd_pid": statusPID(fdStatus), "proc_fd_pid1_command": strings.TrimSpace(strings.ReplaceAll(string(fdPID1), "\x00", " ")), "cgroup_view": strings.TrimSpace(string(cgroup)), "pids_limit": strings.TrimSpace(string(pidsLimit)), "cgroup_fd_pids_limit": strings.TrimSpace(string(fdPidsLimit)), "cpu_max": strings.TrimSpace(string(cpuMax)), "cgroup_fd_cpu_max": strings.TrimSpace(string(fdCpuMax)), "cgroup_readonly": cgroupReadonly, "proc_sys_readonly": procSysReadonly, "proc_keys_masked": keysMasked})
	})
	http.HandleFunc("/file", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PUT" {
			http.Error(w, "PUT required", 405)
			return
		}
		b, err := io.ReadAll(io.LimitReader(r.Body, 4096))
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		if err := os.WriteFile("/workspace/branch.txt", b, 0644); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.WriteHeader(204)
	})
	return http.ListenAndServe("0.0.0.0:"+port, nil)
}

func fixturePort() (string, error) {
	port := os.Getenv("LAB_FIXTURE_PORT")
	if port == "" {
		return "8080", nil
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1024 || n > 65535 {
		return "", fmt.Errorf("invalid fixture port %q", port)
	}
	return strconv.Itoa(n), nil
}

func statusPID(data []byte) int {
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "Pid:") {
			pid, _ := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "Pid:")))
			return pid
		}
	}
	return 0
}

func get(path string) error {
	port, err := fixturePort()
	if err != nil {
		return err
	}
	client := http.Client{Timeout: 5 * time.Second}
	r, err := client.Get("http://127.0.0.1:" + port + path)
	if err != nil {
		return err
	}
	defer r.Body.Close()
	_, err = io.Copy(os.Stdout, r.Body)
	return err
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: lab native-probe | down | fixture | get PATH | worker")
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "native-probe":
		err = nativeProbe()
	case "down":
		var out string
		out, err = cluster("delete", "namespace", namespace, "--wait=false")
		fmt.Print(out)
	case "fixture":
		os.MkdirAll("/workspace", 0755)
		err = fixture()
	case "sandbox":
		err = sandbox()
	case "tools":
		err = tools()
	case "get":
		p := "/state"
		if len(os.Args) > 2 {
			p = os.Args[2]
		}
		err = get(p)
	case "worker":
		err = worker()
	case "criu-source":
		err = criuDockerSource()
	case "loopback-up":
		err = loopbackUp()
	case "io-uring-policy-check":
		err = ioPolicyCheck()
	case "criu-probe":
		err = criuProbe()
	case "sysbox-service-responses":
		err = criuSysboxServiceResponseHelper()
	case "clone-inherited-images":
		err = criuCloneInheritedImageGraph(os.Args[2:])
	case "clone-inherited-images-with-ram-parents":
		err = criuCloneInheritedImageGraphPolicy(os.Args[2:], true)
	case "compile-range-snapshot":
		err = criuCompileRangeSnapshot(os.Args[2:])
	case "compile-range-snapshot-direct":
		err = criuCompileRangeSnapshotPolicy(os.Args[2:], true)
	case "compile-range-snapshot-direct-leased":
		err = criuCompileRangeSnapshotLeasedInputs(os.Args[2:])
	case "derive-inherited-oracle", "compare-image-memory", "compare-captured-memory":
		err = criuInheritedOracleCommand(os.Args[1], os.Args[2:])
	case "prepare-stream-original":
		err = criuPrepareStreamOriginal(os.Args[2:])
	case "controller-proof":
		err = controllerProof()
	default:
		err = fmt.Errorf("unknown command %s", os.Args[1])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
