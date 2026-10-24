# First local release operating procedure

This is the candidate procedure for the scope in [GOAL.md](../GOAL.md).
Production qualification remains false until the final profile binds all
required passing evidence. The older pilot profile does not qualify this
candidate. The current independently reproduced package is
`factorybox-build-worker-29cba13df6534ab9ba999d367d21f51e`, with comparison
`factorybox-build-worker-e23f9862edab4ed99960d598573b608a`.
Older evidence below identifies the package on which it ran and does not replace
the remaining current-package qualification.

The complete Factory planned-loss consumer, including actual full-storage
import failure/cancellation, retained-checkpoint inspection, changed loop
attachment, public epoch retirement, retained Theia, local OpenCode flow and
both graceful worker exits, passes the unchanged shared 420-second gate in
`factorybox-consumer-worker-loss-b3c94cb31be94a4685f658990919f2f6`.
The matching abrupt case also passes in
`factorybox-consumer-worker-loss-e01715a8779545e99f7d1fa33db141bd`, with exact
source-worker SIGKILL after committed pause and graceful replacement exit.
Remaining final-package gates still require their own passing evidence.
The current interrupted native epoch fixtures also pass in
`factorybox-bounded-loss-87e54f0908184d2cb49d1d5d45f74221` (planned) and
`factorybox-bounded-loss-d0df005e97f34218a7380f3c2a8bfa14` (abrupt).
The original 8-GiB recovery and remaining release gates retain their own
acceptance requirements.

The same current package also passes the complete Minimal planned/abrupt
consumers with the same bounded-store, actual pressure, retained-checkpoint,
changed-attachment and public-retirement checks:
`factorybox-consumer-worker-loss-16d115cb54b14e99b2eb63e7ea1ec08e` and
`factorybox-consumer-worker-loss-fc811d31718e47738b8642173a442830`.
These four consumer passes do not complete the remaining native, upstream,
external-agent, operations and predetermined-series qualification.

All eighteen declared native gate groups now pass in
`factorybox-pilot-native-gates-b4189cf518b44b2f870f7988f0417916` against this
delivered core and pinned components, including a fresh eighteen-case CRIU
core run. This leaves the remaining upstream/integration, actual-provider,
operations, historical-error and final-series requirements open.

## Start and verify

