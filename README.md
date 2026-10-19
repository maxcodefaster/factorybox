# Factorybox

Factorybox is an experimental Linux system-container runtime based on Sysbox and CRIU.
The intended lifecycle is start, checkpoint, independent RAM/filesystem fork, pause with
snapshot-RAM release, resume and deletion. It requires no KVM or custom kernel.

This repository is being extracted from the existing runtime experiment. The retained reference
contains the working checkpoint/fork/resume implementation and its regressions. The current
`factorybox` command provides version, platform inspection and local
start/state/checkpoint/fork/pause/resume/delete commands.
There is no production-qualified release.

Sysbox containers share the Linux worker kernel. The initial isolation scope is separate workers
per customer or trust domain. IDEs and agent harnesses are optional applications. Dev Containers
describe development workloads; they do not define the snapshot engine.

## Layout

| Path | Purpose |
| --- | --- |
| `cmd/factorybox` | Diagnostics and local administrative lifecycle commands |
| `internal/lifecycle` | Persisted start/checkpoint/fork intent, exact ownership and reconciliation |
| `internal/sysboxcapture` | Pinned modern libcontainer checkpoint adapter extracted from ramctl |
| `internal/platform` | Explicit reference-platform admission |
| `internal/pageview` | Extracted logical snapshot reader and its native regressions |
| `internal/snapshot` | Extracted CRIU pagemap codec and complete logical memory comparison |
| `upstream/lock.json` | Exact Sysbox/CRIU source commits, archive hashes and patch order |
| `upstream/patches`, `upstream/overlays` | Source inputs applied to the pinned upstream trees |
| `upstream/ports.json` | Provenance of extracted runtime code |
| `tests/reference` | Unchanged source reference, tests and import provenance |
| `tests/reference/fixture` | Sealed full developer lifecycle fixture and required assertion counts |
| `examples/minimal/.devcontainer` | Ordinary minimal Dev Container consumer |
| `tools` | Source preparation and bounded verification commands |
| `artifacts` | Ignored test evidence, including failed runs |
| `dist` | Ignored local build outputs |

The old experiment remains the historical evidence archive. This repository contains source and
provenance rather than copies of its images, binaries, failed runs or large native archives.

## Develop on the Mac

Install mise, then use the pinned Go and Python tools:

```sh
mise install
mise exec -- go test ./cmd/... ./internal/... ./tests/...
mise exec -- python3 -m unittest discover -s tools -p 'test_*.py'
mise exec -- go build -trimpath -buildvcs=false -o dist/factorybox ./cmd/factorybox
./dist/factorybox version
./dist/factorybox doctor
```

`doctor` prints JSON and returns nonzero on the Mac. The runtime executes on Linux. Matching the
reference platform alone does not qualify a node: filesystem, userfaultfd, ownership, leases and
runtime installation need their own checks.
The Go package patterns select maintained commands, libraries and fixtures. Generated evidence
can contain copied upstream Go sources under `artifacts`; those are not packages in this module.
The retained reference modules have their own gates below.

## Local administrative lifecycle

The first extracted API accepts a trusted, administrator-prepared OCI bundle. It runs only on the
reference Linux platform, as worker root, with a private cgroup2 root that has a finite memory
limit. Matching Sysbox manager and filesystem services must already be running inside that worker.
The native fixture provides these services from the same verified component build.

The controller requires the exact runtime executable and SHA-256:

```sh
factorybox --root /var/lib/factorybox \
  --runtime /opt/factorybox/bin/sysbox-runc --runtime-sha256 "$SYSBOX_RUNC_SHA256" \
  start --bundle /work/bundle workspace

factorybox --root /var/lib/factorybox \
  --runtime /opt/factorybox/bin/sysbox-runc --runtime-sha256 "$SYSBOX_RUNC_SHA256" \
  state workspace

factorybox --root /var/lib/factorybox \
  --runtime /opt/factorybox/bin/sysbox-runc --runtime-sha256 "$SYSBOX_RUNC_SHA256" \
  delete workspace
```

The state directory must be root-owned mode 0700. Factorybox copies the OCI configuration,
resolves its rootfs path, assigns an ownership annotation and replaces caller-supplied cgroup
paths with its own persisted identity. It records intent before creating the cgroup and saves
its device, inode and mount identity before launching Sysbox. It executes the verified binary
through its open descriptor. Operations take an exclusive lock; a launcher inherits that lock.

Repeating a completed start reconciles the original process. An attempted start with no confirmed
runtime state returns an uncertain-outcome error and preserves its evidence. It never silently
repeats creation. Deletion checks runtime creation, ownership, saved configuration and cgroup
identity before calling Sysbox. It retains the start request and deletion outcome. Deleted IDs
cannot be reused.

The input rootfs remains caller-owned and must be exclusive to one active sandbox in this state
directory. Factorybox does not delete it. This administrative API is not an untrusted OCI-spec
service or a portable runtime installer. Complete interrupted-capture/restore recovery and
full developer-workload qualification remain incomplete.

Run the extracted lifecycle proof on the existing worker:

```sh
mise exec -- python3 tools/lifecycle_native.py \
  --sysbox-build factorybox-build-sysbox-EXACT_RUN_ID
```

All five native tests passed with zero skips in an owned 3-CPU/3-GiB fixture. They exercise actual
Sysbox start, public CLI state/delete, reconciliation of a lost saved outcome without replacing the
process, shared-rootfs rejection before create intent, uncertain-create preservation, and rejection
of changed runtime creation, cgroup identity, configuration, executable digest and controller root.
The fifth test runs the CLI's extracted source guard against an actual Sysbox process. Its original
PID, runtime creation and overlay UID/GID survive raw cgroup freeze, acknowledged capture-phase
transitions and disarm. This test performs no CRIU dump.
The fixture confirms exact container/volume cleanup and retains controller metadata and failed runs.
It has no IDE or agent dependency. These are lifecycle correctness tests, not fork/resume benchmarks.

The extracted guard carries the reference's controller/helper/tracer failure cases:

```sh
mise exec -- python3 tools/lifecycle_native.py \
  --sysbox-build factorybox-build-sysbox-EXACT_RUN_ID --suite guard
```

