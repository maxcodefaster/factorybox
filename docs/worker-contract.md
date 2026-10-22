# Local pilot worker contract, schema 1

This contract is qualified for the exact local
[pilot profile](../upstream/amd64-pilot-profile.json), with the results and
limits in [pilot-validation.md](pilot-validation.md). The original v0.1
composition remains the frozen comparison. Production qualification remains
false. The worker is a privileged administrative service for one
customer or trust domain. Consumers are separate processes inside its system
containers. No IDE, agent, Python client or Dev Container is required by the
Go runtime.

## Platform and deployment

The current native target is Linux AMD64, kernel `7.2.8-9-omarchy`, 4096-byte
pages and cgroup v2, using the tested same-host Docker/local-volume storage
path. `factorybox doctor` checks the platform tuple; it does not establish
component or lifecycle qualification. Cross-compilation, another filesystem
or a successful image build does not qualify another host.

The image runs `factorybox worker` as root PID 1 in a private cgroup namespace,
with at most 3 CPUs and 3 GiB memory. It needs privileged container access to
its namespaces, mounts, cgroups, FUSE and checkpoint facilities. It checks its
finite resource ceilings and component hashes before publishing readiness.
The included [Compose file](../examples/worker/compose.yaml) expresses this
deployment. It exposes no ports. Its external volume must belong exclusively
to this store; an administrator supplies a random 32-character hexadecimal
store ID and an exact locally built image digest. Never attach the same store
to another trust domain.

The current image contains its pinned native base's build/test utilities.
The reduced production image remains unqualified. This uses the host kernel;
it does not provide VM isolation or imply Kubernetes support.

## Persistent and ephemeral state

Mount the complete owned volume at `/var/lib/sysbox`. The Go supervisor creates
`/var/lib/sysbox/store` mode 0700. It holds the store lease for its lifetime.
Its `store.json` fixes the caller's store ID, `manager/` holds Sysbox-managed
data and `epochs/<random epoch>/controller/` holds that worker's lifecycle
records, bases, captures and fork/recovery inputs. Persist all of them as one
owned storage unit. Copying only CRIU page files is insufficient.

Each fresh worker creates a new epoch. `/run/factorybox`, process handles,
service sockets and mount namespaces are ephemeral. A readiness JSON file
alone is never authority to act on a process. `worker-status` verifies the
live supervisor's birth/executable, held store lease, actual service processes
and Unix socket peers. A stale file or changed store ID is rejected. Stop and
remove the complete old worker before starting its replacement.

During initial startup, wait for `/run/factorybox/worker.json` through Docker's
archive/file API before creating an exec process. This read is only a readiness
hint: subsequently require the live `worker-status` verification above. An
early exec task can occupy the private cgroup root while PID 1 delegates its
controllers; the worker refuses that state. The supplied integration client
performs this two-step check without a fixed startup delay.

