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

For an ordinary administrator-provided bundle, the worker copies `lower`, `upper` and all admitted
private volume directories through pinned directory handles with the reference's
`cp -a --reflink=auto` behavior. Source and saved trees receive complete content/metadata digests.
Copies must have independent inodes and preserve
UID/GID, mode, mtime, hardlink relationships, symlink targets, device identities and extended
attributes. The native fixture includes whiteouts, FIFO/socket nodes, sparse file data and a
trusted overlay xattr. Physical inode identities and ctime are not promised to remain equal.
Foreign mounts and xattr corruption are detected. Source marker and Docker-volume mutations,
followed by original-runtime deletion, leave the captured files intact.

An explicitly prepared Factorybox base avoids repeating that complete lower-layer read and copy.
The public `prepare-base --source PATH BASE_ID` command imports the administrator's source into
`ROOT/bases/BASE_ID/rootfs`, compares the complete source/copy content and metadata, persists its
intent and manifest, and covers the private backing with a read-only self-bind. Preparation is
charged before application startup. The caller binds that prepared root read-only at the ordinary
bundle's `lower` path and adds the OCI annotation `"io.factorybox.base": "BASE_ID"`.
The original mutable input is never trusted as an immutable base or shared into checkpoints.

For example, inside the existing worker mount namespace, with an empty bundle `lower` directory:

```sh
factorybox --root /var/lib/factorybox \
  --runtime /out/sysbox-runc --runtime-sha256 EXACT_SHA256 \
  prepare-base --source /work/base-input developer-base
mount --bind /var/lib/factorybox/bases/developer-base/rootfs /work/bundle/lower
mount -o remount,bind,ro /work/bundle/lower
```

Only this imported immutable lower is shared. Every checkpoint and fork gets its own checked
read-only alias; writable upper layers and private volumes retain complete digests and independent
inodes. Admission checks the canonical base's original mount, inode, ownership, mode and timestamps,
both paths' read-only flags, private root-owned ancestors and absence of descendant mounts.
Aliases retain their original uncovered mountpoint identity for exact deletion and replay.
Worker root remains trusted. The base stays retained with its checkpoints, including after the
original runtime or caller input is removed. Base garbage collection and recreation after losing
the worker mount namespace are not implemented.

The `base` native profile runs the existing capture/fork/source-deletion/recovery assertions with
this explicit prepared-base path. `factorybox-runtime-900bff3d6f9f42f5bc21998a199951f7` passed six
top-level tests and 32 subtests, with no skips and owned cleanup. It includes nine new immutable-base
admission, source-independence and alias-retirement cases. Individual small-workload observations
were 3.627 s for checkpoint and 1.983/1.825 s through the first useful fork/descendant RAM response.
These observations do not establish a latency distribution or qualify the full developer workload.
Use `tools/lifecycle_native.py --suite base` or `--suite base-pause` with the same exact build
arguments as the ordinary capture/pause profiles.
The first `base-pause` run, `factorybox-runtime-a7e274790e7c49f68483b36acda838ec`, failed during
the sixteen-cycle restore/delete test. Its initial 1.979-second pause released all original
anonymous memory and snapshot payload residency, and the first stopped-source fork passed.
The fourth cycle spent about 25 seconds reaching completed backing preparation, then the
test's 30-second public-fork deadline killed its controller during restore. Later operations
correctly refused the still-held operation lock. This does not qualify repeated pause/resume.
The complete 217,055,232-byte failed archive is retained with SHA-256
`af51397334983774024fde07cc5ec005d5968db9d28e90248a42cd7f52b82b53`.
An archive-helper removal exceeded its Docker client timeout; a separate ownership-checked
reconciliation verified that archive again and completed cleanup. The original failed result
remains unchanged.

Each tree records entry counts, logical file bytes, unique hashed content size and charged capture
time. For an immutable lower, the content counts describe its verified import; subsequent alias
capture does not read that payload again. Hardlinked payloads are hashed once per inode per
frozen-tree scan. In two individual
observations of the small native fixture, repeated logical payload reads dropped from about
471 MB to 19 MB and the recorded filesystem phase changed from 1.59 s to 0.32 s. The workload
contained nine roots and 472 entries, mostly an empty private runtime. These observations are
not a full developer-workload fork/resume distribution or p95.