This separate 1-CPU/256-MiB private-cgroup fixture uses two kernel RAM actors. All twelve subtests
passed with zero skips and confirmed owned cleanup. It retains the eleven original scenarios and
adds rejection of a foreign cgroup bind mount. A safe source can
thaw after controller loss. An unacknowledged capture remains frozen for reconciliation, including
after an intermediate writer return. Helper death is reported even when the surviving controller
recovers the source. A live tracer prevents acknowledgement. The original process birth, RAM nonce
and counter survive; the unrelated actor remains independent.

Guard operations hold original cgroup, PID and directory handles, and check the unchanged sandbox
record and worker boot. Thread traversal has depth/count bounds and rejects foreign mounts.
Sysbox's ordinary manager pause changes ownership and volume state, so guarded capture uses the
original kernel cgroup freezer directly. Failed or interrupted CRIU command reconciliation and
network-lock recovery still need extraction and qualification.

The ordinary retained-source RAM capture now has its own native gate:

```sh
mise exec -- python3 tools/lifecycle_native.py \
  --sysbox-build factorybox-build-sysbox-EXACT_RUN_ID \
  --criu-build factorybox-build-criu-EXACT_RUN_ID \
  --native-base-build factorybox-build-native-base-EXACT_RUN_ID --suite capture
```

This uses the reference's pinned runc 1.3.4 library and CRIU Go RPC 6.3.0 behind the
`internal/sysboxcapture` adapter. It preserves opaque Sysbox state in a private raw copy and adds
only a per-capture CRIU configuration annotation to that copy. The original runtime state remains
unchanged. The worker verifies a sealed request, original kernel handles and the exact CRIU
executable before capture. It retains the operation lock and writes its own intent before CRIU
starts. The source guard remains in its capture transaction until the caller acknowledges completion.

Four native checks passed without skips: configuration-view preservation, empty-hook admission,
complete filesystem copy/rejection cases, and the public checkpoint command. The CRIU test captures
an 8192-byte random RAM payload from a Go process inside
Sysbox under a Factorybox identity. The same process advances its RAM state after thaw, while the
captured page-file hashes stay unchanged. The combined capture now also includes the physical
overlay backing layers and private managed volumes. The extended native test also restores an
independent process through the public fork command and recaptures/forks its descendant.
The separate pause suite covers RAM release and repeated resume. The full developer workload
remains unqualified.

Filesystem capture admits the saved ID-mapped, uncloned Sysbox overlay with no executable hooks.
It verifies the controller's overlay mount names the bundle's exact `lower`, `upper` and `work`
directories. The worker's manager root is explicit. Every external bind must be a recognized
private manager volume, the source's Sysbox FUSE tree or `/dev/null`; unowned binds are rejected.
This initial policy is not arbitrary mount or filesystem support.

The worker copies `lower`, `upper` and all admitted private volume directories through pinned
directory handles with the reference's `cp -a --reflink=auto` behavior. Source and saved trees
receive complete content/metadata digests. Copies must have independent inodes and preserve
UID/GID, mode, mtime, hardlink relationships, symlink targets, device identities and extended
attributes. The native fixture includes whiteouts, FIFO/socket nodes, sparse file data and a
trusted overlay xattr. Physical inode identities and ctime are not promised to remain equal.
Foreign mounts and xattr corruption are detected. Source marker and Docker-volume mutations,
followed by original-runtime deletion, leave the captured files intact.

Each tree records entry counts, logical file bytes, actually hashed bytes and charged capture
time. Hardlinked payloads are hashed once per inode per frozen-tree scan. In two individual
observations of the small native fixture, repeated logical payload reads dropped from about
471 MB to 19 MB and the recorded filesystem phase changed from 1.59 s to 0.32 s. The workload
contained nine roots and 472 entries, mostly an empty private runtime. These observations are
not a full developer-workload fork/resume distribution or p95.

The guarded capture also exports Sysbox's virtual proc/sys state at that same frozen point.
A root-only Unix RPC returns a bounded, versioned snapshot of its cached virtual values,
creation time, UID/GID mappings and proc protections. It includes no PID, namespace or FUSE-server
handles. Import creates independent pending state and uses ordinary registration to obtain fresh
kernel identities. Import does not look up the original container; saved mappings and proc
protections remain mandatory at final registration.

The native capture test saves virtual `/proc/sys/kernel/panic` value `17`, lets the same source
continue and change it to `29`, and confirms the saved snapshot stays unchanged. The worker kernel's
value is unchanged throughout. Unfrozen export is rejected. The state-level native test also checks
import after removal of the original registration, independent target mutation, duplicate rejection
and rollback on FUSE-server creation failure. Kernel-backed proc/sys resources still require CRIU;
this snapshot captures Sysbox's own virtual datastore, not an arbitrary host kernel.

The capture fixture uses 3 CPUs/3 GiB and the explicitly selected native-base build for dependencies.
After its writers stop, a separate bounded helper archives only its owned capture and source-layer
directories from a read-only volume. Archive failure retains the owned source volume and fails the
gate. The passing run archived its source and images and confirmed complete container/volume cleanup.
The explicit native-base build removes the historical image dependency for this fixture.
Public checkpoint/fork publication has native proof; the full snapshot lifecycle and production
qualification remain incomplete.

Use an immutable caller-chosen checkpoint ID to retry the same request:

```sh
factorybox --root /var/lib/factorybox \
  --runtime /opt/factorybox/bin/sysbox-runc --runtime-sha256 "$SYSBOX_RUNC_SHA256" \
  --criu /opt/factorybox/bin/criu --criu-sha256 "$CRIU_SHA256" \
  --manager-root /var/lib/sysbox/manager \
  checkpoint --snapshot-id 0123456789abcdef0123456789abcdef workspace
```

Checkpoint saves intent before starting the source guard. It captures RAM, virtual Sysbox state,
overlay layers and admitted private volumes while the source is frozen. It syncs the complete
capture result before acknowledging the final safe point, then thaws the original source.
It publishes ready metadata atomically without replacing an existing generation. Every replay
verifies the original request, all image/metadata hashes and complete filesystem tree digests.
Records have explicit size, regular-file and ownership checks.

