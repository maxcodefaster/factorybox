package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

type criuLazyDaemon struct {
	command         *exec.Cmd
	done            chan error
	logPath         string
	captureControl  bool
	inheritSocket   string
	finished        bool
	exitErr         error
	observePath     string
	observeSequence uint64
	observeClockNS  uint64
	owner           *descendantPagerBinding
}

var criuLazyDaemons []*criuLazyDaemon

// A consumed completion must retain its error for later capture attempts and
// cleanup. A closed channel alone otherwise looks like successful completion.
func (d *criuLazyDaemon) pollCompletion() bool {
	if d.finished {
		return true
	}
	select {
	case err := <-d.done:
		d.finished, d.exitErr = true, err
		return true
	default:
		return false
	}
}

func (d *criuLazyDaemon) materialize(timeout time.Duration) error {
	if d.pollCompletion() {
		return d.exitErr
	}
	if !d.captureControl || d.command == nil || d.command.Process == nil || d.done == nil {
		return fmt.Errorf("lazy daemon has no owned capture control")
	}
	if err := d.command.Process.Signal(syscall.SIGUSR1); err != nil &&
		!errors.Is(err, os.ErrProcessDone) && !errors.Is(err, syscall.ESRCH) {
		return fmt.Errorf("requesting lazy source materialization: %w", err)
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case err := <-d.done:
		d.finished, d.exitErr = true, err
		return err
	case <-timer.C:
		return fmt.Errorf("lazy source materialization timed out before daemon completion")
	}
}

func criuSysboxMaterializeSource(id string) (resultErr error) {
	if os.Getenv("CRIU_SYSBOX_LAZY") != "1" {
		return nil
	}
	if id != "source-sysbox" || criuRoot != "/experiment" {
		return fmt.Errorf("lazy materialization is outside the owned source experiment")
	}
	start := time.Now()
	var source *criuLazyDaemon
	for _, d := range criuLazyDaemons {
		if filepath.Dir(d.logPath) == "/experiment/sysbox/images" {
			if source != nil {
				return fmt.Errorf("multiple page daemons own the source image")
			}
			source = d
		}
	}
	if source == nil {
		return fmt.Errorf("no page daemon owns the resumed source")
	}
	defer func() {
		record("sysbox-lazy-source-materialization", start,
			asJSON(map[string]any{"runtime_id": id, "images": filepath.Dir(source.logPath),
				"daemon_pid": source.command.Process.Pid, "daemon_finished": source.finished,
				"scope": "owned page daemon completion before pre-dump/full capture; preserves current UFFD remove/remap/fault bookkeeping; signal delivery alone is not completion"}), resultErr)
	}()
	return source.materialize(30 * time.Second)
}

// Ordinary dumps already contain complete pages marked eligible for lazy
// restore. The daemon stays outside captured cgroups and reads each branch's
// private images. Only the owned, serial restore request enables lazy-pages.
func criuSysboxRestore(images, executable string, arguments ...string) (output string, resultErr error) {
	return criuSysboxRestoreOwned("", images, executable, arguments...)
}

func criuSysboxRestoreOwned(ownerID, images, executable string, arguments ...string) (output string, resultErr error) {
	if criuLazyWPEnabled() {
		arguments = append([]string{"RUNTIME_FORK_LAZY_WP=1", executable}, arguments...)
		executable = "env"
	}
	if criuSysboxInheritEnabled() {
		arguments = append([]string{"RUNTIME_FORK_LAZY_IDENTITY=1", executable}, arguments...)
		executable = "env"
	}
	if criuParentManifestReuseEnabled() && os.Getenv("CRIU_SYSBOX_LAZY") == "1" {
		arguments = append([]string{"RUNTIME_FORK_PRIVATE_IMAGES=1", executable}, arguments...)
		executable = "env"
	}
	return criuSysboxRestoreUsingOwned(ownerID, images, func() (string, error) {
		return criuSysboxRestoreCommand(images, executable, arguments...)
	})
}

// The prepared copy uses the same page daemon, config lifecycle and native
// readiness gates. Its callback only releases an already-owned waiting child.
func criuSysboxRestoreUsing(images string, restore func() (string, error)) (output string, resultErr error) {
	return criuSysboxRestoreUsingOwned("", images, restore)
}

