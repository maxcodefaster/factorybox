//go:build linux

// A bounded read-only root/pwd observer for an owned PID namespace tree.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/btf"
	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
	"github.com/cilium/ebpf/rlimit"
)

// Accept the optional layout extension only when every encoded kind has the
// exact legacy layout and every record fits the older reader. Type records,
// IDs, strings and their offsets stay byte-identical. Only an in-memory copy
// of the optional header fields is cleared; no kernel or filesystem is edited.
func kernelTypes() (*btf.Spec, string, error) {
	data, err := os.ReadFile("/sys/kernel/btf/vmlinux")
	if err != nil {
		return nil, "", err
	}
	digest := fmt.Sprintf("%x", sha256.Sum256(data))
	if len(data) < 24 || binary.LittleEndian.Uint16(data) != 0xeb9f || data[2] != 1 || data[3] != 0 {
		return nil, digest, fmt.Errorf("unexpected kernel BTF header")
	}
	u32 := func(offset int) uint64 { return uint64(binary.LittleEndian.Uint32(data[offset : offset+4])) }
	hdr := u32(4)
	if hdr == 32 {
		layout := [20][2]byte{{0, 0}, {4, 0}, {0, 0}, {12, 0}, {0, 12}, {0, 12}, {0, 8}, {0, 0}, {0, 0}, {0, 0}, {0, 0}, {0, 0}, {0, 0}, {0, 8}, {4, 0}, {0, 12}, {0, 0}, {4, 0}, {0, 0}, {0, 12}}
		if len(data) < 32 || u32(24) != u32(8)+u32(12) || u32(28) != 80 || u32(16) != u32(24)+80 || hdr+u32(16)+u32(20) != uint64(len(data)) {
			return nil, digest, fmt.Errorf("unsupported kernel BTF layout placement")
		}
		for i, expected := range layout {
			offset := hdr + u32(24) + uint64(i*4)
			if data[offset] != expected[0] || data[offset+1] != expected[1] || data[offset+2] != 0 || data[offset+3] != 0 {
				return nil, digest, fmt.Errorf("kernel BTF kind %d has a new layout", i)
			}
		}
		end := hdr + u32(8) + u32(12)
		for offset := hdr + u32(8); offset < end; {
			if offset+12 > end {
				return nil, digest, fmt.Errorf("truncated kernel BTF record")
			}
			info := binary.LittleEndian.Uint32(data[offset+4 : offset+8])
			kind := (info >> 24) & 127
			count := info & 0xffffff
			if kind >= 20 || count > 65535 {
				return nil, digest, fmt.Errorf("kernel BTF kind/count exceeds legacy reader")
			}
			offset += 12 + uint64(layout[kind][0]) + uint64(count)*uint64(layout[kind][1])
			if offset > end {
				return nil, digest, fmt.Errorf("kernel BTF record exceeds type section")
			}
		}
		clear(data[24:32])
	} else if hdr != 24 {
		return nil, digest, fmt.Errorf("unsupported kernel BTF header size")
	}
	spec, err := btf.LoadSpecFromReader(bytes.NewReader(data))
	return spec, digest, err
}