An incomplete capture refuses a second capture under the same ID. If capture completed but ready
publication was lost, recovery requires the original guard's completed outcome and unchanged
source identity. It checks the frozen source for tracers before thawing. It durably records that
recovery so another lost publication reuses the same result, including after source deletion.
This does not yet recover a failed or interrupted CRIU operation.

The native CLI test proves identical replay, rejection of corrupted metadata and changed manifests,
oversized/symlinked record rejection, incomplete-capture refusal, unresolved-guard refusal, repeated
publication recovery and independence after source deletion. One small-fixture observation measured
0.732 s for the complete CLI checkpoint and 0.062 s for integrity-verified replay. These include
integrity work and are individual observations, not fork/resume measurements or p95.

The filesystem adapter also materializes complete independent copies from a saved generation.
It verifies the parent, requires a new destination and compares every copied tree against its
saved digest. The native test creates two siblings after deleting the original runtime, preserves
internal hardlinks, verifies separate writable inodes and changes one sibling's overlay file and
Docker-volume file. The parent and other sibling retain their complete digests. Existing targets,
nested destinations and canceled requests are rejected.

The internal restore adapter now uses a sealed request and persisted target creation intent.
It creates fresh manager/FUSE registrations from saved virtual-state bytes, copies private
manager volumes and restores through a separate mount namespace. It has no dependency on the
original runtime or its live registration. The target gets its own immutable runtime ID,
ownership annotation, cgroup and kernel namespace identities. Vendor config types stay inside
the pinned Sysbox executable.

The target's original cgroup descriptor crosses the mount-namespace boundary. The child verifies
its saved original mount identity and checks that the new path still identifies that same inode.
The inherited operation lock is checked without creating duplicate Go file owners. A regression
test forces garbage collection between checks; the original duplicate-wrapper implementation
closed the caller's descriptor and failed this test.

The native test deletes the original runtime, restores its exact 8192-byte private RAM payload and
counter, virtual panic value, overlay marker and Docker-volume file. It then checkpoints that
restored process, restores an independent descendant and mutates its RAM, files, private volume
and virtual panic value. Its source's RAM/files/virtual values, the worker kernel value and the
complete saved filesystem digests remain unchanged. Exact deletion and overlay cleanup pass with
at most two live sandboxes.

Fork from an already published checkpoint:

```sh
factorybox --root /var/lib/factorybox \
  --runtime /opt/factorybox/bin/sysbox-runc --runtime-sha256 "$SYSBOX_RUNC_SHA256" \
  --criu /opt/factorybox/bin/criu --criu-sha256 "$CRIU_SHA256" \
  --manager-root /var/lib/sysbox/manager \
  fork --source workspace --snapshot-id 0123456789abcdef0123456789abcdef branch-one
```

The target ID identifies one immutable fork request. Factorybox persists its runtime owner, source
checkpoint digest, pinned recipe, OCI input and saved virtual/runtime metadata before preparing
backing or creating a process. It verifies the complete saved generation and copies independent
filesystem and RAM-image backing. It creates a private overlay and persists kernel identities
before restore. The original runtime may already be deleted, and its old rootfs paths may be absent.
Forking never checkpoints or freezes a later live source.

The backend syncs its successful completion before returning it to the controller. The receipt
binds the exact sealed request to its target owner, runtime creation and process start time.
Repeated commands return the same running target. A lost controller outcome can be completed from
that receipt without another restore. Missing or mismatched receipts and unfinished backing
preparation refuse replay and preserve evidence. They do not yet provide automatic reconciliation
of every interrupted preparation or CRIU failure.

Ordinary start cannot reuse a reserved fork ID. Deletion verifies the original rootfs mount ID
as well as its device/inode before unmounting a fork-owned overlay. A foreign bind mount of the
same rootfs inode is rejected before stopping its process. Caller-owned start rootfs directories,
retained fork backing, snapshots and history stay on disk. Deleted target IDs cannot be recreated.

Before mounting a new fork overlay, Factorybox persists the uncovered mountpoint's device, inode
and mount identity, bound to the fork owner, OCI input and checkpoint digest. If runtime deletion
and overlay unmount finished but the controller lost its final saved outcome, repeated deletion
can confirm that same empty original directory and finish its tombstone. A replacement directory
or foreign bind mount is rejected and retained. Forks without this original mountpoint proof
refuse this recovery; an absent mount alone is insufficient.

The native proof exercises public CLI replay, lost completion recovery, rejection of missing/wrong
receipts, incomplete preparation and uncertain-restore preservation, source-rootfs independence and
exact deletion. It also stops the actual public controller while its backend finishes, kills that
controller after durable completion, and recovers the same process/owner/private RAM. A completed
fork and its replay remain usable while the source checkpoint directory is unavailable.
Two individual small-fixture observations measured 1.55 s and 1.03 s from public
fork invocation to the first private RAM response, including verification, filesystem/image copies,
mount, restore and receipt reconciliation. Other shared-worker observations have exceeded 3 s,
reaching about 7 s during concurrent qualification work.
These are complete small-fixture observations, not full developer-workload distributions or p95.

Pause retains the original Sysbox runtime ID and creation identity while releasing its processes
and snapshot payload cache:

```sh
factorybox --root /var/lib/factorybox \
  --runtime /opt/factorybox/bin/sysbox-runc --runtime-sha256 "$SYSBOX_RUNC_SHA256" \
  --criu /opt/factorybox/bin/criu --criu-sha256 "$CRIU_SHA256" \
  --manager-root /var/lib/sysbox/manager \
  pause --snapshot-id aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa branch-one

factorybox --root /var/lib/factorybox \
  --runtime /opt/factorybox/bin/sysbox-runc --runtime-sha256 "$SYSBOX_RUNC_SHA256" \
  --criu /opt/factorybox/bin/criu --criu-sha256 "$CRIU_SHA256" \
  --manager-root /var/lib/sysbox/manager \
  resume --snapshot-id aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa branch-one
```