Filesystem generation copies now use one `syncfs` durability barrier after all complete target
digests, instead of flushing the same filesystem separately for each layer or empty private volume.
The generation records that shared barrier time as `durability_us`; per-tree `capture_us` excludes it.
Standalone tree imports retain their own barrier. Every entry still undergoes mount,
content, metadata and source-inode independence checks. A failed barrier prevents publication.
The barrier also flushes other dirty data on the same filesystem, so its latency depends on
other writes there.

The metadata benchmark contains 2,048 regular files in sixteen directories and 1 MiB of content.
Two individual Btrfs comparisons measured 13.519 to 0.661 seconds and 6.578 to 0.823 seconds.
The repeat reused the exact earlier binaries; their frozen source contexts differ only in
`internal/sysboxcapture/filesystem_linux.go`. Source-reading time in that repeat was 0.123 and
0.126 seconds. The approximately eightfold repeat improvement is for this filesystem workload,
not a complete developer checkpoint/fork or a latency distribution. Evidence is retained in
`factorybox-filesystem-benchmark-17fad8c1d1904430945319d35df07128` and
`factorybox-filesystem-benchmark-b96cb66755f94632a3904b0e2616a67d`.
In an owned Linux fixture with the compiled adapter test binary and private `/var/lib/sysbox`:

```sh
FACTORYBOX_NATIVE_RETAINED_CAPTURE=1 /sysboxcapture.test \
  -test.run='^$' -test.bench='^BenchmarkFilesystemCloneNative$' -test.benchtime=1x
```

The large developer-base digest has a separate diagnostic benchmark. Its read-only input replays
the archived base's content and tar metadata: 41,384 entries, 1,538,002,936 logical bytes and
1,528,828,345 unique content bytes. This replay does not qualify original xattrs or running
developer processes. The initial scan reached its 60-second deadline after 38,675 entries.
Its heap profile attributed about 1,035 MiB of allocation to `io.Copy` buffers.

The scanner now reuses one 32-KiB buffer for the complete tree. The allocation regression
rejected the old scanner's 1,210,174,776 allocated bytes and accepted the corrected scanner's
44,715,232 bytes, with identical entry counts and digest
`ed1df47804576af70723ff76cf36c34b07bcd73d96599e58adbcdc3a0f6bd5ec`.
The corrected scan took 46.078 seconds in that repeat. Other observations ranged from about
18 seconds to the unchanged deadline, including a failed corrected repeat. No reliable scan
latency improvement is established. A reused 1-MiB buffer was also tested; it did not establish
a speed advantage over the smaller buffer and is not the selected implementation.
The regression artifacts are `factorybox-developer-digest-2fa20c11e1304220a610fb13a045e2c7`
and `factorybox-developer-digest-0d988b190c50488fa4e080cdcf4922e1`.

The same buffer change passed the native capture/fork gate in
`factorybox-runtime-d6a8ac7e7eb1487e9daa3b9aefa61abb`, with five top-level tests,
23 subtests, no skips and owned cleanup. This does not replace the full developer lifecycle.

Two further read hints were rejected on the archived-base fixture. Sequential advice for
large files measured 56.215 seconds against a 40.873-second control. Evicting each scanned
file's clean pages first measured 22.167 seconds, but its repeat measured 42.814 seconds
against a 38.673-second control. Every completed scan retained the same digest and counts.
Neither hint is in the runtime; these observations establish no reliable latency gain.
The frozen commands, source contexts and profiles remain under the corresponding
`factorybox-developer-digest-*` artifacts.

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
360-second test timeout within its 420-second runner deadline. These observations do not qualify
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
Consumers recheck the selected image's current local digest, configuration identity and native
platform before allocating fixtures. A saved successful build does not prove that a later Docker
cache still contains its image; restore or rebuild that exact input instead of selecting a tag.

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
Once preparation has frozen `runtime-rootfs.tar`, the lifecycle consumer verifies that exact
regular, nonsymlink archive's size and SHA-256, plus its pinned role and platform metadata.
It does not require the unused source image to remain in Docker's image cache. Consumers that
execute an image directly still verify its actual cached identity and repository digest.

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

That developer run failed its first checkpoint because CRIU reported
`Can't dump nested time namespace` for a Docker child. A minimal Sysbox parent and one child
in a new time namespace reproduced the error in a 452-ms checkpoint request, without Docker,
the Connector or an IDE. The original failed runs remain retained. The unchanged upstream
`timens_nested` case declares an expected checkpoint failure; it is not proof of the extension.
Logs are observed through the CLI's stderr and the guest's merged filesystem. Exec after an
unresolved failed capture is unsuitable because the source can still be frozen.

