//go:build linux

// Package sysboxcapture keeps the reference's pinned modern libcontainer
// checkpoint implementation behind a Factorybox adapter. It never destroys or
// rewrites the source Sysbox runtime state.
package sysboxcapture

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"factorybox/internal/fdstore"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	_ "github.com/opencontainers/cgroups/devices"
	"github.com/opencontainers/runc/libcontainer"
	"golang.org/x/sys/unix"
)

type Request struct {
	RuntimeRoot        string
	RuntimeID          string
	Owner              string
	Rootfs             string
	RuntimeCreated     string
	PID                int
	Generation         string
	Criu               string
	Bundle             string
	ManagerRoot        string
	Base               *FilesystemTree
	SharedSource       *SharedSource
	FDStoreSocket      string
	FDStoreKey         string
	PreviousFDStoreKey string
	SourceThreads      map[int]string
	ProcessCaptured    func(Result) error
}

type Result struct {
	SourcePID       int                 `json:"source_pid"`
	MappedUID       int                 `json:"mapped_uid"`
	MappedGID       int                 `json:"mapped_gid"`
	SourceStateSHA  string              `json:"source_state_sha256"`
	Images          string              `json:"images"`
	LeaveRunning    bool                `json:"leave_running"`
	CompleteWriter  bool                `json:"complete_writer"`
	SnapshotEntries int                 `json:"snapshot_entries"`
	Filesystem      *FilesystemSnapshot `json:"filesystem,omitempty"`
}

func sha(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

func writePrivate(path string, data []byte) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(data)
	return errors.Join(writeErr, file.Sync(), file.Close())
}

