package main

import (
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

type descendantGraphFile struct {
	Path     string
	Identity descendantLeaseIdentity
	Lease    *descendantFileLease
}

type descendantImageNode struct {
	Path, Target, ParentLink string
	Identity                 descendantLeaseIdentity
	Files                    []descendantGraphFile
	Parent                   *descendantImageNode
}

func descendantGraphWithin(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, "../") && !filepath.IsAbs(relative)
}

func descendantCloneImageGraph(head, target string) (_ map[string]any, resultErr error) {
	return descendantCloneImageGraphPolicy(head, target, false)
}

func descendantCloneImageGraphPolicy(head, target string, readonlyRAMParents bool) (_ map[string]any, resultErr error) {
	if filepath.Clean(head) != head || filepath.Clean(target) != target ||
		!strings.HasPrefix(head, "/experiment/") || !strings.HasPrefix(target, "/experiment/") {
		return nil, fmt.Errorf("image graph paths are outside the owned experiment")
	}
	sourceRoot := filepath.Dir(head)
	actualHead, err := filepath.EvalSymlinks(head)
	if err != nil || actualHead != head {
		return nil, fmt.Errorf("image graph source path is not canonical: %v", err)
	}
	actualParent, err := filepath.EvalSymlinks(filepath.Dir(target))
	if err != nil || actualParent != filepath.Dir(target) {
		return nil, fmt.Errorf("image graph target parent is not canonical: %v", err)
	}
	if descendantGraphWithin(sourceRoot, target) {
		return nil, fmt.Errorf("image graph target is inside its source")
	}
	rootInfo, err := os.Lstat(head)
	if err != nil || !rootInfo.IsDir() {
		return nil, fmt.Errorf("image graph head is not a real directory: %v", err)
	}
	owner := descendantLeaseFileIdentity(rootInfo)
	nodes, visiting := map[string]*descendantImageNode{}, map[string]bool{}
	var order []*descendantImageNode
	var discover func(string) (*descendantImageNode, error)
	discover = func(path string) (*descendantImageNode, error) {
		if !descendantGraphWithin(sourceRoot, path) {
			return nil, fmt.Errorf("image graph parent escaped its owned root")
		}
		if visiting[path] {
			return nil, fmt.Errorf("image graph parent cycle")
		}
		if node := nodes[path]; node != nil {
			return node, nil
		}
		info, err := os.Lstat(path)
		if err != nil || !info.IsDir() {
			return nil, fmt.Errorf("image graph node is not a real directory: %v", err)
		}
		identity := descendantLeaseFileIdentity(info)
		if identity.UID != owner.UID || identity.GID != owner.GID {
			return nil, fmt.Errorf("image graph parent has a different owner")
		}
		name := fmt.Sprintf("parent-%d", len(order))
		if len(order) == 0 {
			name = "head"
		}
		node := &descendantImageNode{Path: path, Target: filepath.Join(target, name), Identity: identity}
		nodes[path], visiting[path] = node, true
		order = append(order, node)
		entries, err := os.ReadDir(path)
		if err != nil {
			return nil, err
		}
		present := map[string]bool{}
		for _, entry := range entries {
			item := filepath.Join(path, entry.Name())
			if entry.Name() == "parent" {
				link, err := os.Readlink(item)
				if err != nil || filepath.IsAbs(link) {
					return nil, fmt.Errorf("image graph parent is not a relative link: %v", err)
				}
				parent, err := filepath.EvalSymlinks(filepath.Join(path, link))
				if err != nil {
					return nil, err
				}
				node.Parent, err = discover(parent)
				if err != nil {
					return nil, err
				}
				node.ParentLink = link
				continue
			}
			if !criuInheritParentEntry(entry.Name()) &&
				entry.Name() != "runtime-config.json" && entry.Name() != "descriptors.json" {
				continue
			}
			info, err := os.Lstat(item)
			if err != nil || !info.Mode().IsRegular() {
				return nil, fmt.Errorf("image graph payload is not a regular file: %v", err)
			}
			saved := descendantLeaseFileIdentity(info)
			if saved.UID != owner.UID || saved.GID != owner.GID {
				return nil, fmt.Errorf("image graph payload has a different owner")
			}
			node.Files = append(node.Files, descendantGraphFile{Path: item, Identity: saved})
			present[entry.Name()] = true
		}
		if !present["inventory.img"] || !present["pstree.img"] {
			return nil, fmt.Errorf("image graph node lacks complete process metadata")
		}
		visiting[path] = false
		return node, nil
	}
	rootNode, err := discover(head)
	if err != nil {
		return nil, err
	}
	notify := make(chan os.Signal, 1)
	signal.Notify(notify, syscall.SIGIO)
	defer signal.Stop(notify)
	leases := map[[2]uint64]*descendantFileLease{}
	var created os.FileInfo
	defer func() {
		for _, lease := range leases {
			resultErr = errors.Join(resultErr, lease.close())
		}
		if resultErr != nil && created != nil {
			current, err := os.Lstat(target)
			if err == nil && os.SameFile(current, created) {
				resultErr = errors.Join(resultErr, os.RemoveAll(target))
			} else {
				resultErr = errors.Join(resultErr, fmt.Errorf("changed image-graph target preserved: %v", err))
			}
		}
	}()
	for _, node := range order {
		for index := range node.Files {
			file := &node.Files[index]
			key := [2]uint64{file.Identity.Device, file.Identity.Inode}
			lease := leases[key]
			if lease == nil {
				lease, err = descendantAcquireFileLease(file.Path)
				if err != nil {
					return nil, err
				}
				leases[key] = lease
			}
			if lease.Saved != file.Identity {
				return nil, fmt.Errorf("image graph inode changed before its read lease")
			}
			file.Lease = lease
		}
	}
	if err := os.Mkdir(target, 0700); err != nil {
		return nil, err
	}
	created, err = os.Lstat(target)
	if err != nil {
		return nil, err
	}
	hashes := map[string]string{}
	parentLinks := map[string]string{}
	var clonedBytes, copiedRAMBytes int64
	var copiedRAMFiles int
	for _, node := range order {
		if err := os.Mkdir(node.Target, 0700); err != nil {
			return nil, err
		}
		for _, file := range node.Files {
			destination := filepath.Join(node.Target, filepath.Base(file.Path))
			var fs unix.Statfs_t
			if err := unix.Fstatfs(int(file.Lease.File.Fd()), &fs); err != nil {
				return nil, err
			}
			if readonlyRAMParents && node != rootNode && fs.Type == unix.TMPFS_MAGIC {
				if err := descendantCopyLeasedRAMParent(file.Lease, destination); err != nil {
					return nil, err
				}
				copiedRAMBytes += file.Identity.Size
				copiedRAMFiles++
			} else {
				if err := descendantCloneLeasedPage(file.Lease, destination); err != nil {
					return nil, err
				}
				clonedBytes += file.Identity.Size
			}
			hashes[destination] = file.Lease.SHA256
		}
		if node.Parent != nil {
			link, err := filepath.Rel(node.Target, node.Parent.Target)
			if err != nil {
				return nil, err
			}
			if err := os.Symlink(link, filepath.Join(node.Target, "parent")); err != nil {
				return nil, err
			}
			parentLinks[filepath.Join(node.Target, "parent")] = link
			if err := os.Lchown(filepath.Join(node.Target, "parent"), int(owner.UID), int(owner.GID)); err != nil {
				return nil, err
			}
		}
		if err := os.Chown(node.Target, int(owner.UID), int(owner.GID)); err != nil {
			return nil, err
		}
		directory, err := os.Open(node.Target)
		if err != nil {
			return nil, err
		}
		if err := errors.Join(directory.Sync(), directory.Close()); err != nil {
			return nil, err
		}
	}
	for _, node := range order {
		info, err := os.Lstat(node.Path)
		if err != nil || descendantLeaseFileIdentity(info) != node.Identity {
			return nil, fmt.Errorf("image graph directory changed during cloning: %v", err)
		}
		if node.Parent != nil {
			link, err := os.Readlink(filepath.Join(node.Path, "parent"))
			if err != nil || link != node.ParentLink {
				return nil, fmt.Errorf("image graph parent changed during cloning: %v", err)
			}
		}
		for _, file := range node.Files {
			info, err := os.Lstat(file.Path)
			if err != nil || descendantLeaseFileIdentity(info) != file.Identity {
				return nil, fmt.Errorf("image graph source path changed during cloning: %v", err)
			}
			if err := file.Lease.verify(); err != nil {
				return nil, err
			}
		}
	}
	if err := os.Chown(target, int(owner.UID), int(owner.GID)); err != nil {
		return nil, err
	}
	directory, err := os.Open(target)
	if err != nil {
		return nil, err
	}
	if err := errors.Join(directory.Sync(), directory.Close()); err != nil {
		return nil, err
	}
	return map[string]any{
		"head": rootNode.Target, "nodes": len(order), "payload_files": len(hashes),
		"source_inodes_leased": len(leases), "kernel_cloned_bytes": clonedBytes, "hashes": hashes,
		"readonly_ram_parent_policy":     readonlyRAMParents,
		"verified_ram_parent_copy_bytes": copiedRAMBytes, "verified_ram_parent_copy_files": copiedRAMFiles,
		"private_parent_links": true,
		"parent_links":         parentLinks,
		"scope": "Private inherited graph under original-FD read leases. FICLONE is mandatory except " +
			"for explicitly admitted bounded readonly tmpfs ancestors copied and fully read back into private Btrfs files. " +
			"RAM copy bytes are charged separately and never claimed as zero-copy.",
	}, nil
}

func criuCloneInheritedImageGraph(args []string) error {
	return criuCloneInheritedImageGraphPolicy(args, false)
}

func criuCloneInheritedImageGraphPolicy(args []string, readonlyRAMParents bool) error {
	if len(args) != 2 {
		return fmt.Errorf("clone-inherited-images requires source head and fresh target directory")
	}
	proof, err := descendantCloneImageGraphPolicy(args[0], args[1], readonlyRAMParents)
	if err != nil {
		return err
	}
	fmt.Println(asJSON(proof))
	return nil
}