Pause persists intent, freezes the original source and uses the ordinary complete retained-source
writer. It copies and syncs filesystem/image backing at that frozen point before stopping only the
original cgroup through its pinned descriptor. The stop intent follows complete backing and a final
untraced-source check. The guard accepts completion only after its original PID-fd has exited and
the original cgroup tree is empty. It clears that empty cgroup's freezer so Sysbox reports stopped.
It does not call Sysbox manager pause, unregister services or change worker-wide settings.

The pause snapshot is ordinary file-backed storage. Pause rejects RAM-backed payloads, syncs their
files and applies `POSIX_FADV_DONTNEED` only to its owned page images. `mincore` checks their resident
pages without faulting them in. Completion requires zero resident payload bytes. The retained pause
record includes compact before/after cgroup memory and snapshot residency observations.

Resume verifies that the exclusive working filesystem still matches the pause point. It copies
private restore images, persists the attempted operation, reconstructs the ID-mapped root in a
private mount namespace and restores through the pinned Sysbox/CRIU path. It refreshes filesystem
and manager namespace bindings before releasing tasks. A per-operation CRIU configuration avoids
worker-wide changes. Its completion receipt binds the request to the renewed process while retaining
the original runtime ID, owner and creation time. Repeated requests and lost controller outcomes
reconcile that process. Unconfirmed restores refuse a second restore.

Before the first restore attempt, a lost preparation receipt can be recovered when every private
input file still matches the complete immutable pause generation. The controller verifies and syncs
those files and their directories, then publishes the missing receipt without replacing an image.
An incomplete or damaged copy stays retained and cannot start a restore. A native test removes the
receipt, first withholds a RAM image to prove refusal, then restores it and confirms successful
resume with the identical original page-file inode.

Resume also requires the entire original cgroup tree to be empty with its freezer cleared.
The controller checks events through the identity-verified original cgroup before changing pause
intent, before preparing images and immediately before the restore call. A native negative fixture
places an unrelated process in a child cgroup while the original init is stopped. Resume refuses
before creating its operation directory, retains the identical pause record and leaves that process
alive. The original admission check accepted this fixture; the regression now passes.

The native pause suite uses an actual 32-MiB anonymous payload with a retained checksum:

```sh
mise exec -- python3 tools/lifecycle_native.py \
  --sysbox-build factorybox-build-sysbox-EXACT_RUN_ID \
  --criu-build factorybox-build-criu-EXACT_RUN_ID \
  --native-base-build factorybox-build-native-base-EXACT_RUN_ID --suite pause
```

It proves anonymous source memory falling to zero, complete RAM payload files moving from resident
to zero resident bytes, a fork from the stopped checkpoint, identical resumed RAM/files/private
volume/virtual values, receipt recovery, an independent fork of a resumed source, and a second
pause/resume of modified current state. It retains pause history and immutable runtime identities
with renewed process IDs. The bounded fixture uses 3 CPUs/3 GiB, at most two live sandboxes and a
180-second test timeout within its 240-second runner deadline. These observations do not qualify
the full developer workload or sub-3-second p95.

An earlier terminating-writer draft copied filesystem backing before CRIU's own `/tmp` creation;
the native resume check rejected the mismatch. The selected path captures the complete backing
after the retained writer and stops processes only after successful backing. Saved images/source
data survive failed transitions. Automatic recovery of every failed capture, partial preparation,
resume failure or partially completed runtime deletion still requires hardening. Recovery requires the same live
worker and unchanged exclusive working backing; worker restart/cross-node migration remain unsupported.

## Pinned sources

```sh
mise exec -- python3 tools/sources.py prepare
mise exec -- python3 tools/sources.py verify
```

Preparation downloads immutable source archives, checks SHA-256, applies patches with zero fuzz
and verifies the final source hashes. The CRIU inputs start from the experiment's recorded compiled
sources, with later changes declared as ordered patches and updated final hashes. Upstream assertions
are preserved. Declared compatibility patches correct diagnostic formatting, update an obsolete
namespace mock and capability-function name, and give the registration fixture a real namespace
reference. The original and prepared hashes of every changed test file are pinned in the lock.
Every other original upstream test file must remain byte-for-byte unchanged. A fresh checkout can
fetch these sources without the old experiment directory. `--offline` uses previously verified
cached archives.

Source preparation does not install a runtime, change the worker kernel or run the upstream suites.
The selected CRIU source starts from the recorded CAST AI nested-namespace commit. It is not the
unmodified upstream CRIU release. Sysbox components also carry explicit experimental changes.
The Sysbox umbrella integration suite is pinned separately at v0.7.1. Declared test-only
compatibility patches load its missing Cgroup helper and wait for actual daemon-listener readiness.
Original test bodies and assertions remain unchanged. Individual runtime component commits remain
explicit in the lock.

Cached trees also record the complete source tree and its preparation recipe. A changed
implementation file, patch or recipe is rejected even when its tests are untouched. When a reviewed
recipe changes, prepare one exact component again; the previous tree is retained under `.cache/retired`:

```sh
mise exec -- python3 tools/sources.py prepare --component sysbox-runc --refresh
```

Build an explicit native-test/CRIU toolchain from the pinned public Envbox image and the dated Ubuntu
APT snapshot. Package signatures remain enabled; the snapshot's expired validity timestamp is
accepted. The build records all installed package versions and the exact output image manifest:

```sh
mise exec -- python3 tools/build_native_base.py
mise exec -- python3 tools/build_criu.py \
  --native-base-build factorybox-build-native-base-EXACT_RUN_ID
```

The CRIU compile command uses that exact built image, 2 CPUs, 1 GiB, no network and an owned
temporary container. It records whether the resulting binary matches the historical build.
Before compiling CRIU it runs 31 C regression cases against the actual prepared scope header.
The original header rejects all three valid Factorybox ID cases; the added patch accepts only
`/sys/fs/cgroup/fbx-` followed by exactly 32 lowercase hexadecimal characters. The explicit
owned-collection mode is still required. The original experiment names remain accepted; missing
mode, malformed IDs, unrelated roots, descendants and traversal paths remain rejected. All 31
cases and the subsequent native CRIU build passed. This gate checks canonical scope admission,
not kernel ownership by itself or successful checkpoint/restore.
The native base can be reconstructed without the historical experiment image. A repeatable compiler
recipe does not by itself prove bit-identical CRIU output or production runtime qualification.
The historical default builder remains available only when its exact retained image is present.

