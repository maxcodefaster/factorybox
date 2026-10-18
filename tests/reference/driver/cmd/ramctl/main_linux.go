package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	_ "github.com/opencontainers/cgroups/devices"
	"github.com/opencontainers/runc/libcontainer"
	"github.com/opencontainers/runc/libcontainer/configs"
	"golang.org/x/sys/unix"
)

// Namespace dump helpers can create images as host root. Restored Sysbox
// processes read them as mapped root, so keep every image private to that UID.
func ownSnapshotFiles(images string, uid, gid int) error {
	entries, err := os.ReadDir(images)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Name() == "original-writer" && entry.IsDir() &&
			os.Getenv("RUNTIME_FORK_LAZY_INHERIT_ORACLE") == "2" &&
			os.Getenv("RUNTIME_FORK_DESCENDANT_STREAM_ORACLE") == "1" {
			if os.Getenv("RUNTIME_FORK_DESCENDANT_IMAGES") != images {
				return fmt.Errorf("stream writer directory is outside its actual capture")
			}
			if _, _, err := lineageInheritedParent(images); err != nil {
				return err
			}
			child := filepath.Join(images, entry.Name())
			fd, err := unix.Open(child, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
			if err != nil {
				return err
			}
			var saved unix.Stat_t
			err = unix.Fstat(fd, &saved)
			if err == nil && saved.Mode&07777 != 0700 {
				err = fmt.Errorf("stream writer directory is not private")
			}
			if err == nil && !((saved.Uid == uint32(uid) && saved.Gid == uint32(gid)) ||
				(saved.Uid == uint32(os.Geteuid()) && saved.Gid == uint32(os.Getegid()))) {
				err = fmt.Errorf("stream writer directory has a foreign creator")
			}
			if err == nil {
				err = unix.Fchown(fd, uid, gid)
			}
			if err == nil {
				err = ownSnapshotFiles(child, uid, gid)
			}
			if err == nil {
				err = unix.Fsync(fd)
			}
			unix.Close(fd)
			if err != nil {
				return err
			}
			continue
		}
		if entry.Name() == "parent" && entry.Type()&os.ModeSymlink != 0 {
			var base, expected string
			if os.Getenv("RUNTIME_FORK_DESCENDANT_INHERIT_CAPTURE") == "1" {
				var err error
				base, expected, err = lineageInheritedParent(images)
				if err != nil {
					return err
				}
			} else {
				if (os.Getenv("CRIU_SYSBOX_DELTA") != "1" && os.Getenv("CRIU_SYSBOX_LAZY_INHERIT") != "1") || (images != "/experiment/sysbox/images-again" && images != "/experiment/sysbox/images-repeat") {
					return fmt.Errorf("parent image link is outside the owned delta")
				}
				expected = "../images-base"
				if os.Getenv("RUNTIME_FORK_RICH_EPOCH_CHAIN") == "1" && os.Getenv("RUNTIME_FORK_EPOCH_REBASE") == "1" && images == "/experiment/sysbox/images-repeat" {
					expected = "../images-again"
				}
				base = filepath.Join(filepath.Dir(images), strings.TrimPrefix(expected, "../"))
			}
			link, err := os.Readlink(filepath.Join(images, "parent"))
			if err != nil || link != expected {
				return fmt.Errorf("unexpected parent image link: %q %v", link, err)
			}
			info, err := os.Lstat(base)
			if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("invalid owned base: %v", err)
			}
			var fs unix.Statfs_t
			if err := unix.Statfs(base, &fs); err != nil || fs.Flags&unix.ST_RDONLY == 0 {
				return fmt.Errorf("owned base is not read-only: %v", err)
			}
			if err := os.Lchown(filepath.Join(images, "parent"), uid, gid); err != nil {
				return err
			}
			continue
		}
		fd, err := unix.Open(filepath.Join(images, entry.Name()), unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
		if err != nil {
			return err
		}
		var stat unix.Stat_t
		err = unix.Fstat(fd, &stat)
		if err == nil && stat.Mode&unix.S_IFMT != unix.S_IFREG {
			err = fmt.Errorf("snapshot entry is not a regular file: %q", entry.Name())
		}
		if err == nil && os.Getenv("RUNTIME_FORK_LAZY_INHERIT_ORACLE") == "2" &&
			os.Getenv("RUNTIME_FORK_DESCENDANT_STREAM_ORACLE") == "1" &&
			(stat.Nlink != 1 || !((stat.Uid == uint32(uid) && stat.Gid == uint32(gid)) ||
				(stat.Uid == uint32(os.Geteuid()) && stat.Gid == uint32(os.Getegid())))) {
			err = fmt.Errorf("stream snapshot entry has a foreign creating identity")
		}
		if err == nil {
			err = unix.Fchown(fd, uid, gid)
		}
		if err == nil {
			err = unix.Fchmod(fd, 0600)
		}
		unix.Close(fd)
		if err != nil {
			return err
		}
	}
	return nil
}

