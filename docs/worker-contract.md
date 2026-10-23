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

The current candidate confirms an empty original cgroup and zero anonymous
memory accounting before committing a pause. Small-task accounting can settle
after the process exits. The confirmation is bounded to three seconds and the
caller's context; a timeout leaves the stopped checkpoint for exact retry.
Snapshot residency is checked separately. No global memory flush is used.

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

This implementation is not qualified. The earlier BusyBox sleep and direct
read-only-request/NULL-remainder failures remain preserved in the evidence.
The integrated timer continuation in candidate
`factorybox-build-worker-2fca9b438e664527a211a44df38bd0f7` passes the full
four-test retirement suite, including both timer regressions. However, a caught
handler that makes its dedicated remainder page read-only causes the prepared
wait to return EFAULT where the native control returns EINTR. The delivered
candidate still has this copy-order defect.

A separate test-only native-wait activation experiment passes source, fork,
resume and queued-signal cases, with ordinary and frozen native controls.
It verifies an actual armed kernel timer and preserves the protected remainder
and syscall ABI. Restored tasks have already been exposed by the CLI before
this experiment activates them. Production integration must hold tasks before
exposure and qualify failures, multiple threads and continuation lifetime.
Selection recovery across epochs, full-consumer retirement, broader exhaustion
faults and epoch retirement remain unverified or unimplemented. See the
[current timer evidence and remaining integration work](production-progress.md#remainder-mapping-changed-by-the-signal-handler).

Candidate `factorybox-build-worker-d8cc5e7841bb4b6190fbb6331ff7fc5f` adds an
optional sealed backend hold before restored tasks are released. Native fork
and resume counter tests confirm the hold through CRIU detach until explicit
thaw. Socket disconnects and a foreign acknowledgement reject restore and
preserve unresolved operation evidence. The public lifecycle has not enabled
this protocol or integrated timer activation. The remainder copy-order defect
and the remaining release gates still block qualification. See the
[restore hold proof](production-progress.md#holding-restored-tasks-before-application-continuation).

The subsequent candidate
`factorybox-build-worker-edd5cff5a26a4488b2fd11de136434ac` enables that sealed
hold for ordinary fork and resume. It verifies the held paused runtime receipt,
activates each staged private timer under an independent tracer-exit guard,
then thaws and publishes the ordinary completion. Retained-source checkpoint
also activates before source release. The protected-remainder regression now
passes for source checkpoint, fork and resume; all four signal boundaries and
the original full retirement suite pass on the same package. These results
supersede the earlier public-path copy-order failure for these tested cases.
Interrupted activation repair, broader pending-signal states, continuation-page
lifetime and the remaining release gates still block production qualification.
See the [integrated activation evidence](production-progress.md#activating-native-waits-before-source-and-restore-release).

The same candidate also passes committed-pause recovery after both planned
and abrupt bounded-worker replacement, actual guest ENOSPC, the 18 selected
CRIU cases with two restore cycles each and the existing selected Sysbox Go
and Bats suites. Original upstream skips remain visible. These checks do not
resolve activation after a failed capture; that gap is reproduced and corrected
by the subsequent candidate described below.
See the [additional gates and remaining abort path](production-progress.md#component-and-replacement-gates-after-timer-activation).

Candidate `37dd732100cd45be93cc7a123ee39f1a` activates staged timers after
confirmed process/network rollback and before acknowledging a recovered abort.
An actual post-admission ENOSPC regression reproduces EFAULT on the previous
package and obtains the original EINTR, ABI and interruption-time remainder on
this candidate. The activation worker has its own exact parent intent; the
original capture-worker identity remains unchanged. Orphan and delegated cure
must keep the source held when timer mutation lacks its completed receipt.
A subsequent native delivered-CLI pause test kills the actual controller and
worker, reaps that worker, then verifies the original guard holds the staged
source frozen and untraced with SIGUSR1 pending and no handler progress. The
unresolved pause refuses deletion and preserves its record. This proves one
orphan hold boundary; guard-loss reconciliation, interrupted mutation repair
and the other release gates remain open. See [confirmed abort activation](production-progress.md#native-timer-activation-after-confirmed-capture-abort)
and [the native orphan hold](production-progress.md#native-orphan-hold-after-timer-preparation).

The complementary native guard-loss test also passes on these same package
bytes. After actual guard/worker SIGKILL at the completed-process copy boundary,
the surviving original controller retains the frozen, untraced source and its
pending SIGUSR1. Exact pause replay and deletion refuse without changing the
record; the staged code remains unchanged. This boundary has no retained-cure
grant, so it does not qualify delegated recovery after an accepted grant or
automatic repair. See [the surviving-controller hold](production-progress.md#native-guard-loss-with-a-surviving-timer-controller).

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

The full-consumer bounded-store path is not qualified yet. Its first minimal
native run passes all eight original lifecycle phases, then fails worker Stop
with `device or resource busy`. A delivered-worker minimizer proves that a
single completed immutable base leaves a canonical read-only self-bind which
prevents ordinary bounded-filesystem unmount; an empty worker stops cleanly.
Keep all backing and identity records when Stop is unconfirmed. Retention
requires a complete verified archive before an exactly owned stopped volume
may be removed. The optional larger decoded-stream observation allowance does
not enlarge the physical artifact budget. See
[the bounded-consumer result](production-progress.md#first-full-minimal-consumer-on-the-bounded-store).

The current bounded exit candidate withdraws readiness, takes the controller
operation lock and requires confirmed sandbox deletion before publishing a
durable epoch fence and releasing exact checkpoint aliases and canonical base
mounts. Completed internal timer guards have their own verified completion
path. The base backing and checkpoint contents stay in the persistent store.
The bounded minimal lifecycle, seven Stop protection cases and full checkpoint
retirement suite pass on the first complete candidate. Larger Factory RAM
generations expose an insufficient internal verification deadline.

The next candidate gives complete exit verification 30 seconds. Its caller
provides a 45-second graceful Stop window for verification and both service
shutdowns, for example Docker's `--stop-timeout=45`. The consumer proof limits
that call by its remaining 420-second lifecycle budget. The complete bounded
Factory/Theia/OpenCode-fixture lifecycle passes on that candidate, including
worker exit 0 and cleanup. All 13 native core regression gates also pass on
those package bytes.

A further opt-in candidate binds completed, released and unresumed pauses in
the durable exit fence. It verifies the exact stopped runtime and empty cgroup
before non-forced runtime deletion, retaining the paused record and complete
checkpoint. Caller-owned source mounts are retained. Runtime-owned fork
overlays and exact immutable aliases are released only after fencing. Recovery
input bases outside `bases/` require their complete matching recovery intent
and private preparation before canonical mount release. Partial or unknown
imports refuse cleanup.

Recovery can read an exact pause retained by that fence. All ordinary lifecycle
operations in the old epoch remain closed. A present completion record must
match its original intent. An interrupted exit can lack a completion record
while preserving a complete recovery selection, but
this never establishes that its worker has been fenced. The caller must still
stop/remove the exact old worker and retain exclusive ownership of the store.
The exact candidate now passes all 13 native core regression gates, eight Stop
protection cases and eight full bounded consumer cases, with an independently
identical package/image rebuild. Both examples pass planned/abrupt worker
replacement and the original lifecycle. Interrupted incomplete pauses are
refused while their earlier committed state remains recoverable. Factory
includes its separate Theia peer and local OpenCode model fixture.
Large-ledger admission, external-provider and all remaining release
qualification stay open; see
[the paused drain evidence](production-progress.md#bounded-committed-pause-drain-and-recovery).

The next candidate also imports a committed base when the exact retained
bounded store receives a different loop device. That translation requires the
private persistent source epoch, matching boot/recipe/store owner and complete
image identity, plus a fresh verification of the current mounted store. The
exact committed base must retain its inode and all recorded metadata. Covering
mount rejection, full content verification and a new independent read-only base
remain required. The old process and peer fields are never adopted; callers
still fence the old worker. Standalone imports keep the strict device check.
The exact candidate passes all 13 native core gates, eight Stop protections,
both base-library tests and twelve full bounded consumer cases, including all
four minimal/Factory planned/abrupt forced-loop combinations. Its separate
package/image reproduction is identical. Factory uses its separate Theia and
local OpenCode fixture; external inference remains a separate release gate.
See [the device-remap evidence](production-progress.md#retained-store-remounted-on-another-loop-device)
for the preserved failure, offline inspection and acceptance scope.

Actual exit-fence interruption now passes for both complete consumers, each
also combined with a changed loop device. An external observer checks the exact
live owned process and recipe before pidfd SIGKILL at the matching fence. The
unchanged intent and absent completion are verified after process death, and
all observer handles close before replacement. Every original recovery/replay,
RAM/file, independent-fork and cleanup assertion remains. Runtime bytes are
unchanged; see [the interrupted-drain evidence](production-progress.md#actual-interruption-after-the-worker-exit-fence).
This qualifies same-boot process loss only; callers still fence the source and
own the intact store. Host restart, power loss, migration and production
qualification remain outside this evidence.

Worker exit checks the encoded size of its full completion ledger before
publishing the exit fence or releasing a canonical base mount. This uses the
ordinary publisher's JSON encoder and 4-MiB limit, including the newline and
the maximum UTC RFC3339Nano completion timestamp. A refused ledger leaves both
fences absent and the backing bases intact. The native public-API cases cover
both an oversized intent and a fitting intent whose completion would exceed
the limit. Their trace requires zero canonical-base unmounts and observes the
later busy-store close, so an inactive trace cannot satisfy the test.
See [the size-admission evidence](production-progress.md#large-exit-ledger-admission).
The independently reproduced package also passes all 13 native core gates,
eight Stop protections, both base-library tests, twelve complete bounded
consumer cases and four actual interrupted drains on these new bytes.
This admission guard does not bound the number of accepted work items or
qualify checkpoint/epoch retirement; those remain separate release work.

The SHM startup correction replaces execution of Go code after a raw clone
with a fresh helper executable through Go's fork/exec path. The helper has a
three-second deadline and verifies its private user, mount and IPC namespaces
and exact mappings before its open-only probe. It never writes the three SHM
sysctls. Build it explicitly with `tools/build_sysbox.py --shm-startup-reexec`;
the default component path remains unchanged. The patch is applied only to a
new private source context, with every original test file unchanged. A worker
built from that context includes `source-patches/sysbox-shm-reexec.patch` and
the full before/after declaration, bound to its package recipe and loader.
All three Sysbox component hashes change. Existing recipe/hash guards remain;
this does not establish that old snapshots can be restored with the new
components. Keep the old package and intact store for recovery or rollback,
and require proven compatibility or a controlled finish of old work before
switching. See [the startup evidence](production-progress.md#shm-startup-raw-clone-failure-under-gc-pressure).

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

## Canonical base import admission

The bounded-store candidate limits canonical bases to 2 MiB of compact JSON
entries in the eventual worker-exit ledger, half its unchanged 4-MiB bound.
Before creating a new base directory or intent, it adds existing complete base
records and a prospective record with the exact new paths, maximum numeric
widths and maximum completion timestamp width. A changed or unfinished base
inventory refuses a new import. This guard counts retained metadata, not guest
file bytes, and does not allocate a disk reservation.

The error reports current bytes, the new import's upper bound and the budget.
Repeating a complete base ID still verifies and returns its original imported
base, including at this boundary. A rejected new ID owns no directory, intent
or canonical mount. No old base is removed to make room.

At this boundary, stop accepting new base imports. Finish or fully pause each
owned sandbox, stop the worker through its normal drain path and require its
successful exit before replacing it on the same owned persistent store. The
replacement has a fresh controller epoch and a fresh metadata budget. Retained
old payloads still consume the store's physical capacity. A new epoch does not
free those bytes or authorize deleting old epoch directories.

This guard covers canonical base imports only. Checkpoint aliases, fork/recovery
metadata, pause histories and old epoch retirement remain separate release
requirements. The existing full exit-ledger completion preflight still runs
before any fence or release. This change alone does not qualify production.

## Sandbox record admission candidate

Candidate `factorybox-build-worker-b8724db2ca594eb9b86989129604a411`
refuses new sandbox metadata before starting a new sandbox, checkpoint, pause
or resume when the complete prospective record or capture request would exceed
its existing reader. The durable sandbox record has a 1-MiB limit including
its final newline. The complete source guard has a separate 16-KiB packet
limit, and the sealed capture/timer request has a 64-KiB limit. Long paths and
JSON escaping count toward these encoded-byte limits; no fixed number of
accepted pause cycles is promised.

Admission reserves successful and failed pause outcomes and completion metadata.
A resume also reserves its following pause. New durable lifecycle error
summaries are limited to 4 KiB of raw text, retaining the original error's
prefix, byte count and SHA-256. The caller still receives the full error.
Existing errors, history, ownership and snapshot identities are never shortened
to make admission pass. The atomic record publisher independently refuses an
oversized replacement before creating a temporary record file.

On `new sandbox metadata refused`, stop sending new work to that sandbox and
retain the complete command result and diagnostics. Inspect `state` using the
same worker and sandbox identity. Do not edit `record.json`, erase its history,
retry an uncertain transition with a different request, or delete backing to
make the request fit. Free disk space does not increase a JSON reader limit.

For a confirmed `paused` record with the exact completed pause checkpoint and
the original stopped runtime, the tested alternative is an independent public
`fork` from that checkpoint into a new sandbox ID. Use its exact original
source and snapshot IDs with the ordinary command prefix above, for example:

```sh
/opt/factorybox/factorybox --worker-run-root /run/factorybox fork \
  --source source --snapshot-id "$confirmed_pause_snapshot" replacement
```

Supply the snapshot from the verified original pause, and verify the new
sandbox's application/RAM/file state before choosing it as the continuation.
Keep the source and its recovery checkpoint protected. This route applies to
a known admission refusal with an intact committed pause; it does not resolve
an uncertain attempted restore. A running source that cannot admit another
checkpoint remains running, but its earlier checkpoint cannot stand in for
unsaved application progress. Preserve that source for explicit reconciliation.

The native case injects history into an actual owned paused source near the
1-MiB limit. It proves refusal before transition or resume backing, unchanged
source bytes and stopped identity, and a useful independent fork with the
original private RAM/files. This is separate from a complete worker exit-ledger
budget. Admission of aggregate checkpoint/fork/recovery aliases and paused
arrays, old epoch retirement and final production qualification remain open.