Run the selected ordinary CRIU checkpoint/restore core cases:

```sh
mise exec -- python3 tools/criu_core.py \
  --criu-build factorybox-build-criu-EXACT_RUN_ID \
  --native-base-build factorybox-build-native-base-EXACT_RUN_ID
```

All 18 unchanged upstream C tests passed with zero skips and two complete ordinary dump/restore
cycles each. They cover file/anonymous/shared mappings, copy-on-write, threads and TLS, futexes,
pipes, Unix socket queues, eventfd/epoll/signalfd, timers, descriptor flags, append mode, file locks
and memfd contents/metadata. Each cycle confirms that the original processes exit and the restored
root keeps its namespace PID with a new process birth. Each test then executes its own original
assertions, must finish in `PASS` and cannot hide an earlier `FAIL` behind a later `PASS`.

`tests/criu-core-cases.json` declares the selection and required arguments. The adapter uses the
upstream static C daemon/readiness/SIGTERM protocol in host flavor inside its owned PID/net/IPC
container. It uses upstream's UID 18943, GID 58467 and supplementary groups for the sixteen
unprivileged cases; the two `suid` cases retain root. It verifies actual credentials before dump
and after each restore. It checks descriptors and refuses unadmitted hooks, dependencies and flavors.
It preserves compiler `-Werror` and every original test assertion. It does not invoke `zdtm.py`'s
entry point, which unconditionally configures global hugepages. `/proc/sys/vm` is read-only in the
fixture. The test source crosses the Mac file share as a hash-verified archive, preserving dangling
upstream symlinks. A first adapter attempt failed restore because unreaped killed children still
held their dead session leader's PID. The adapter now waits for and reaps the complete original
test tree before restoration.

The bounded 3-CPU/3-GiB gate records exact source/binary identities, per-cycle timings and original
logs/images, then archives its stopped owned volume before cleanup. This qualifies these 18
ordinary C cases on the reference ARM64 worker. It does not qualify the full ZDTM matrix, namespace
flavors, a complete developer workload or a latency distribution. The 1030 verified unchanged CRIU
test files are an integrity count, not an execution count.

Build the selected Sysbox runc, fs and mgr components from prepared sources:

```sh
mise exec -- python3 tools/build_sysbox.py
```

The recipe pins the Go and Envbox image digests, disables automatic Go toolchain replacement and
records native compiler/package versions and every context input. The manager includes upstream's
`idmapped_mnt` build tag. A successful build includes the selected inherited fork, registration and
seccomp regressions. It does not establish complete upstream compatibility. Signed APT inputs use
the same dated Ubuntu snapshot as the public native base. The complete installed package manifest
and APT source list are retained with their hashes.

Run two builds without layer reuse and with independent empty Go module/compiler caches:

```sh
mise exec -- python3 tools/build_sysbox.py --clean
mise exec -- python3 tools/build_sysbox.py --clean \
  --compare factorybox-build-sysbox-FIRST_SUCCESSFUL_RUN_ID
```

The comparison requires identical Sysbox runc/fs/mgr binary hashes and installed package manifests.
Two such builds passed on the reference ARM64 builder and reproduced the exact three binaries used
by the preceding native qualification. This proves byte-identical component output with these
inputs on this builder. It does not qualify another architecture or establish a deterministic OCI
image manifest.

CRIU also builds from fresh source inside a new owned container each time. Compare it against one
successful build with the identical pinned native base:

```sh
mise exec -- python3 tools/build_criu.py \
  --native-base-build factorybox-build-native-base-EXACT_RUN_ID \
  --compare factorybox-build-criu-FIRST_SUCCESSFUL_RUN_ID
```

The comparison fails if the binary differs and retains both outputs for inspection.
`upstream/criu-build.json` pins `SOURCE_DATE_EPOCH` and the exact CRIU source recipe.
Upstream uses the build time in its kernel-feature cache magic, so an unfixed build time changes
the executable. The initial comparison found 26 differing bytes, including the ELF build ID.
The builder now uses upstream's existing reproducible-build setting. Review and advance this
release epoch when changing CRIU's source/cache recipe; the build refuses an unreviewed source
recipe change. No CRIU assertion or source patch is needed for this setting.
Two independent builds with that pinned epoch produced the identical CRIU executable on the
reference ARM64 builder. The 31 source-scope cases also passed in each build.
The restore helper is in a separate package so the core library can compile under its natural
Sysbox module name. The component build compiles that test package and the full libcontainer
integration package as guards; execution is a separate native gate. The full upstream Go gate
keeps vet enabled.

## Retained reference

```sh
mise exec -- python3 tools/reference.py verify
mise exec -- python3 tools/reference.py check
mise exec -- python3 tools/reference.py build
```

`verify` checks all 232 imported reference and Sysbox input files. `check` executes the original
host-compatible driver tests. `build` cross-compiles the Linux ARM64 reference and test programs.
Cross-compilation is not native qualification.

The one-time import tool is retained for provenance. Ordinary development must not overwrite the
reference. Port behavior with its tests into `internal`; compare against the unchanged reference.
Fixed `/experiment` paths and optional application-specific assertions remain inside that reference
until their production counterparts have independent proof.

## Native regression on the existing worker

These commands require the existing `orbstack` Docker context and the exact locally retained base
image configuration `sha256:041140e968e8efdbc6b2f33651f84bc19c13593c6d3f527e90af648f967dd52f`.
They never pull a replacement implicitly.

```sh
mise exec -- python3 tools/reference.py native
mise exec -- python3 tools/native_core.py
mise exec -- python3 tools/native_failure.py
```

