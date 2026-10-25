//go:build linux

package sysboxcapture

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"factorybox/internal/fdstore"
	"golang.org/x/sys/unix"
)

type sourceMount struct{ root, point string }

func sourceMounts(pid string) (map[uint64]sourceMount, error) {
	file, err := os.Open("/proc/" + pid + "/mountinfo")
	if err != nil {
		return nil, err
	}
	defer file.Close()
	result := map[uint64]sourceMount{}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 4096), 1<<20)
	decode := strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 10 || len(result) >= 65536 {
			return nil, fmt.Errorf("source mount table exceeds its bound")
		}
		id, err := strconv.ParseUint(fields[0], 10, 64)
		if err != nil || id == 0 {
			return nil, fmt.Errorf("invalid source mount identity")
		}
		result[id] = sourceMount{decode.Replace(fields[3]), decode.Replace(fields[4])}
	}
	return result, scanner.Err()
}

func sharedDirectoryMapping(pid, fd int, source *SharedSource, id uint64) (fdstore.Directory, error) {
	var result fdstore.Directory
	worker, err := sourceMounts("self")
	if err != nil {
		return result, err
	}
	root, exists := worker[source.MountID]
	if !exists {
		return result, fmt.Errorf("canonical source mount disappeared")
	}
	relative, err := filepath.Rel(root.point, source.Source)
	if err != nil || relative == ".." || strings.HasPrefix(relative, "../") {
		return result, fmt.Errorf("canonical source is outside its pinned mount")
	}
	canonicalRoot := filepath.Join(root.root, relative)
	var extended unix.Statx_t
	if err := unix.Statx(fd, "", unix.AT_EMPTY_PATH, unix.STATX_MNT_ID, &extended); err != nil {
		return result, err
	}
	mounts, err := sourceMounts(strconv.Itoa(pid))
	if err != nil {
		return result, err
	}
	mount, exists := mounts[extended.Mnt_id]
	if !exists {
		return result, fmt.Errorf("source directory mount disappeared")
	}
	relative, err = filepath.Rel(canonicalRoot, mount.root)
	if err != nil || relative == ".." || strings.HasPrefix(relative, "../") {
		return result, fmt.Errorf("shared directory bind root is outside the canonical source")
	}
	return fdstore.Directory{Name: fmt.Sprintf("factorybox-dir:%d", id), MountPoint: mount.point, MountRootRelative: relative}, nil
}