The ordered `criu-root-owned-time.patch` adds capture and restore for time namespaces owned
and used by the source root user namespace. It saves monotonic and boottime offsets relative
to the source root, creates each saved namespace once and restores the original sharing
relationships with independent kernel identities. It retains the exactly pinned upstream
commit and all 1,030 original CRIU test files. Two fresh builds reproduced the same executable,
SHA-256 `44ca6469f17b27bd294812b67119b4bc0107249b2deea957d7fc203e99e05988`.
This extension has focused native proof; the complete developer lifecycle remains unqualified.
The full developer retry, `factorybox-devx-lifecycle-fc9af7475bd2499a91964ca3ed78577d`,
spent 237.193 seconds preparing the rootfs, then exceeded its 120-second readiness limit.
The Connector had spent 39.544 seconds acquiring project images and Compose was still starting
services. The outer 420-second deadline interrupted cleanup. No checkpoint ran, so this attempt
provides no developer fork/resume timing or verdict on nested-time restoration. Failed input
and runtime data remain separate from passing evidence.

The later full run, `factorybox-devx-lifecycle-5679e94057894b978c9ba992b046fa04`,
prepared its frozen rootfs in 36.498 seconds and reached an actual Connector app response
99.669 seconds after prepared start. The CLI start call itself took 0.974 seconds; that is
not application readiness. Its first checkpoint passed the earlier nested-time failure,
then CRIU rejected an open root descriptor held by a `runc` process:
`Can't lookup mount=8117 for fd=9 path=/`. No full developer fork or resume completed.
The failed run retained a complete 3,004,170,240-byte archive before successful owned cleanup.
The fixed global sysctl sentinels were unchanged and the fixture was not OOM-killed.

The lifecycle gate samples `cpu.stat`, memory counters and `io.stat` around CLI operations
and useful responses. These cover the whole bounded fixture, including controller and Sysbox
services. Missing pressure files are recorded as unavailable; their absence does not identify
a kernel configuration or explain a slow startup. These observations do not establish p95.

A minimal Sysbox actor holding a detached proc-root `O_PATH` descriptor reproduced the same
mount-lookup error without Docker, Connector or IDE. Two runs,
`factorybox-runtime-1682de96256a4e959b6f716ca0e2b64d` and
`factorybox-runtime-438d325c031c4d9ba3618bf2b937420d`, failed their checkpoint requests in
0.605 and 0.501 seconds respectively. Both archived their failed data and completed owned cleanup.
The descriptor's mount was absent from `mountinfo`, its proc-root inode was 1, and its
`self` link identified the actor's own PID namespace. The ordered
`criu-detached-proc-root.patch` is being tested against this reproducer; its presence does not
qualify the developer lifecycle.
The first basic restore regression passed both default and restricted proc mounts, including
shared mount IDs, RAM and source independence, in
`factorybox-runtime-4a1a7dcddbac453d9aa08bce5bcb899b`.
A stronger check then cloned the restored handle in a private helper namespace to read its
actual kernel options. That check failed with `EINVAL` in `open_tree`. An ordinary `openat`
directory handle had outlived the `fsmount` descriptor used to create the anonymous mount.
The candidate now retains the mount descriptor itself. Separate canonical `O_PATH` roots of
one detached proc mount currently share its restored open file description; original distinct
open descriptions are not preserved. Other descriptor flags, ancestor PID-namespace ownership
and proc trees with descendant mounts remain unsupported. This is an experimental limitation,
not general detached-mount or arbitrary-state qualification.
The stricter gate passed both default and restricted mounts in
`factorybox-runtime-83ebc197dc784ed8a9f967056f19352d`, with five top-level tests, eight subtests,
zero skips and owned cleanup. It re-clones each restored handle to read the actual kernel
`hidepid`/`subset` options, verifies mount flags and sharing, and checks independent RAM
mutation. The tested CRIU build `factorybox-build-criu-8c8770c07fa744e5af567b55c100eb51`
has SHA-256 `9228c3e8e5ea6d619d62efb91d5273a22739446bf616551dbc480204f9019019`.
Fresh build `factorybox-build-criu-061cfbce00f94d30b2180e982d30dc91` reproduced that exact
binary with the same pinned builder and source recipe. Both builds passed all 31 C scope
checks; all 1,030 original CRIU test files remain unchanged.