The selected reference ownership/graph regression uses one uniquely owned 1 CPU/256 MiB container
and a uniquely owned volume. This selected suite is not the complete upstream Sysbox/CRIU gate.
The extracted core tests use 1 CPU/128 MiB without a privileged container. Each stores stdout,
stderr, input identities, outcome and cleanup observations under `artifacts/`. Removal requires
the exact resource name and owner label. A Docker failure is not proof that a resource is absent.
The extracted native core suite has passed all 12 platform, pagemap, inherited-memory and page-reader
tests with zero skips and successful owned cleanup. The reference ownership/graph suite has passed
50 top-level tests. Neither is a full lifecycle or upstream qualification.
The separate source-preservation suite has passed all three native tests and 16 subtests with zero
skips and owned cleanup. It covers controller/helper death, live-tracer rejection, preservation of
foreign PIDs, and rejection of replaced or foreign-mounted cgroups before deletion.

The developer lifecycle runner retains the original 3 CPU/3 GiB probe, separate 1 CPU/768 MiB
Theia, 420-second deadline and maximum two live sandboxes. Supply successful named build artifacts:

```sh
mise exec -- python3 tools/reference_lifecycle.py \
  --criu-build factorybox-build-criu-EXACT_RUN_ID \
  --sysbox-build factorybox-build-sysbox-EXACT_RUN_ID
```

The runner builds an owned image with the new binaries and uses a unique Kubernetes namespace.
It reserves 2.5 probe CPUs on the shared worker while retaining the original 3-CPU limit. This
allows admission beside the retained Factory workloads; observations from this shared scheduling
profile are qualification runs, not a clean performance baseline.
It retains all 461 assertion records from the sealed successful reference, including complete
independent RAM/files, forks from forked/resumed sources, pause RAM release, private Docker,
Compose, Dev Containers, browser/OpenCode responses and native Theia read/edit. A zero process exit
without those assertions fails. Namespace deletion includes the exact saved UID as an API
precondition. Failed runs retain streamed logs and available native metadata.

The full lifecycle is not yet independently qualified in this repository. The first attempts found
runner transport/admission issues. A later attempt reached private Docker, Compose and Dev Container
assertions, then OrbStack's internal Docker agent stopped unexpectedly on October 18, 2026 at
14:56:50 UTC. Both test containers subsequently terminated with exit code 255. The worker kernel did
not reboot. The cause of the agent termination is unresolved, and this run provides no complete
fork/resume latency result. Its exact namespace cleanup was later confirmed separately.

If owned namespace deletion outlasts the runner, preserve its artifact and reconcile only that run:

```sh
mise exec -- python3 tools/reconcile_lifecycle.py factorybox-lifecycle-EXACT_RUN_ID
```

This command verifies saved UIDs and ownership, can remove a confirmed terminal Pod with its UID
precondition, and records a new observation without rewriting the original result. It leaves
namespace finalizers and unrelated cluster services intact. Exit 75 means cleanup remains pending.

This is a reference fixture. It still depends on the existing local Theia image and a read-only
Btrfs rootfs cache at its recorded worker path. It does not provide the public lifecycle API or
a portable installation. Theia browser editing and active model inference are not asserted.

Run the full upstream Go unit suites in an owned container with its own Docker daemon:

```sh
mise exec -- python3 tools/upstream_unit.py \
  --sysbox-build factorybox-build-sysbox-EXACT_RUN_ID
```

The complete native `libcontainer` unit package has passed 27 tests and 30 subtests with zero
skips after correcting the restore helper's package boundary. Run that package from its independently
compiled test binary with:

```sh
mise exec -- python3 tools/upstream_unit.py \
  --sysbox-build factorybox-build-sysbox-EXACT_RUN_ID --suite libcontainer
```

Run the full libcontainer integration package, or one exact test during diagnosis:

```sh
mise exec -- python3 tools/upstream_unit.py \
  --sysbox-build factorybox-build-sysbox-EXACT_RUN_ID --suite runc-integration
mise exec -- python3 tools/upstream_unit.py \
  --sysbox-build factorybox-build-sysbox-EXACT_RUN_ID \
  --suite runc-integration --test TestExecInError
```

The integration package passed 47 tests with 12 explicit upstream skips after fixing lost exec
errors. Sysbox's additional-process initializer now resolves the executable before closing its
error channel and invokes the resolved path after closing internal FDs. The unchanged upstream
test checks all 42 missing-executable attempts. The original failure and successful regression
are retained separately.

The runner checks runc, fs, mgr, ipc and each nested libs module, retaining failures and skips per
test. It also rejects failed test events hidden by an upstream `TestMain` that exits zero.
The test rootfs follows upstream's immutable Busybox ARM64 commit and has a recorded SHA-256.
The full suite's private Docker daemon uses a uniquely owned Btrfs volume at `/var/lib/docker`,
the upstream-expected `overlay2` driver, and a digest-pinned Alpine image under its local test alias.
ID-mapping probes use a separate owned volume at `/var/lib/sysbox`, rather than the test image's
overlay mount. Synthetic HugeTLB parser fixtures always exercise both configured fixture sizes,
including negative cases on workers without configured HugeTLB pages.
No worker Docker socket is mounted. The bounded test controller moves into its own cgroup leaf
before enabling controllers within the private root; node-wide cgroups and sysctls stay untouched.
Do not run the Sysbox umbrella Makefile on the Mac or retained worker. Its integration scripts need
a separately prepared, isolated test environment and include host-oriented cleanup commands.

The complete fs module has passed 67 tests and 73 subtests with zero skips. Its positive registration
fixture checks the retained namespace FD, and namespace-event tests check distinct UID/GID values.
The complete ipc module has passed three tests and three subtests with zero skips, including a real
local RPC server rejecting pre-registration, registration and update. Module bookkeeping and malformed
client warnings are corrected with vet enabled.

The mount-table parser accepts the observed empty cgroup2 source field while rejecting missing
separators, missing required fields and incomplete reads. Original parser tests and the added
regressions pass. After adding virtual-state snapshots, all 18 Go module suites passed together
in one native run: 393 top-level tests and 315 subtests. This includes the frozen FUSE export,
independent import after source-registration removal, exact wire encoding and path-scope tests.
After the new resume adapter, the complete Runc module passed 279 top-level tests and 227 subtests
with 14 explicit upstream skips. The unchanged modules retain their preceding combined pass. The manager's
four shiftfs tests explicitly skip on this worker, which has no shiftfs module. The
broader umbrella/outer-systemd and CRIU checkpoint/restore matrices remain unqualified.
The separately selected 18 ordinary CRIU C cases have passed their two-cycle gate.

