//go:build linux

package sysboxcapture

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"factorybox/internal/fdstore"
	"golang.org/x/sys/unix"
	"google.golang.org/protobuf/encoding/protowire"
)

// RetainedFile binds a CRIU file-description ID to the original live kernel
// object. No file contents or path replacement grants that identity.
type RetainedFile struct {
	Name   string `json:"name"`
	Device uint64 `json:"device"`
	Inode  uint64 `json:"inode"`
}

type imageField struct {
	number  protowire.Number
	integer uint64
	data    []byte
}

func imageFields(data []byte) ([]imageField, error) {
	var fields []imageField
	for len(data) > 0 {
		number, kind, n := protowire.ConsumeTag(data)
		if n < 0 || number <= 0 || len(fields) >= 65536 {
			return nil, fmt.Errorf("invalid bounded CRIU protobuf")
		}
		data = data[n:]
		field := imageField{number: number}
		switch kind {
		case protowire.VarintType:
			field.integer, n = protowire.ConsumeVarint(data)
		case protowire.BytesType:
			field.data, n = protowire.ConsumeBytes(data)
		default:
			n = protowire.ConsumeFieldValue(number, kind, data)
		}
		if n < 0 {
			return nil, fmt.Errorf("invalid bounded CRIU field")
		}
		fields = append(fields, field)
		data = data[n:]
	}
	return fields, nil
}

func imageInteger(fields []imageField, number protowire.Number) uint64 {
	for _, field := range fields {
		if field.number == number {
			return field.integer
		}
	}
	return 0
}

func imageBytes(fields []imageField, number protowire.Number) []byte {
	for _, field := range fields {
		if field.number == number {
			return field.data
		}
	}
	return nil
}

func readImage(path string, magic uint32) ([][]imageField, error) {
	file, err := os.OpenFile(path, os.O_RDONLY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil || !stat.Mode().IsRegular() || stat.Size() > 32<<20 {
		return nil, fmt.Errorf("CRIU descriptor image exceeds its bound")
	}
	data, err := io.ReadAll(io.LimitReader(file, (32<<20)+1))
	if err != nil {
		return nil, err
	}
	if len(data) < 8 || len(data) > 32<<20 || binary.LittleEndian.Uint32(data) != 0x54564319 || binary.LittleEndian.Uint32(data[4:]) != magic {
		return nil, fmt.Errorf("CRIU descriptor image has an unknown pinned schema")
	}
	data = data[8:]
	var records [][]imageField
	for len(data) > 0 {
		if len(data) < 4 || len(records) >= 65536 {
			return nil, fmt.Errorf("CRIU descriptor record bound exceeded")
		}
		size := uint64(binary.LittleEndian.Uint32(data))
		data = data[4:]
		if size > uint64(len(data)) {
			return nil, fmt.Errorf("truncated CRIU descriptor record")
		}
		fields, err := imageFields(data[:size])
		if err != nil {
			return nil, err
		}
		records = append(records, fields)
		data = data[size:]
	}
	return records, nil
}

type retainedReference struct {
	id       uint64
	kind     uint64
	path     string
	flags    uint64
	required bool
}

func sharedReferences(images string, source *SharedSource, external map[string]bool) (map[uint64]retainedReference, error) {
	records, err := readImage(images+"/files.img", 0x56303138)
	if err != nil {
		return nil, err
	}
	result := map[uint64]retainedReference{}
	for _, record := range records {
		kind, id := imageInteger(record, 1), imageInteger(record, 2)
		if id == 0 || id > 1<<32-1 {
			return nil, fmt.Errorf("invalid CRIU file identity")
		}
		reference := retainedReference{id: id, kind: kind}
		if kind == 1 {
			regular, err := imageFields(imageBytes(record, 3))
			if err != nil {
				return nil, err
			}
			reference.path = "/" + strings.TrimPrefix(string(imageBytes(regular, 6)), "/")
			reference.flags = imageInteger(regular, 2)
			reference.required = reference.path == source.Destination || strings.HasPrefix(reference.path, source.Destination+"/") || external[strings.TrimPrefix(reference.path, "/")]
		} else if kind != 8 && kind != 12 {
			continue
		}
		if _, exists := result[id]; exists {
			return nil, fmt.Errorf("duplicated CRIU retained-file identity")
		}
		result[id] = reference
	}
	return result, nil
}

func imageProcess(pid, rootDepth int) (int, error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return 0, err
	}
	tgid := 0
	var ids []string
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[0] == "Tgid:" {
			tgid, _ = strconv.Atoi(fields[1])
		}
		if len(fields) > 1 && fields[0] == "NStgid:" {
			ids = fields[1:]
		}
	}
	if tgid != pid {
		return 0, nil
	}
	if rootDepth < 1 || len(ids) < rootDepth {
		return 0, fmt.Errorf("source has no original root PID namespace identity")
	}
	id, err := strconv.Atoi(ids[rootDepth-1])
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("invalid image process identity")
	}
	return id, nil
}