Use the existing qualified Linux AMD64 host and boot, with a separate worker
and complete private store per customer or trust domain. Keep the exact image
digest, package/component manifests, store ID, volume identity and ownership
label. The [Compose example](../examples/worker/README.md#bounded-store-candidate)
provides the bounded-store arguments and the 45-second graceful stop window.
Preserve the 3 CPU/3 GiB worker and two-sandbox ceilings. A separate Theia has
its own 1 CPU/768 MiB ceiling.

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
That acceptance remains required.

The smaller delivered 512-MiB runtime fixture now passes actual useful restore
of its retained checkpoint after exact owned pressure reclaim, followed by a
fresh public checkpoint/fork, pause/resume and an independent descendant after
source deletion. Evidence is
`factorybox-bounded-store-301ead43c5d64a5585c904620e8d9cc5`. This does not
qualify recovery of the earlier complete Factory store or recovery interrupted
by capacity pressure during worker replacement.

Use the public identity- and dependency-checked checkpoint/epoch retirement
commands in the [worker contract](worker-contract.md#manual-epoch-retirement-candidate).
Drain first. An old committed pause needs a verified independent successor
before its epoch can be retired. Preserve the exact JSON request, preparation,
acceptance and completion receipts. Once maintenance is accepted, ordinary
work remains fenced until exact completion, including across worker replacement.
Unknown objects, live dependencies and changed ownership require refusal.
Automatic retention remains deferred.

Candidate `factorybox-build-worker-ac0f93d4f9ab4c0aaa20814541442715` exposes
explicit cancellation of an unpublished recovery copy. It preserves
`import.json` and permanently reserves that failed target
ID. Save the complete inspection outside the bounded store, then submit that
exact file:

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
initial `import.json` publication uses the separate protocol below. Root-owned focused tests and
complete planned-loss ENOSPC consumers now pass for both the minimal example
and Factory with Theia/local OpenCode. Their exact candidate has an independently
identical package/image build. Evidence is bound by
`factorybox-import-cancellation-closure-99e5f00053194df0997f0c3cfca22839`.
The remaining full-consumer loss combinations still require acceptance.

Candidate `factorybox-build-worker-f3086761c73449e3aef5f636a7ebd68b` also distinguishes an initial metadata stage whose
`import.json` was never published. Its inspection reports
`reservation_absent: true`, with no invented owner or preparation hash. It
accepts only an empty private operation directory or one exact private
publication temporary, with no payload, intent or target object. Explicit
cancellation removes that complete metadata stage without allocating another
inode in the store. Save its request and completion outside the store. This
unaccepted stage has no durable missing-entry replay: after interruption obtain
a fresh inspection of any remainder; an absent stage or new same-name
generation cannot inherit the previous request. A committed import reservation
keeps the terminal-receipt and reserved-ID behavior described above. Focused
root-owned tests and both complete native planned/abrupt replacement fixtures
pass. Both hit actual initial journal ENOSPC, cancel at zero free inodes,
preserve the original backup, complete every original 32-MiB RAM/files recovery
assertion and stop the replacement cleanly. An independent build reproduces
the complete package and image. The binding is
`factorybox-import-initial-closure-d9879765b7994d4eac13330ccaea21b8`.
This closes the basic initial-publication fixture, not mounted-stage or full
consumer acceptance.

A mounted partial import refuses inspection at its actual mount boundary.
Do not remove its files or infer mount-release authority from the error.
Withdraw new work, verify the exact owning worker, fence and remove that worker,
and attach the same intact owned store to a replacement on the same host/boot.
Then inspect using the failed controller's exact `--source-root`, save the full
new inspection externally and submit it to `cancel-recovery-import`. This
cancels retained file backing after the old mount namespace has disappeared;
the cancellation itself never unmounts anything. The original reservation
remains and exact completion replay stays available.

This sequence now passes both complete basic planned/abrupt source-worker
fixtures on the f308 runtime core. Each additionally kills the actual import
controller after its immutable mount exists, proves mount-specific refusal,
hard-fences the import worker, cancels through a third epoch and completes all
original RAM/files recovery assertions with a clean final exit. The binding is
`factorybox-import-mounted-closure-ad889dbcedd7473588d7957773325b1e`.
Full consumer and final-package qualification remain separate.

The complete minimal recovery-pressure consumer also passes on f308 in
`factorybox-consumer-worker-loss-6bf4c40fa77c4e1d98ad6111797d6268`, with a
32-GiB store, abrupt worker loss, changed loop attachment, intact useful
recovery, cancellation at zero available bytes and complete public epoch
retirement followed by every original consumer assertion. The replacement
stops with exit 0. Independent binding is
`factorybox-import-abrupt-consumer-closure-a97964e88e594406a5db9907ceba3614`.
The remaining full-consumer loss/example combinations still need acceptance
on the current/final package.

The current image passes the complete existing public retirement fault fixture
in `factorybox-bounded-loss-29c1db8cc9db4ae089f9667c5122e266`. It includes
actual preparation ENOSPC, public inspection/cancellation while space remains
scarce, refusal of foreign/live dependencies, interrupted public deletion,
another worker loss, changed loop attachment and exact completion with a useful
successor. The actual reclaimed capacity is 83,058,688 bytes. This 1-GiB runtime
fixture is separate from full consumer and update/rollback acceptance.

The complete current minimal consumer also passes in
`factorybox-consumer-worker-loss-e29c1e60b15a415181cbfab4e4b50436`, with an
actual 32-GiB store, hard worker loss, changed loop attachment, full retained
checkpoint inspection, ordinary public epoch retirement, exact replay and
useful independent work after old payload removal. It observes 2,048,659,456
bytes returned to available store capacity and a clean replacement-worker stop.
The current Factory counterpart also passes in
`factorybox-consumer-worker-loss-296fe89873b049b7910acf8cd6c442e3`, with
planned replacement, separate Theia, local OpenCode fixture, full retained
inspection, public retirement and every subsequent original independent
lifecycle assertion. It observes 2,889,646,080 bytes returned to available store
capacity and both workers stop cleanly. These two observations do not qualify
the other loss combinations, recovery import under capacity pressure, external
inference, update/rollback or the final release series.

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
Exact update/rollback exercises remain open; these instructions alone do not
close that release requirement.

## Current operating evidence and remaining acceptance

The revised bounded Compose/workflow passes on the actual delivered image in
`factorybox-worker-example-3f93920dace74945b3cb7d405211a90d`, using 32 GiB.
Both clean stops, changed-input-identity refusal, complete worker replacement,
recovery/replay, a subsequent checkpoint and an independent descendant pass.
Independent binding is
`factorybox-bounded-example-proof-b1eee18e7e604e5e8ef023c583f5a8dc`.
The first 4-GiB attempt fails with ENOSPC while copying recovery backing;
its frozen source, logs and failed result remain preserved. Its superseded
generated store dump was fully verified and retired with receipts in
`factorybox-resolved-archive-retention-77aa2afc7d8d4a01af5b88e952c7276e` and
`factorybox-resolved-archive-continuation-91b268542a1d465fbf8777640889207f`.
The original 4-GiB verdict stays failed. The
same workflow and actor pass with the previously exercised 32-GiB capacity.
This uses a small administrator-prepared actor and does not replace complete
minimal, Factory/Theia or external-agent acceptance.

The remaining goal also requires final-package storage/fault cases, actual
external-agent continuation/cancellation/uncertain-effect handling, tested
update/rollback, ten predetermined complete lifecycles per consumer and the
final requirement-by-requirement release profile.