// Capture uses a private read-only state view to supply a per-capture CRIU
// configuration. The original Sysbox state, registration and ownership remain
// under Sysbox. The caller must hold and validate the source guard throughout.
func Capture(request Request) (result Result, resultErr error) {
	result = Result{SourcePID: request.PID, Images: request.Generation + "/images",
		LeaveRunning: true, CompleteWriter: true}
	if request.SharedSource != nil {
		defer func() {
			if resultErr != nil {
				resultErr = errors.Join(resultErr, fdstore.Release(request.FDStoreSocket, request.FDStoreKey))
			}
		}()
	}
	container, err := libcontainer.Load(request.RuntimeRoot, request.RuntimeID)
	if err != nil {
		return result, err
	}
	status, err := container.Status()
	if err != nil || status != libcontainer.Paused {
		return result, fmt.Errorf("capture requires the frozen source: %v: %v", status, err)
	}
	state, err := container.State()
	if err != nil {
		return result, err
	}
	config := container.Config()
	checkpointConfiguration, err := checkpointConfigurationFromLabels(config.Labels)
	if err != nil {
		return result, err
	}
	if request.SharedSource != nil {
		extra, err := sharedFDConfiguration(request.FDStoreSocket, request.FDStoreKey)
		if err != nil {
			return result, err
		}
		checkpointConfiguration = append(checkpointConfiguration, extra...)
		external, _, err := previousSharedDescriptions(request)
		if err != nil {
			return result, err
		}
		checkpointConfiguration = append(checkpointConfiguration, external...)
	}
	owner := ""
	for _, label := range config.Labels {
		if strings.HasPrefix(label, "io.factorybox.owner=") {
			owner = strings.TrimPrefix(label, "io.factorybox.owner=")
		}
	}
	if request.RuntimeID != "fbx-"+request.Owner || owner != request.Owner ||
		state.InitProcessPid != request.PID || config.Rootfs != request.Rootfs ||
		state.Created.Format("2006-01-02T15:04:05.999999999Z07:00") != request.RuntimeCreated ||
		config.Cgroups == nil || config.Cgroups.Path != "/"+request.RuntimeID {
		return result, fmt.Errorf("capture source runtime identity differs")
	}
	for _, namespace := range config.Namespaces {
		if namespace.Path != "" {
			return result, fmt.Errorf("capture cannot retain an external namespace")
		}
	}
	if len(config.UIDMappings) != 1 || len(config.GIDMappings) != 1 ||
		config.UIDMappings[0].ContainerID != 0 || config.GIDMappings[0].ContainerID != 0 ||
		config.UIDMappings[0].HostID <= 0 || config.GIDMappings[0].HostID <= 0 {
		return result, fmt.Errorf("capture requires the saved single root ID mappings")
	}
	result.MappedUID, result.MappedGID = int(config.UIDMappings[0].HostID), int(config.GIDMappings[0].HostID)

	path := filepath.Join(request.RuntimeRoot, request.RuntimeID, "state.json")
	original, err := os.ReadFile(path)
	if err != nil {
		return result, err
	}
	result.SourceStateSHA = sha(original)
	if err := writePrivate(request.Generation+"/source-state.json", original); err != nil {
		return result, err
	}
	filesystem, err := planFilesystem(request, original)
	if err != nil {
		return result, err
	}
	// Only this private copy gets the RPC configuration annotation. Load()
	// presents a read-only view, and successful dump does not replace the
	// original Sysbox state with the modern library's configuration schema.
	shadow, err := stateWithConfig(original, request.Generation+"/criu.conf")
	if err != nil {
		return result, err
	}
	for _, directory := range []string{request.Generation + "/runtime",
		request.Generation + "/runtime/" + request.RuntimeID} {
		if err := os.Mkdir(directory, 0700); err != nil {
			return result, err
		}
	}
	if err := writePrivate(request.Generation+"/runtime/"+request.RuntimeID+"/state.json", shadow); err != nil {
		return result, err
	}
	if err := writePrivate(request.Generation+"/criu.conf", checkpointConfiguration); err != nil {
		return result, err
	}
	if err := os.Mkdir(result.Images, 0700); err != nil {
		return result, err
	}
	if err := os.Chown(result.Images, result.MappedUID, result.MappedGID); err != nil {
		return result, err
	}
	container, err = libcontainer.Load(request.Generation+"/runtime", request.RuntimeID)
	if err != nil {
		return result, err
	}
	configData, err := json.Marshal(container.Config())
	if err != nil {
		return result, err
	}
	if err := writePrivate(result.Images+"/runtime-config.json", configData); err != nil {
		return result, err
	}
	if err := os.Setenv("SYSBOX_FORK_COLLECT_SCOPE", "1"); err != nil {
		return result, err
	}
	// The pinned reference library invokes "criu". The dedicated helper's
	// parent verifies the executable and every directory in this search path.
	if err := os.Setenv("PATH", filepath.Dir(request.Criu)+":/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"); err != nil {
		return result, err
	}
	options := &libcontainer.CriuOpts{
		ImagesDirectory: result.Images, WorkDirectory: result.Images,
		EmptyNs: 0, ShellJob: true, FileLocks: true, TcpEstablished: true,
		LeaveRunning: true, ManageCgroupsMode: "full",
	}
	if err := container.Checkpoint(options); err != nil {
		return result, err
	}
	retained, err := retainSharedFiles(request)
	if err != nil {
		return result, err
	}
	after, err := os.ReadFile(path)
	if err != nil || sha(after) != result.SourceStateSHA {
		return result, fmt.Errorf("capture changed the original Sysbox state: %v", err)
	}
	// The process-changing CRIU operation has completed and been waited.
	// Later work only copies/verifies filesystem backing and owns images.
	if request.ProcessCaptured != nil {
		if err := request.ProcessCaptured(result); err != nil {
			return result, err
		}
	}
	result.Filesystem, err = captureFilesystem(filesystem, request.Generation+"/filesystem")
	if err != nil {
		return result, err
	}
	result.Filesystem.SharedSource = request.SharedSource
	if request.SharedSource != nil {
		result.Filesystem.FDStoreKey, result.Filesystem.RetainedFiles = request.FDStoreKey, retained
	}
	filesystemData, err := json.MarshalIndent(result.Filesystem, "", "  ")
	if err != nil {
		return result, err
	}
	if err := writePrivate(request.Generation+"/filesystem.json", append(filesystemData, '\n')); err != nil {
		return result, err
	}
	generation, err := os.Open(request.Generation)
	if err != nil {
		return result, err
	}
	if err := errors.Join(generation.Sync(), generation.Close()); err != nil {
		return result, err
	}
	result.SnapshotEntries, err = ownImages(result.Images, result.MappedUID, result.MappedGID)
	if err != nil {
		return result, err
	}
	directory, err := os.Open(result.Images)
	if err != nil {
		return result, err
	}
	return result, errors.Join(directory.Sync(), directory.Close())
}