func processFDImage(images string, imagePID int) ([][]imageField, error) {
	ids, err := readImage(fmt.Sprintf("%s/ids-%d.img", images, imagePID), 0x54432030)
	if err != nil {
		return nil, err
	}
	if len(ids) != 1 {
		return nil, fmt.Errorf("CRIU process has no unique file-table identity")
	}
	filesID := imageInteger(ids[0], 2)
	if filesID == 0 || filesID > 1<<32-1 {
		return nil, fmt.Errorf("CRIU process has an invalid file-table identity")
	}
	return readImage(fmt.Sprintf("%s/fdinfo-%d.img", images, filesID), 0x56213732)
}

func sharedExternalIdentity(fd int, source *SharedSource) (string, error) {
	var stat unix.Stat_t
	var extended unix.Statx_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return "", err
	}
	if source == nil || uint64(stat.Dev) != source.Device || stat.Mode&unix.S_IFMT != unix.S_IFREG {
		return "", fmt.Errorf("retained external description is not an admitted source file")
	}
	if err := unix.Statx(fd, "", unix.AT_EMPTY_PATH, unix.STATX_MNT_ID, &extended); err != nil {
		return "", err
	}
	if extended.Mask&unix.STATX_MNT_ID == 0 || extended.Mnt_id == 0 {
		return "", fmt.Errorf("retained original source mount is unavailable")
	}
	return fmt.Sprintf("file[%x:%x]", extended.Mnt_id, stat.Ino), nil
}

