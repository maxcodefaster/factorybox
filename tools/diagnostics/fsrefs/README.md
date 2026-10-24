# Owned root and working-directory snapshots

This optional Linux AMD64 diagnostic observes root/pwd context snapshots in one
owned PID namespace tree. It is not a runtime service or production dependency.
Use the existing native resource, artifact, deadline and ownership guards.

The loader validates the actual kernel BTF, register layout, PID ancestry
layout, nested fs_struct sequence counter and allocator tracepoint arguments.
It reads root/pwd mount pointers only across a stable even sequence counter.
Eight kprobes record copy/setter returns and ordinary unmount entry/return.
The allocator's kmem_cache_free raw tracepoint closes tracked context
generations, requiring the slab object size to equal the live fs_struct size.
It never reads the released object. Pointer generations remain separate.
No observed object, syscall outcome or lifecycle assertion is changed.

The recorded copy caller can be the parent that creates another task's context.
It is not the new task's identity or an ownership grant. A last snapshot may be
stale: namespace copying, pivot_root and other kernel paths can change a context
without calling the observed setters. Pre-coverage contexts and transitions
before setter return are also outside this inventory. Allocator release is an
upper lifetime bound, not the exact instant a mount reference was dropped.
Neither empty snapshots nor apparently overlapping snapshots can by themselves
explain a historical EBUSY. Corroborate a candidate with actual process/mount
evidence. Namespace-only references and file descriptors need separate probes.

Build `factorybox-fs-ref-tracer-44dffef8db3c488691c8e478330ed541` passes the
three actual kernel controls in
`factorybox-fs-reference-control-d32071f3a3054426a5f8a0352a7dac85`.
A descendant process holds cwd, holds a chroot root/pwd, or only inherits cwd
through fork while its creator changes away. It has no open fd on the target
mount. Ordinary unmount returns EBUSY while held and succeeds after exact helper
exit. Every matching context generation is released before successful cleanup.
All four error counters and eight kprobe miss counters are zero. Raw tracepoint
records share the bounded ring buffer; it has no kretprobe instance-miss counter.

The initial build ea55cbeb is retained with its BTF layout rejection. Build
775b596b loads and observes all three held states, but its free_fs_struct probes
do not see actual process-exit release on this kernel. Its native control passed;
host decoding first failed on raw-output permissions. That failed verdict and
the privileged read-only follow-up showing absent release events remain part
of the diagnostic history. The allocator tracepoint fixes the observer coverage;
it does not attribute the historical Factorybox mount failure. The final control
also declares output-file ownership after observer shutdown for host decoding.

The frozen build recipes, pinned Cilium v0.17.3 headers, exact binaries and all
native control sources are under their artifact directories. New builds/native
runs require reference.new_artifact and frozen inputs. Never modify an earlier
build/control in place or convert its failed verdict into a pass.
