//go:build linux

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	criu "github.com/checkpoint-restore/go-criu/v4/rpc"
	"github.com/nestybox/sysbox-ipc/sysboxFsGrpc"
	ipc "github.com/nestybox/sysbox-ipc/sysboxMgrLib"
	sh "github.com/nestybox/sysbox-libs/idShiftUtils"
	"github.com/opencontainers/runc/libcontainer"
	"github.com/opencontainers/runc/libcontainer/configs"
	"github.com/opencontainers/runc/libsysbox/sysbox"
	specs "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/urfave/cli"
	"golang.org/x/sys/unix"
)

// Deliberately restricted to the two owned fixture identities. The caller
// clones the upper/lower layers and images while the original tree is stopped.
var experimentCopyCommand = cli.Command{
	Name:  "experiment-restore-copy",
	Usage: "restore the stopped Sysbox fixture into an independent identity",
	Flags: []cli.Flag{cli.StringFlag{Name: "image-path"},
		cli.StringFlag{Name: "copy-id", Value: "copy-sysbox"},
		cli.StringFlag{Name: "copy-dir", Value: "/experiment/sysbox-copy"}},
	Action: func(context *cli.Context) (resultErr error) {
		if err := checkArgs(context, 1, exactArgs); err != nil {
			return err
		}
		plan, err := experimentOwnedForkPlan(context.Args().First(), context.String("copy-id"),
			context.String("copy-dir"), context.String("image-path"))
		if err != nil || context.GlobalString("root") != "/experiment/runtime" {
			return fmt.Errorf("copy is outside the owned fixture")
		}
		sourceID, copyID := plan.Source.ID, plan.Target.ID
		rootfs := plan.Target.Root + "/rootfs"
		timing := newExperimentCopyTiming("copy-parent")
		complete := false
		defer timing.finish(&complete)
		source, err := getContainer(context)
		if err != nil {
			return err
		}
		status, err := source.Status()
		if err != nil {
			return err
		}
		if err := experimentCopySourceStatus(status, plan.Source.Cgroup); err != nil {
			return err
		}
		data, err := json.Marshal(source.Config())
		if err != nil {
			return err
		}
		var config configs.Config
		if err := json.Unmarshal(data, &config); err != nil {
			return err
		}
		if config.Rootfs != plan.Source.Root+"/rootfs" || config.RootfsUidShiftType != sh.IDMappedMount || config.RootfsCloned ||
			config.Cgroups == nil || filepath.Base(config.Cgroups.Path) != plan.Source.Cgroup ||
			filepath.Clean(config.Cgroups.Path) != config.Cgroups.Path ||
			len(config.Hooks) != 0 || len(config.ShiftfsMounts) != 0 {
			return fmt.Errorf("unsupported source configuration: rootfs=%s shift=%v cloned=%v cgroups=%+v hooks=%v shiftfs=%v", config.Rootfs, config.RootfsUidShiftType, config.RootfsCloned, config.Cgroups, config.Hooks, config.ShiftfsMounts)
		}
		for _, ns := range config.Namespaces {
			if ns.Path != "" {
				return fmt.Errorf("fork cannot join saved namespace %s", ns.Type)
			}
		}
		if len(config.UidMappings) != 1 || len(config.GidMappings) != 1 || config.UidMappings[0].ContainerID != 0 || config.GidMappings[0].ContainerID != 0 {
			return fmt.Errorf("fork requires the saved single root mappings")
		}
		config.Rootfs = rootfs
		config.Cgroups.Path = filepath.Join(filepath.Dir(config.Cgroups.Path), plan.Target.Cgroup)
		if status == libcontainer.Paused {
			if config.Cgroups.Resources == nil || config.Cgroups.Unified["cgroup.freeze"] != "" {
				return fmt.Errorf("unsupported frozen source resources")
			}
			// Pause updates the source's saved resource configuration. The child
			// must run in its own thawed cgroup while the source stays frozen.
			config.Cgroups.Freezer = configs.Thawed
		}
		sbox := sysbox.NewSysbox(copyID, true, true)
		sbox.RootfsUidShiftType, sbox.BindMntUidShiftType = config.RootfsUidShiftType, config.BindMntUidShiftType
		sbox.OrigRootfs = rootfs
		if _, err := os.Lstat(filepath.Join(context.GlobalString("root"), copyID)); !os.IsNotExist(err) {
			return fmt.Errorf("copy runtime identity already exists or is inaccessible: %v", err)
		}
		uid, gid := config.UidMappings[0], config.GidMappings[0]
		spec := &specs.Spec{Root: &specs.Root{Path: rootfs}, Linux: &specs.Linux{
			UIDMappings: []specs.LinuxIDMapping{{ContainerID: uint32(uid.ContainerID), HostID: uint32(uid.HostID), Size: uint32(uid.Size)}},
			GIDMappings: []specs.LinuxIDMapping{{ContainerID: uint32(gid.ContainerID), HostID: uint32(gid.HostID), Size: uint32(gid.Size)}},
		}}
		timing.mark("load-and-validate-source-config")
		if err := sbox.Mgr.Register(spec); err != nil {
			return err
		}
		timing.mark("manager-register")
		defer func() {
			if !complete {
				fsErr := sbox.Fs.Unregister()
				mgrErr := sbox.Mgr.Unregister()
				resultErr = errors.Join(resultErr, fsErr, mgrErr)
				if os.Getenv("RUNTIME_FORK_COPY_PIPELINE") == "1" {
					data, _ := json.Marshal(map[string]bool{
						"fs_unregistered": fsErr == nil, "manager_unregistered": mgrErr == nil,
					})
					fmt.Fprintf(os.Stderr, "RUNTIME_FORK_COPY_PIPELINE_CLEANUP %s\n", data)
				}
			}
		}()
		if err := sbox.Fs.GetConfig(); err != nil {
			return err
		}
		timing.mark("fs-config")
		if sbox.Fs.Mountpoint != "/experiment/sysboxfs" {
			return fmt.Errorf("unexpected Sysbox FUSE root")
		}
		if err := sysboxFsGrpc.SendContainerPreRegistration(&sysboxFsGrpc.ContainerData{Id: copyID, ForkSourceId: sourceID}); err != nil {
			return err
		}
		sbox.Fs.PreReg = true
		timing.mark("fs-pre-register")
		if err := experimentCopyPipelineRegistered(40 * time.Second); err != nil {
			return err
		}
		if err := experimentCopyMounts(&config, sbox, sourceID); err != nil {
			return err
		}
		timing.mark("manager-mounts-and-volume-copy")
		if err := experimentCopyPipelineBarrier(40 * time.Second); err != nil {
			return err
		}
		timing.mark("await-complete-images")
		// Fs mounts in the service's shared controller namespace. Create the
		// private restore namespace only after pre-registration completes, so
		// the child inherits every new FUSE mount. Pass metadata via sealed FD.
		if err := experimentLaunchPreparedCopy(context, &config, sbox, plan); err != nil {
			return err
		}
		timing.mark("launch-and-wait-prepared-child")
		complete = true
		return nil
	},
}