Run the complete selected upstream Bats daemon-lifecycle file:

```sh
mise exec -- python3 tools/upstream_bats.py \
  --sysbox-build factorybox-build-sysbox-EXACT_RUN_ID \
  --native-base-build factorybox-build-native-base-EXACT_RUN_ID --suite lifecycle
```

All seven cases passed with zero skips in the first original run. They cover PID files, graceful
and SIGKILL restart of both daemons, and rejection of overlapping instances. A repeat under worker
load exposed the fixed one-second startup assumption: PID checks ran before startup completed.
A declared fixture patch retains that delay, then waits at most five seconds for the actual RPC
listener. It never polls the PID-file predicate being tested. All original test bodies and PID-file
assertions are byte-identical. Both daemons create their PID file before opening that listener.
The complete lifecycle file passed again with seven executed passes and zero skips after that
fixture fix, with unchanged binaries, input hashes and observed global sentinels.
The fixture owns its PID/network/cgroup namespaces, native data volumes and daemon logs. It uses
the exact selected Sysbox binaries, signed dated APT dependencies and Bats 1.2.1. Capabilities for
kernel modules, raw I/O, reboot, time and syslog are absent from effective, permitted and bounding
sets before any daemon starts.

Prepare the original ARM64 Systemd test images in a separate owned private Docker daemon, then
run the whole selected upstream guest-Systemd file:

```sh
mise exec -- python3 tools/bats_images.py \
  --native-base-build factorybox-build-native-base-EXACT_RUN_ID
mise exec -- python3 tools/upstream_bats.py \
  --sysbox-build factorybox-build-sysbox-EXACT_RUN_ID \
  --native-base-build factorybox-build-native-base-EXACT_RUN_ID \
  --suite systemd --images factorybox-bats-images-EXACT_RUN_ID
```

`upstream/bats-images.json` fixes the two upstream ARM64 image manifests. Acquisition checks
architecture and digest, creates the original test aliases only inside its new daemon, and records
the archive hash and exact image configuration IDs. The test fixture loads that verified archive
offline and checks those IDs again. It has no worker Docker socket. Selected global kernel
sentinels are compared before and after; no helper writes or restores a kernel value.

The full selected Systemd file executed with three passes, two original upstream skips and one
failure. Focal boot/service restart and both mount-conflict/override cases passed. The `/proc`
exposure case expects a present `/proc/kcore` whose read fails with `Permission denied`.
The reference OrbStack worker does not expose `/proc/kcore`, even in a separate privileged
non-Sysbox fixture. The actual error is `No such file or directory`. That assertion remains
unchanged and the gate remains failed. A matching kernel/platform is still needed to qualify
the complete original exposure check; this task changes neither the kernel nor the assertion.

An initial Systemd attempt exposed a fixture incompatibility: adding a read-only `/proc/sys`
parent bind prevents Sysbox's existing helper from mounting the target PID namespace's procfs.
A separate bounded namespace probe reproduced the difference with identical capabilities.
The daemon-lifecycle fixture retains that extra read-only bind; the guest-Systemd fixture uses
the ordinary privileged upstream topology with module-loading still blocked. This required no
Sysbox runtime patch.

The pinned Systemd file also omitted its existing Cgroup helper import. Its declared patch adds
that import only, with original/prepared hashes in the source lock. It preserves every test body,
assertion and skip condition. The Bionic case requires cgroup v1; Arch has its original unconditional
upstream skip. Both remain explicit in evidence. The runner checks the complete TAP plan, exact
case titles and numbers, process exit, OOM state and observed global sentinels. A missing case or
unexpected skip fails the gate. Failed native volumes are archived before exact owned cleanup;
archive failure retains those volumes.

These are direct Sysbox/Docker integration cases. They do not qualify the whole umbrella suite,
outer-host Systemd service packaging, Factorybox RAM restoration of Systemd or the complete
developer composition. The native fixture has a 3-CPU/3-GiB ceiling, a 330-second Bats deadline
and at most one guest container. Upstream's fixed 40-second waits are retained, so this suite
is not a startup-latency benchmark.

## Release requirements

Factorybox has no production-qualified release yet. Required gates include:

- Complete bounded failure recovery across public lifecycle transitions, including partial
  capture/preparation/restore and lost deletion outcomes.
- Pinned upstream Sysbox unit/core/integration suites and CRIU core checkpoint/restore suites.
  Failed or unrun required suites block qualification. Preserve upstream assertions.
- Independent files and RAM across repeated forks, forks of resumed/forked sources and actual
  pause RAM release followed by correct resume.
- Bounded controller/helper failure recovery, source preservation and exact cleanup.
- A supported-platform matrix backed by native execution. Initially only the exact tested ARM64
  worker is a candidate. AMD64 and cloud workers remain unqualified.
- Reproducible native builds, component license notices and a chosen license for Factorybox's
  original code before public distribution.
- Honest request, preparation, first-use, CPU/I/O and retained-memory observations. The sub-3-second
  p95 goal remains unachieved. Historical component improvements and tiny samples do not establish it.

Recovery is currently scoped to the same running worker. Cross-node migration and RAM recovery after
worker restart are outside the first release. Kubernetes deployment needs appropriate worker access
and runtime installation; a container image alone does not make arbitrary managed clusters supported.

`examples/minimal/.devcontainer/devcontainer.json` is an ordinary digest-pinned Linux workspace.
It has no IDE or agent dependency and requires no access to a node Docker socket.
`examples/factory` adds an ordinary two-service Compose Dev Container and the existing Factory
Connector V1 assignment format. Both services use the same exact native Node image. The
`node` remote user keeps UID 1000 with `updateRemoteUserUID: false`; the example prepares checkout
ownership explicitly instead of letting the CLI rebuild the image to match the root daemon user.
Its app reports an instance nonce and counter held only in RAM, reads the workspace marker and
post-create hook file, and requests its dependency through Compose DNS.

Prepare existing Factory consumer images by exact role digests, then export the runtime rootfs:

