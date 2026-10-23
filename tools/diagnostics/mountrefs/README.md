# Owned-namespace mount observation

This optional Linux AMD64 diagnostic observes file lifetimes and unmounts in
one explicitly owned outer PID namespace. It is not a runtime service or a
production dependency. Run it only inside an owned native fixture with the
existing resource, artifact and ownership guards.

The frozen build recipe and its pinned Cilium v0.17.3 helper headers are in
`artifacts/factorybox-mount-ref-tracer-dfa89db897b8405cb842d1d5c0466593/`.
A new build needs a new `reference.new_artifact` allocation, frozen Go/C/header
inputs and recorded commands and hashes. Never rerun the completed build in
place. The Go module and kernel are not modified by the diagnostic.

The command requires new paths for `--object`, `--output`, `--ready` and
`--result`. Create the target mounts only after readiness. All five probes
attach before tracking is enabled. SIGTERM detaches this process's probes,
drains its ring and records kernel error and missed-event counters. Nonzero or
unavailable counters fail observation qualification. Events and tracked tables
are bounded; the CLI writes at most 4,194,304 events.

Each little-endian event is 72 bytes, decoded by `tools/mount_ref_events.py`:
monotonic timestamp, file pointer, mount pointer, host PID/TID, original open
generation, opener namespace PID, legacy mount ID, operation, result and comm.
Operations are successful `path_openat` return, `__fput` entry, `__fput` return,
`path_umount` entry and `path_umount` return. Negative unmount results retain
the original kernel errno. A legacy mount ID of zero is retained as an
observation with its mount pointer, never promoted into ownership authority.

Successful path opens include O_PATH and exclude failed opens. Only opens in
the exact selected outer PID namespace enter the tracked set. This does not
include all descendant namespaces. A subsequent release of a tracked file can
be observed on another task or a kernel worker. File pointer generations keep
reallocated objects separate. The interval through `__fput` return is an
observed upper bound; the mount reference can have been dropped inside that
function before it returns.

No file contents or pathnames are collected. Cwd, root, namespace, pre-coverage
and non-path-open references are outside this inventory. A listed interval
identifies a candidate reference for further investigation. An empty list
cannot prove the absence of another mount reference or explain a historical
failure. The held-file controls validate the observer, not the historical
Factorybox defect. See `docs/production-progress.md` for the retained red/green
controls and exact original admission replay.