```sh
mise exec -- python3 tools/lifecycle_native.py --suite mountfd \
  --sysbox-build factorybox-build-sysbox-EXACT_RUN_ID \
  --criu-build factorybox-build-criu-EXACT_RUN_ID \
  --native-base-build factorybox-build-native-base-EXACT_RUN_ID
```

The full developer run `factorybox-devx-lifecycle-1d377aa0dc324f38abc537cd61122b1b`
prepared in 67.105 seconds and reached the app in another 56.750 seconds. CRIU's retained log
reports a complete process-tree dump in 3.293 seconds. The public checkpoint still failed:
the capture worker reached its 60-second deadline after CRIU, with the 1.63-GB base-layer copy
present but no upper-layer copy yet. No complete checkpoint, fork or resume was published.
Its stopped-volume archive exceeded 600 seconds, so the original failed volume was
retained for reconciliation. The partial archive is not a completed cleanup receipt.
Reconciliation `e204abb2881d4268a19ab61afbd264b7` later completed the
4,626,317,312-byte archive and owned cleanup. Its SHA-256 is
`29c8d26f0b56b41e68fea4812f8a1ebbc56a17b0e0ec506bdb990c1517313a9e`.
The original failed result is unchanged. A byte-for-byte comparison proved that the earlier
3,873,865,728-byte partial stream was a prefix of that completed archive; only that duplicate
partial payload was reclaimed, with its hash and reconstruction length recorded.

The retry with the filesystem barrier change,
`factorybox-devx-lifecycle-d9a5aa5c060c4babb9654277cd923f30`, prepared in 19.074 seconds and
reached the app in another 39.654 seconds. CRIU completed its dump in 1.226 seconds. The public
checkpoint still reached the capture worker's 60-second deadline with no upper-layer snapshot
present. Read-only inspection found 41,379 entries and 1.529 GB of unique file content in the
base, plus 25,222 entries and 1.174 GB in the writable layer. There was no oversized sparse
file explaining the delay. The metadata benchmark improvement has not yet qualified this full
filesystem workload or produced a developer fork/resume result.
The run completed its owned cleanup after retaining a 4,634,705,920-byte archive with SHA-256
`e8067441df6d1bff34f587e8ba316f5d8cb80b271a0c4a73a0da34939ceaebf5`.

A private capture-only phase probe in
`factorybox-devx-lifecycle-d71123b3a09549a79877d3f410d0a529` narrowed the deadline to the
first base-layer source digest, before cloning started. The CLI now reports the worker's
context deadline explicitly rather than only `signal: killed`. Its 61.712-second command
used 29.099 CPU seconds without quota throttling. Whole-fixture counters recorded about
1.16 GB of additional disk reads, swap activity and page reclamation; those counters are
not isolated to the digest. These observations do not establish the cause of the slow scan.
The temporary phase probes were removed. The later explicit prepared-base path below bypasses
that repeated lower-layer scan; complete developer fork/resume qualification remains incomplete.
The failed run completed owned cleanup after retaining a 3,036,676,096-byte archive with SHA-256
`048f4be076cbadc71bdab3cd4f1328fd518bc7fa0afa14d142aeae8493227c7f`.

The first full developer run with an owned prepared base,
`factorybox-devx-lifecycle-bffc08820b4343b39d9e491d6a480b06`, published its complete public checkpoint
in 47.994 seconds. Its 41,384-entry lower was reused through a checked read-only alias in 0.003268
seconds. Capturing the 25,100-entry private Docker volume, with 1,174,254,497 unique content bytes,
took 29.548 seconds. The remaining checkpoint time includes CRIU, metadata/RAM-image verification,
saved-filesystem verification and source release. The checkpoint contains 256 metadata/image files
totaling 222,464,301 bytes; that count is not a RAM-only size.
Cold preparation took 217.834 seconds, including 142.578 seconds for the one-time base import and
OCI setup. The real Connector app answered after another 89.496 seconds. The first fork then
exceeded the unchanged 420-second whole-lifecycle deadline, before any useful fork response.
No complete developer fork/resume passed. The run retained a complete 5,857,345,536-byte archive
with SHA-256 `3fadb1880ecc71f08ab591124b529ce7fa8c1e90c6b5b68d4706988a1cc71c33` and completed owned
cleanup. Preparation remains an explicit cost; these are individual shared-worker observations.