```sh
mise exec -- python3 tools/devx_inputs.py \
  --factory-artifacts /absolute/path/to/runtime-artifacts.json --pull-missing
mise exec -- python3 tools/devx_project_images.py
mise exec -- python3 tools/devx_compose.py \
  --consumer-inputs factorybox-devx-inputs-EXACT_RUN_ID \
  --project-images factorybox-devx-project-images-EXACT_RUN_ID \
  --native-base-build factorybox-build-native-base-EXACT_RUN_ID
```

The artifact JSON supplies `build-runtime-host` and `developer-ide` digest references. Preparation
checks Linux ARM64 and actual Docker repository digests, records versions, and exports a
never-started runtime container without anonymous volumes. Digest comparison accepts Docker's
familiar `node@sha256:...` spelling for `docker.io/library/node:TAG@sha256:...`; repository and digest
must still match. The observed existing runtime contains Docker 29.9.0, Compose 5.6.0,
Dev Containers CLI 0.89.0, Node 24.21.0, the real Factory Connector, OpenCode server 2.0.27
and Bun 1.4.3.

The component gate passed with both services, the real CLI's `read-configuration`, `up` and `exec`,
the post-create hook, Compose DNS/HTTP and the app's initial RAM state. It uses a separate
3-CPU/3-GiB container with private PID/network/cgroup namespaces and its own Docker socket and
data volumes. A test-only PID 1 helper delegates controllers within its bounded private cgroup
and drops global module/raw-I/O/boot/time/syslog capabilities before starting the image's init.
The native script checks those capabilities after exec and compares fixed global kernel
sentinels before and after. It verifies frozen inputs and archives failed volumes before exact
owned cleanup.
The final example, including its explicit Compose identity and `wget` healthcheck, passed again
in `factorybox-devx-compose-dd27b3f3f6ab461a8e4ec22b3c9c3f23`.

An initial offline load exposed that Docker's saved OCI archive did not restore `RepoDigests`.
The passing fixture instead pulls through a loopback-only, read-only test mirror that verifies
every retained blob checksum and serves the original manifest bytes from that archive. Its
outer network is disabled. The ordinary example and Connector retain their exact digest pins;
no tag substitution or digest-check bypass is used.

This passing component gate does not qualify Factorybox capture/fork/resume of the developer
stack, an active Connector control connection, separate Theia or OpenCode inference. The
assignment's endpoint and example identities require explicit test or installation wiring.
Separate Theia and optional OpenCode composition remain in progress. IDEs and agent harnesses
remain outside the core lifecycle. Production Factory integration and public publication are
separate steps after qualification.

Run the developer stack through the public Factorybox CLI with the exact prepared inputs:

```sh
mise exec -- python3 tools/devx_lifecycle.py \
  --consumer-inputs factorybox-devx-inputs-EXACT_RUN_ID \
  --project-images factorybox-devx-project-images-EXACT_RUN_ID \
  --native-base-build factorybox-build-native-base-EXACT_RUN_ID \
  --sysbox-build factorybox-build-sysbox-EXACT_RUN_ID \
  --criu-build factorybox-build-criu-EXACT_RUN_ID
```

This separate gate freezes its Go source context, scripts, ordinary example, component binaries
and image archives. Its outer fixture has private namespaces, a 3-CPU/3-GiB limit, no external
network and a 420-second deadline. At most two live Sysbox containers are permitted by its
sequential lifecycle. The real Factory Connector authenticates its parent and injected child
against a test authority inside the captured network. Requests then traverse the child HTTP
protocol to the real app and its Compose dependency. The authority, app nonce and counter
live in RAM. The gate requires complete captured state, independent app mutations and files,
forks of forks, pause RAM release, resume and a fork of a resumed sandbox.

The first complete prepared-start response passed with the real Connector, Docker vfs,
Compose DNS, Dev Container hook and app RAM in
`factorybox-devx-lifecycle-91f7518db5c34efcbd4cf0d801044514`.
Rootfs preparation took 41.528 seconds; the subsequent public start through first app response
took 91.620 seconds on the shared worker. These are individual preparation/start observations.
They are not fork/resume timings or a latency distribution. The ordinary Compose example fixes
its name to the assignment's `factorybox-example`; an earlier attempt correctly failed the
Connector's project-identity check. A repeat's one-second Node healthchecks became unhealthy
under worker load in a 0.25-CPU service. The passing prepared-start uses the existing `wget`
applet instead of launching another Node process for each check.

The same developer run failed its first checkpoint: the selected CRIU reports
`Can't dump nested time namespace` for a Docker child. It therefore provides no complete
developer fork, pause or resume qualification. The selected CRIU nested-namespace adapter admits
user, UTS, network, PID, IPC and cgroup namespaces; time namespaces remain unsupported.
The unchanged upstream `timens_nested` case also declares an expected checkpoint failure.
This needs a focused reproducer and an explicit compatibility decision before modifying CRIU.
The full gate remains failed. An initial diagnostic attempted exec after the failed capture;
that is unsuitable for an unresolved frozen source and has been removed from the fixture.
Logs are observed through the real CLI's stderr and the guest's merged filesystem instead.

The captured test authority proves the actual local protocol path. It does not prove real
external Factory handoff, fresh credentials for a fork, external TCP recovery, Theia, browser
editing or model inference. Those remain separate requirements. The closed test authority
and test stderr wrapper are never runtime or application dependencies.

Failed volumes are fully archived before removal. Archives now stream through Docker's attach
connection into a host file, with Docker logging disabled for that binary stream. The helper
refuses foreign or live volume attachments and checks ownership again before removal.
Raw streaming completed the retained multi-gigabyte developer archive after both a bind-share
write and two compressed attempts exceeded their deadlines. Compression is not retained on the
selected path. If a failed developer run explicitly retains its volume, reconcile only that run:

```sh
mise exec -- python3 tools/reconcile_devx.py factorybox-devx-lifecycle-EXACT_RUN_ID
```

Reconciliation writes a separate receipt and leaves the original failed result unchanged.
It allows up to 600 seconds for the stopped multi-gigabyte archive at the same 1-CPU/256-MiB
helper limit. This does not extend the active lifecycle's 420-second deadline.
Archive failure preserves the original volume. A successful cleanup receipt is not a
successful lifecycle result.
