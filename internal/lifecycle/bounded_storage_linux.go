//go:build linux

package lifecycle

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"factorybox/internal/sysboxcapture"
	"golang.org/x/sys/unix"
)

func (e *Engine) boundedObject(path string) error {
	active := e.config.BoundedStore
	if active == nil {
		return nil
	}
	if !withinStore(active.Target, path) {
		return fmt.Errorf("path is outside bounded store: %s", path)
	}
	if err := canonical(path); err != nil {
		return err
	}
	var stat unix.Stat_t
	if err := unix.Lstat(path, &stat); err != nil {
		return err
	}
	if uint64(stat.Dev) != active.BlockDevice || (stat.Mode&unix.S_IFMT != unix.S_IFDIR && stat.Mode&unix.S_IFMT != unix.S_IFREG) {
		return fmt.Errorf("path is not backed by the bounded filesystem: %s", path)
	}
	return nil
}

// Initial nested mounts are not silently included in the capacity claim. Guest
// mounts subsequently created in the sandbox's private namespace remain subject
// to the runtime's namespace/device policy and cgroup memory limits.
func noInitialDescendantMounts(path string) error {
	file, err := os.Open("/proc/self/mountinfo")
	if err != nil {
		return err
	}
	defer file.Close()
	unescape := strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 10 {
			return fmt.Errorf("invalid mount inventory")
		}
		if strings.HasPrefix(unescape.Replace(fields[4]), path+"/") {
			return fmt.Errorf("initial descendant mount not admitted under bounded path: %s", path)
		}
	}
	return scanner.Err()
}

func (e *Engine) boundedBundle(bundle, root string, spec map[string]json.RawMessage) error {
	if e.config.BoundedStore == nil {
		return nil
	}
	if err := e.boundedObject(bundle); err != nil {
		return err
	}
	var fs unix.Statfs_t
	if err := unix.Statfs(root, &fs); err != nil {
		return err
	}
	if fs.Type == unix.OVERLAYFS_SUPER_MAGIC {
		if root != bundle+"/rootfs" {
			return fmt.Errorf("bounded overlay root must use the admitted bundle layout")
		}
		if err := sysboxcapture.CheckOverlayLayout(bundle); err != nil {
			return err
		}
		for _, name := range []string{"lower", "upper", "work"} {
			path := bundle + "/" + name
			if err := e.boundedObject(path); err != nil {
				return err
			}
			if err := noInitialDescendantMounts(path); err != nil {
				return err
			}
		}
	} else if err := e.boundedObject(root); err != nil {
		return err
	}
	if err := noInitialDescendantMounts(root); err != nil {
		return err
	}
	var mounts []struct {
		Type    string   `json:"type"`
		Source  string   `json:"source"`
		Options []string `json:"options"`
	}
	if err := json.Unmarshal(spec["mounts"], &mounts); err != nil {
		return err
	}
	for _, mount := range mounts {
		bind := mount.Type == "bind"
		for _, option := range mount.Options {
			bind = bind || option == "bind" || option == "rbind"
		}
		if bind {
			// Even an initially read-only external bind needs separate proof
			// that guest remounts cannot upgrade it. Refuse it in this profile.
			if !filepath.IsAbs(mount.Source) {
				return fmt.Errorf("bounded bind requires a canonical internal source")
			}
			if err := e.boundedObject(mount.Source); err != nil {
				return err
			}
			if err := noInitialDescendantMounts(mount.Source); err != nil {
				return err
			}
			continue
		}
		switch mount.Type {
		case "proc", "sysfs", "tmpfs", "devpts", "mqueue", "cgroup", "cgroup2":
		default:
			return fmt.Errorf("mount type is outside the bounded storage profile: %s", mount.Type)
		}
	}
	return nil
}