The fork path now verifies published RAM/metadata once under its exclusive operation lock, verifies
each source filesystem tree at materialization, then verifies every target byte and metadata entry
against that saved digest. It no longer rescans those same trees at each intermediate call.
An existing intent retried in a later operation receives fresh validation. The copied target still
rejects altered content with preserved size/mtime and has independent writable inodes.
`factorybox-runtime-2cc3decc656d443dbffb7f40f0667bef` passed the prepared-base capture/fork profile
with six top-level tests, 35 subtests, no skips and owned cleanup after this change. Its three added
copy regressions cover the complete known digest, changed content and cancellation before target
creation. The full developer lifecycle has not yet been qualified with this reduced scanning path.

The next full run, `factorybox-devx-lifecycle-7cd93f97c5b6488387b7c1e90b4c016e`, prepared in
177.635 seconds and reached the app after another 81.897 seconds. It published a complete
checkpoint in 71.628 seconds. Its private Docker capture took 43.962 seconds; the shared
filesystem durability barrier took 0.163 seconds. The first fork completed its backing
preparation, then CRIU failed restoring `cgroup.subtree_control` with `EBUSY`.
The unchanged 420-second lifecycle deadline interrupted that request before a useful fork
response. Its first archive reached the 600-second archive deadline; the original volume
was retained for reconciliation. Reconciliation `ca39d90ebf5d4bf39f195ee452433a8a` subsequently
verified the complete 9,067,036,672-byte archive with SHA-256
`0cf9aded11a881518e06ebfa369892310d073d867ca39667da82bd1a4cea9148` and completed owned cleanup.
That cleanup receipt does not change the failed lifecycle result.

`factorybox-runtime-d2b79504c8ec4ac28127b333d05c28ed` reproduced the same restore failure in
12.58 seconds with a parent actor and two populated delegated cgroups, without Docker,
Compose, Connector or IDE. Its checkpoint succeeded, while the public fork's retained
CRIU log reports the same `cgroup.subtree_control` error. That failed fixture completed
archival and owned cleanup. This case remains a required regression for the tested delegated
hierarchy; it does not qualify arbitrary cgroup restoration.

Two diagnostic-only CRIU builds reproduced the failure and identified CRIU itself as the
process directly populating the target root. Sysbox restore called `ApplyChildCgroup` without
creating `init.scope`; the inherited `EnterPid` helper silently skipped that missing directory.
The ordered `sysbox-runc-restore-delegation-leaf.patch` now prepares the ordinary Sysbox leaf
before moving CRIU into it. The original CRIU executable is unchanged, and the temporary
diagnostics are absent from the selected sources.

`factorybox-runtime-903784cb6d8a4123b74f12a51afe81c3` confirms that the first error is gone,
but the delegated test still fails. CRIU next tries to enter the target root to create its
cgroup namespace after controller delegation has already been activated. That migration also
returns `EBUSY`. At that revision, the restore-ordering failure kept Docker fork/resume
unqualified. The regression additionally requires empty delegated parents, exact root and
Docker controller sets, preserved leaf membership and independent RAM mutation.

The ordered `criu-delegation-after-cgroupns.patch` addresses that ordering failure. It keeps
early cgroup type restoration, then restores unified controller properties top-down after
the root task has created its cgroup namespace and moved to its final group, before the
forking stage and controller-file restoration. A stronger regression also exposed missing
`memory.max` capture in the pinned CRIU property list. `criu-v2-memory-limit.patch` adds that
property; the saved hard limit no longer silently becomes `max`.

`factorybox-runtime-b7d81a1f21754d90b7507e86996097da` passed all six tests and 18 subtests,
with zero skips and owned cleanup. The actual delegated hierarchy, two populated leaves,
different memory limits and an open `memory.max` descriptor survive checkpoint/fork.
Changing the restored limit through the fork's controller is visible through its retained
descriptor and leaves the original RAM and limit unchanged. Individual observations were
0.381 seconds for checkpoint and 0.626 seconds for fork through the useful state and
independence checks.