// The source's original descriptions remain pinned in PID 1 while it runs.
// CRIU must treat those exact objects as external on the next dump: their
// original mount can be detached after the prior source namespace exited.
func previousSharedDescriptions(request Request) (string, map[string]bool, error) {
	external := map[string]bool{}
	if request.PreviousFDStoreKey == "" {
		return "", external, nil
	}
	if !strings.HasPrefix(request.PreviousFDStoreKey, request.Owner+":") || request.PreviousFDStoreKey == request.FDStoreKey {
		return "", nil, fmt.Errorf("previous shared descriptions belong to another source or generation")
	}
	names, files, err := fdstore.Get(request.FDStoreSocket, request.PreviousFDStoreKey)
	if err != nil {
		return "", nil, err
	}
	defer func() {
		for _, file := range files {
			file.Close()
		}
	}()
	for index, name := range names {
		if !strings.HasPrefix(name, "factorybox-reg:") {
			continue
		}
		key, err := sharedExternalIdentity(int(files[index].Fd()), request.SharedSource)
		if err != nil {
			return "", nil, err
		}
		external[key] = true
	}
	var keys []string
	for key := range external {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var configuration strings.Builder
	for _, key := range keys {
		fmt.Fprintf(&configuration, "external %s\n", key)
	}
	return configuration.String(), external, nil
}

func retainSharedFiles(request Request) ([]RetainedFile, error) {
	if request.SharedSource == nil {
		return nil, nil
	}
	if request.FDStoreSocket == "" || request.FDStoreKey == "" || len(request.SourceThreads) == 0 {
		return nil, fmt.Errorf("shared source requires its live worker descriptor store")
	}
	_, external, err := previousSharedDescriptions(request)
	if err != nil {
		return nil, err
	}
	references, err := sharedReferences(request.Generation+"/images", request.SharedSource, external)
	if err != nil {
		return nil, err
	}
	rootStatus, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", request.PID))
	if err != nil {
		return nil, err
	}
	rootDepth := 0
	for _, line := range strings.Split(string(rootStatus), "\n") {
		if strings.HasPrefix(line, "NStgid:") {
			rootDepth = len(strings.Fields(line)) - 1
		}
	}
	var threads []int
	for tid := range request.SourceThreads {
		threads = append(threads, tid)
	}
	sort.Ints(threads)
	files := map[uint64]*os.File{}
	directories := map[uint64]fdstore.Directory{}
	defer func() {
		for _, file := range files {
			file.Close()
		}
	}()
	retain := func(reference retainedReference, fd, pid int) error {
		file := os.NewFile(uintptr(fd), "original shared-source description")
		if _, exists := files[reference.id]; exists {
			return file.Close()
		}
		var stat unix.Stat_t
		if err := unix.Fstat(fd, &stat); err != nil {
			file.Close()
			return err
		}
		if reference.kind == 1 {
			if uint64(stat.Dev) != request.SharedSource.Device {
				file.Close()
				if reference.required {
					return fmt.Errorf("shared description crosses its admitted source filesystem")
				}
				return nil
			}
			if stat.Mode&unix.S_IFMT == unix.S_IFDIR {
				directory, err := sharedDirectoryMapping(pid, fd, request.SharedSource, reference.id)
				if err != nil {
					file.Close()
					return err
				}
				directories[reference.id] = directory
			} else if stat.Mode&unix.S_IFMT != unix.S_IFREG {
				file.Close()
				return fmt.Errorf("shared source description is neither a regular file nor directory")
			}
		}
		if external[strings.TrimPrefix(reference.path, "/")] {
			key, err := sharedExternalIdentity(fd, request.SharedSource)
			if err != nil || key != strings.TrimPrefix(reference.path, "/") {
				file.Close()
				return fmt.Errorf("original shared description differs from its pinned external identity: %v", err)
			}
		}
		files[reference.id] = file
		if len(files) > fdstore.MaximumFiles {
			return fmt.Errorf("shared source requires more than %d retained descriptions", fdstore.MaximumFiles)
		}
		return nil
	}
	for _, pid := range threads {
		birth, err := networkProcessStart(pid)
		if err != nil || birth != request.SourceThreads[pid] {
			return nil, fmt.Errorf("shared description process changed: %v", err)
		}
		imagePID, err := imageProcess(pid, rootDepth)
		if err != nil {
			return nil, err
		}
		if imagePID == 0 {
			continue
		}
		fdImage, err := processFDImage(request.Generation+"/images", imagePID)
		if err != nil {
			return nil, err
		}
		pidfd, err := unix.PidfdOpen(pid, 0)
		if err != nil {
			return nil, err
		}
		for _, record := range fdImage {
			reference, needed := references[imageInteger(record, 1)]
			if !needed || files[reference.id] != nil {
				continue
			}
			fd := imageInteger(record, 4)
			if imageInteger(record, 3) != reference.kind || fd > 1<<20 {
				unix.Close(pidfd)
				return nil, fmt.Errorf("saved shared description differs from its original FD table")
			}
			original, err := unix.PidfdGetfd(pidfd, int(fd), 0)
			if err != nil {
				unix.Close(pidfd)
				return nil, err
			}
			if err := retain(reference, original, pid); err != nil {
				unix.Close(pidfd)
				return nil, err
			}
		}
		unix.Close(pidfd)
		fsImage, err := readImage(fmt.Sprintf("%s/images/fs-%d.img", request.Generation, imagePID), 0x51403912)
		if err != nil {
			return nil, err
		}
		if len(fsImage) != 1 {
			return nil, fmt.Errorf("source has no unique working directory image")
		}
		for index, name := range []string{"cwd", "root"} {
			reference, needed := references[imageInteger(fsImage[0], protowire.Number(index+1))]
			if !needed || files[reference.id] != nil {
				continue
			}
			original, err := unix.Open(fmt.Sprintf("/proc/%d/%s", pid, name), unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
			if err != nil {
				return nil, err
			}
			if err := retain(reference, original, pid); err != nil {
				return nil, err
			}
		}
		mmImage, err := readImage(fmt.Sprintf("%s/images/mm-%d.img", request.Generation, imagePID), 0x57492820)
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		for _, record := range mmImage {
			for _, field := range record {
				if field.number != 14 {
					continue
				}
				vma, err := imageFields(field.data)
				if err != nil {
					return nil, err
				}
				reference, needed := references[imageInteger(vma, 4)]
				if !needed || reference.kind != 1 || files[reference.id] != nil {
					continue
				}
				path := fmt.Sprintf("/proc/%d/map_files/%x-%x", pid, imageInteger(vma, 1), imageInteger(vma, 2))
				original, err := unix.Open(path, int(reference.flags)&unix.O_ACCMODE|unix.O_CLOEXEC, 0)
				if err != nil {
					return nil, err
				}
				if err := retain(reference, original, pid); err != nil {
					return nil, err
				}
			}
		}
	}
	var ids []uint64
	for id, reference := range references {
		if files[id] == nil {
			if reference.kind != 1 || reference.required {
				return nil, fmt.Errorf("CRIU shared description %d has no retained original kernel object", id)
			}
			continue
		}
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	names := make([]string, 0, len(ids))
	var originals []*os.File
	var retained []RetainedFile
	var directoryMappings []fdstore.Directory
	for _, id := range ids {
		file := files[id]
		if file == nil {
			return nil, fmt.Errorf("CRIU shared description %d has no retained original kernel object", id)
		}
		kind := map[uint64]string{1: "reg", 8: "inotify", 12: "fanotify"}[references[id].kind]
		name := fmt.Sprintf("factorybox-%s:%d", kind, id)
		if directory, exists := directories[id]; exists {
			name = directory.Name
			directoryMappings = append(directoryMappings, directory)
		}
		var stat unix.Stat_t
		if err := unix.Fstat(int(file.Fd()), &stat); err != nil {
			return nil, err
		}
		retained = append(retained, RetainedFile{Name: name, Device: uint64(stat.Dev), Inode: stat.Ino})
		names = append(names, name)
		originals = append(originals, file)
	}
	if len(directoryMappings) > 0 {
		rootFD, err := unix.Open(request.SharedSource.Source, unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		if err != nil {
			return nil, err
		}
		root := os.NewFile(uintptr(rootFD), "canonical source root")
		defer root.Close()
		var stat unix.Stat_t
		if err := unix.Fstat(rootFD, &stat); err != nil {
			return nil, err
		}
		if uint64(stat.Dev) != request.SharedSource.Device || stat.Ino != request.SharedSource.Inode {
			return nil, fmt.Errorf("canonical source root changed during capture")
		}
		names = append(names, fdstore.SourceRootName)
		originals = append(originals, root)
		retained = append(retained, RetainedFile{Name: fdstore.SourceRootName, Device: uint64(stat.Dev), Inode: stat.Ino})
	}
	if err := fdstore.PutDirectories(request.FDStoreSocket, request.FDStoreKey, names, originals, directoryMappings); err != nil {
		return nil, err
	}
	return retained, nil
}

func VerifyRetainedFiles(socket, key string, expected []RetainedFile) error {
	names, files, err := fdstore.Get(socket, key)
	if err != nil {
		return err
	}
	defer func() {
		for _, file := range files {
			file.Close()
		}
	}()
	if len(names) != len(expected) {
		return fmt.Errorf("worker retained-file generation changed")
	}
	for index, file := range files {
		var stat unix.Stat_t
		if err := unix.Fstat(int(file.Fd()), &stat); err != nil {
			return err
		}
		if names[index] != expected[index].Name || uint64(stat.Dev) != expected[index].Device || stat.Ino != expected[index].Inode {
			return fmt.Errorf("worker retained-file identity changed")
		}
	}
	return fdstore.VerifyRestore(socket, key)
}

func sharedFDConfiguration(socket, key string) (string, error) {
	if !cleanAbsolute(socket) || len(socket) >= 108 || filepath.Base(socket) != "retained-fds.sock" || strings.ContainsAny(socket, "| \t\r\n") || len(key) != 65 {
		return "", fmt.Errorf("invalid private worker retained-file configuration")
	}
	return "factorybox-fdstore " + socket + "|" + key + "\n", nil
}
