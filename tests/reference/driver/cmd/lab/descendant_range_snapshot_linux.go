package main

import (
	"errors"
	"fmt"
	"math"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// Resolve only the current virtual-address coverage. A parent may split one
// child range into several local/inherited ranges, so callers must advance by
// the returned length rather than assuming the full child range was resolved.
func (memory *descendantMemoryMap) extent(address, limit uint64) (*os.File, int64, uint64, error) {
	index := sort.Search(len(memory.Saved.Ranges), func(index int) bool {
		entry := memory.Saved.Ranges[index]
		return entry.Address+entry.Pages*4096 > address
	})
	if limit == 0 || index == len(memory.Saved.Ranges) || memory.Saved.Ranges[index].Address > address {
		return nil, 0, 0, fmt.Errorf("range snapshot has no source at %#x", address)
	}
	entry := memory.Saved.Ranges[index]
	count := min(limit, entry.Address+entry.Pages*4096-address)
	if entry.Flags&4 != 0 {
		offset := entry.Offset + int64(address-entry.Address)
		if memory.Data == nil || offset < 0 || uint64(offset) > uint64(memory.DataBytes) ||
			count > uint64(memory.DataBytes)-uint64(offset) {
			return nil, 0, 0, fmt.Errorf("range snapshot source payload is incomplete")
		}
		return memory.Data, offset, count, nil
	}
	if memory.Parent == nil || entry.Flags&1 == 0 {
		return nil, 0, 0, fmt.Errorf("range snapshot lost its inherited source")
	}
	return memory.Parent.extent(address, count)
}

// Compile an already independent file-backed input graph. All source files
// remain under their original inode read leases until every output is synced.
// FICLONERANGE is mandatory; RAM copies belong to the earlier explicit graph
// admission and must remain separately charged by the caller.
func descendantCompileRangeSnapshot(head, target string) (_ map[string]any, resultErr error) {
	return descendantCompileRangeSnapshotPolicy(head, target, false, false)
}

type descendantRangeAssembly struct {
	Head                  string
	AllowRAMParents       bool
	KernelBytes, RAMBytes int64
	RAMExtents            int
	RAMFiles              map[string]bool
}

func descendantCompileRangeSnapshotPolicy(head, target string, readonlyRAMParents, leaseOnlyPages bool) (_ map[string]any, resultErr error) {
	return descendantCompileRangeSnapshotProtectedPolicy(head, target, readonlyRAMParents, leaseOnlyPages, nil)
}

func descendantCompileRangeSnapshotProtectedPolicy(head, target string, readonlyRAMParents, leaseOnlyPages bool,
	protectionOut **descendantBackingLeases) (_ map[string]any, resultErr error) {
	start := time.Now()
	if leaseOnlyPages && !readonlyRAMParents {
		return nil, fmt.Errorf("lease-only page inputs require direct original-graph range assembly")
	}
	if filepath.Clean(head) != head || filepath.Clean(target) != target ||
		!strings.HasPrefix(head, "/experiment/") || !strings.HasPrefix(target, "/experiment/") {
		return nil, fmt.Errorf("range snapshot paths are outside the owned experiment")
	}
	sourceRoot := filepath.Dir(head)
	actual, err := filepath.EvalSymlinks(head)
	actualParent, parentErr := filepath.EvalSymlinks(filepath.Dir(target))
	if err != nil || actual != head || parentErr != nil || actualParent != filepath.Dir(target) {
		return nil, fmt.Errorf("range snapshot requires canonical disjoint input/output paths: %v %v", err, parentErr)
	}
	rootInfo, err := os.Lstat(head)
	if err != nil || !rootInfo.IsDir() {
		return nil, fmt.Errorf("range snapshot head is not a directory: %v", err)
	}
	owner := descendantLeaseFileIdentity(rootInfo)
	type node struct {
		path, parent string
		identity     descendantLeaseIdentity
	}
	var nodes []node
	leases := map[string]*descendantFileLease{}
	inodes := map[[2]uint64]*descendantFileLease{}
	notify := make(chan os.Signal, 1)
	signal.Notify(notify, syscall.SIGIO)
	defer signal.Stop(notify)
	var outputProtection *descendantBackingLeases
	var created os.FileInfo
	defer func() {
		for _, lease := range inodes {
			resultErr = errors.Join(resultErr, lease.close())
		}
		if outputProtection != nil {
			if resultErr != nil {
				resultErr = errors.Join(resultErr, outputProtection.close(false))
			} else {
				*protectionOut = outputProtection
			}
		}
		if resultErr != nil && created != nil {
			current, err := os.Lstat(target)
			if err == nil && os.SameFile(current, created) {
				resultErr = errors.Join(resultErr, os.RemoveAll(target))
			} else {
				resultErr = errors.Join(resultErr, fmt.Errorf("changed range snapshot output preserved: %v", err))
			}
		}
	}()
	visited := map[string]bool{}
	for directory := head; directory != ""; {
		if !descendantGraphWithin(sourceRoot, directory) || visited[directory] || len(nodes) >= 64 {
			return nil, fmt.Errorf("range snapshot has an escaped, cyclic or excessive parent graph")
		}
		if descendantGraphWithin(directory, target) || descendantGraphWithin(target, directory) {
			return nil, fmt.Errorf("range snapshot output overlaps an input node")
		}
		visited[directory] = true
		info, err := os.Lstat(directory)
		if err != nil || !info.IsDir() {
			return nil, fmt.Errorf("range snapshot input node is not a directory: %v", err)
		}
		identity := descendantLeaseFileIdentity(info)
		if identity.UID != owner.UID || identity.GID != owner.GID {
			return nil, fmt.Errorf("range snapshot input node has a foreign owner")
		}
		current := node{path: directory, identity: identity}
		entries, err := os.ReadDir(directory)
		if err != nil {
			return nil, err
		}
		next := ""
		for _, entry := range entries {
			path := filepath.Join(directory, entry.Name())
			if entry.Name() == "parent" {
				link, err := os.Readlink(path)
				if err != nil || filepath.IsAbs(link) {
					return nil, fmt.Errorf("range snapshot parent is not a relative link: %v", err)
				}
				current.parent = link
				next, err = filepath.EvalSymlinks(filepath.Join(directory, link))
				if err != nil {
					return nil, err
				}
				continue
			}
			if !criuInheritParentEntry(entry.Name()) &&
				entry.Name() != "runtime-config.json" && entry.Name() != "descriptors.json" {
				continue
			}
			info, err := os.Lstat(path)
			if err != nil || !info.Mode().IsRegular() {
				return nil, fmt.Errorf("range snapshot input is not a regular file: %v", err)
			}
			saved := descendantLeaseFileIdentity(info)
			if saved.UID != owner.UID || saved.GID != owner.GID || len(leases) >= 8192 {
				return nil, fmt.Errorf("range snapshot has foreign or excessive input files")
			}
			key := [2]uint64{saved.Device, saved.Inode}
			lease := inodes[key]
			hashContents := !leaseOnlyPages || !criuRAMPageName.MatchString(entry.Name())
			if lease == nil {
				lease, err = descendantAcquireFileLeasePolicy(path, hashContents)
				if err != nil {
					return nil, err
				}
				inodes[key] = lease
			}
			if hashContents && lease.SHA256 == "" {
				lease.SHA256, err = descendantLeaseHash(lease.File)
				if err != nil {
					return nil, err
				}
				if err := lease.verify(); err != nil {
					return nil, err
				}
			}
			leases[path] = lease
			if lease.Saved != saved {
				return nil, fmt.Errorf("range snapshot input changed before its lease")
			}
		}
		if leases[directory+"/inventory.img"] == nil || leases[directory+"/pstree.img"] == nil {
			return nil, fmt.Errorf("range snapshot input lacks complete process metadata")
		}
		nodes = append(nodes, current)
		directory = next
	}
	if err := os.Mkdir(target, 0700); err != nil {
		return nil, err
	}
	inputAdmissionTime := time.Since(start)
	var metadataCloneTime, pageAssemblyTime time.Duration
	created, err = os.Lstat(target)
	if err != nil {
		return nil, err
	}
	graph, err := descendantOpenMemoryGraph(head)
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, graph.close()) }()
	hashes := map[string]string{}
	assembly := &descendantRangeAssembly{Head: head, AllowRAMParents: readonlyRAMParents, RAMFiles: map[string]bool{}}
	var metadataBytes, pageBytes int64
	var maps int
	entries, err := os.ReadDir(head)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		name := entry.Name()
		lease := leases[filepath.Join(head, name)]
		if lease == nil || criuRAMPageName.MatchString(name) || criuInheritAliasName.MatchString(name) {
			continue
		}
		if strings.HasPrefix(name, "pagemap-") {
			operationStart := time.Now()
			match := descendantMemoryName.FindStringSubmatch(name)
			if match == nil {
				return nil, fmt.Errorf("unsupported range snapshot pagemap identity")
			}
			memory, err := graph.load(head, match[1])
			if err != nil {
				return nil, err
			}
			complete, written, err := descendantAssembleRangePages(memory, target, owner, leases, assembly)
			if err != nil {
				return nil, err
			}
			encoded, err := criuEncodeSavedMap(complete)
			if err != nil {
				return nil, err
			}
			if err := descendantOracleWrite(filepath.Join(target, name), encoded, owner); err != nil {
				return nil, err
			}
			pageBytes += written
			maps++
			pageAssemblyTime += time.Since(operationStart)
		} else {
			operationStart := time.Now()
			if err := descendantCloneLeasedPage(lease, filepath.Join(target, name)); err != nil {
				return nil, err
			}
			metadataBytes += lease.Saved.Size
			metadataCloneTime += time.Since(operationStart)
		}
	}
	if maps == 0 || pageBytes == 0 || pageBytes != assembly.KernelBytes+assembly.RAMBytes {
		return nil, fmt.Errorf("range snapshot has no complete current memory")
	}
	certificationStart := time.Now()
	for _, current := range nodes {
		info, err := os.Lstat(current.path)
		if err != nil || descendantLeaseFileIdentity(info) != current.identity {
			return nil, fmt.Errorf("range snapshot input directory changed: %v", err)
		}
		if current.parent != "" {
			link, err := os.Readlink(filepath.Join(current.path, "parent"))
			if err != nil || link != current.parent {
				return nil, fmt.Errorf("range snapshot input parent changed: %v", err)
			}
		}
	}
	for path, lease := range leases {
		if err := lease.verify(); err != nil {
			return nil, err
		}
		info, err := os.Lstat(path)
		if err != nil || descendantLeaseFileIdentity(info) != lease.Saved {
			return nil, fmt.Errorf("range snapshot input alias changed: %v", err)
		}
	}
	if err := os.Chown(target, int(owner.UID), int(owner.GID)); err != nil {
		return nil, err
	}
	inputCertificationTime := time.Since(certificationStart)
	if protectionOut != nil {
		if *protectionOut != nil || !readonlyRAMParents || !leaseOnlyPages {
			return nil, fmt.Errorf("retained output leases require fresh direct range assembly with leased page inputs")
		}
		outputProtection, err = descendantNewRangeProtection(target, owner)
		if err != nil {
			return nil, err
		}
	}
	outputHashStart := time.Now()
	outputs, err := os.ReadDir(target)
	if err != nil {
		return nil, err
	}
	for _, entry := range outputs {
		path := filepath.Join(target, entry.Name())
		if outputProtection != nil && criuRAMPageName.MatchString(entry.Name()) {
			lease, err := descendantAcquireFileLease(path)
			if err != nil {
				return nil, err
			}
			outputProtection.Pages[entry.Name()] = lease
			if len(outputProtection.Pages) > 128 || lease.Saved.Size < 0 ||
				lease.Saved.Size > int64(criuRAMSnapshotLimit)-outputProtection.Bytes ||
				lease.Saved.UID != owner.UID || lease.Saved.GID != owner.GID ||
				lease.Saved.Mode&07777 != 0600 || lease.Saved.Links != 1 {
				return nil, fmt.Errorf("retained complete output page lost private bounded ownership")
			}
			outputProtection.Bytes += lease.Saved.Size
			if err := errors.Join(lease.File.Sync(), lease.verify()); err != nil {
				return nil, err
			}
			hashes[path] = lease.SHA256
			continue
		}
		file, err := os.OpenFile(path, os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
		if err != nil {
			return nil, err
		}
		hash, hashErr := descendantLeaseHash(file)
		if err := errors.Join(hashErr, file.Sync(), file.Close()); err != nil {
			return nil, err
		}
		hashes[path] = hash
	}
	directory, err := os.Open(target)
	if err != nil {
		return nil, err
	}
	if err := errors.Join(directory.Sync(), directory.Close()); err != nil {
		return nil, err
	}
	if outputProtection != nil {
		if len(outputProtection.Pages) == 0 || outputProtection.Bytes != pageBytes {
			return nil, fmt.Errorf("retained output leases omitted complete current pages")
		}
		if err := outputProtection.verify(); err != nil {
			return nil, err
		}
	}
	outputHashTime := time.Since(outputHashStart)
	var inputHashBytes, inputPageHashBytes, inputUnhashedPageBytes int64
	for _, lease := range inodes {
		if lease.SHA256 != "" {
			inputHashBytes += lease.Saved.Size
			if criuRAMPageName.MatchString(filepath.Base(lease.Path)) {
				inputPageHashBytes += lease.Saved.Size
			}
		} else if criuRAMPageName.MatchString(filepath.Base(lease.Path)) {
			inputUnhashedPageBytes += lease.Saved.Size
		} else {
			return nil, fmt.Errorf("range snapshot metadata lost its complete input hash")
		}
	}
	return map[string]any{
		"head": target, "complete_range_snapshot": true, "nodes": 1,
		"memory_maps": maps, "logical_memory_bytes": pageBytes,
		"retained_output_page_read_leases": protectionOut != nil,
		"compiler_phase_us": map[string]int64{
			"input_admission": inputAdmissionTime.Microseconds(), "metadata_clone_and_sync": metadataCloneTime.Microseconds(),
			"page_range_assembly_and_sync": pageAssemblyTime.Microseconds(), "input_certification": inputCertificationTime.Microseconds(),
			"output_hash_sync_and_lease_admission": outputHashTime.Microseconds(),
		},
		"kernel_range_clone_bytes":        assembly.KernelBytes,
		"direct_original_graph":           readonlyRAMParents,
		"lease_only_page_inputs":          leaseOnlyPages,
		"source_input_hash_bytes":         inputHashBytes,
		"source_input_page_hash_bytes":    inputPageHashBytes,
		"source_unhashed_page_bytes":      inputUnhashedPageBytes,
		"verified_ram_range_copy_bytes":   assembly.RAMBytes,
		"verified_ram_range_copy_extents": assembly.RAMExtents,
		"verified_ram_range_copy_files":   len(assembly.RAMFiles),
		"kernel_cloned_metadata_bytes":    metadataBytes, "source_inodes_leased": len(inodes),
		"input_nodes": len(nodes), "hashes": hashes, "parent_links": map[string]string{},
		"elapsed_us": time.Since(start).Microseconds(),
		"scope": "Complete current logical memory assembled under source inode read leases. File-backed extents require kernel range reflinks; " +
			"explicitly admitted bounded readonly RAM ancestor extents are copied and synced/read back, with copy bytes charged separately. " +
			"Private metadata and original lazy flags retained; no inherited ranges, PID aliases or parent dependency.",
	}, nil
}