func run() (resultErr error) {
	object := flag.String("object", "", "frozen CO-RE object")
	output := flag.String("output", "", "new binary event file")
	ready := flag.String("ready", "", "new readiness JSON")
	result := flag.String("result", "", "new completion JSON")
	flag.Parse()
	if *object == "" || *output == "" || *ready == "" || *result == "" {
		return fmt.Errorf("object, output, ready and result required")
	}
	var ns syscall.Stat_t
	if err := syscall.Stat("/proc/self/ns/pid", &ns); err != nil {
		return err
	}
	spec, err := ebpf.LoadCollectionSpec(*object)
	if err != nil {
		return err
	}
	kernel, kernelSHA, err := kernelTypes()
	if err != nil {
		return err
	}
	for name, count := range map[string]int{"copy_fs_struct": 1, "__traceiter_kmem_cache_free": 4, "set_fs_pwd": 2, "set_fs_root": 2, "path_umount": 2} {
		var fn *btf.Func
		if err := kernel.TypeByName(name, &fn); err != nil {
			return err
		}
		proto, ok := fn.Type.(*btf.FuncProto)
		if !ok || len(proto.Params) != count {
			return fmt.Errorf("unexpected kernel trace prototype for %s", name)
		}
		if name == "__traceiter_kmem_cache_free" {
			for index, expected := range []string{"__data", "call_site", "ptr", "s"} {
				if proto.Params[index].Name != expected {
					return fmt.Errorf("unexpected allocator tracepoint argument %d", index)
				}
			}
		}
	}
	var fs *btf.Struct
	if err := kernel.TypeByName("fs_struct", &fs); err != nil {
		return err
	}
	var current btf.Type = fs
	for _, part := range []struct{ structure, member string }{
		{"fs_struct", "seq"}, {"seqlock", "seqcount"},
		{"seqcount_spinlock", "seqcount"}, {"seqcount", "sequence"},
	} {
		structure, ok := btf.UnderlyingType(current).(*btf.Struct)
		if !ok || structure.Name != part.structure {
			return fmt.Errorf("unsupported fs_struct sequence layout at %s", part.structure)
		}
		current = nil
		for _, member := range structure.Members {
			if member.Name == part.member {
				current = member.Type
				break
			}
		}
		if current == nil {
			return fmt.Errorf("missing fs_struct sequence member %s", part.member)
		}
	}
	if sequence, ok := btf.UnderlyingType(current).(*btf.Int); !ok || sequence.Size != 4 {
		return fmt.Errorf("fs_struct sequence is not a 32-bit integer")
	}
	if err := spec.RewriteConstants(map[string]any{"pidns_dev": uint64(ns.Dev), "pidns_ino": ns.Ino, "fs_struct_bytes": fs.Size}); err != nil {
		return err
	}
	var upid *btf.Struct
	if err := kernel.TypeByName("upid", &upid); err != nil {
		return err
	}
	if upid.Size != 16 || len(upid.Members) != 2 || upid.Members[0].Name != "nr" || upid.Members[0].Offset != 0 || upid.Members[1].Name != "ns" || upid.Members[1].Offset != 64 {
		return fmt.Errorf("kernel upid layout differs")
	}
	var regs *btf.Struct
	if err := kernel.TypeByName("pt_regs", &regs); err != nil {
		return err
	}
	for field, offset := range map[string]btf.Bits{"di": 14 * 64, "si": 13 * 64, "ax": 10 * 64} {
		found := false
		for _, member := range regs.Members {
			if member.Name == field && member.Offset == offset {
				found = true
			}
		}
		if !found {
			return fmt.Errorf("unexpected kernel x86-64 probe register layout for %s", field)
		}
	}
	if err := rlimit.RemoveMemlock(); err != nil {
		return err
	}
	collection, err := ebpf.NewCollectionWithOptions(spec, ebpf.CollectionOptions{Programs: ebpf.ProgramOptions{KernelTypes: kernel}})
	if err != nil {
		return err
	}
	defer collection.Close()
	var links []link.Link
	var once sync.Once
	var shutdownErr error
	var missed [8]uint64
	shutdown := func() {
		once.Do(func() {
			for i, l := range links {
				info, err := l.Info()
				if err == nil && i < len(missed) {
					perf := info.PerfEvent()
					if perf == nil || perf.Kprobe() == nil {
						err = fmt.Errorf("kernel probe miss counter unavailable")
					} else {
						var ok bool
						missed[i], ok = perf.Kprobe().Missed()
						if !ok || missed[i] != 0 {
							err = fmt.Errorf("kernel probe %d missed events or lacks a counter: %d", i, missed[i])
						}
					}
				}
				shutdownErr = errors.Join(shutdownErr, err, l.Close())
			}
		})
	}
	defer func() { shutdown(); resultErr = errors.Join(resultErr, shutdownErr) }()
	probes := []struct {
		name, function string
		returned       bool
	}{
		{"copying", "copy_fs_struct", false}, {"copied", "copy_fs_struct", true},
		{"setting_pwd", "set_fs_pwd", false}, {"set_pwd", "set_fs_pwd", true},
		{"setting_root", "set_fs_root", false}, {"set_root", "set_fs_root", true},
		{"unmounting", "path_umount", false}, {"unmounted", "path_umount", true},
	}
	for _, probe := range probes {
		var l link.Link
		var err error
		if probe.returned {
			l, err = link.Kretprobe(probe.function, collection.Programs[probe.name], nil)
		} else {
			l, err = link.Kprobe(probe.function, collection.Programs[probe.name], nil)
		}
		if err != nil {
			return fmt.Errorf("attach %s: %w", probe.name, err)
		}
		links = append(links, l)
	}
	raw, err := link.AttachRawTracepoint(link.RawTracepointOptions{Name: "kmem_cache_free", Program: collection.Programs["freed"]})
	if err != nil {
		return fmt.Errorf("attach allocator tracepoint: %w", err)
	}
	links = append(links, raw)
	reader, err := ringbuf.NewReader(collection.Maps["events"])
	if err != nil {
		return err
	}
	defer reader.Close()
	f, err := os.OpenFile(*output, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	// All creation, update and final-release probes must exist before tracking.
	// An earlier acquisition could otherwise outlive unobserved release.
	if err := collection.Maps["enabled"].Update(uint32(0), uint32(1), ebpf.UpdateAny); err != nil {
		return err
	}
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(signals)
	stop := make(chan struct{})
	stopped := make(chan struct{})
	defer close(stop)
	go func() {
		select {
		case <-signals:
			shutdown()
			reader.Flush()
			close(stopped)
		case <-stop:
		}
	}()
	initial := map[string]any{"schema": 1, "pid": os.Getpid(), "pid_namespace_device": ns.Dev, "pid_namespace_inode": ns.Ino,
		"kernel_btf_sha256": kernelSHA,
		"fs_struct_bytes":   fs.Size, "raw_tracepoint": "kmem_cache_free",
		"reference_class": "fs_struct root and pwd",
		"event_bytes":     88, "scope": "Observed fs_struct copy/root/pwd snapshots and tracked context release in this owned PID namespace tree. A copy event identifies the caller, not the new task. Release completion is an upper lifetime bound. Transitions before hook return, pre-coverage contexts, namespace-only and other mount references remain outside this inventory. No paths, contents or ownership authority."}
	data, _ := json.Marshal(initial)
	if err := os.WriteFile(*ready, append(data, '\n'), 0644); err != nil {
		return err
	}
	count := uint64(0)
	for {
		record, err := reader.Read()
		if errors.Is(err, ringbuf.ErrFlushed) {
			break
		}
		if err != nil {
			return err
		}
		if len(record.RawSample) != 88 || count >= 4<<20 {
			return fmt.Errorf("bounded trace size exceeded or event layout differs")
		}
		if _, err := f.Write(record.RawSample); err != nil {
			return err
		}
		count++
	}
	<-stopped
	if err := f.Sync(); err != nil {
		return err
	}
	var counters [4]uint64
	for i := uint32(0); i < 4; i++ {
		if err := collection.Maps["errors"].Lookup(i, &counters[i]); err != nil {
			return err
		}
	}
	initial["events"], initial["kernel_error_counts"] = count, counters
	initial["kernel_probe_missed"] = missed
	initial["passed"] = counters == [4]uint64{} && shutdownErr == nil
	data, _ = json.MarshalIndent(initial, "", "  ")
	if err := os.WriteFile(*result, append(data, '\n'), 0644); err != nil {
		return err
	}
	if counters != [4]uint64{} {
		return fmt.Errorf("trace lost events or could not identify an object: %v", counters)
	}
	return shutdownErr
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
