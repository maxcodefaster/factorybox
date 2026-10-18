package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type descendantPagerBinding struct {
	Layout                    descendantLayout
	Images                    string
	ImageDevice, ImageInode   uint64
	ImageUID, ImageGID        uint32
	KernelPID                 int
	KernelStart, HostCgroup   string
	DaemonStart               string
	CgroupDevice, CgroupInode uint64
	Socket                    descendantLeaseIdentity
	Bound                     bool
}

func descendantPreparePager(daemon *criuLazyDaemon, ownerID, images string) error {
	switch os.Getenv("RUNTIME_FORK_DESCENDANT_INHERIT_PROOF") {
	case "", "0":
		return nil
	case "1":
	default:
		return fmt.Errorf("invalid descendant inheritance proof setting")
	}
	if ownerID == "" {
		return nil // The original fresh-source contract remains unchanged.
	}
	layout, err := descendantOwnedLayout(ownerID)
	if err != nil || (ownerID != "copy-sysbox" && !descendantID.MatchString(ownerID)) {
		return fmt.Errorf("pager ownership is outside a canonical descendant: %v", err)
	}
	if !criuSysboxInheritEnabled() || !criuLazyWPEnabled() ||
		!descendantGraphWithin(layout.Root, images) || filepath.Clean(images) != images {
		return fmt.Errorf("descendant pager requires owned images and active inheritance/write tracking")
	}
	actual, err := filepath.EvalSymlinks(images)
	if err != nil || actual != images {
		return fmt.Errorf("descendant pager image path is not canonical: %v", err)
	}
	info, err := os.Lstat(images)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("descendant pager image directory is unavailable: %v", err)
	}
	saved := descendantLeaseFileIdentity(info)
	if saved.UID != 165536 || saved.GID != 165536 || info.Mode().Perm() != 0700 {
		return fmt.Errorf("descendant pager lost private mapped-root image ownership")
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return err
	}
	daemon.inheritSocket = filepath.Join(layout.Root, "i-"+hex.EncodeToString(nonce[:])+".sock")
	if len(daemon.inheritSocket) >= 108 {
		return fmt.Errorf("descendant pager endpoint exceeds the native socket limit")
	}
	daemon.owner = &descendantPagerBinding{
		Layout: layout, Images: images, ImageDevice: saved.Device, ImageInode: saved.Inode,
		ImageUID: saved.UID, ImageGID: saved.GID,
	}
	return nil
}