type experimentPreparedCopy struct {
	Config configs.Config
	Sysbox *sysbox.Sysbox
	Plan   experimentForkPlan
}

func experimentLaunchPreparedCopy(context *cli.Context, config *configs.Config, sbox *sysbox.Sysbox, plan experimentForkPlan) error {
	fd, err := unix.MemfdCreate("sysbox-prepared-copy", unix.MFD_CLOEXEC|unix.MFD_ALLOW_SEALING)
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(fd), "sysbox-prepared-copy")
	defer file.Close()
	if err := json.NewEncoder(file).Encode(experimentPreparedCopy{Config: *config, Sysbox: sbox, Plan: plan}); err != nil {
		return err
	}
	if _, err := file.Seek(0, 0); err != nil {
		return err
	}
	if _, err := unix.FcntlInt(file.Fd(), unix.F_ADD_SEALS, unix.F_SEAL_WRITE|unix.F_SEAL_GROW|unix.F_SEAL_SHRINK|unix.F_SEAL_SEAL); err != nil {
		return err
	}
	cmd := exec.Command("unshare", "--mount", "--propagation", "private", os.Args[0],
		"--criu", context.GlobalString("criu"), "--root", context.GlobalString("root"), "experiment-restore-prepared-copy", plan.Source.ID)
	cmd.ExtraFiles = []*os.File{file}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}

