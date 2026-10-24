# Owned-namespace mount observation

This optional Linux AMD64 diagnostic observes file lifetimes and unmounts in
one explicitly owned outer PID namespace and its descendants. It is not a runtime
service or a production dependency. Run it only inside an owned native fixture with the
existing resource, artifact and ownership guards.

The original frozen build recipe and pinned Cilium v0.17.3 helper headers are in
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

Successful path opens include O_PATH and exclude failed opens. The current
filter checks the typed current-task PID namespace chain for the exact selected
outer namespace, including descendant tasks. It returns that ancestor's PID;
the loader verifies the bulk-read `upid` layout against the live kernel BTF.
The event layout and five file/unmount probes are unchanged. A subsequent release
of a tracked file can be observed on another task or a kernel worker. File pointer generations keep
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

The old active-namespace-only build remains frozen. Controlled counterexample
`factorybox-mount-descendant-control-5e7f788aa487470cacc9aa196b24402e`
has a real child-PID-namespace O_PATH holder, actual EBUSY while held and normal
unmount after release, but the old observer misses that held interval. Its
coverage verdict remains false. This is a diagnostic coverage gap, not an
attribution of the historical Factorybox failure. A new observer build must
pass the same descendant control before it is used for attribution.

Build `factorybox-mount-ref-tracer-97707c9501da4164a0328a551d71804e`
passes that control in
`factorybox-mount-descendant-control-b8df8af032a442609f75bfebfc50fdb1`.
The child-PID-namespace holder's exact mount interval spans EBUSY and ends
before the successful cleanup completes. All read/loss/map and missed-probe
counters are zero. Both cases use byte-identical control sources; only the
observer build differs. Cwd, root and namespace-reference coverage is still
outside the observer.