// This driver controls only the probe's containers. The libcontainer API lets
// CRIU save the entire owned network tree instead of runc CLI's empty-net mode.
func run() error {
	if len(os.Args) == 2 && os.Args[1] == "dirty-broker" {
		return dirtyBrokerRun()
	}
	if len(os.Args) != 5 && len(os.Args) != 6 {
		return fmt.Errorf("expected checkpoint|restore runtime-root container-id images, or restore-copy with cloned-rootfs")
	}
	op, root, id, images := os.Args[1], os.Args[2], os.Args[3], os.Args[4]
	if (op == "restore-copy") != (len(os.Args) == 6) {
		return fmt.Errorf("only restore-copy accepts cloned-rootfs")
	}
	if !regexp.MustCompile(`^[a-z][a-z0-9-]{1,63}$`).MatchString(id) {
		return fmt.Errorf("invalid probe runtime identity %q", id)
	}
	paths := []string{root, images}
	if op == "restore-copy" {
		paths = append(paths, os.Args[5])
	}
	for _, path := range paths {
		if !strings.HasPrefix(path, "/experiment/") || filepath.Clean(path) != path {
			return fmt.Errorf("path is outside the owned experiment: %q", path)
		}
	}
	opts := &libcontainer.CriuOpts{
		ImagesDirectory: images, WorkDirectory: images,
		EmptyNs: 0, ShellJob: true, FileLocks: true,
		TcpEstablished: os.Getenv("CRIU_SYSBOX_TOOLS") == "1",
		// Restore the saved controllers on the owned root before its children.
		// Soft mode skips the root created by runc, losing Docker's delegation.
		ManageCgroupsMode: "full",
	}
	configPath := filepath.Join(images, "runtime-config.json")
	switch op {
	case "checkpoint", "checkpoint-only", "checkpoint-retain-source", "pre-dump-retain-source", "checkpoint-retain-lineage":
		if op == "pre-dump-retain-source" && (os.Getenv("CRIU_SYSBOX_PRE_DUMP") != "1" || os.Getenv("CRIU_SYSBOX_RETAIN_SOURCE") != "1" || root != "/experiment/runtime" || id != "source-sysbox" || images != "/experiment/sysbox/images-base") {
			return fmt.Errorf("pre-dump is outside the owned live source")
		}
		c, err := libcontainer.Load(root, id)
		if err != nil {
			return err
		}
		if op == "pre-dump-retain-source" {
			status, err := c.Status()
			if err != nil || status != libcontainer.Running {
				return fmt.Errorf("pre-dump requires the running owned source: %v %v", status, err)
			}
		}
		if err := os.MkdirAll(images, 0700); err != nil {
			return err
		}
		retained := op != "checkpoint"
		if retained {
			// Include runtime exec roots parented outside the sandbox's PID-1
			// process tree. CRIU must prove complete owned cgroup collection.
			if err := os.Setenv("SYSBOX_FORK_COLLECT_SCOPE", "1"); err != nil {
				return err
			}
			// Sysbox's restored root reads images as its mapped host UID.
			// Give that identity access without making private snapshots public.
			config := c.Config()
			if len(config.UIDMappings) != 1 || len(config.GIDMappings) != 1 ||
				config.UIDMappings[0].ContainerID != 0 || config.GIDMappings[0].ContainerID != 0 ||
				config.UIDMappings[0].HostID <= 0 || config.GIDMappings[0].HostID <= 0 {
				return fmt.Errorf("retained Sysbox checkpoint requires a single saved root ID mapping")
			}
			if err := os.Chown(images, int(config.UIDMappings[0].HostID), int(config.GIDMappings[0].HostID)); err != nil {
				return err
			}
			if err := os.Chmod(images, 0700); err != nil {
				return err
			}
		}
		data, err := json.Marshal(c.Config())
		if err != nil {
			return err
		}
		if err := os.WriteFile(configPath, data, 0600); err != nil {
			return err
		}
		if op == "checkpoint-retain-source" {
			repeat := images == "/experiment/sysbox/images-repeat" && (os.Getenv("CRIU_SYSBOX_DELTA") == "1" || os.Getenv("RUNTIME_FORK_RICH_EPOCH_CHAIN") == "1")
			if os.Getenv("CRIU_SYSBOX_RETAIN_SOURCE") != "1" || root != "/experiment/runtime" || id != "source-sysbox" || (images != "/experiment/sysbox/images-again" && !repeat) {
				return fmt.Errorf("retained live source is outside the owned fork")
			}
			status, err := c.Status()
			if err != nil || status != libcontainer.Paused {
				return fmt.Errorf("retained source must already be paused: %v %v", status, err)
			}
			opts.LeaveRunning = true
			if os.Getenv("CRIU_SYSBOX_LAZY_INHERIT") == "1" {
				if os.Getenv("CRIU_SYSBOX_DELTA") == "1" || os.Getenv("CRIU_SYSBOX_PRE_DUMP") == "1" ||
					os.Getenv("RUNTIME_FORK_LAZY_INHERIT_SOCKET") != "/experiment/sysbox/lazy-inherit.socket" ||
					os.Getenv("RUNTIME_FORK_LAZY_INHERIT_DAEMON_PID") == "" ||
					os.Getenv("RUNTIME_FORK_LAZY_INHERIT_FRESH") != "1" {
					return fmt.Errorf("invalid owned lazy inheritance capture")
				}
				opts.ParentImage = "../images-base"
				if repeat && os.Getenv("RUNTIME_FORK_RICH_EPOCH_CHAIN") == "1" && os.Getenv("RUNTIME_FORK_EPOCH_REBASE") == "1" {
					opts.ParentImage = "../images-again"
				}
			}
			if os.Getenv("CRIU_SYSBOX_DELTA") == "1" {
				if os.Getenv("CRIU_SYSBOX_ASYNC_TRACK") != "1" || os.Getenv("CRIU_SYSBOX_PRE_DUMP") != "1" {
					return fmt.Errorf("delta capture requires the owned tracked pre-dump")
				}
				opts.ParentImage = "../images-base"
			}
		}
		if op == "checkpoint-retain-lineage" {
			lineageRoot := "/experiment/sysbox-copy"
			if regexp.MustCompile(`^branch-[0-9a-f]{32}$`).MatchString(id) {
				lineageRoot = filepath.Join("/experiment/branches", id)
			} else if id != "copy-sysbox" {
				return fmt.Errorf("lineage source identity is outside the owned roots")
			}
			if os.Getenv("RUNTIME_FORK_DESCENDANT") != "1" || root != "/experiment/runtime" ||
				filepath.Dir(images) != lineageRoot || c.Config().Rootfs != lineageRoot+"/rootfs" ||
				!regexp.MustCompile(`^images-[0-9a-f]{32}$`).MatchString(filepath.Base(images)) {
				return fmt.Errorf("lineage capture is outside the owned child")
			}
			status, err := c.Status()
			if err != nil || status != libcontainer.Paused {
				return fmt.Errorf("lineage source must be frozen: %v %v", status, err)
			}
			opts.LeaveRunning = true
			if os.Getenv("RUNTIME_FORK_DESCENDANT_INHERIT_CAPTURE") == "1" {
				_, relative, err := lineageInheritedParent(images)
				if err != nil {
					return err
				}
				if os.Getenv("RUNTIME_FORK_LAZY_INHERIT_SOCKET") == "" ||
					os.Getenv("RUNTIME_FORK_LAZY_INHERIT_DAEMON_PID") == "" ||
					os.Getenv("RUNTIME_FORK_LAZY_INHERIT_FRESH") != "1" {
					return fmt.Errorf("inherited lineage capture lacks its bound pager endpoint")
				}
				opts.ParentImage = relative
			}
		}
		if op == "pre-dump-retain-source" {
			opts.PreDump, opts.LeaveRunning = true, true
		}
		if err := c.Checkpoint(opts); err != nil {
			return err
		}
		if retained {
			// Sysbox must own its manager/fs cleanup. This mode tests its
			// process dump without discarding Sysbox registration metadata.
			config := c.Config()
			return ownSnapshotFiles(images, int(config.UIDMappings[0].HostID), int(config.GIDMappings[0].HostID))
		}
		return c.Destroy()
	case "restore", "restore-copy", "restore-existing":
		data, err := os.ReadFile(configPath)
		if err != nil {
			return err
		}
		var config configs.Config
		if err := json.Unmarshal(data, &config); err != nil {
			return err
		}
		if op == "restore-copy" {
			// The caller clones files while the source is stopped. Each restore
			// has its own image directory, writable root and cgroup subtree.
			if config.Cgroups == nil || !strings.HasPrefix(config.Cgroups.Path, "/criu-probe-") ||
				filepath.Dir(config.Cgroups.Path) != "/" || config.Rootfs == os.Args[5] {
				return fmt.Errorf("copy source is outside the owned cgroup subtree or shares its writable root")
			}
			if info, err := os.Stat(os.Args[5]); err != nil || !info.IsDir() {
				return fmt.Errorf("cloned filesystem must exist: %v", err)
			}
			config.Rootfs = os.Args[5]
			config.Cgroups.Path = "/criu-probe-" + id
			cgPath := filepath.Join("/sys/fs/cgroup", config.Cgroups.Path)
			if err := os.Mkdir(cgPath, 0755); err != nil {
				return err
			}
			if err := os.WriteFile(filepath.Join(cgPath, "cgroup.type"), []byte("threaded"), 0644); err != nil {
				_ = os.Remove(cgPath)
				return err
			}
		}
		var c *libcontainer.Container
		if op == "restore-existing" {
			c, err = libcontainer.Load(root, id)
		} else {
			c, err = libcontainer.Create(root, id, &config)
		}
		if err != nil {
			return err
		}
		process := &libcontainer.Process{Init: true, Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr}
		if err := c.Restore(process, opts); err != nil {
			if op != "restore-existing" {
				_ = c.Destroy()
			}
			return err
		}
		state, err := c.State()
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(state)
	default:
		return fmt.Errorf("unsupported operation %q", op)
	}
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