// Private child of experiment-restore-copy; no pathname-based preparation.
var experimentPreparedCopyCommand = cli.Command{
	Name: "experiment-restore-prepared-copy",
	Action: func(context *cli.Context) error {
		if err := checkArgs(context, 1, exactArgs); err != nil {
			return err
		}
		if context.GlobalString("root") != "/experiment/runtime" {
			return fmt.Errorf("prepared copy is outside the owned fixture")
		}
		timing := newExperimentCopyTiming("prepared-child")
		complete := false
		defer timing.finish(&complete)
		file := os.NewFile(3, "sysbox-prepared-copy")
		defer file.Close()
		seals, err := unix.FcntlInt(file.Fd(), unix.F_GET_SEALS, 0)
		if err != nil || seals&(unix.F_SEAL_WRITE|unix.F_SEAL_GROW|unix.F_SEAL_SHRINK|unix.F_SEAL_SEAL) != (unix.F_SEAL_WRITE|unix.F_SEAL_GROW|unix.F_SEAL_SHRINK|unix.F_SEAL_SEAL) {
			return fmt.Errorf("missing sealed preparation descriptor: %v", err)
		}
		var prepared experimentPreparedCopy
		if err := json.NewDecoder(file).Decode(&prepared); err != nil {
			return err
		}
		plan, err := experimentOwnedForkPlan(prepared.Plan.Source.ID, prepared.Plan.Target.ID,
			prepared.Plan.Target.Root, prepared.Plan.Images)
		if err != nil || plan != prepared.Plan || context.Args().First() != plan.Source.ID {
			return fmt.Errorf("invalid sealed lineage plan")
		}
		config, sbox := prepared.Config, prepared.Sysbox
		if sbox == nil || sbox.Id != plan.Target.ID || sbox.Mgr == nil || sbox.Mgr.Id != sbox.Id || !sbox.Mgr.Enabled() ||
			sbox.Fs == nil || sbox.Fs.Id != sbox.Id || !sbox.Fs.Enabled() || !sbox.Fs.PreReg || sbox.Fs.Reg || config.Rootfs != plan.Target.Root+"/rootfs" ||
			config.Cgroups == nil || filepath.Base(config.Cgroups.Path) != plan.Target.Cgroup {
			return fmt.Errorf("invalid prepared copy identity")
		}
		timing.mark("decode-and-validate-prepared-config")
		source, err := getContainer(context)
		if err != nil {
			return err
		}
		status, err := source.Status()
		if err != nil {
			return err
		}
		if err := experimentCopySourceStatus(status, plan.Source.Cgroup); err != nil {
			return err
		}
		timing.mark("reload-and-validate-frozen-source")
		images := plan.Images
		if err := libcontainer.ExperimentRestoreRoot(config, images); err != nil {
			return err
		}
		timing.mark("rebuild-idmapped-rootfs")
		factory, err := loadFactory(context, sbox)
		if err != nil {
			return err
		}
		copy, err := factory.Create(sbox.Id, &config)
		if err != nil {
			return err
		}
		timing.mark("factory-create")
		defer func() {
			if !complete {
				_ = copy.Destroy()
			}
		}()
		options := &libcontainer.CriuOpts{SysboxSeccompRestore: true, ImagesDirectory: images, WorkDirectory: images,
			ShellJob: true, FileLocks: true, EmptyNs: 0, ManageCgroupsMode: criu.CriuCgMode_FULL,
			TcpEstablished: os.Getenv("CRIU_SYSBOX_TOOLS") == "1"}
		process := &libcontainer.Process{Init: true, Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr}
		if err := copy.Restore(process, options); err != nil {
			return err
		}
		timing.mark("libcontainer-restore")
		state, err := copy.State()
		if err != nil {
			return err
		}
		timing.mark("read-restored-state")
		complete = true
		return json.NewEncoder(os.Stdout).Encode(state)
	},
}

