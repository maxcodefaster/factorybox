# First local release operating procedure

This is the historical 6016/829f package procedure for the scope in
[GOAL.md](../GOAL.md). The [readiness checklist](first-release-status.md) tracks
the latest integration and release gaps. "Current package" below refers only
to the package named in this procedure, not to the latest repository build.
Production qualification remains false until the final profile binds all
required passing evidence. The older pilot profile does not qualify this
candidate. The current independently reproduced package is
`factorybox-build-worker-6016296965364344a8ef9eede420dbc8`, with comparison
`factorybox-build-worker-829faef7ea3143038bebf72493474070`.
Older evidence below identifies the package on which it ran and does not replace
the remaining current-package qualification.

The current package passes the complete original abrupt Factory pressure/epoch
case in `factorybox-consumer-worker-loss-77412471c65b4ca988471b01cebe5c28`,
including actual import ENOSPC, exact cancellation/replay, a changed loop slot,
an independent successor pause, public epoch retirement, useful second resume
and subsequent independent descendants. All seventeen main Go/storage groups
now pass with exact core/component binding in
`factorybox-current-main-native-closure-9c499b4ac32a4ac0824da84515a910ce`;
the same CRIU component's eighteen cases/two cycles are bound separately.
The additional eleven targeted timer/restore-hold/metadata groups, complete
retirement/capacity cases, both interrupted public epoch cases, all four full
pressure consumers and all fifteen integration cases now pass as indexed in
the current status. The twenty predeclared ordinary trials and all five
operating cases are now complete and independently verified. The five further
storage cases, including both exit-size boundaries with declared administrative
legacy preparation, are also independently closed. All four original complete
interrupted-drain consumers and the separate original CLI planned/abrupt worker
restart are now independently closed on the same package. Actual-provider
acceptance, historical busy-mount attribution and final profile audit remain
open. See the
[current release status](first-release-status.md) for the current matrix.

Use the package above for the procedures below. Older package experiments and
their preserved failures are recorded in [production progress](production-progress.md).
Their identifiers are provenance, not deployment or rollback choices.

## Start and verify