func stateWithConfig(data []byte, configPath string) ([]byte, error) {
	var state map[string]json.RawMessage
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, err
	}
	var config map[string]json.RawMessage
	if err := json.Unmarshal(state["config"], &config); err != nil {
		return nil, err
	}
	var labels []string
	if err := json.Unmarshal(config["labels"], &labels); err != nil {
		return nil, err
	}
	filtered := make([]string, 0, len(labels)+1)
	for _, label := range labels {
		if !strings.HasPrefix(label, "org.criu.config=") {
			filtered = append(filtered, label)
		}
	}
	filtered = append(filtered, "org.criu.config="+configPath)
	config["labels"], _ = json.Marshal(filtered)
	state["config"], _ = json.Marshal(config)
	return json.Marshal(state)
}

func ownImages(path string, uid, gid int) (int, error) {
	return ownImagesWithBarrier(path, uid, gid, unix.Syncfs)
}

func ownImagesWithBarrier(path string, uid, gid int, barrier func(int) error) (int, error) {
	directory, err := openDirectory(path)
	if err != nil {
		return 0, err
	}
	defer directory.Close()
	entries, err := directory.ReadDir(-1)
	if err != nil {
		return 0, err
	}
	for _, entry := range entries {
		fd, err := unix.Openat2(int(directory.Fd()), entry.Name(), &unix.OpenHow{
			Flags:   unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK,
			Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_XDEV,
		})
		if err != nil {
			return 0, err
		}
		var stat unix.Stat_t
		err = unix.Fstat(fd, &stat)
		if err == nil && (stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Nlink != 1) {
			err = fmt.Errorf("complete snapshot entry is not an independent regular file: %s", entry.Name())
		}
		if err == nil {
			err = unix.Fchown(fd, uid, gid)
		}
		if err == nil {
			err = unix.Fchmod(fd, 0600)
		}
		closeErr := unix.Close(fd)
		if err := errors.Join(err, closeErr); err != nil {
			return 0, err
		}
	}
	// Every image is an independent regular file on this directory's
	// filesystem. Persist all payload/ownership changes together instead of
	// forcing its journal separately for every small CRIU image.
	if err := barrier(int(directory.Fd())); err != nil {
		return 0, fmt.Errorf("persist complete image generation: %w", err)
	}
	if err := directory.Sync(); err != nil {
		return 0, err
	}
	return len(entries), nil
}

// CheckExecutableParents protects the PATH lookup used by the pinned library.
// The executable itself must also be verified by the caller through its FD.
func CheckExecutableParents(path string) error {
	if filepath.Base(path) != "criu" || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return fmt.Errorf("CRIU must have its canonical executable name in an absolute path")
	}
	for directory := filepath.Dir(path); ; directory = filepath.Dir(directory) {
		info, err := os.Lstat(directory)
		if err != nil {
			return err
		}
		stat := info.Sys().(*syscall.Stat_t)
		if !info.IsDir() || stat.Uid != 0 || info.Mode().Perm()&0022 != 0 {
			return fmt.Errorf("CRIU executable directory permits a different writer: %s", directory)
		}
		if directory == "/" {
			return nil
		}
	}
}