func experimentCopySourceStatus(status libcontainer.Status, cgroup string) error {
	if status == libcontainer.Stopped {
		return nil
	}
	if os.Getenv("CRIU_SYSBOX_RETAIN_SOURCE") != "1" || status != libcontainer.Paused {
		return fmt.Errorf("fork source must be stopped or explicitly frozen: %v", status)
	}
	path := filepath.Join("/sys/fs/cgroup", cgroup)
	freeze, err := os.ReadFile(path + "/cgroup.freeze")
	if err != nil || strings.TrimSpace(string(freeze)) != "1" {
		return fmt.Errorf("source freeze was not requested: %v", err)
	}
	events, err := os.ReadFile(path + "/cgroup.events")
	if err != nil || !strings.Contains("\n"+string(events), "\nfrozen 1\n") || !strings.Contains("\n"+string(events), "\npopulated 1\n") {
		return fmt.Errorf("kernel has not frozen the populated source cgroup: %v", err)
	}
	return nil
}

func experimentCopyMounts(config *configs.Config, sbox *sysbox.Sysbox, sourceID string) error {
	timing := newExperimentCopyTiming("manager-volumes")
	complete := false
	defer timing.finish(&complete)
	const manager = "/experiment/sysbox-mgr/"
	kinds := map[string]ipc.MntKind{
		"/var/lib/docker": ipc.MntVarLibDocker, "/var/lib/kubelet": ipc.MntVarLibKubelet,
		"/var/lib/k0s": ipc.MntVarLibK0s, "/var/lib/rancher/k3s": ipc.MntVarLibRancherK3s,
		"/var/lib/rancher/rke2": ipc.MntVarLibRancherRke2, "/var/lib/buildkit": ipc.MntVarLibBuildkit,
		"/var/lib/containerd/io.containerd.snapshotter.v1.overlayfs": ipc.MntVarLibContainerdOvfs,
	}
	var reqs []ipc.MountReqInfo
	for _, mount := range config.Mounts {
		if mount.Device != "bind" || !strings.HasPrefix(mount.Source, manager) {
			continue
		}
		kind, ok := kinds[mount.Destination]
		if !ok || !mount.IDMappedMount || filepath.Base(mount.Source) != sourceID || filepath.Clean(mount.Source) != mount.Source {
			return fmt.Errorf("unsupported manager mount: %s -> %s", mount.Source, mount.Destination)
		}
		reqs = append(reqs, ipc.MountReqInfo{Kind: kind, Dest: mount.Destination})
	}
	volumes, err := sbox.Mgr.ReqMounts(config.RootfsUidShiftType, reqs)
	if err != nil {
		return err
	}
	timing.mark("request-manager-mounts")
	byDestination := map[string]string{}
	for _, volume := range volumes {
		if strings.HasPrefix(volume.Source, manager) {
			byDestination[volume.Destination] = volume.Source
		}
	}
	fsSource := sbox.Fs.Mountpoint + "/" + sourceID + "/"
	for _, mount := range config.Mounts {
		if mount.Device != "bind" {
			continue
		}
		switch {
		case strings.HasPrefix(mount.Source, manager):
			target, ok := byDestination[mount.Destination]
			if !ok || target != filepath.Join(filepath.Dir(mount.Source), sbox.Id) {
				return fmt.Errorf("manager returned an unexpected fork volume")
			}
			// Reflinks preserve independent inodes; never hard-link writable files.
			out, err := exec.Command("cp", "-a", "--reflink=auto", "--", mount.Source+"/.", target).CombinedOutput()
			if err != nil {
				return fmt.Errorf("clone manager volume: %s: %w", out, err)
			}
			timing.mark("copy-" + mount.Destination)
			mount.Source = target
		case strings.HasPrefix(mount.Source, fsSource):
			mount.Source = sbox.Fs.Mountpoint + "/" + sbox.Id + "/" + strings.TrimPrefix(mount.Source, fsSource)
		case mount.Source == "/dev/null":
		default:
			return fmt.Errorf("unsupported external bind source %s", mount.Source)
		}
	}
	complete = true
	return nil
}