The current production candidate adds a private `startup-failure.json` in its
epoch when startup fails after a service was launched. It reads bounded procfs
metadata for the exact saved services and their children before stopping them.
The original error still determines command failure; a diagnostic never grants
readiness. This addition is outside the frozen pilot profile. Its native fault
and full-consumer evidence is recorded in
[production progress](production-progress.md#worker-startup-failure-records).

RAM images and filesystem copies consume persistent storage. Pause records
report actual released anonymous memory and resident snapshot pages; a small
controller record is not a substitute for those measurements. See the native
results for example sizes and timings. Plan capacity for independent mutable
filesystems, active checkpoints and interrupted operations as well as bases.

The production candidate exposes `--worker-run-root /run/factorybox capacity`.
It reports the controller filesystem's total, free and available bytes and its
actual directory/mount identity. These are filesystem counters, not the size
of this store. The local worker requires 12 GiB free plus 4 GiB of preflight
headroom before starting new backing writes. Existing completed requests,
inspection and deletion can still reconcile below that floor. A capture also
allocates an independent 8-MiB private rollback reserve before freezing its
source. Its exact inode can supply rollback metadata space after capture fails.
This candidate behavior is outside the frozen pilot profile.
Operations that still write completion/deletion metadata can fail if the
filesystem has no space at all; the admission exemption is not a success promise.

The preflight observation does not reserve 4 GiB, cap guest writes or guarantee
space throughout an operation. It is not a hard store quota or coordination
between workers. Use bounded storage for that separate requirement; hard
quotas and general concurrent-write exhaustion remain release work. The native
pressure result covers the concrete failure described in
[production progress](production-progress.md#capacity-admission-and-rollback-space).

An additional experimental profile explicitly accepts `worker --bounded-root
ABSOLUTE_PRIVATE_DIRECTORY --bounded-bytes BYTES`. It creates a fixed image from
512 MiB to 64 GiB, with no automatic migration, resizing or reformatting. The
store becomes `DIRECTORY/data/store`; image identity and the outer lease remain
outside that filesystem. Its internal admission floor is 64 MiB plus 128 MiB
headroom, separate from the host's 12 GiB plus 4 GiB checks. Both observations
appear in `capacity`. All admitted disk-backed bundle layers and bind sources
must reside in the bounded filesystem; initially read-only external binds are
also refused. This profile requires existing loop/ext4 support and direct I/O.
It has native small-actor checkpoint/fork/pause/resume and real guest ENOSPC
proofs. A separate 32-MiB RAM actor passes planned and abrupt same-host worker
replacement through the delivered CLI, including independent resumed descendants.
It also refuses duplicate ownership, changed store identity/capacity and partial
initialization without rewriting image bytes. Full-consumer qualification remains
open. This profile does not
change the pilot deployment or production selection. See
[the exact evidence and remaining limits](production-progress.md#opt-in-bounded-worker-and-actual-guest-capacity).

## Local commands and responses

Administrative clients execute the image's own binary inside the worker:

```sh
/opt/factorybox/factorybox worker-status
/opt/factorybox/factorybox --worker-run-root /run/factorybox start --bundle /var/lib/sysbox/input/bundle source
/opt/factorybox/factorybox --worker-run-root /run/factorybox state source
/opt/factorybox/factorybox --worker-run-root /run/factorybox checkpoint --snapshot-id aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa source
/opt/factorybox/factorybox --worker-run-root /run/factorybox fork --source source --snapshot-id aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa branch
/opt/factorybox/factorybox --worker-run-root /run/factorybox pause --snapshot-id bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb source
/opt/factorybox/factorybox --worker-run-root /run/factorybox resume --snapshot-id bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb source
/opt/factorybox/factorybox --worker-run-root /run/factorybox delete branch
```

An administrator prepares the OCI bundle and all its private rootfs backing.
The current admitted capture layout is an overlay at `BUNDLE/rootfs` backed by
exact sibling paths `BUNDLE/lower`, `BUNDLE/upper`, `BUNDLE/work`; the tested
mount uses `index=off`. External bind mounts, hooks, alternate shift modes and
other mount layouts need separate support/qualification. Sysbox-managed
private data volumes and its virtual filesystem mounts are explicitly checked.
The evidenced Factory service/reconnect profile has at most one non-init
foreign-parent exec root per PID namespace. Multiple such roots in one PID
namespace are refused before capture as `nested-exec-session`; the attached
minimal reproducer otherwise fails its CRIU session-ID restore. This boundary
does not qualify arbitrary interactive TTY or foreign exec sessions.
No image pull, Docker daemon, package installer or application startup policy
is hidden in these commands. `prepare-base --source ABSOLUTE_DIRECTORY BASE_ID`
can import and verify a reusable immutable base before start. Its complete
initial preparation is charged separately from prepared-start latency.

The selected Factory consumer starts its private Docker daemon with
`--storage-driver=overlay2` and the qualified CRIU components. The original
example inputs remain unchanged; acceptance drivers select that option only
in copied daemon inputs and retain both hashes. An integrating administrator
must configure that private consumer accordingly. The runtime's lifecycle
does not require Docker, Theia or OpenCode inside a sandbox.

Successful commands write one JSON object on stdout. Diagnostics use stderr;
nonzero exit is failure even when stdout contains a durable partial record.
`worker-status.schema`, lifecycle `record.version` and checkpoint `version`
are currently 1. Consumers must require the version they understand, preserve
the exact returned ownership/operation identifiers and tolerate added object
fields. The CLI has its own command contract; Sysbox CLI compatibility is not
promised. Sandbox IDs use lowercase letters/digits/hyphens, beginning with a
letter; checkpoint IDs are immutable 32-character hexadecimal values.

`start`, `fork`, pause/resume and deletion persist intent and completion.
Repeating a completed request is reconciled against its exact saved identity.
An uncertain attempted operation is not automatically reissued. Never change
the request or select a fresh target ID merely to bypass such a refusal.
Preserve records, diagnostics and incomplete backing for explicit reconciliation.
Deletion checks the exact saved object ownership before touching kernel state;
it does not garbage-collect unrelated containers, volumes or old epochs.
It confirms removal of the owned runtime, empty cgroups and owned fork overlay
mount. Checkpoints, operation records and filesystem backing remain retained;
automatic checkpoint retirement, store quotas and old-epoch garbage collection
are not implemented by this command. Production operation needs an independently
qualified retention/capacity policy that preserves live descendant and recovery
inputs. See the proposed [production release gates](../README.md#path-to-the-first-production-release).

## Experimental checkpoint selections and retirement

The unselected bounded-store candidate adds `select-checkpoint`,
`release-checkpoint` and `retire-checkpoint`. Each requires one positional source
sandbox ID and `--snapshot-id`, `--source-owner` and `--checkpoint-sha256` naming
the exact complete saved checkpoint. Selection/release additionally require
`--selection-id`. The checkpoint hash is its canonical complete JSON identity,
also returned by a committed pause, not a hash of pretty-printed CLI output.
Use the same `--worker-run-root /run/factorybox` profile as other lifecycle calls.

A selection persists until explicitly released. Released selection IDs cannot
be reactivated; use another explicit ID for a later selection. An unresumed
committed pause independently protects its recovery checkpoint. Incomplete
sandbox/fork work and remaining filesystem references block retirement. A
completed fork or resumed source must have verified independent private images.
Retirement preserves operation/provenance metadata and reserves the original
checkpoint identity permanently. New forks cannot reuse a retiring generation.
Exact completed fork/resume replay can continue using its own backing.

`retire-checkpoint` currently requires the explicit bounded-store profile.
It persists an exact deletion inventory and completion reserve, verifies owned
objects and dependencies, and unlinks only inventoried payload names. It never
truncates payload data. Verified immutable aliases are removed while their shared
base stays intact. After interruption, repeat the exact request; unknown entries,
changed objects, successors and foreign mounts cause refusal. Before/after
capacity is observed separately because live open references can retain blocks.
Do not manually clear an uncertain intent or interpret a partial result as a
completed retirement.

This implementation is not qualified. The required immutable-base test still
fails because the pinned BusyBox `sleep 300` exits after CRIU interrupts its
relative wait. The failure is retained alongside passing RAM-actor retirement
tests. A required direct-syscall regression reproduces the premature exit with
read-only input and a NULL remainder pointer; source/fork register and original
deadline assertions remain blocked by that first failure. Read-only kernel
restart-state diagnostics are not a runtime timer correction. Selection recovery
across epochs, full-consumer retirement, exhaustion
faults and epoch retirement remain unverified or unimplemented. See the exact
[progress and failed-suite evidence](production-progress.md#experimental-checkpoint-retirement-and-an-unresolved-interrupted-sleep).

## Recovering a committed pause after worker replacement

Before removing the old worker, retain its successful pause response and
`worker-status.lifecycle.root`. Require `record.phase == "paused"`, a nonempty
`record.pause.completed_at` and the full
`record.pause.checkpoint_sha256`. The old worker must be fenced and its entire
container removed. Start a fresh worker with the same store ID, complete
persistent volume, component identities and qualified host.

```sh
/opt/factorybox/factorybox --worker-run-root /run/factorybox recover-paused \
  --source-root /var/lib/sysbox/store/epochs/OLD_EPOCH/controller \
  --source source --snapshot-id bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb \
  --source-owner EXACT_PAUSED_RECORD_OWNER \
  --checkpoint-sha256 EXACT_FULL_CHECKPOINT_SHA256 recovered
```

This imports and fully verifies independent filesystem and RAM backing, then
restores `recovered` as a live sandbox. A subsequent `resume` of the old object
is unnecessary: that old object belongs to the fenced epoch. Repeating this
exact recovery target/request returns the same completed live identity.
Changed identities, corrupt state, missing publication and uncertain partial
imports/restores are rejected. The caller must fence the old worker; a lease
on a copied file is not distributed fencing.

The native proofs cover planned and abrupt same-host whole-worker loss after
a complete committed pause. Running unsaved RAM, host reboot, lost storage,
cross-node migration and changed hardware remain outside this contract.

## Filesystem notification search paths

Filesystems can refuse reopening an inotify handle. For that case an
administrator can put a JSON array in the OCI annotation
`io.factorybox.checkpoint.irmap-scan-paths`, for example:

```json
{"io.factorybox.checkpoint.irmap-scan-paths":"[\"/application/data\",\"/home/project\"]"}
```

These are paths in the captured sandbox's namespace, passed only as CRIU
inode-to-path fallback roots. They are preserved through descendants and saved
captures. Admission bounds the array to 32 roots/16 KiB, rejects duplicates,
noncanonical paths, config-token characters, whitespace/control characters,
the root directory and `/proc`, `/sys`, `/dev` subtrees. Paths containing spaces
are not supported by this initial annotation. Keep search roots narrow and
declare every data/config location actually watched by the consumer. Scans
are included in checkpoint timing. A missing path can be optional, but an
unresolved live watch causes checkpoint failure. The option does not disable
watchers or guarantee arbitrary watch topology recovery.

## Builds and evidence

`tools/build_worker.py` takes exact completed native-base, Sysbox and CRIU
build IDs. It freezes the Go sources, toolchain and source lock, checks module
inputs offline, assembles a deterministic package and builds a local image
without network or build cache. `--compare BUILD_ID` requires identical package
and image identities from a separate build. It never publishes the image.
The package includes component hashes, license notices and its build recipe.
Every new build/native run uses the repository's artifact capacity guard.

Rebuild the pilot CRIU composition with `tests/criu-retained-candidate/build.py`
and the exact native-base build, passing `--exit-hold --exit-hold-socket
--nested-overlay --nested-overlay-stable-paths --nested-overlay-shared-root`.
Then use `tools/build_worker.py --native-base-build BASE --sysbox-build SYSBOX
--retained-candidate CRIU`; use the successful exact artifact names printed by
each build. Repeat with `--compare WORKER_BUILD` for the independent package
and image comparison. The source lock and ordered patch hashes must match the
pilot profile; another platform still needs native qualification.

`tools/verify_pilot_profile.py` rechecks the selected 44 local evidence results,
the full matched observations, unchanged runtime source, exact executed Docker
CLI bytes, component and worker reproductions, declared fixture patches and
upstream source integrity. It requires the referenced local artifacts/images;
they are not public downloads. Its passing result is
`factorybox-pilot-qualification-273f02beb1984e8c8299bdefe6ee76cc`.

The original minimal and Factory examples remain the acceptance consumers.
Native worker-loss tests exercise the delivered image, not a renamed proof
harness serving as the worker. Required native/upstream failures or unrun
suites continue to block qualification; functional successes and benchmark
targets are reported separately.