The selected CRIU build is `factorybox-build-criu-001ec24844f74fdb9e419a82ee0a02ab`, SHA-256
`b28304c5a101e47606da7be0e0d04d5f900a2abfef9e2a9c99b86bf2fc11e12e`. It passed all 31 C scope
checks and the 18 unchanged two-cycle core cases in
`factorybox-criu-core-6e2f34bbcf324e9c8a9f48df5a07875b`. Fresh build
`factorybox-build-criu-6aa4515b633c45b195cb23cac67d7e74` reproduced those exact binary bytes.
All 1,030 original CRIU test files
remain unchanged. The prepared-base pause suite passed all six tests and 23 subtests in
`factorybox-runtime-e3e3576c78964f4db6cc8725fff5fd31`, including all sixteen restore/delete
cycles, receipt recovery, fork of a resumed source and another pause/resume. Its individual
pause/resume observations were 0.678/0.490 seconds, with zero snapshot payload residency at
pause. Both native gates completed owned cleanup.

The full developer run `factorybox-devx-lifecycle-713833c262d442ed8b995377a28babd1`
prepared in 15.584 seconds and reached the app after another 22.749 seconds. Its checkpoint
completed in 5.661 seconds, and the first independent fork answered through the real
Connector app channel in 4.672 seconds. Source and fork mutations remained independent,
and the fork continued after source deletion. The next checkpoint of that fork failed:
a live `runc` process held an unattached proc directory at `/2814/task/2814/fd`, outside
the existing canonical-root descriptor support. No developer descendant, pause or resume
result passed. The run retained its complete 8,562,671,616-byte failed archive and completed
owned cleanup. Its SHA-256 is
`135b64d98c4469989964d1de8bff04ddca498eb821fbbf5a18367179a5927244`.
These are individual observations on the same shared worker, not an established latency
distribution or a claim that the cgroup changes caused the filesystem timing difference.

The strengthened `mountfd` gate reproduces that recapture failure without Docker or Connector
in `factorybox-runtime-54d6261597b74b99812ef061e26aaaf9`. Both original canonical-root fork
checks pass before the actor opens a readable `/self/task/1/fd` directory through the detached
proc root. Recapture then fails at the same unknown-mount check for `/1/task/1/fd`, in both
the default and restricted proc cases. The test retains every earlier assertion and adds
required descendant restore, descriptor use and RAM checks after deleting the captured parent;
those later assertions remain unexecuted until recapture is fixed. The failed gate completed
archival and exact owned cleanup. Readable proc-directory support remains a release blocker.

The same Sysbox build passed the prepared-base capture/fork suite in
`factorybox-runtime-1efcd5827a034a5c91911701d2612e1a`, with six tests, 35 subtests, zero skips
and owned cleanup. Its individual tiny-actor observations were 0.235 seconds for checkpoint
and 0.523/0.518 seconds through useful fork/descendant responses. These exclude base preparation
and do not qualify developer-workload latency or p95.

The same build also passed all six tests and 23 subtests of the prepared-base pause suite in
`factorybox-runtime-d3b7ceb781a74cf998ee22f29c5c8b2c`, with zero skips and owned cleanup.
The full 32-MiB pause took 0.427 seconds and released all original anonymous memory and
snapshot payload residency. Resume through useful RAM, file, volume and virtual-state checks
took 0.386 seconds. All sixteen restore/delete cycles passed, with useful fork responses
between 0.567 and 1.020 seconds and a descriptive median of 0.626 seconds. The suite then
forked a resumed source and paused/resumed modified state again. Earlier observations on this
worker were substantially slower; this run does not establish a causal latency improvement
from the leaf fix or a production latency distribution.

Run the delegated-cgroup regression:

```sh
mise exec -- python3 tools/lifecycle_native.py --suite delegation \
  --sysbox-build factorybox-build-sysbox-EXACT_RUN_ID \
  --criu-build factorybox-build-criu-EXACT_RUN_ID \
  --native-base-build factorybox-build-native-base-EXACT_RUN_ID
```

Run the positive nested-time lifecycle gate:

```sh
mise exec -- python3 tools/lifecycle_native.py --suite time \
  --sysbox-build factorybox-build-sysbox-EXACT_RUN_ID \
  --criu-build factorybox-build-criu-EXACT_RUN_ID \
  --native-base-build factorybox-build-native-base-EXACT_RUN_ID
```

`factorybox-runtime-ca012188291449daa50e82d18e80659a` passed all five top-level tests and
six subtests without skips and completed owned cleanup. The actual Sysbox tree has a root
actor, two actors sharing one private time namespace and another in a separate namespace.
The gate checks exact relative monotonic/boottime offsets and every actor's 8,192-byte RAM
payload and counter across checkpoint, independent fork, source deletion and pause/resume.
Mutating the fork leaves the original actor state unchanged. This gate contains no Docker
daemon, IDE or Connector.