func criuSysboxRestoreUsingOwned(ownerID, images string, restore func() (string, error)) (output string, resultErr error) {
	requestStart := time.Now()
	finishPrefetch, err := criuStartEagerPrefetch(images)
	if err != nil {
		return "", err
	}
	defer func() { resultErr = errors.Join(resultErr, finishPrefetch()) }()
	pageLogLevel := "3"
	switch os.Getenv("CRIU_SYSBOX_LAZY_LOW_LOG") {
	case "", "0":
	case "1":
		pageLogLevel = "2"
	default:
		return "", fmt.Errorf("CRIU_SYSBOX_LAZY_LOW_LOG must be 0 or 1")
	}
	prefetchMode := "1"
	switch os.Getenv("CRIU_SYSBOX_LAZY_PREFETCH") {
	case "", "1":
	case "0":
		prefetchMode = "0"
	default:
		return "", fmt.Errorf("CRIU_SYSBOX_LAZY_PREFETCH must be 0 or 1")
	}
	defer func() {
		var logBytes, pageLogBytes int64
		if info, err := os.Stat(filepath.Join(images, "restore.log")); err == nil {
			logBytes = info.Size()
		}
		if info, err := os.Stat(filepath.Join(images, "lazy-pages.log")); err == nil {
			pageLogBytes = info.Size()
		}
		record("sysbox-"+filepath.Base(images)+"-restore-request", requestStart,
			asJSON(map[string]any{"images": images, "restore_log_bytes": logBytes, "lazy_page_log_bytes_at_restore_return": pageLogBytes, "lazy_page_log_level": pageLogLevel, "lazy_background_prefetch": prefetchMode == "1", "lazy": os.Getenv("CRIU_SYSBOX_LAZY") == "1", "profiled": os.Getenv("CRIU_SYSBOX_PROFILE") == "1", "scope": "restore command and lazy daemon setup; page log extent is sampled here, not final; service readiness and RAM assertions follow"}), resultErr)
	}()
	if os.Getenv("CRIU_SYSBOX_LAZY") != "1" {
		return restore()
	}
	if filepath.Clean(images) != images || !strings.HasPrefix(images, "/experiment/") {
		return "", fmt.Errorf("lazy restore images outside owned experiment: %q", images)
	}
	const configPath = "/etc/criu/runc.conf"
	base, err := os.ReadFile(configPath)
	if err != nil || !strings.Contains(string(base), "nested-ns\n") || strings.Contains(string(base), "lazy-pages") {
		return "", fmt.Errorf("unexpected owned restore configuration: %q %v", base, err)
	}
	start := time.Now()
	out, err := criuCommand(5*time.Second, "/usr/local/sbin/criu", "check", "--feature", "uffd-noncoop")
	record("sysbox-lazy-kernel-preflight", start, out, err)
	if err != nil {
		return out, err
	}
	d := &criuLazyDaemon{done: make(chan error, 1), logPath: filepath.Join(images, "lazy-daemon.log"), captureControl: true}
	if err := descendantPreparePager(d, ownerID, images); err != nil {
		return "", err
	}
	log, err := os.Create(d.logPath)
	if err != nil {
		return "", err
	}
	d.command = exec.Command("/usr/local/sbin/criu", "lazy-pages", "-D", images, "-v"+pageLogLevel, "-o", "lazy-pages.log")
	d.command.Env = append(os.Environ(), "RUNTIME_FORK_LAZY_PREFETCH="+prefetchMode, "RUNTIME_FORK_LAZY_CAPTURE_CONTROL=1")
	demandMode := "0"
	if criuChildDemandEnabled() && images == "/experiment/sysbox-copy/images" {
		demandMode = "1"
	}
	d.command.Env = append(d.command.Env, "RUNTIME_FORK_ADAPTIVE_DEMAND="+demandMode)
	if criuLazyObserveEnabled() {
		d.observePath = filepath.Join(filepath.Dir(images), "lazy-observe.json")
		d.command.Env = append(d.command.Env, "RUNTIME_FORK_LAZY_OBSERVE=1",
			"RUNTIME_FORK_LAZY_OBSERVE_PATH="+d.observePath)
	}
	if criuLazyWPEnabled() {
		d.command.Env = append(d.command.Env, "RUNTIME_FORK_LAZY_WP=1")
	}
	if criuSysboxInheritEnabled() {
		d.command.Env = append(d.command.Env, "RUNTIME_FORK_LAZY_IDENTITY=1")
		if images == "/experiment/sysbox/images" {
			d.inheritSocket = "/experiment/sysbox/lazy-inherit.socket"
			if os.Getenv("RUNTIME_FORK_RICH_EPOCH_CHAIN") == "1" && os.Getenv("RUNTIME_FORK_EPOCH_REBASE") == "1" {
				d.command.Env = append(d.command.Env, "RUNTIME_FORK_LAZY_EPOCH_SOCKET=/experiment/sysbox/lazy-epoch.socket")
			}
		}
		if d.inheritSocket != "" {
			d.command.Env = append(d.command.Env, "RUNTIME_FORK_LAZY_INHERIT_SOCKET="+d.inheritSocket)
		}
	}
	d.command.Stdout, d.command.Stderr = log, log
	if err := d.command.Start(); err != nil {
		log.Close()
		return "", err
	}
	criuLazyDaemons = append(criuLazyDaemons, d)
	go func() {
		d.done <- d.command.Wait()
		log.Close()
		close(d.done)
	}()
	defer func() {
		if resultErr != nil {
			d.command.Process.Kill()
			if !d.pollCompletion() {
				d.exitErr = <-d.done
				d.finished = true
			}
			data, _ := os.ReadFile(filepath.Join(images, "lazy-pages.log"))
			output += string(data)
		}
	}()
	socket := filepath.Join(images, "lazy-pages.socket")
	deadline := time.Now().Add(5 * time.Second)
	for {
		select {
		case daemonErr := <-d.done:
			d.finished, d.exitErr = true, daemonErr
			return "", fmt.Errorf("lazy daemon exited before restore: %v", daemonErr)
		default:
		}
		info, err := os.Lstat(socket)
		if err == nil && info.Mode()&os.ModeSocket != 0 {
			break
		}
		if err != nil && !os.IsNotExist(err) {
			return "", err
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("lazy daemon socket readiness timed out")
		}
		time.Sleep(10 * time.Millisecond)
	}
	info, err := os.Stat(images)
	if err != nil {
		return "", err
	}
	owner, ok := info.Sys().(*syscall.Stat_t)
	if !ok || owner.Uid == 0 {
		return "", fmt.Errorf("lazy Sysbox image directory must retain mapped-root ownership")
	}
	if err := os.Chown(socket, int(owner.Uid), int(owner.Gid)); err != nil {
		return "", err
	}
	if err := os.Chmod(socket, 0600); err != nil {
		return "", err
	}
	if err := os.WriteFile(configPath, append(base, []byte("lazy-pages\n")...), 0644); err != nil {
		return "", err
	}
	defer func() {
		if err := os.WriteFile(configPath, base, 0644); err != nil && resultErr == nil {
			resultErr = err
		}
	}()
	record("sysbox-lazy-daemon-ready", start, fmt.Sprintf("pid=%d private images=%s socket uid=%d mode=0600; %s", d.command.Process.Pid, images, owner.Uid, criuSysboxRAMSnapshotScope(images)), nil)
	output, resultErr = restore()
	if resultErr == nil {
		resultErr = descendantBindRestoredPager(d)
	}
	return output, resultErr
}

