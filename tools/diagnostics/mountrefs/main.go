//go:build linux

// A bounded read-only file lifetime observer for an owned PID namespace tree.
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
	if err := spec.RewriteConstants(map[string]any{"pidns_dev": uint64(ns.Dev), "pidns_ino": ns.Ino}); err != nil {
		return err
	}
	kernel, kernelSHA, err := kernelTypes()
	if err != nil {
		return err
	}
	for name, count := range map[string]int{"path_openat": 3, "__fput": 1, "path_umount": 2} {
		var fn *btf.Func
		if err := kernel.TypeByName(name, &fn); err != nil {
			return err
		}
		proto, ok := fn.Type.(*btf.FuncProto)
		if !ok || len(proto.Params) != count {
			return fmt.Errorf("unexpected kernel trace prototype for %s", name)
		}
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
	var missed [5]uint64
	shutdown := func() {
		once.Do(func() {
			for i, l := range links {
				info, err := l.Info()
				if err == nil {
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
	for _, name := range []string{"opened", "closing_file", "closed", "unmounting", "unmounted"} {
		var l link.Link
		var err error
		switch name {
		case "opened":
			l, err = link.Kretprobe("path_openat", collection.Programs[name], nil)
		case "closing_file":
			l, err = link.Kprobe("__fput", collection.Programs[name], nil)
		case "closed":
			l, err = link.Kretprobe("__fput", collection.Programs[name], nil)
		case "unmounting":
			l, err = link.Kprobe("path_umount", collection.Programs[name], nil)
		case "unmounted":
			l, err = link.Kretprobe("path_umount", collection.Programs[name], nil)
		}
		if err != nil {
			return fmt.Errorf("attach %s: %w", name, err)
		}
		links = append(links, l)
	}
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
	// No open can enter the tracked set until its final-release entry and
	// return probes are both attached. Otherwise an early close can leave a
	// stale per-thread entry before the return probe exists.
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
		"open_hook":         "path_openat",
		"event_bytes":       72, "scope": "Only successful path opens, including O_PATH, and unmounts in this exact owned outer PID namespace and its descendants. Final releases only for those tracked files, including a kernel worker. No file contents or paths; cwd, root and namespace references are outside this observer."}
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
		if len(record.RawSample) != 72 || count >= 4<<20 {
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
	var counters [3]uint64
	for i := uint32(0); i < 3; i++ {
		if err := collection.Maps["errors"].Lookup(i, &counters[i]); err != nil {
			return err
		}
	}
	initial["events"], initial["kernel_error_counts"] = count, counters
	initial["kernel_probe_missed"] = missed
	initial["passed"] = counters == [3]uint64{} && shutdownErr == nil
	data, _ = json.MarshalIndent(initial, "", "  ")
	if err := os.WriteFile(*result, append(data, '\n'), 0644); err != nil {
		return err
	}
	if counters != [3]uint64{} {
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