Factorybox probes the already hash-verified CRIU executable through its open descriptor.
Only its bounded, versioned feature response admits this extension. Unknown commands,
schemas, fields, trailing data and oversized responses admit no additional resources.
The runtime checks each owned thread's `time` and `time_for_children` handles while the
exact cgroup tree is frozen, before entering CRIU's guarded capture phase. It verifies
namespace type, ownership and membership. A differing `time_for_children`, or a private
clock owned or used by a nested user namespace, remains unsupported. Older binaries also
reject distinct child clocks. Rejection happens before ptrace, network hooks or snapshot
payload writes, and the source guard then releases the original tree.
The rejected checkpoint ID remains reserved with its original intent and rejection.
An early rejected pause returns the same running source and retains `rejected_at` and `error`
in pause history. Lost publication of that result can be recovered only from its exact
pre-capture rejection, completed safe guard outcome and still-matching live kernel identities.
An in-flight, missing or contradictory outcome cannot promote a pending pause to running.

Run the focused native namespace-admission regression:

```sh
mise exec -- python3 tools/lifecycle_native.py --suite namespaces \
  --sysbox-build factorybox-build-sysbox-EXACT_RUN_ID \
  --criu-build factorybox-build-criu-EXACT_RUN_ID \
  --native-base-build factorybox-build-native-base-EXACT_RUN_ID
```

It checks rejection with byte-identical original RAM, useful work after failure, retained
failed pause intent, immutable failed IDs and controller-outcome recovery. With the new CRIU,
the rejection fixture uses a thread whose future-children time namespace differs from its
current clock. After that thread retires, the same source can capture normally.
`factorybox-runtime-b551c5440cdf40509a8357ffd377a0de` passed six top-level tests and seven
subtests without skips and completed owned cleanup.

The same new CRIU also passed the 18 original two-cycle C core cases in
`factorybox-criu-core-ebe803d277c9474f84866e057ec516ee`. The existing capture/fork gate passed
five top-level tests and 23 subtests in `factorybox-runtime-48ad8897891e4e7b933a1665b8161924`.
The 32-MiB pause/resume gate passed five top-level tests and ten subtests in
`factorybox-runtime-ada7e659804744499cdf5fd27ef9e696`. All completed exact owned cleanup.
An earlier pause run, `factorybox-runtime-bcba9c9807d74ee38d6cee5982c3bdd5`, failed when
removing an apparently empty restored cgroup with `EBUSY`. The unchanged repeat passed;
the original failure remains unresolved and blocks a clean stability claim.
The extended regression repeats actual restore/delete sixteen times, checking every restored
RAM payload and the absence of each deleted cgroup before proceeding. It passed in
`factorybox-runtime-b426517a09d4407ba60c91afb17bbc27`, with five top-level tests, eleven subtests,
zero skips and exact cleanup. This increases coverage; it has not established the cause or
a fix for the original intermittent failure.

The later prepared-base run `factorybox-runtime-d826ce46fc1a48c3ad9a5138306b3302` exposed an
empty `init.scope` child still present at the failed parent removal. A minimal native regression,
`factorybox-runtime-a098c9d138fd430aa96711b6015f6ce0`, reproduced `EBUSY` in 2.42 seconds without
starting a guest. The controller's retirement routine had only removed the parent directory.
The extracted cleanup now pins and validates the complete empty original subtree before removing
children in postorder. It checks inode/mount identity again before every removal, retains foreign
mounts and populated groups, and never signals a process. A confirmed Runtime delete may briefly
precede `populated 0`; only that confirmed deletion receives a two-second, identity-checked wait.
The complete-tree regression passed all three cases in
`factorybox-runtime-8b793195f4a840b1a09c6bf25c0d51cc`, including preservation of an owned sibling
when a foreign mount prevents whole-tree admission. This focused proof does not replace the
sixteen-cycle restore/delete gate or qualify the developer workload.

```sh
mise exec -- python3 tools/lifecycle_native.py --suite lifecycle \
  --test TestNativeEmptyCgroupRetirement \
  --native-base-build factorybox-build-native-base-EXACT_RUN_ID \
  --sysbox-build factorybox-build-sysbox-EXACT_RUN_ID
```