func criuSysboxLazyCleanup() {
	for _, d := range criuLazyDaemons {
		start := time.Now()
		if d.pollCompletion() {
			criuLazyLogRecords("sysbox-lazy-daemon-finished", start, filepath.Join(filepath.Dir(d.logPath), "lazy-pages.log"), d.exitErr)
		} else {
			d.command.Process.Kill()
			d.exitErr = <-d.done
			d.finished = true
			criuLazyLogRecords("sysbox-lazy-daemon-stopped-at-owned-workload-teardown", start, filepath.Join(filepath.Dir(d.logPath), "lazy-pages.log"), nil)
		}
	}
	criuLazyDaemons = nil
}

// Keep each native record below the CRI stream's line limit. Otherwise stderr
// can interleave with a split JSON record and invalidate harvested evidence.
// The initial handoffs and final faults matter; repeated background retries
// need not turn cleanup into an unbounded log transfer.
func criuLazyLogRecords(name string, start time.Time, path string, resultErr error) {
	data, readErr := os.ReadFile(path)
	if resultErr == nil {
		resultErr = readErr
	}
	if len(data) > 98304 {
		data = append(append(data[:65536:65536], []byte("\n... bounded diagnostic middle omitted ...\n")...), data[len(data)-32768:]...)
	}
	for len(data) > 4096 {
		record(name, start, path+"\n"+string(data[:4096]), nil)
		data = data[4096:]
	}
	record(name, start, path+"\n"+string(data), resultErr)
}