func descendantCompileDirectRangeSnapshot(head, target string) (map[string]any, error) {
	return descendantCompileRangeSnapshotPolicy(head, target, true, false)
}

func descendantCompileRangeSnapshotLeasedInputs(head, target string) (map[string]any, error) {
	return descendantCompileRangeSnapshotPolicy(head, target, true, true)
}

func descendantAssembleRangePages(memory *descendantMemoryMap, target string, owner descendantLeaseIdentity,
	leases map[string]*descendantFileLease, assembly *descendantRangeAssembly) (_ criuSavedMap, written int64, resultErr error) {
	complete := criuSavedMap{PageID: memory.Saved.PageID}
	file, err := os.OpenFile(filepath.Join(target, fmt.Sprintf("pages-%d.img", complete.PageID)),
		os.O_RDWR|os.O_CREATE|os.O_EXCL|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return complete, 0, err
	}
	defer func() { resultErr = errors.Join(resultErr, file.Close()) }()
	var ramReadback []descendantRAMRangeReadback
	for _, entry := range memory.Saved.Ranges {
		count := entry.Pages * 4096
		if count > math.MaxInt64 || written > math.MaxInt64-int64(count) {
			return complete, written, fmt.Errorf("complete range snapshot page-file size overflow")
		}
		complete.Ranges = append(complete.Ranges, criuSavedRange{
			Address: entry.Address, Pages: entry.Pages, Flags: entry.Flags&2 | 4, Offset: written})
		for address, remaining := entry.Address, count; remaining > 0; {
			source, offset, size, err := memory.extent(address, remaining)
			if err != nil {
				return complete, written, err
			}
			lease := leases[source.Name()]
			if lease == nil {
				return complete, written, fmt.Errorf("range snapshot extent lacks a held source lease")
			}
			info, err := source.Stat()
			if err != nil || descendantLeaseFileIdentity(info) != lease.Saved {
				return complete, written, fmt.Errorf("range snapshot extent differs from its original leased inode: %v", err)
			}
			if offset%4096 != 0 || size == 0 || size%4096 != 0 || written%4096 != 0 {
				return complete, written, fmt.Errorf("range snapshot extent is not page aligned")
			}
			if err := lease.verify(); err != nil {
				return complete, written, err
			}
			var fs unix.Statfs_t
			if err := unix.Fstatfs(int(lease.File.Fd()), &fs); err != nil {
				return complete, written, err
			}
			if assembly.AllowRAMParents && filepath.Dir(lease.Path) != assembly.Head && fs.Type == unix.TMPFS_MAGIC {
				readback, err := descendantCopyLeasedRAMRange(lease, file, offset, int64(size), written)
				if err != nil {
					return complete, written, err
				}
				ramReadback = append(ramReadback, readback)
				assembly.RAMBytes += int64(size)
				assembly.RAMExtents++
				assembly.RAMFiles[lease.Path] = true
			} else {
				if err := unix.IoctlFileCloneRange(int(file.Fd()), &unix.FileCloneRange{
					Src_fd: int64(lease.File.Fd()), Src_offset: uint64(offset),
					Src_length: size, Dest_offset: uint64(written),
				}); err != nil {
					return complete, written, fmt.Errorf("mandatory kernel range reflink failed: %w", err)
				}
				assembly.KernelBytes += int64(size)
			}
			if err := lease.verify(); err != nil {
				return complete, written, err
			}
			written += int64(size)
			address += size
			remaining -= size
		}
	}
	if err := errors.Join(file.Chown(int(owner.UID), int(owner.GID)),
		descendantVerifySyncedRAMRanges(file, ramReadback)); err != nil {
		return complete, written, err
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() != written {
		return complete, written, fmt.Errorf("assembled snapshot has an incomplete page file: %v", err)
	}
	return complete, written, nil
}

func criuCompileRangeSnapshot(args []string) error {
	return criuCompileRangeSnapshotPolicy(args, false)
}

func criuCompileRangeSnapshotPolicy(args []string, direct bool) error {
	if len(args) != 2 {
		return fmt.Errorf("compile-range-snapshot requires an independent source head and fresh target")
	}
	proof, err := descendantCompileRangeSnapshotPolicy(args[0], args[1], direct, false)
	if err != nil {
		return err
	}
	fmt.Println(asJSON(proof))
	return nil
}

func criuCompileRangeSnapshotLeasedInputs(args []string) error {
	if len(args) != 2 {
		return fmt.Errorf("compile-range-snapshot-direct-leased requires a source graph and fresh target")
	}
	proof, err := descendantCompileRangeSnapshotLeasedInputs(args[0], args[1])
	if err != nil {
		return err
	}
	fmt.Println(asJSON(proof))
	return nil
}
