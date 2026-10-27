# Local worker contract, schema 1

The historical [pilot profile](../upstream/amd64-pilot-profile.json) retains only
its exact results and limits in [pilot-validation.md](pilot-validation.md).
The [current readiness checklist](first-release-status.md) is the single list of
open release work. The older 6016/829f package and its
[operating procedure](first-release-operations.md) retain their own evidence;
they do not qualify the latest binaries. Older named package experiments below
are development history and do not establish defects in the current package.
Production qualification remains false.

The worker is a privileged administrative service for one
customer or trust domain. Consumers are separate processes inside its system
containers. No IDE, agent, Python client or Dev Container is required by the
Go runtime.

## Platform and deployment

Ordinary worker execution recognizes Linux AMD64 with kernel
`7.2.8-9-omarchy` and the separate local ARM64 profile with kernel
`7.0.14-orbstack-00380-ga7e0a2dc9535`. Both use 4096-byte pages, cgroup v2 and
the tested same-host Docker/local-volume storage path. The ARM64 profile does
not promote a new production candidate or rewrite the historical AMD64 pilot.
`factorybox doctor` checks the platform tuple; it does not establish component
or lifecycle qualification. Cross-compilation, another filesystem, a different
ARM64 kernel/page size or a successful image build does not qualify another host.
The local ARM64 tuple has Factory/worker functional evidence. The declared
Systemd proc fixture now checks the host's actual kcore capability: original
read-denial assertions for an existing node, strict guest absence and a failed
ENOENT read for an absent node. It keeps kmsg denial, zeroed kallsyms and the
original skip set on both architectures. This removes a test's kernel-config
assumption, not the requirement to pass all native gates on each release image.
Tuple admission and `doctor` do not override those gates.

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
it does not provide VM isolation. The optional HTTP probe and epoch-bound CLI
support the [Kubernetes consumer admission lane](kubernetes-worker.md); that lane
has not been qualified against a running Kubernetes cluster.

## Persistent and ephemeral state

Mount the complete owned volume at `/var/lib/sysbox`. The selected bounded
Compose profile mounts its fixed filesystem at `/var/lib/sysbox/fixed-store/data`
and creates `store/` there with mode 0700. The older unbounded pilot instead
uses `/var/lib/sysbox/store`. The Go supervisor holds the store lease for its
lifetime. Its `store.json` fixes the caller's store ID, `manager/` holds Sysbox-managed
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

New worker builds also accept optional `--health-listen IP:PORT`. It serves only
`GET /readyz`, with an empty 204 for the verified original live worker and 503
when that verification fails. It starts inside PID 1 after delegation, without
creating an exec task. It returns no status identities or administrative API.
The default is disabled. Use an explicit numeric bind address and keep the
health port within the administrative network.

Remote consumers can pin lifecycle calls to the admitted worker by supplying
both `--worker-store-id STORE_ID` and `--worker-epoch EPOCH` before the command,
alongside `--worker-run-root /run/factorybox`. Both IDs must be 32 lowercase
hexadecimal characters and match live `worker-status`. A mismatch fails before
the lifecycle engine opens. This protects a delayed command from reaching a
replacement epoch between orchestrator inspection and exec. It neither selects
recovery inputs nor grants recovery after a reboot or cross-node move.

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