Use the existing qualified Linux AMD64 host and boot, with a separate worker
and complete private store per customer or trust domain. Keep the exact image
digest, package/component manifests, store ID, volume identity and ownership
label. The [Compose example](../examples/worker/README.md#bounded-store-candidate)
provides the bounded-store arguments and the 45-second graceful stop window.
Preserve the 3 CPU/3 GiB worker and two-sandbox ceilings. A separate Theia has
its own 1 CPU/768 MiB ceiling.

Keep the delivered privileged outer worker's capability set. The selected
timer inspection reads kernel symbols and BPF state; dropping `CAP_SYSLOG`
in a wrapper masks the required symbol and checkpoint admission refuses.
The native one-capability comparison and corrected complete Docker-root
interruption proof are recorded in `docs/production-progress.md`. This does
not add a capability to inner workloads or permit changing host sysctls.

Before starting, verify that the previous exact worker has stopped and been
removed. Keep the full volume exclusively attached to this trust domain.
Read `/run/factorybox/worker.json` through the container file/archive API before
creating any exec process. Then require the delivered CLI's `worker-status`:

```sh
/opt/factorybox/factorybox worker-status
/opt/factorybox/factorybox --worker-run-root /run/factorybox capacity
```

The readiness file is only a publication hint. The status command verifies the
live supervisor, services, lease and peers. Require the actual store ID and
bounded image size, its distinct bounded filesystem, enabled capacity policy
and separate host capacity observation. Keep the complete JSON responses.
Prepare workload bundles and their mutable backing inside the bounded store.

## Drain and stop

Stop submitting work through the administrative client. Finish or explicitly
cancel outstanding consumer requests and preserve uncertain tool results.
For each live sandbox, choose a complete pause to retain its state or exact
deletion when its state is intentionally discarded. Keep every successful
pause's source epoch/root, source ID/owner, snapshot ID and full checkpoint
SHA-256. Require `phase=paused`, `completed_at`, zero anonymous workload memory
and zero resident snapshot bytes. An interrupted pause is not a recovery input.

The administrator owns its prepared input mounts. Record their original
identities before use and release only the same inactive mounts after pause.
The example's `bundle_mounts.py` checks its exact overlay/base pair and rejects
changed, nested or stacked mounts. Runtime-owned mounts remain the worker's
responsibility. Keep the backing directories and every selected checkpoint.

Send SIGTERM through the exact dedicated Compose service with its 45-second
grace period. Require the original container identity, `Running=false`,
`ExitCode=0` and `OOMKilled=false` before removing the container. A killed
process alone does not prove a successful drain. On failure retain the store,
container metadata and diagnostics; do not prune volumes or force an unmount.

## Same-host recovery

Fence and remove the exact old worker. Start the exact admitted image on the
same host and boot with the same complete store and store ID. A new worker gets
a new epoch. Pass the saved committed pause identity to `recover-paused` as
described in the [worker contract](worker-contract.md#recovering-a-committed-pause-after-worker-replacement).
Keep the target ID and entire request unchanged for exact replay.

Verify useful application work and its retained RAM/files, then a new
checkpoint and an independent fork. An earlier worker's live process handles
and readiness file grant no authority. Missing/corrupt backing, changed boot,
foreign identities and uncertain incomplete imports require investigation.
Running unsaved RAM, host restart and cross-node recovery are outside this
release scope.

## Capacity and manual retirement

Inspect `capacity` before accepting further work and retain every admission
refusal. At a limit, close new intake and keep existing sources, confirmed
pauses and selected recovery inputs. Do not delete partial imports to make an
uncertain recovery appear fresh. The original 8-GiB ENOSPC attempt is retained;
its integrity inspection alone does not prove useful recovery after reclaim.
The original selected 8-GiB pause now has an actual independent useful rescue in
`factorybox-enospc-original-rescue-e66b1b1a3bdb4cf39d49f6c67d6eb13d`.
This uses its original exact EEF CLI/CRIU/runtime recipe, unchanged read-only
image on its original loop0/device identity, a separate writable destination,
and fresh actual service prefixes matching the saved recipe. The full original
application and settled cancellation match; one new useful application mutation
passes without replay, followed by exact cleanup and unchanged full-image hash.
Total lifecycle time is 93.33 seconds. This is a forensic recovery of this old
selected state, not qualification of the old package as a rollback version or
compatibility with the new CRIU recipe. Keep the exact saved recipe for such
states; do not substitute a path alias for an actual manager service prefix.

Use the public identity- and dependency-checked checkpoint/epoch retirement
commands in the [worker contract](worker-contract.md#manual-epoch-retirement-candidate).
Drain first. An old committed pause needs a verified independent successor
before its epoch can be retired. Preserve the exact JSON request, preparation,
acceptance and completion receipts. Once maintenance is accepted, ordinary
work remains fenced until exact completion, including across worker replacement.
Unknown objects, live dependencies and changed ownership require refusal.
Automatic retention remains deferred.

The current package supports explicit cancellation of an unpublished recovery
copy. It preserves `import.json` and permanently reserves that failed target ID.
Save the complete inspection outside the bounded store, then submit that exact
file:

```sh
/opt/factorybox/factorybox --worker-run-root /run/factorybox \
  inspect-recovery-import --target failed-target > /output/import-inspection.json
/opt/factorybox/factorybox --worker-run-root /run/factorybox \
  cancel-recovery-import --request-file /output/import-inspection.json
```

For a fenced earlier controller, add its exact `--source-root` to inspection.
The operation requires the same host/boot, current verified store attachment,
no published target and a complete unchanged inventory. It removes only that
operation's private copy and at most one exact interrupted publication file.
It never releases mounts or removes the original backup. After an interrupted
deletion, inspect the surviving copy again. After confirmed completion, exact
replay returns the original cancellation receipt. A fresh recovery needs a
different target ID. Unknown entries require separate investigation. A failed
initial `import.json` publication uses the separate protocol below.

The same package distinguishes an initial metadata stage whose
`import.json` was never published. Its inspection reports
`reservation_absent: true`, with no invented owner or preparation hash. It
accepts only an empty private operation directory or one exact private
publication temporary, with no payload, intent or target object. Explicit
cancellation removes that complete metadata stage without allocating another
inode in the store. Save its request and completion outside the store. This
unaccepted stage has no durable missing-entry replay: after interruption obtain
a fresh inspection of any remainder; an absent stage or new same-name
generation cannot inherit the previous request. A committed import reservation
keeps the terminal-receipt and reserved-ID behavior described above.

A mounted partial import refuses inspection at its actual mount boundary.
Do not remove its files or infer mount-release authority from the error.
Withdraw new work, verify the exact owning worker, fence and remove that worker,
and attach the same intact owned store to a replacement on the same host/boot.
Then inspect using the failed controller's exact `--source-root`, save the full
new inspection externally and submit it to `cancel-recovery-import`. This
cancels retained file backing after the old mount namespace has disappeared;
the cancellation itself never unmounts anything. The original reservation
remains and exact completion replay stays available.

All four complete current-package Minimal/Factory pressure consumers pass after
planned or abrupt worker loss, including retained backing, cancellation, useful
recovery and retirement. The independent binding is
`factorybox-current-pressure-consumers-5f8ed07f5393427abba34b1d93973935/closure.json`.
Both interrupted native epoch cases also pass in
`factorybox-current-targeted-native-gates-1d4dba81823b45019d0d37e747face60/epoch-closure.json`.
These proofs retain the original assertions; they do not qualify a different
host, a host reboot or cross-node recovery.

## Update and rollback

Before changing a package or component recipe, either prove compatibility for
the exact retained state or finish and drain all old work. Never restore an old
snapshot under an unproven recipe. Keep the complete old package, store and
records until the new operating path has passed acceptance.

For a failed first deployment, withdraw intake, stop/fence the failed worker
and retain its intact store. Return only to an independently verified package
with proven compatibility for those exact recovery inputs. An older development
candidate with a known defect is not an accepted fallback. If no compatible
fallback is qualified, leave service withdrawn and preserve the data.

The passed current-package update exercise
`factorybox-operations-transition-bd2cde20e1164bea9683769e8b04be7d`
uses this controlled-empty boundary. It ends and deletes the synthetic old
workload, proves the old store has no live objects, forks or captures, releases
only the administrator's exact inactive input mounts, and stops the old worker
before starting the current recipe in a separate fresh store. The old store
remains sealed and its complete file identities/content are compared afterward.
The old EEF package is only a synthetic source and an intentionally refused
deployment candidate; it is not an approved rollback version.

The rollback part first commits a current-recipe pause, refuses an actual
different-component deployment against the approved current manifest, then
returns to the exact approved current image and recovers the saved request.
It verifies preserved RAM/files, exact replay, new useful work and independent
descendants before clean stop. The independent operating closure below binds
this result to the selected package. It does not make the old source package
an approved fallback or establish compatibility across different recipes.

## Current operating evidence and remaining acceptance

The bounded Compose/workflow passes on the current delivered image in
`factorybox-worker-example-83994d5e4825404fb3eb121c0afda237`, using 32 GiB.
Both clean stops, changed-input-identity refusal, complete worker replacement,
recovery/replay, a subsequent checkpoint and an independent descendant pass.
The same independent closure also verifies startup refusals, bounded-store
identity, all eight stop protections and actual update/rollback:
`factorybox-current-release-handoff-ff320800515d412d836e5ac38d149dd9/operations-closure/closure.json`.
The original CLI planned/abrupt replacement and four complete interrupted-drain
consumers, including both changed-loop cases, are independently bound in
`factorybox-current-release-handoff-ff320800515d412d836e5ac38d149dd9/drain-restart-closure/closure.json`.

All five additional storage cases and all twenty ordinary trials are also
independently complete on this package. Remaining mandatory gates are the eight
actual-provider continuation/cancellation/uncertain-effect cases, attribution of
the historical busy-mount failure, and the final requirement-by-requirement
profile. The 100-run series, 24-hour run, further optimization, automatic
retention and package minimization remain deferred as agreed in GOAL.md.