func descendantBindRestoredPager(daemon *criuLazyDaemon) error {
	binding := daemon.owner
	if binding == nil {
		return nil
	}
	if daemon.command == nil || daemon.command.Process == nil || daemon.pollCompletion() {
		return fmt.Errorf("descendant pager exited before ownership could be bound")
	}
	identity, err := criuRuntimeIdentity(binding.Layout.ID)
	if err != nil {
		return err
	}
	pid, ok := identity["init_pid"].(int)
	if !ok || pid <= 0 {
		return fmt.Errorf("descendant pager runtime identity has no native PID")
	}
	binding.KernelPID = pid
	binding.HostCgroup, _ = identity["host_cgroup"].(string)
	binding.KernelStart, err = criuSysboxThreadStart(pid)
	if err != nil {
		return err
	}
	binding.DaemonStart, err = criuSysboxThreadStart(daemon.command.Process.Pid)
	if err != nil {
		return err
	}
	cgroup, err := os.Lstat(binding.Layout.Cgroup)
	if err != nil || !cgroup.IsDir() {
		return fmt.Errorf("descendant pager cgroup anchor is unavailable: %v", err)
	}
	group := descendantLeaseFileIdentity(cgroup)
	binding.CgroupDevice, binding.CgroupInode = group.Device, group.Inode
	deadline := time.Now().Add(5 * time.Second)
	for {
		info, err := os.Lstat(daemon.inheritSocket)
		if err == nil {
			saved := descendantLeaseFileIdentity(info)
			if info.Mode()&os.ModeSocket == 0 || info.Mode().Perm() != 0600 ||
				saved.UID != uint32(os.Geteuid()) {
				return fmt.Errorf("descendant pager endpoint is not a private controller socket")
			}
			binding.Socket = saved
			break
		}
		if !os.IsNotExist(err) || daemon.pollCompletion() || time.Now().After(deadline) {
			return fmt.Errorf("descendant pager inheritance endpoint unavailable: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	for _, other := range criuLazyDaemons {
		if other != daemon && other.owner != nil && other.owner.Layout.ID == binding.Layout.ID &&
			!other.pollCompletion() {
			return fmt.Errorf("multiple live pagers claim the same descendant")
		}
	}
	binding.Bound = true
	if err := binding.verify(daemon, identity); err != nil {
		binding.Bound = false
		return err
	}
	record("sysbox-descendant-pager-identity-bound", time.Now(), asJSON(map[string]any{
		"runtime": binding.Layout, "images": binding.Images, "image_device": binding.ImageDevice,
		"image_inode": binding.ImageInode, "native_pid": binding.KernelPID,
		"native_start": binding.KernelStart, "daemon_pid": daemon.command.Process.Pid,
		"daemon_start": binding.DaemonStart, "endpoint": daemon.inheritSocket,
	}), nil)
	return nil
}

func (binding *descendantPagerBinding) verify(daemon *criuLazyDaemon, identity map[string]any) error {
	if !binding.Bound || daemon.owner != binding || daemon.command == nil || daemon.command.Process == nil ||
		daemon.pollCompletion() || identity["id"] != binding.Layout.ID ||
		identity["init_pid"] != binding.KernelPID || identity["host_cgroup"] != binding.HostCgroup {
		return fmt.Errorf("descendant pager does not own the current runtime identity")
	}
	for pid, expected := range map[int]string{
		binding.KernelPID: binding.KernelStart, daemon.command.Process.Pid: binding.DaemonStart,
	} {
		actual, err := criuSysboxThreadStart(pid)
		if err != nil || actual != expected {
			return fmt.Errorf("descendant pager process incarnation changed: %v", err)
		}
	}
	images, err := os.Lstat(binding.Images)
	if err != nil || !images.IsDir() {
		return fmt.Errorf("bound descendant image directory is unavailable: %v", err)
	}
	image := descendantLeaseFileIdentity(images)
	if image.Device != binding.ImageDevice || image.Inode != binding.ImageInode ||
		image.UID != binding.ImageUID || image.GID != binding.ImageGID || images.Mode().Perm() != 0700 {
		return fmt.Errorf("bound descendant image directory was replaced")
	}
	cgroup, err := os.Lstat(binding.Layout.Cgroup)
	if err != nil || !cgroup.IsDir() {
		return fmt.Errorf("bound descendant cgroup is unavailable: %v", err)
	}
	group := descendantLeaseFileIdentity(cgroup)
	if group.Device != binding.CgroupDevice || group.Inode != binding.CgroupInode {
		return fmt.Errorf("bound descendant cgroup was replaced")
	}
	endpoint, err := os.Lstat(daemon.inheritSocket)
	if err != nil || endpoint.Mode()&os.ModeSocket == 0 ||
		descendantLeaseFileIdentity(endpoint) != binding.Socket {
		return fmt.Errorf("bound descendant pager endpoint changed: %v", err)
	}
	return nil
}

func descendantBoundPager(layout descendantLayout, identity map[string]any) (*criuLazyDaemon, error) {
	var found *criuLazyDaemon
	for _, daemon := range criuLazyDaemons {
		if daemon.owner == nil || daemon.owner.Layout != layout || daemon.pollCompletion() {
			continue
		}
		if found != nil {
			return nil, fmt.Errorf("multiple live pagers own the descendant source")
		}
		if err := daemon.owner.verify(daemon, identity); err != nil {
			return nil, err
		}
		found = daemon
	}
	if found == nil {
		return nil, fmt.Errorf("no live pager owns this descendant source incarnation")
	}
	return found, nil
}

func descendantPagerEndpointOwned(daemon *criuLazyDaemon, images string) bool {
	return descendantInheritProofEnabled() && daemon.owner != nil &&
		daemon.owner.Bound && daemon.owner.Images == images && daemon.inheritSocket != ""
}