The host preflight observation does not reserve 4 GiB, cap guest writes or
guarantee space throughout an operation. The selected bounded filesystem below
separately limits its own backing and applies its own admission policy. This
is not capacity coordination between workers. The earlier host-only pressure
result covers the concrete failure described in
[production progress](production-progress.md#capacity-admission-and-rollback-space).

The first-release candidate uses `worker --bounded-root
ABSOLUTE_PRIVATE_DIRECTORY --bounded-bytes BYTES`. It creates a fixed image from
512 MiB to 64 GiB, with no automatic migration, resizing or reformatting. The
store becomes `DIRECTORY/data/store`; image identity and the outer lease remain
outside that filesystem. Its internal admission floor is 64 MiB plus 128 MiB
headroom, separate from the host's 12 GiB plus 4 GiB checks. Both observations
appear in `capacity`. All admitted disk-backed bundle layers and bind sources
must reside in the bounded filesystem; initially read-only external binds are
also refused. This profile requires existing loop/ext4 support and direct I/O.
The current package passes native capacity, retained-recovery and admission
tests plus all four complete Minimal/Factory pressure consumers after planned
or abrupt same-host worker loss. It also refuses duplicate ownership, changed
store identity/capacity and partial initialization without rewriting image
bytes. The [current release status](first-release-status.md) binds those
results. The historical pilot deployment remains unchanged. These results
cover the tested capacity and failure cases, not arbitrary workloads or
coordination of several workers' host-space use.

## Local commands and responses

Administrative clients execute the image's own binary inside the worker:

The paths below use the delivered bounded Compose layout. Prepare the complete
bundle on that filesystem. For recovery, save and use the old worker's exact
`worker-status.lifecycle.root`; substitute its actual epoch for `OLD_EPOCH`.

```sh
/opt/factorybox/factorybox worker-status
/opt/factorybox/factorybox --worker-run-root /run/factorybox start --bundle /var/lib/sysbox/fixed-store/data/store/input/bundle source
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
automatic checkpoint retirement and old-epoch garbage collection are not
implemented by this command. Use the bounded profile and explicit identity- and
dependency-checked retirement procedures below. Their current-package evidence
is indexed in the [operating procedure](first-release-operations.md); automatic
retention remains deferred.

## Independent outer-network copies

The local candidate can fork a committed private-source Pause with
`io.factorybox.outbound-ipv4=true` inside its original worker epoch. It persists
another owner, subnet, veth pair and MAC before creating the target. Paused
Sandboxes and pending forks reserve their subnets even without live routes.
The source's saved files remain unchanged. The private target changes only
the exact outer-netdev mapping in `criu.conf`, then replaces its copied outer
address and interface identity while its restored cgroup is still held.

Capture writes `images/network-copy.json` into the hashed checkpoint inventory.
Admission inspects actual socket and network-namespace cookies and verifies
that every captured thread shares its inspected process file table. It accepts
unconnected TCP/UDP sockets, wildcard or loopback listeners and established
loopback connections with both unique endpoints in the same captured namespace.
It refuses original-outer-address bindings, connected routed/external sockets,
raw sockets, unknown namespaces and unproved file tables before publishing a
fork intent. Matching IP/port tuples across nested namespaces are not sufficient
peer-ownership proof. A copy refusal does not authorize resetting the source
or changing its checkpoint.

For same-identity Pause, the guard isolates only the verified original worker
veth end before capture and keeps it down while the frozen source is destroyed.
Otherwise closing the original sockets sends FIN to uncaptured peers, which
then reject the restored stream. Confirmed capture rollback reactivates that
same owned pair after socket/filter rollback, before source release. Resume
configures its freshly restored pair while the target is held. Neither operation
changes the worker uplink or another Sandbox's interface. The focused
`--suite network-resume` requires the original external TCP peer and memory,
not a replacement connection.

This does not authorize shared canonical Source copies, worker replacement,
credential transfer or a Factory Staging template. Consumers must separately
prepare clean role-appropriate state. Existing checkpoints without the socket
admission image cannot gain network-copy authority through a retry. Reproduce
the focused native lifecycle with `tools/bounded_store_native.py --worker-build
EXACT_BUILD --suite network-fork`; full consumer and release gates remain
separate.

## Experimental checkpoint selections and retirement

The bounded-store candidate provides `select-checkpoint`,
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

`retire-resume` uses the same exact checkpoint, owner and hash arguments to
retire a completed Resume's independent private RAM image copy. It returns
`payload_kind: "resume-images"`. The current Resume stays protected until a
later complete Pause or confirmed Delete. Retirement never removes its successor's
images. This operation is separate from retiring the checkpoint that supplied them.

After confirmed native Delete and ordinary release of the original caller-owned
overlay/base mounts, private mutable filesystem backing can be retired:

```sh
factorybox --worker-run-root /run/factorybox retire-sandbox \
  --sandbox-owner EXACT_ORIGINAL_OWNER \
  --input-sha256 EXACT_ORIGINAL_START_INPUT_SHA256 SANDBOX
```

The bounded-store operation returns `payload_kind: "sandbox-backing"`. It
requires the original admitted bundle directory identity and removes only
inventoried private upper/work/volume trees and a deleted fork's initial private
images. It preserves canonical Source, registered common bases, configuration,
operation receipts and the bundle anchor. It does not unmount caller-owned mounts
or reclaim checkpoint/Resume generations implicitly. Retire those with their own
exact requests. Native Delete by itself still retains all this backing.

Both operations retain a durable inventory and completion reserve. Repeating
the exact request continues interrupted deletion or returns its original completed
receipt. Foreign hardlinks, mounted payloads, unexpected entries and successor
objects cause refusal. Before/after capacity reports are observations of the
shared filesystem, not a byte charge for one Sandbox. They do not establish
production qualification or cross-worker recovery.

The following timer-integration notes describe preserved older package
experiments. The selected package passes the complete retirement suite and
targeted timer/restore-hold cases as indexed in the current release status;
full production qualification still requires the remaining gates.

The earlier BusyBox sleep and direct
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
  --source-root /var/lib/sysbox/fixed-store/data/store/epochs/OLD_EPOCH/controller \
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

An actual 8-GiB full-consumer recovery attempt reached ENOSPC while copying
private RAM images. Independent read-only verification of its complete saved
image confirms that every published RAM/metadata file and all nine saved
filesystem trees remain intact, with the original source still committed
paused. Its separate useful rescue subsequently passes using the exact old
saved recipe and a separate writable destination in
`factorybox-enospc-original-rescue-e66b1b1a3bdb4cf39d49f6c67d6eb13d`.
The failed original attempt remains failed. This forensic rescue does not
qualify the old package as a fallback or prove new-recipe compatibility. It
does not authorize deleting uncertain partial imports or adopting copied store
identities. Use the explicit inspection/cancellation protocol in the operating
procedure. Exact evidence is indexed in the current release status.

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
Then use `tools/build_worker.py --platform linux/amd64 --native-base-build BASE --sysbox-build SYSBOX
--retained-candidate CRIU`; use the successful exact artifact names printed by
each build. Repeat with `--compare WORKER_BUILD` for the independent package
and image comparison. The source lock and ordered patch hashes must match the
pilot profile; another execution tuple still needs native qualification.

For the local ARM64 profile, use `--platform linux/arm64` and exact completed
ARM64 component inputs. The current Factory consumer also requires the ordered
shared-description CRIU delta. `tools/build_worker_pipeline.py` assembles that
complete composition without Candidate admission flags. The
[Factory consumer evidence](factory-consumer.md#native-arm64-local-execution)
records the ordinary delivered image and its exact local scope. An ordinary
build still does not publish, install or qualify an arbitrary ARM64 host.

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

This 2-MiB guard covers canonical base imports. The aggregate candidate below
also accounts for the other exit-ledger arrays. Old epoch retirement remains
a separate release requirement. The existing full exit-ledger completion
preflight still runs before any fence or release. These changes alone do not
qualify production.

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
original private RAM/files. The aggregate candidate below adds a complete
worker exit-ledger budget. Old epoch retirement and final production
qualification remain open.

## Aggregate worker metadata admission candidate

Candidate `factorybox-build-worker-0b75abed9eb0490db9f722e0354ddfb4`
applies a shared encoded-byte budget before admitting a new sandbox, base,
checkpoint, pause, resume, fork or paused recovery in a bounded worker. It
counts canonical bases, recovered bases, immutable capture/fork aliases and
paused sandbox records, including pending fork/recovery inputs. It reserves
the encoded next-pause outcome of each known active or unresolved sandbox,
using the same maximum field/error widths as local record admission, capped
by the independent 1-MiB publisher limit. An intent without its prepared
record reserves the full 1 MiB. The next pause alias also counts. A committed
paused record counts at its actual compact JSON size. The envelope includes both maximum-width
timestamps and all array keys, brackets and separators within the unchanged
4-MiB reader limit.

The earlier 2-MiB canonical-base and local record/transport guards still apply.
An unknown or incomplete inventory is not silently discounted. Retired capture
aliases remain conservatively charged until worker replacement. This check
reads metadata; it does not rehash all saved RAM pages for every new request,
publish an exit fence, adopt old process identities or authorize cleanup. The
strict exit integrity and ownership preflight remains independently required.

On `new worker metadata refused`, retain the complete error and current data.
Stop submitting new work to this worker epoch. Use exact completed requests
for replay and inspect the owned sandbox states. Fully pause or delete live
sandboxes through their existing lifecycle operations while admission permits
it, require a successful normal worker stop, then replace it on the same owned
store. Already committed pauses remain recovery inputs for the new epoch.
If a legacy inventory already exceeds the bound, preserve it for explicit
reconciliation; do not truncate histories or remove directories to force drain.
A new epoch resets this metadata budget but does not reclaim old payload bytes
or implement manual epoch retirement.

## Releasing a recovery selection retained by an earlier worker

The unselected bounded-store candidate provides `release-recovery-selection`
through the validated current `--worker-run-root /run/factorybox`. Supply
`--source-root` naming the exact earlier controller, the positional source
sandbox ID, `--snapshot-id`, `--source-owner`, `--checkpoint-sha256` and
`--selection-id`. The source must be a distinct epoch of this same bounded
store, on the same boot and with the same pinned runtime/CRIU recipe. Its
persisted image identity must match the current attachment; a former loop
number is not reused as authority. Unbounded or explicit controller options
cannot invoke this command.

Ordinary `release-checkpoint` remains an operation of its original live
controller and obeys the worker-exit fence. The maintenance command instead
locks the current controller and then the existing retained controller. It
opens no old runtime state and initializes no old directory. It publishes only
the release of the exact selected generation, preserving its original selection
and selected timestamp. An exact retry returns the same durable result.
Other selections remain effective. Releasing all selections still leaves an
unresumed committed pause protected, and does not remove any payload or reopen
the old lifecycle. Checkpoint/epoch backing retirement remains a separate
operation requiring its own dependency and ownership proof.

## Inspecting a retained complete checkpoint

`inspect-recovery-checkpoint` uses the same validated current worker and exact
earlier `--source-root`, positional source sandbox ID, `--snapshot-id`,
`--source-owner` and `--checkpoint-sha256`. It requires no selection ID and
does not release one. It holds the current and old controller locks while
checking the published generation, every RAM/opaque metadata file and every
saved filesystem tree. The old lifecycle remains fenced.

A vanished read-only mount is not immutable runtime authority. For a retained
base, this inspection checks its canonical stored inode, original metadata and
complete content digest. Its former alias must be the exact original empty
uncovered directory. Unexpected covering mounts, changed contents, changed
metadata and replacement directories are refused. Only the independently
verified identical store image and epoch can authorize a changed loop device;
old mount IDs are never adopted. Inspection creates no new mount or copy and
does not modify the saved manifests.

Successful output is a fresh observation with the exact request, verification
time, serialized checkpoint-file count/bytes and filesystem-tree count. The
file-byte field excludes filesystem-tree payloads and does not measure store
capacity. This report grants no deletion authority, releases no committed
pause, and does not establish a recovery handover. The separate candidate
protocol below performs its own dependency and owned-payload admission.

## Internal retirement maintenance fence candidate

The candidate implements an internal store-wide maintenance fence. Once an
exact authorized preparation has been accepted, ordinary lifecycle work,
recovery inspection and selection release are refused until deletion and
durable completion are verified. The refusal also applies to explicit
controller options and to replacement workers on this same store and boot.
Private source preparation and acceptance remain as a permanent source fence.

An exact retry retains the original acceptance and fence identity. A file with
identical bytes at a successor inode, an external hardlink, changed attachment
identity or premature completion cannot clear that fence. Only verified
same-image reattachment can translate former device/mount IDs. Stored original
plans are preserved; translation produces a separate in-memory plan.

The earlier native coordination fixture authorizes only its complete original
RAM image directory and then recovers a separately preserved useful successor.
That evidence does not qualify the full public retirement protocol below.

## Manual epoch retirement candidate

The bounded-store candidate provides `begin-epoch-retirement` and `retire-epoch`.
They require the current verified `--worker-run-root` and an exact JSON
`--request-file`, without positional sandbox arguments. Bounded actor cases
cover replacement after accepted partial deletion, a registered unused base,
actual interruption of the public unlink process and a second epoch with
fork/resume history. The separate full Factory case also retires its actually
used base and original RAM backing while preserving a useful independent
successor. Both complete current native epoch cases and all four pressure
consumers are independently closed on the selected package. The operating
procedure links their exact current evidence. Complete final-package
qualification remains required before production use.

First drain all sandboxes in this store to fully committed pauses or confirmed
deletions. Release every active selection protecting a source checkpoint through
`release-recovery-selection`. Each committed pause in the retiring epoch must
have an explicit successor: recover its exact checkpoint into another epoch,
perform useful work, and commit a new complete pause. An independently paused
fork of that recovered sandbox is also eligible after its parent is deleted;
the original recovery and fork records must still prove the complete lineage.

The request names `source_root` and a `handovers` array. Each handover contains
`source` with `source_id`, `snapshot_id`, `source_owner` and
`checkpoint_sha256`, plus `successor` with the same fields and its distinct
`source_root`. Use the actual published IDs and SHA-256 values. A missing,
running, changed, unrelated or incomplete successor cannot withdraw an old
committed pause. An epoch containing only deleted sandboxes needs no handovers.

Run the candidate CLI inside the current worker:

```sh
/opt/factorybox/factorybox --worker-run-root /run/factorybox \
  begin-epoch-retirement --request-file /output/retirement-request.json
/opt/factorybox/factorybox --worker-run-root /run/factorybox \
  retire-epoch --request-file /output/retirement-request.json
```

`begin-epoch-retirement` verifies the complete source inventory, independent
successor RAM and filesystem, all other locked controller epochs and current
mount dependencies. It admits completion metadata before any deletion and
allocates a real 1-MiB completion reserve. The largest serialized completion
must fit half that reserve, retaining the other half for publication metadata.
Its durable acceptance closes the
whole store to ordinary lifecycle mutations, recovery readers and selection
release. It removes no payload. `retire-epoch` can perform both stages directly
or continue exactly this accepted request after partial deletion and a worker
replacement on the same host and boot. Preserve the request unchanged.

Unknown entries, changed metadata, replaced directories, external hardlinks,
covering mounts, live dependencies and incomplete operations cause refusal.
An unaccepted partial preparation preserves access to intact recovery inputs;
it does not authorize deleting its files or clearing a store fence by hand.
After acceptance, keep the store out of service until its exact continuation
completes. A failed retry remains a failure and retains the fence.

The new candidate also provides `inspect-epoch-preparation --source-root PATH`
and `cancel-epoch-preparation --request-file INSPECTION.json`. Both require the
current verified worker. Inspection needs no new allocation in the bounded
store; save its output outside that store. It reports the complete exact private
preparation metadata, including inode, mount, mode, owner and full content hashes.
The directory's birth time and change time bind the observation to that exact
generation, including when a filesystem later reuses an inode number.
An incomplete encoded inventory is metadata to cancel, never deletion authority
over the RAM/filesystem paths it might describe.

After checking the reported source and inventory, pass that unchanged JSON to
the cancellation command. Cancellation locks the controller forest, refuses any
store acceptance fence or source acceptance/completion, and requires every
inspected object to remain present and unchanged. Only the known private
preparation names can be removed. Unknown files, symlinks, external hardlinks,
changed content, ownership, attachment or covering mounts refuse. Caller inputs,
checkpoint/base payloads and recovery handovers are outside this operation.

If cancellation is interrupted, inspect the remaining metadata again and use
that new complete inspection. The old inspection cannot authorize missing
entries or a newly created stage at the same path. If the stage is already
absent, cancellation has nothing to remove; prepare a new retirement request
through the ordinary complete admission. An accepted retirement must instead
continue through `retire-epoch` with its original request. Planned and abrupt
bounded cases verify these commands under actual preparation ENOSPC with
16 KiB available, including refusal of changed/foreign metadata and accepted
retirement, original-data equality and complete subsequent useful recovery.
Their independently reproduced candidate package and exact evidence are in
`factorybox-epoch-cancellation-notes-2e452dda65b449d18837f5ed009c2035`.
This is separate from the earlier fixture-only cleanup evidence and does not
qualify the complete first release.

The payload inventory covers controller-owned registered bases, published RAM
and saved filesystem trees, private imported/forked backing and completed resume
images. Original requests, records, configuration, provenance and retirement
receipts remain. Caller input bundles and vendor-manager storage outside the
controller remain outside this deletion authority. Source inspection remains
permanently closed after successful retirement; the verified successors become
usable after the original global fence is removed. An exact completed retry
returns the saved report while still verifying retained metadata and payload
absence. Capacity before and after is observed; absent names do not promise a
particular number of released blocks. Automatic retention remains deferred.