The lifecycle-only fixture can use the same explicit native base and pinned Sysbox binaries as
checkpoint fixtures. This focused cgroup case skips Sysbox daemons because it starts no runtime.
Native fixture failures are archived before removing their owned data volumes.
The complete start/state/delete suite passed in
`factorybox-runtime-cbb14487a49a4410912093bd8ed62f5b`, with six native top-level tests,
the three cgroup retirement subtests, zero skips and owned cleanup. A prepared-base
pause repetition still exceeded its test deadline in
`factorybox-runtime-df086e29ed9f4ea7be53431b3412224c`; that failed archive and cleanup receipt remain
retained. The fixture now reports each restore/delete cycle's useful-fork and deletion times.
Its public-fork subprocess also bounds inherited output-pipe waiting after its existing deadline,
so a surviving backend cannot hide the failing operation behind the suite's final timeout.
With the original 180-second inner timeout, the instrumented
`factorybox-runtime-02f04878b8dc4ab2914c66aa0dfdaf7a` completed twelve restore/delete cycles before
the suite timed out. Every cycle preserved its RAM payload and removed its exact cgroup.
The checkpoint-based native runner now gives its complete test process 360 seconds inside the
unchanged 420-second outer lifecycle deadline. No assertion, iteration count or resource ceiling
was reduced. This separates the full correctness gate from the still-unachieved latency target.

`factorybox-runtime-c349b3143ba84fd584dd526e60597feb` passed all six top-level tests and 23 subtests
with that budget, zero skips, no OOM and owned cleanup. All sixteen actual restore/delete cycles
passed, followed by resume/receipt recovery, an independent fork of a resumed source and another
pause/resume of modified state. The test process took 187.76 seconds; the whole native container
ran about 209.8 seconds. Individual observations were 4.535 seconds for pause, 5.564 seconds through
resume and useful state checks, and 2.250 to 8.783 seconds through useful fork responses in the
sixteen-cycle regression. These observations qualify the tested state/ownership behavior on this
worker, not production readiness, the full developer stack or any sub-three-second p95.

The combined mount-descriptor and filesystem-barrier changes passed the required focused
regressions with the newer CRIU build:

| Gate | Run | Observed result |
| --- | --- | --- |
| CRIU core | `factorybox-criu-core-36ff2840fbec4700b59a0ade2b641d96` | 18 unchanged C cases, two dump/restore cycles each |
| Capture/fork | `factorybox-runtime-72d1ef25e49f4a75a56bddef413fec7e` | 5 top-level tests, 23 subtests |
| Pause/resume | `factorybox-runtime-f6d328e7b7db4fe296ce64caf123ef70` | 5 top-level tests, 11 subtests, including sixteen restore/delete cycles |
| Nested time | `factorybox-runtime-7a08a1a7b40a4703b43952574807288c` | 5 top-level tests, 6 subtests |
| Namespace rejection | `factorybox-runtime-d19eb4621a5041c2a1dfa3bd80a3cf00` | 6 top-level tests, 7 subtests |

All completed owned cleanup; the Go gates had no skips. These focused passes do not replace
the failing full developer lifecycle or the unresolved upstream/platform requirements.

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
The archive client also inherits a process-local output file-size limit calculated from the
host's available disk space at launch, leaving a 4-GiB reserve and respecting any lower existing
hard limit. A reached cap cannot publish an archive receipt, even if a client reports zero exit.
The stopped volume remains retained. The parent's limits are unchanged; a real child-write
regression checks that behavior.
If Docker completes volume removal after its caller times out, a subsequent reconciliation
requires an existing complete archive for that exact run and owner. It requires a regular
file with the recorded byte count and SHA-256 before recording cleanup. A missing volume by
itself, a changed archive or another inspection error cannot establish success.
Completed failed-volume payloads may be retained with lossless Zstandard encoding to limit local
disk use. An `archive-encoding.json` receipt records both encoded bytes/SHA-256 and the original
TAR bytes/SHA-256, plus the original result-file hash. The original TAR is removed only after
streaming the whole decompressed payload and matching its exact original size and digest.
The original failed result remains unchanged. Use the receipt's reconstruction command, or stream
the `.tar.zst` through `zstd --decompress --stdout` into a TAR reader without allocating another
complete TAR file. Frozen runtime inputs and benchmark input archives are not rewritten by this
retention step.
The actual timed-out deletion from `factorybox-devx-lifecycle-fc9af7475bd2499a91964ca3ed78577d`
was recovered this way in reconciliation `a680c1717b94427585905e344254b5ff`, reusing the
verified 3,088,056,320-byte archive. The original failed lifecycle and cleanup results are
unchanged.
