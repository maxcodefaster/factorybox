# Local production work

Status on 2026-10-24: active, **production qualification false**. The accepted
[goal](../GOAL.md) extends the completed local pilot. Its frozen starting point
is commit `521e05799853c8546c9db5de31d178c548c49573` and artifact
`factorybox-production-baseline-0c99cab81e224d1db537b81d12a2e2f9`.
The existing pilot profile, imported reference, Factory repositories and
historical experiment remain unchanged. No new Go runtime/package is selected
by the observations below.

## External agent, first actual tool proof

`factorybox-external-opencode-d4edf4d1c2c84b46b539fb9824369467` passes with the
frozen consumer's actual OpenCode **2.0.27**, Node **24.21.0** and external
`glm-5.3-flash-free`. Live catalog and
[published pricing](https://opencode.ai/v2/docs/console/models/) were fetched
and hashed before the run. Only that explicitly free model was admitted;
there were no account credentials or paid fallback. This offer and availability
are provider state observed at that time, not a permanent service commitment.

The actual agent requests one foreground shell running the synthetic held
tool. The controller observes 16 MiB private RAM, changes its hash/counter,
delivers a filesystem watch event and releases the tool. The tool exits 37.
The provider's next answer acknowledges the actual private nonce, RAM hash
and exit. Both model responses succeed with reported cost zero. The two
provider exchanges take 6.749 s and 8.115 s in that run, including buffered
stream transport. These are model exchanges, not Factorybox lifecycle timings.
The stricter repeat
`factorybox-external-opencode-7dd4d9d8147f44adbe0bf6f8d939d310` also passes,
requiring exactly one completed shell call with the exact command, timeout and
exit metadata. Its model exchanges take 18.235 s and 11.792 s. Provider latency
varies substantially even between these two successful synthetic observations.

The optional test relay admits one model/path and bounded synthetic bodies.
It forwards the actual OpenCode client headers and its public provider bearer,
with no invented client identity or inference retries. The test container
has no network; its consumer bridge reaches the host HTTPS relay through one
owned Unix socket. This adapter is not a Go lifecycle dependency, direct
provider TCP restoration, or an already qualified post-restore connection.

Preserved development failures are explicit:

| Artifact suffix | Outcome |
| --- | --- |
| free-model `41b110f8b72841759f1eff805ab57748` | Raw unauthenticated protocol probe refused with `FreeTierError`; the free tier requires the real OpenCode client. |
| external-opencode `86dd55e94c8a4095bd0406424cd44b85` | Wrong local Docker context before a consumer was launched. |
| external-opencode `44ff509661e24bbfa6bc7bfe7f8e8cff` | New diagnostic's JavaScript syntax error; no inference. |
| external-opencode `e0fe39996c9544adaefdc4e349a3578a` | The experimental provider module has no ordinary `model` export; no inference. The documented compatible provider works. |
| external-opencode `c371bd64b3744630ad3d93ceb91f23a9` | First real inference and held tool work; the new controller reads the result file too early. Failed verdict retained. |

The last failure's copied output had a root-owned mode-0700 watch directory.
Its exact permission-only repair receipt verifies unchanged signal bytes.
The runner now retains Docker's output archive and extracts as the local user,
so capacity accounting can traverse owned outputs. All test containers/volumes
from these completed runs were removed after ownership checks.

The later complete ordinary lifecycle below extends this first tool proof.
Provider faults and complete worker replacement still need separate results.
The previous deterministic-model lifecycle proofs remain separate.

## Actual external agent through the complete lifecycle

`factorybox-provider-gateway-277aa53b688f40b29ee71f6dd3829fdb` passes its native
namespace and exact-stop test on the frozen C9 worker. The optional test helper
joins only the target network namespace. It remains outside the captured PID
and mount tree. Two real synthetic HTTP exchanges succeed through the owned
Unix relay, each with a closed client connection. A wrong saved process birth
is refused before signaling; the correct stop confirms pidfd exit and clean
process completion. This test alone does not qualify checkpoint/restore.

The full result
`factorybox-consumer-worker-loss-51bb604fdf87451485cb1af3b384b794` passes with
the actual pinned OpenCode, freshly admitted free model, private Docker/Compose,
Dev Containers and separate Theia. Original session, private tool RAM and
file/watch assertions hold through checkpoint, fork, fork of fork, pause,
resume and fork of the resumed branch. Source, branch and after-resume fork
each finish exactly their original one shell invocation, change their own RAM,
deliver a real watch event and ingest exit 37. Each real external model answer
acknowledges that environment's actual nonce and RAM hash.

Four optional gateway lifetimes, one arm and three finishes, all stop with
the same verified executable/birth/netns and exit code zero. No helper remains
at a capture. Later inference uses a newly attached gateway in the current
restored namespace. This establishes quiescent external inference, not the
restoration of an in-flight external TCP connection. The lifecycle runtime has
no provider dependency. The
[ordered fixture extension](../tests/compatibility/external-provider-gateway.patch)
keeps all existing assertions. Only explicitly external arm/finish calls get
90 seconds; all others retain 30 seconds and the whole lifecycle retains 420.

The native test takes 142.33 seconds. Provider exchanges take 3.189, 1.315,
12.274 and 9.842 seconds, all HTTP 200, with no relay retries. This is one
successful observation, not a distribution or production release. Exact
owned cleanup, unchanged frozen inputs and global sysctls also pass.
Portable checks with the new helper, IPC and fixture integration pass all six
gates in `factorybox-checks-cf337992003f4914aa890c5c94293e22`, including vet,
232 immutable reference files and both production fixture declarations.

Planned same-host, same-boot worker replacement also passes in
`factorybox-consumer-worker-loss-2be65549f1ad4d7caee70ea282f0b4a1`.
The first worker commits its pause and exits. A newly owned worker recovers
the original tool/session, removes the old saved controller only after verified
recovery, takes a new checkpoint and makes an independent fork. Replaying the
recovery request preserves the same target; deleting the recovered source
leaves its descendant useful. Both real tool results reach the external model
without another shell invocation. Three provider requests and three separately
closed gateway lifetimes pass. The native prepare/recovery tests take 58.68
and 65.33 seconds; outer setup adds about 8.9 seconds to the prepare probe.
Abrupt worker loss and injected provider faults are still separate gates.

The first abrupt-case attempt,
`factorybox-consumer-worker-loss-167e4e64219446bd9686be3a6cc5050d`, fails before
any checkpoint or intentional worker loss. Its first provider request is byte
identical to the two successful runs, including the direct `shell` tool and
system prompt. The real model instead invokes `execute` to search a separate
code-mode catalog, which has browser tools and no shell. It repeats catalog
searches rather than starting the requested held tool. A fourth inference
returns HTTP 429, with the provider body reporting an unavailable endpoint.
The arm deadline fails; an active request prevents a clean gateway shutdown,
so the fixture also refuses normal release. No capture proceeds. Exact owned
cleanup and unchanged global sysctls pass. The complete failure dump was first
preserved through lossless repack
`factorybox-storage-repack-cd42f368c0074adba8482dd250845de6`.
This attempt does not test abrupt worker recovery. The next independent
attempt clarifies the actual direct-shell versus code-mode distinction in
the synthetic prompt. It does not hide failed requests or relax the one-shell,
RAM, watch, result, deadline or gateway-exit assertions.

After the clarified-prompt proof below passes, storage retirement
`factorybox-storage-retirement-8ac75107b52a43b8ab04dbd47517e4b2` fully verifies
this superseded pre-tool failure's complete decoded stream and retains all
member metadata/hashes plus 1,990,546 bytes of selected diagnostics. Original
host provider exchanges, source, result and logs remain. It retires only the
785,014,225-byte repacked mutable-store archive; that complete old filesystem
is no longer reconstructable. The failure remains a failed attempt, and the
selected unresolved runtime failures are unaffected.

The independent clarified-prompt run
`factorybox-consumer-worker-loss-457fe987231b45e9b037625adb9c5def` passes the
actual abrupt case. The old worker receives SIGKILL only after its confirmed
pause and gateway release, and exits 137. Its separate IDE is already gone.
The replacement worker completes all original recovery, replay, post-recovery
checkpoint/fork, independent mutation, source-deletion and real-agent assertions.
The prepare probe takes 88.827 seconds before intentional loss; the recovery
probe takes 94.502 seconds. Three actual provider exchanges take 29.041,
14.008 and 23.401 seconds. Both workers, all helper processes and owned volumes
are removed. This remains same-host, same-boot recovery from the complete store.

The completed-response fault passes its complete ordinary lifecycle in
`factorybox-consumer-worker-loss-236f0b01d84a41bf8828a43ac3896fb3`.
After the source's actual shell result reaches the provider, the relay buffers
its HTTP-200 completion and closes the client transport without delivering it.
OpenCode retries inference with the exact same request bytes and session.
The relay performs no inference retry. The next response is delivered; the
agent ingests the actual nonce, changed RAM hash and exit 37. All three
independent environments retain exactly one shell invocation. All original
checkpoint/fork/fork-of-fork/pause/RAM-release/resume/fork-of-resumed and separate
Theia assertions pass. The host verifies that the lost exchange contained the
completed source tool's actual nonce and RAM hash. Later branch completions also
pass. This is one concrete lost-completion case, not an exactly-once guarantee
for arbitrary remote tool side effects or in-flight TCP restoration.

The real Unix-HTTP regression retains the absent-fault red check
`factorybox-checks-abf89540117042558abb4df32e58932e`. A subsequent test helper
mistake, treating HTTPConnection as a context manager, remains in
`factorybox-checks-bec1b3e1ebd346cb8181f76ef0b23429`. Correct bounded closure
then passes all six portable gates in
`factorybox-checks-627156309b59468b821255c2fd01d489`. Its transport regression
checks the dropped response after actual synthetic upstream completion,
successful delivery before/afterward, exactly three upstream calls for three
client requests and unchanged replay bytes.

## Fresh start, first bounded consumer patch

The original Dev Containers **0.89.0** feature-control function fetches an
external catalog even for an empty feature list. In the recorded offline
Factory example, its failed fetch appears about five seconds after
`Resolving Remote` begins. This is a measured elapsed window, not a separate
instrumented HTTP duration.

[Ordered patch 1](../tests/consumer-optimizations/0001-devcontainer-empty-features.json)
adds an early return only when the union of configured and additional
features is empty. Original bundled CLI SHA-256 is
`22b5b3a7345608f552db2f1af589ca06c7274d116778d00aa67cbfe1e1cafc65`;
patched SHA-256 is
`891e207b41c10a52d112cb16d6cf4cbf4ed587db42e87b92befecac86471d233`.
It changes only a newly unpacked owned consumer tree, before immutable-base
preparation. Frozen consumer images/tars and source examples stay intact.

`factorybox-devcontainer-feature-probe-143e65d62c654486963b18117a23fd35`
executes the actual original and patched feature-control/matching functions.
All nine cases pass: absent/empty, configured/additional features, allowed and
blocked identities, prefix boundaries and stopping an existing container
after a blocked feature. Every nonempty case retains the same effects and
original rejection data. This is a bounded function test; the complete
native examples below separately verify the consumer integration.

| Complete ordinary lifecycle | Control | Candidate |
| --- | ---: | ---: |
| Factory first useful work from prepared base, corrected authority | 23.771 s | 18.895 s |
| Factory cold rootfs/base preparation, separate | 26.106 s | 24.428 s |
| Minimal first useful work from prepared base | 9.082 s | 3.291 s |
| Minimal cold rootfs/base preparation, separate | 25.976 s | 24.010 s |

These are **one observation per configuration/example**, sequential on this
host with the same C9 worker/components, Overlay2 and example-specific inputs.
Factory includes separate bounded Theia and the deterministic-model running
agent tool. Both paths retain all eight useful-work phases, complete private
RAM/filesystem assertions, pause/resume, descendant tests and exact cleanup.
There is no p95 estimate or performance release from this small sample.
The earlier, uncorrected authority measured 33.903 s and 25.115 s. Its total
difference also includes an approximately four-second image phase difference;
do not attribute all 8.8 seconds to the catalog patch. Some portable checks/
feature diagnostics overlapped that first candidate's later lifecycle phases,
not its fresh-start interval. Those early observations remain retained.
A formal comparison
must retain host load and all observations in a predetermined order.

| Case | Exact native artifact |
| --- | --- |
| Factory candidate, corrected authority | `factorybox-consumer-worker-loss-2a7e69735d6449adbce8c9a605d8e246` |
| Factory control, corrected authority | `factorybox-consumer-worker-loss-dc20bdf4d6d64c2b9044e946ae6da637` |
| Factory candidate, earlier authority | `factorybox-consumer-worker-loss-f675ac682e724398b1251f63d23cafde` |
| Factory control, earlier authority | `factorybox-consumer-worker-loss-319a94527b5a465986aff66e6137a475` |
| Minimal candidate | `factorybox-consumer-worker-loss-222ae4ff938f448ca3dfe89f5710059c` |
| Minimal control | `factorybox-consumer-worker-loss-6eea7959891b4d748a540b9565a3dd02` |

The candidate's copied setup wrapper has before/after hashes and an actual
patch receipt in each artifact. Its immutable prepared-base digest covers
the patched consumer. `consumer_start_profile.py` extracts retained connector
and nested Dev Containers spans without summing overlapping intervals.
Factory profiles `546ccb043e0b4deb8bb1b1444b90b5f8`,
`10c20062a8d5466c8dc9dd8cd46c1486` and
`eaa16907322c489fbf54ebbbad2c771e` retain original/control/candidate analyses.

## Fixture timing correction

The separate diagnostic
`factorybox-consumer-worker-loss-f3e29eaee4db4416b57eb3b9f949e752` passes its
complete lifecycle and proves why the previous Factory authority added ten
seconds. The app module loads at 06:53:25.719. The first proxy request was
sent slightly earlier at 06:53:25.536 and received an immediate Connector
`stream-error` at 06:53:25.537 because the app port was not ready. The proof
authority handled only `error`; it ignored the terminal `stream-error` and
waited until 06:53:35.533 for its ten-second deadline. The next read succeeds.
The dependency fetch itself takes about 20 ms. The earlier diagnostic
`128ba2c0144649788b0879825c528b7e` also completes and measures about 19 ms.
Neither changes the application's dependency URL, two-second timeout or errors.

The added websocket regression first fails against the original authority:
`factorybox-stream-error-regression-7fbd4a20583e4911af565fbb88f170a4` reports a
deadline instead of the actual terminal stream error. The one-line ordered
[fixture patch](../tests/compatibility/actor-stream-error.patch) adds the real
terminal frame to the existing error case. The
[production fixture declaration](../tests/production-fixture-extensions.json)
binds exact original/patched hashes, previous manifest and frozen original
bytes. Authentication, readiness, full response/JSON checks, mutation refusal,
resource/deadline assertions and vet remain. The new regression also retains
the legacy error and successful response paths. Both complete Factory variants
pass afterward; their corrected first useful-work values appear above.

This is a measurement correction, not a production runtime optimization.
Historical native results and profiles are unchanged. Do not subtract a
constant from every old sample or mix authorities in a new comparison.
`benchmark_consumer_start.py` fixes twenty new full lifecycles in an interleaved
order, five per configuration/example, with the corrected authority in both.
Its predeclared selection requires at least 20% and one second prepared-start
reduction in each example, and other lifecycle medians within the existing
max(20%, 250 ms) regression limit. All preparation, memory, failures and host
load remain in the report; no p95 is estimated.

The first timing collector itself failed because it read a mode-0600 native
response through the unprivileged host user. That failed verdict is
`factorybox-consumer-worker-loss-0a603d76dece4c3ca95296e367dc5c92`. Reading inside
the exact owned worker fixes it, as the complete replacement `128ba2c...`
proves. Its superseded 1.45-GB compressed volume was retired only after full
4.23-GB decompression verification, with every member hash and selected
diagnostics preserved in
`factorybox-storage-retirement-eddb7b96a8724b6a8572babc26969e6c`.
The unresolved historical Sysbox-fs startup failure remains intact.

## Repeating the checks

On this host, all commands use `mise exec --`. Native tools need
`FACTORYBOX_DOCKER_CONTEXT=default FACTORYBOX_DOCKER_ELEVATION=sudo` in their
environment. They create a new guarded artifact, own unique resources and
retain input hashes. Do not substitute host-installed OpenCode or mutate the
frozen component inputs.

- `python3 tools/check.py` runs Go checks with vet, the tagged consumer build,
  Python checks, reference and fixture verification, and diff whitespace checks.
- `python3 tools/external_opencode_probe.py --consumer-inputs factorybox-devx-inputs-e67b951171c5481982908b2603d42f82`
  repeats the external tool proof with current free-price admission.
- `python3 tools/devcontainer_feature_probe.py --consumer-inputs factorybox-devx-inputs-e67b951171c5481982908b2603d42f82 --source-artifact factorybox-devcontainer-empty-features-ca58eb5bb7db45069f2f6eda3def5057`
  repeats the actual feature-control function tests.
- `tools/consumer_worker_loss.py --consumer-empty-features` adds the candidate
  patch only to a complete ordinary lifecycle. Existing mandatory arguments
  still identify the exact worker, consumers and project images.

Portable checks `factorybox-checks-8fb498c76c2c4aad9f2d9faaaee0d6ec` pass all six
gates on that revision, including 232 immutable reference files. Later new
checks also pass in `factorybox-checks-4c691babb1a4402584e07f858bfb5621`, including
the actual websocket regression and the original fixture hashes. Later new
changes still need a final fresh receipt.
The complete current portable revision passes in
`factorybox-checks-f92f4de6ea934022b2a6bf0909073299`. The first implementation
milestone is committed locally as `37389ef`, with the existing Go patch-context
whitespace rule extended in `bb35414`. No public push or image publication was
performed.

The twenty-run comparison stopped with a failed fifteenth native run in
`factorybox-consumer-start-comparison-88e95d80871c44afb590c575a76ac154`.
Fourteen complete lifecycles pass. The next reference Factory run,
`factorybox-consumer-worker-loss-0977945385bd4c528164eaf2dc4fa86a`, fails
restoring the fork of its resumed branch. Its preserved CRIU log reports
`Can't fork for 4463: File exists`, followed by `Restoring FAILED`.
The restore-listener connection error in the public CLI is a later symptom.
The worker and separate Theia were cleaned up with exact ownership checks;
global sysctls are unchanged. The complete failed volume remains recoverable
through its verified encoding receipt. This is an unresolved lifecycle
failure, not a successful performance qualification. The fixed matrix is
retained without replacing its failed row or omitting it from the verdict.

Lossless repack `factorybox-storage-repack-a730ec1b25f247d5b35d8ba4e0851609`
verifies all 12,155,092,992 decoded bytes against the original hash before
replacing its compressed representation, reclaiming 2,625,294,484 bytes.
Diagnostic inspection `factorybox-failure-inspection-781ddb347ace431a8bf6e3d5506d3cbc`
again verifies the complete decoded payload and retains only the requested
restore log and process-tree/core/identity images as bounded hashed files.
No archive member path is extracted. The saved process 4463 has own-namespace
PID 2, under nested init 1328. The conflicting occupant is not yet identified.

## Read-only restore collision diagnosis

The optional ordered
[CRIU diagnostic patch](../upstream/patches/criu-pid-collision-diagnostics-candidate.patch)
logs only after a nested `clone3` returns `EEXIST`. It records requested IDs,
bounded current/visible process status and, if available, the occupant's pidfd
metadata. It opens no writable proc files, sends no signal, changes no PID
counter, retries nothing and restores the original errno. It is a diagnostic,
not a repair or selected optimization. The existing builder extension is
declared with exact original/patched hashes and preserves its default path.

Native build `factorybox-build-criu-retained-251286b9d2a744c28da64ae6a41817c1`
passes with binary SHA-256
`45ba3635a5f4da5becb3c49298e00e896d6c5366d1ca3be53e813d8306b4879c`
and 1,030 unchanged upstream test files. This counts source preservation,
not execution of those suites. Its new unqualified worker is
`factorybox-build-worker-a3d8b92f5ded4b719d088e6fd6894570`, image
`sha256:4ad5d1deceb2cb4b25a5f7858334eff2f9bf8806755dac39830cd5b66164061c`.
The Go CLI remains byte-identical to C9. No reference profile changes.

Three fixed full Factory lifecycles pass in
`factorybox-restore-collision-probe-fde61870ac1b4ad090e62dda903a636f`.
Its source/fixture hashes remain frozen across all cases. Results are
`factorybox-consumer-worker-loss-0bcc296d87c44accb17c20719d6f3a62`,
`factorybox-consumer-worker-loss-58c53118245b4bcfb959b3d0a3c0837d` and
`factorybox-consumer-worker-loss-69140a9ec0744b31aee79923ca29cc5d`.
The collision does not recur, so no conflicting occupant is identified and
no repair is claimed. The original failure and its complete dump stay open.
Portable checks, fixture declarations, vet and reference preservation pass in
`factorybox-checks-d21fe8ad17e746c8823f027e2dc65853`.

The separate preserved Sysbox-fs startup failure is still unexplained. Its
saved process listing shows filesystem PID 37 in `do_wait` after the `FUSE dir`
log, while manager readiness passes. It lacks the child's identity/stack.
That narrows the observation to a child wait; it does not establish which
startup probe hung or justify selecting the rejected SHM change.

The existing startup diagnostic now retains its original sixteen starts,
60-second readiness limit, 420-second outer deadline and failure inspection.
Its ordered fixture extension adds explicit native AMD64 admission, the
selected manager's disabled-binfmt option, optional process-local GC settings
and a further unchanged-global-sysctls assertion. It changes no SHM code.
All sixteen `GOGC=100` starts pass in
`factorybox-startup-manager-5a2fca19ae614e29987d45ff3dc71c71`; all sixteen
`GOGC=1` starts pass in
`factorybox-startup-manager-d8cc249e1c9b45d7bb1e365618cdc2c3`.
Exact input preservation, native resource limits and owned cleanup pass in both.
These are daemon-only observations without a sandbox/rootfs workload, not
application startup measurements or an explanation of the old timeout.
Portable checks pass in `factorybox-checks-d0ed9c93b605423ca1f23bd482ec0b9c`.

## Worker startup failure records

The new Go worker writes a private, exclusive epoch `startup-failure.json`
before stopping its owned services when their startup fails. Collection checks
saved process births and ancestry, opens process directories to avoid PID reuse,
and records procfs status, wait channel, syscall, stack and per-thread children.
It reads no command line, environment, process memory or application files.
Limits are 32 process attempts, 128 tasks and 256 KiB of procfs data. A two-second
scan budget is checked between reads; it is not an interruptible kernel-read
deadline. Missing/forbidden entries are explicit. Failure to persist diagnostics
does not mask the original error or authorize readiness. No new signal, retry,
SHM change or relaxed 60-second readiness limit is introduced.

Candidate build `factorybox-build-worker-0e84fd1c3a86433c9b3587884fcb2721` passes,
with CLI SHA-256
`020d26973cf2ef924102973b00f1dd7f18fbbdb192cc6559ba9660cfd84c4575`,
image `sha256:7a4fb100d9a26a2ccc43753decce7d057eba8c4cb2124920e7307b576aff7683`
and package SHA-256
`a6b9654fdda33c3629f1c8e837e0fc45f2cbb72bca8a4b3ba73a156587d4cce5`.
It retains the unselected, read-only CRIU collision diagnostic above. This is
the first Go implementation change in this production programme. The frozen
pilot profile and selected C9 package remain unchanged.

Native fault proof
`factorybox-worker-startup-failure-695881d466f64f06b56d5bf1e0220d77` passes
both a deliberately blocked filesystem service with a real child in `waitpid`
and an early service exit. It runs the delivered Go binary with explicitly
replaced private fixture components, leaving its delivered bundle untouched.
Both cases exit with the original error and no readiness publication. The
timeout report contains its live waiting parent and child before cleanup.
Its scan takes 2.3 ms and retains 11.2 KiB of procfs data. This proves the
recording path, not the cause of the historical real Sysbox-fs timeout.
Exact cleanup, frozen inputs, resource ceilings and unchanged global sysctls
pass. The portable regression also captures a real waiting parent/child,
rejects a wrong birth and expired/byte-limited scans, and refuses to overwrite
the first report.

The final archive-first startup probe also passes both faults in
`factorybox-worker-startup-failure-3a6238ebbbb94b5eafc212121cc2a077`.
It waits for the post-delegation service log through Docker's file API before
attempting its early, rejected status command. Time to worker exit is 60.811 s
for the blocked start and 2.686 s for the deliberate service exit. These are
fault-handling timings, not application start benchmarks. Final portable checks
pass in `factorybox-checks-f138bef4819f47c8a40c363caffcf432`.

Development fixture failures remain visible. Attempt
`factorybox-worker-startup-failure-6090dabe2f1244ae9c023d6c99a88bc1` exits in
Python setup before Go starts because the native base lacks `hashlib.file_digest`.
The replacement uses bounded streaming hashing. Attempt
`factorybox-worker-startup-failure-330477e1ec434264999f2ee3e809330b` records the
parent and child correctly, but its shell waits through `sigsuspend`, failing
the diagnostic test's specific `do_wait` assertion. Its complete failed store
is retained and verified. The next fixture creates a direct `waitpid` parent;
the assertion remains. These are fixture errors, not repaired runtime failures.

Full Factory lifecycle
`factorybox-consumer-worker-loss-02c19de9490241cbafae7b6abc75194d` also passes
on this new binary with deterministic OpenCode, private Docker/Compose/Dev
Containers and separate Theia. All original capture/fork/fork-of-fork/pause/
RAM-release/resume/fork-of-resumed, independent tool/watch results and cleanup
assertions hold. The native test takes 113.58 s; the outer probe takes 123.10 s.
This is one regression observation, not a new platform,
upstream-suite qualification or performance selection. Portable checks pass in
`factorybox-checks-17f2ba3d7afc4179b9fcdf29d1cc4c02` with vet, 232 unchanged
reference files and all four declared production fixture extensions.

## Capacity admission and rollback space

The candidate adds an administrative `capacity` command. It reads `statfs`
through the anchored controller directory and verifies its device, inode and
mount identity against the current path. It reports total/free/available bytes,
the observation time and admission decision. Invalid counters, overflow and
changed roots refuse new writes. These are whole-filesystem counters, not
per-store usage. The local worker publishes and verifies a fixed 12-GiB free
floor plus 4-GiB write headroom. This is a preflight check, not a reservation,
hard quota or protection from concurrent guest/other-worker writes.

New start, base import, capture/pause, fork, resume and recovery backing paths
check available capacity before new writes. Completed replay, state inspection,
capacity observation and deletion remain possible below the floor. Explicit
standalone controllers can supply a positive floor/reserve pair; historical
fixtures with both values zero retain their original behavior. The validated
worker path supplies its required local policy. Native tests retain all old
assertions and use separate new fixtures.

The first pressure attempt
`factorybox-storage-admission-72ca4897635c48dda5a1fe827c287ea0` confirms that a
RAM-backed tmpfs cannot satisfy the existing file-backed snapshot-RAM release
check. Its attempted pause is refused and its complete failed store is retained.
The new test separates real file-backed pause/resume from bounded tmpfs pressure;
it does not weaken that release check or qualify tmpfs as production pause storage.

The next attempt
`factorybox-storage-admission-0836dc5c079f48b68ddbcfb0f2c41f4f` finds a real
failure after successful admission. Copying a 160-MiB nonzero source file into
the private 128-MiB filesystem produces actual `ENOSPC`. The rollback cannot
write its receipts and the source remains frozen. The test fails its unchanged
running-source assertion. Logs, frozen inputs and the complete failed store are
retained. A passing preflight check alone did not prevent this failure.

The correction allocates 8 MiB of actual private blocks for each capture before
freezing. On a capture-worker failure it releases only that saved inode, then
performs the original validated rollback and receipt checks. Normal completion
also releases the reserve. The zero-length inode and original receipt remain
for exact replay. Device, inode, owner, mode, link count, size and guard request
must match; another guard, a substituted file or a shared inode is rejected.
No failed checkpoint payload or old generation is removed by this mechanism.
It does not bypass network, tracer, process-identity or thaw authorization.

Candidate `factorybox-build-worker-c45d9c2ed6a44c41bd642a0f01728a59` passes its
build with CLI SHA-256
`b93ce974052824e3f7ae712b509da617a9887b1e53dde2211d28e8106a956b9b`, image
`sha256:1ffb5060cac4b6fe51dad1b980be9224cdfcb886aae544bab9dda5c3dde5910c`
and package SHA-256
`a7826b67c2e43d49db2d8bb6a0a70311e00ae820cb975ac10990ed33f038c0b0`.
It retains the same Sysbox and diagnostic CRIU inputs. This is unselected.

All three native storage tests pass in
`factorybox-storage-admission-1f877b3686094956b947a5dd2b8afed5`:
five real low-space refusals before new backing, exact completed checkpoint
replay below the floor and actual post-admission `ENOSPC`; a separate real
file-backed pause with rejected resume leaving its record byte-identical,
followed by successful resume under the restored policy; and refusal to
truncate a foreign replacement/shared inode or release another guard's reserve,
with exact release replay succeeding. Test times are 1.83 s, 1.78 s and 0.03 s.
The failed capture returns failure, restores its original source to running,
retains its original private RAM/file/watch state and verifies the complete
older checkpoint. Its partial new capture remains retained. The 128-MiB
pressure filesystem is enlarged to 144 MiB only for subsequent fixture cleanup,
after the recovery assertions. The host disk is not filled. Delivered CLI
capacity reports its required policy; exact cleanup, inputs and unchanged global
sysctls pass. This concrete rollback proof is not general disk-failure qualification.

Portable checks pass in `factorybox-checks-f4a369f161354a36a7b4af64ce59bcc1`.
Both complete consumer regressions pass on these bytes: Factory with separate
Theia and deterministic OpenCode in
`factorybox-consumer-worker-loss-bd5b62b47eea4657bac6a18517e70415`, native
116.46 s and outer probe 125.097 s; minimal in
`factorybox-consumer-worker-loss-522793c807c843fc85270db15e51bee1`, native
39.76 s and outer probe 48.396 s. These are complete-test durations, not
individual application start times or a performance comparison.

Native guard regression passes one test and all twelve subtests in
`factorybox-runtime-3ef96769c13f4ac6a9c0b72f60bc60cf`. Native capture passes
twelve tests and 78 subtests in
`factorybox-runtime-c697bf5c37324a9d931540364e2ae979`. Neither has a skip.
Their parent `factorybox-pilot-native-gates-5379a6d71df94c7f8e7000cd2eceb294`
then stops before the pause suite can allocate an artifact: completed evidence
has crossed the required eight-GiB starting-headroom threshold. The parent is
failed, pause is unrun there and admission is not attempted. No native pause
assertion failed in that attempt. The missing suites need a separate result
after maintenance, not omission from the original failed parent.

The subsequent parent
`factorybox-pilot-native-gates-877a7bb5d1d1484596f9ee863db68818` completes the
pause suite: twelve tests, 67 subtests and no skips in
`factorybox-runtime-d9b30d20509349e89446b38599c7a803`. Its admission suite
then fails in `factorybox-runtime-8829166825b0476888776bdfd2c7d5ae`.
`TestProcDirectoryAdmissionNative/transient-foreign-proc-directory` passes its
original RAM, rejection/settled-capture and independent-fork assertions, but
the original test-overlay cleanup returns `EBUSY`. Sixteen top-level tests and
77 subtests pass; the failed assertion is preserved, with no skip. Its complete
captured source archive has original SHA-256
`74c2866160dd59fccfc0c01554d564fa511584d8b65d95a9463a6bdf1ca2140b`.
Outer exact container/volume cleanup passes. This does not explain the busy
mount, qualify admission or permit an assertion change. It is a new open
release blocker; the passing consumer/pressure suites do not override it.

Artifact maintenance preserves all failed stores. Two closed Bats dumps are
losslessly re-encoded by
`factorybox-storage-repack-71a5ac7d952047cb820bbe3c3e21699d` and
`factorybox-storage-repack-fcd4ffcef96e47a3933d7d6e5a9b82f8`, each verifying all
718,274,560 raw bytes. Closed passing consumer test executables also receive
whole-decompression checks and individual encoding/reconstruction receipts.
The old failed admission candidate's optional compiler cache is retired only
after the passing replacement, with exact source/package/component checks, in
`factorybox-storage-retire-cache-e408a4fe30f9489290aeee1ccc896b45`.
Frozen sources, results, logs, runnable component bundles and failed payloads
remain preserved; the historical experiment is untouched.
The new candidate's cache is retired after its full Factory pass in
`factorybox-storage-retire-cache-3f24a5de5fe744d8862e649754a00018`. Its closed
capture dump is losslessly repacked in
`factorybox-storage-repack-9b0bf9c906d6431bb70ded90739e2622`, with all
218,103,808 raw bytes verified. The compactor now also covers large generated
consumer test/actor executables after exact cleanup. Each retains its own
encoding, original hash, whole-decompression check and reconstruction command.
Frozen runnable component bundles and original consumer image inputs remain
directly available. Nested dump repacking checks the containing run's result;
a regression refuses a nested frozen-build input before any decoder starts.

The original immediate failed unmount now also records bounded, read-only
process/thread/descriptor/mount metadata from that fixture's private PID
namespace before outer shutdown. It does not retry the unmount, signal a
process, use lazy/forced unmount or change the failed assertion. The ordered
fixture patch declares the exact old/new engine-test hashes. The hash verifier
also checks that a production extension of an already extended fixture starts
at the previous final hash; its regression refuses a forged discontinuity.
Portable checks pass in `factorybox-checks-6c00580d8bd241bca81c8d93282d0ddf`.
The earlier checker failure
`factorybox-checks-ed2f5d3cee5349d8b679de8f8dec6faf` remains retained.

Three predetermined complete admission observations pass in
`factorybox-admission-cleanup-probe-1fbe8b233b414064a614899616caf253`.
Each passes seventeen top-level tests and 78 subtests, without skips, using the
same candidate core and pinned components. Their child native gate parents are
`factorybox-pilot-native-gates-bb6bc1478d0142a6a16966a3e27fb9b9`,
`factorybox-pilot-native-gates-c0d70f6342a24c97816dcf54afdb0c9e` and
`factorybox-pilot-native-gates-9f8a27e3656a4735a8caa98a7afac577`.
No busy-overlay report is triggered. This does not explain or fix the original
failure. A prior three-observation attempt,
`factorybox-admission-cleanup-probe-037e46dde1214f17a68900843e527ae7`, passes
only its first native suite before the next run is refused by the eight-GiB
starting-headroom guard. Its parent remains failed, with two observations unrun.

The separately labelled targeted diagnostic
`factorybox-proc-cleanup-diagnostic-c6906b410dc94a3083bc169bd8eb3abd` then
passes twenty fixed repetitions and all forty original held/transient proc
subtests. It reconstructs the already verified native test executable with the
same SHA-256, retaining the original setup and assertions; only an explicitly
recorded invocation adapter selects the two subtests with count twenty and
stop-on-first-failure. The native container runs for 158.21 s. The original
immediate overlay unmount is exercised every time. Exact cleanup, unchanged
frozen inputs and unchanged global sysctls pass. No busy-overlay report is
triggered. All 3,181,379,584 archived source bytes receive whole-decompression
verification before the raw archive is removed. Neither this targeted result
nor the three full admission passes resolves the original failure.

The current diagnostic CRIU bytes also pass the eighteen declared unchanged
core C cases, with two dump/restore cycles per case, in
`factorybox-criu-core-7dab24bfa7004ec38f86676f16d89de3`, parent
`factorybox-pilot-native-gates-6e621512540845329b36b022b919982d`.
Cleanup passes and all 1,030 upstream test files remain unchanged. Eighteen
executed cases are not a claim to have run the complete ZDTM matrix. Portable
checks subsequently pass in
`factorybox-checks-fa334ce7145b4d01b09d0b60bf57eb70`.

Maintenance `factorybox-storage-share-encoded-c36ffd65a71a4a51b7edca8a879e0888`
shares byte-identical compressed, closed passing consumer-test inputs through
hardlinks, reclaiming 764,334,080 allocated bytes. Original raw SHA-256,
complete decoded verification, encoded bytes/hash/mode/ownership and both
receipts must match before each link. Source, results, logs, failed stores and
each reconstruction receipt remain intact; linked archives must never be
rewritten in place. The initial attempt
`factorybox-storage-share-encoded-9d467e2ae1aa4618bb86113fe05b48d2` takes no
action because hashing changes access time; the subsequent verifier binds the
existing immutable identity fields instead. The two explicitly qualified old
v0.1 builds also retire only their optional compiler caches in
`factorybox-storage-retire-cache-6c6410e237e14979a1d995cfe317d441` and
`factorybox-storage-retire-cache-7273f92359774891b6637a774fb02b19`, with their
exact native package/source/component qualification rechecked before and after.
No historical executable is reconstructed or historical archive expanded.
Maintenance `factorybox-storage-share-native-25b839abf5104917add9651a20f3c082`
also shares 73 verified identical native-test inputs of closed passing runs,
with per-input durable intent/completion receipts. It excludes the source of
the still-active targeted diagnostic. Frozen bytes, executable modes, ownership,
sources, native results and reconstruction receipts remain exact. Its observed
whole-artifact allocation delta overlaps the diagnostic's archive compaction;
that delta is not an isolated measurement of reclaimed storage.

## Controller recovery with zero free snapshot space

Two native attempts on candidate `factorybox-build-worker-c45d9c2ed6a44c41bd642a0f01728a59`
confirm another real failure:
`factorybox-storage-admission-a5c1872a381046948e1a6daa4163b642` and
`factorybox-storage-admission-bbda3a4ab636490cb0d12d4dacf75b78`.
After successful CRIU, the test observes the exact capture worker's GNU copy
child and stops that pidfd-pinned child. It stops the exact controller, kills
the capture worker and verifies that its copy child exits with it. It allocates
all remaining blocks on the private 128-MiB tmpfs. Actual capacity is zero and
a separate write returns `ENOSPC`. The second attempt additionally requires
every controller thread to be stopped, the original source still frozen and
the original eight-MiB reserve still allocated before continuing the controller.
The controller cannot write its abort/outcome receipts and leaves its original
source frozen. The checkpoint case fails after 2.27 s and 2.34 s; the pause case
is unrun in those failed attempts. Complete failed stores and logs are retained.

The correction releases the exact private reserve at the beginning of failed
controller capture cleanup, before the existing validated abort/recovery
protocol writes its receipts. The killed worker cannot run its own release
defer. The existing guard, tracer, cgroup, source-birth and receipt requirements
are preserved; there is no diagnostic thaw, retry or snapshot removal.

Candidate `factorybox-build-worker-5ada8cb272494383aca6b95cad7f4d30` builds with
CLI SHA-256 `d3d2f5566dc0b9c0815e434eddf2cc7faad41928b1fdce35038d2ef85239b53e`,
image `sha256:cb8dff98c26f3060848d8b33b13c571e65577bdf04970db2ae7cc78d98ee9cab`
and package SHA-256
`ddc1c71779ee3b5a024c6098e4ae88e5efba96321213d9e201621cf646fd8b05`.
It uses the same pinned Sysbox and diagnostic CRIU components and remains unselected.

The unchanged strengthened fault assertions pass for checkpoint and pause in
`factorybox-storage-admission-e4eec8eba2d34cbaaa49aad37bafa542`, 3.04 s and
3.22 s respectively. Before removing the owned pressure file, each failed
operation restores the original PID/creation identity to running, preserves
its private RAM/files/watch state and permits useful work. Its earlier complete
checkpoint verifies byte-for-byte. After pressure is cleared to admit new
backing, that older checkpoint produces a second independent useful sandbox.
No incomplete checkpoint or killed-worker outcome is published; failed pause
history remains exact. Cleanup, frozen inputs and global sysctls pass.

The previous complete storage suite also passes on this candidate in
`factorybox-storage-admission-de8cde98038c4831a471cf85447e0e99`: actual copy
`ENOSPC` and all five preflight refusals, real file-backed pause/resume admission,
and foreign/shared/wrong-guard reserve refusal plus exact release replay.
Test times are 2.48 s, 2.16 s and 0.03 s. These two pressure scenarios do not
qualify arbitrary simultaneous writers, controller death or every metadata
publication failure.

Both complete storage suites are now explicit default native gates. The old
base lifecycle invocation names its same six original tests exactly, instead
of also selecting the new pressure tests through their shared name prefix in
a fixture without their required setup. Its ordered patch records the baseline
and final hashes, preserves every original assertion and retains the expected
six tests and zero skips. The new pressure suites keep their own delivered-worker
setup and required proof checks; they are not counted as passing by exclusion
from the old fixture.

Both complete examples pass on these new bytes: Factory with separate Theia,
Docker/Compose/Dev Containers and deterministic OpenCode in
`factorybox-consumer-worker-loss-22b67525b1284ca0ae9207b7ce94d011`, native
115.36 s; minimal in
`factorybox-consumer-worker-loss-9847ab09fef14ac5a76b9bf36b9ddb98`, native
40.14 s. These are whole-test durations, not a startup comparison. The new
build retires only its optional compiler cache after that full Factory proof in
`factorybox-storage-retire-cache-79904133070c4cf88dbc4b7e7221cd72`.

Parent `factorybox-pilot-native-gates-0d1a02c6201a4f41a9125121db2422ba`
passes the six original base lifecycle tests and three subtests in
`factorybox-runtime-be7df28bd0c147e98558c5e9d361b190`, guard one/twelve in
`factorybox-runtime-57810c6b639841e6b46650242a22ec72`, capture twelve/78 in
`factorybox-runtime-00530f1b177848909efcbf7f145ba331` and pause twelve/67 in
`factorybox-runtime-1534cd5f9b2e44de821bec0cd0c3d9d3`. Each has zero skips.
It then stops before admission can allocate its artifact, because retained
evidence uses 24.10 GiB and the next native run must retain eight GiB of
headroom inside the 32-GiB budget. No admission assertion runs there;
worker-loss is unrun in that parent. Its failed result is preserved.

Maintenance `factorybox-storage-repack-ab4a1b86c515441890c7863fccad6ca2`
losslessly re-encodes the closed passing admission archive, verifying all
801,112,064 raw bytes and reclaiming 14,482,608 encoded bytes. The exact
original decoded hash and reconstruction pointer remain available. Native
component-input sharing first refuses a writable build-linked input without
changing any files in
`factorybox-storage-share-components-c40d48237f3b4588b413df0268622849`.
The next operation skips those already shared writable inputs and shares only
verified read-only copies of closed passing native fixture components in
`factorybox-storage-share-components-d435c186601c49be98b0d57912bd3edc`.
Its 39 per-input intent/completion receipts preserve bytes, mode, ownership and
native results, reclaiming 920,551,424 allocated bytes. No build bundle,
failed/active input, snapshot or guest payload changes. Duplicate inode
timestamps can become the canonical inode's timestamps. This is evidence
storage maintenance, not a runtime memory or startup optimization.

The separate parent
`factorybox-pilot-native-gates-881c109114c94c7a893cdf132a11e481` then passes
the previously unrun admission and capture-worker-loss suites on the same
candidate core: seventeen/78 in
`factorybox-runtime-ca0a6ea34f8648d1988899d5d8227397` and twelve/64 in
`factorybox-runtime-06a1d81fb7aa4162a33ef0ed311299fe`, each without skips.
This includes the original live-worker/forged-receipt refusals and failed-pause
reconciliation assertions. Portable checks pass in
`factorybox-checks-30bfc8a5961144b3bedefddc648ae52a`. The earlier headroom-stopped
parent remains failed; the original busy-overlay, nested PID collision and
historical startup timeout remain unresolved.

## Repeated-run inputs within the evidence budget

`factorybox-build-consumer-tests-1f25fb5df92841db81e7abc881d33fda` freezes the
exact five Go helper executables already used by the complete passing Factory
proof `factorybox-consumer-worker-loss-22b67525b1284ca0ae9207b7ce94d011`.
Compacted helpers are reconstructed with their complete original decoded
hashes; the original encodings and proofs stay intact. The new completed input
build keeps these copies read-only and binds their hashes to that native proof,
the exact worker ID and the complete Go core/test source manifest. A later run
may share them through verified immutable hardlinks only when that whole source
manifest still matches. Changed assertions require freshly compiled inputs.
There is no application state, prepared workload, checkpoint or base reuse.

Portable checks pass in `factorybox-checks-eaddde8f595942749d399a4ef31279fc`.
Negative file-based regressions reject changed test source even with unchanged
runtime code, changed/writable/redirected helper files, another worker, an
incomplete producer proof and a changed original result receipt. Vet and every
original native assertion remain enabled. The default consumer runner still
compiles its inputs; reuse requires the explicit `--probe-input-build` option.

Both complete examples pass through this input path: Factory in
`factorybox-consumer-worker-loss-c22671efce1249b8a67d85b1bb5eabec`, native
114.35 s and outer probe 121.633 s; minimal in
`factorybox-consumer-worker-loss-8628cc42d11a486386bce08fe089f4b0`.
Each checks its complete lifecycle with fresh owned containers/volumes and
exact cleanup. Host-side test-helper preparation is recorded separately from
workload preparation and native lifecycle timings. This is test storage and
preparation reuse, not a measured runtime optimization or production selection.

## Twenty complete Factory observations on the current candidate

`factorybox-factory-repeat-diagnostic-cea5bb016a7f45b5943e5d8f53bc6ce5`
passes all twenty predetermined serial cases on worker build `5ada8cb2…`.
Every case starts fresh owned storage and checks the complete original Factory
lifecycle with separate bounded Theia, private Docker/Compose/Dev Containers and
the actual OpenCode executable using the deterministic local model. Only the
previously native-tested Go helper inputs are shared. No prepared workload or
application base is reused. Programme inputs stay frozen throughout; the
original 420-second deadline, two-sandbox maximum and resource limits remain.

The separately hashed analysis
`factorybox-factory-repeat-analysis-ebe551acb76a4f02be8836bc2c4710e9`
rechecks all twenty original result receipts and selected underlying output
files. These are descriptive observations on this shared host, without a
matched optimization control, p95 estimate, latency guarantee or production
selection:

| Recorded boundary | Median seconds | Observed minimum–maximum seconds |
| --- | ---: | ---: |
| Worker readiness | 0.365 | 0.340–0.570 |
| Fresh rootfs and verified base preparation | 24.059 | 21.246–35.829 |
| Prepared start to first app response | 23.612 | 20.001–24.937 |
| Prepared start to separate Theia readiness | 23.665 | 20.049–24.998 |
| Source checkpoint command | 10.116 | 7.814–10.985 |
| Fork to first app response | 8.341 | 7.551–9.167 |
| Fork of fork to first app response | 8.411 | 7.945–9.772 |
| Pause command | 9.590 | 8.857–10.835 |
| Resume to first app response | 3.916 | 3.407–4.524 |
| Fork of resumed source to first app response | 8.762 | 7.604–9.583 |
| Complete native case | 116.415 | 107.860–127.170 |
| Whole driver case, including surrounding work | 138.322 | 130.112–150.401 |

Prepared-start observations exclude the separately charged worker/rootfs/base
preparation. Fork observations follow a completed, separately charged
checkpoint. App responses and Theia readiness have distinct measurements;
OpenCode continuation assertions do not become external-model latency claims.
All twenty have distinct source runtime identities. Their sixty completed held
tool results retain the expected session, private 16-MiB tool RAM, exit 37 and
model acknowledgement.

Every pause receipt records anonymous sandbox memory and resident snapshot
pages at zero afterwards. The sandbox cgroup still accounts for
12,587,008–33,181,696 bytes of other memory; the worker and separate IDE also
remain running. Complete fork, fork-of-fork, source deletion, resume and
fork-of-resumed assertions pass. These runs do not explain the preserved
Sysbox-fs timeout, nested-PID collision or admission-fixture busy mount. They do
not refresh the separate real external-model proof on this candidate or replace
the required final 100-case series per example and 24-hour soak.

## Native quota primitive, still outside the runtime

Read-only capability evidence
`factorybox-storage-quota-capability-9e0224822a924d2786de25a80971da7b`
finds already enabled, consistent full Btrfs quotas on this host, without a
rescan in progress. No global quota switch, rescan, module installation or
existing qgroup limit is changed.

The isolated 64-MiB referenced-byte quota proof
`factorybox-storage-quota-native-e168207f02be458cbc6012ddfe961c95`
passes in a fresh exact labelled volume. It creates and identifies only its own
new Btrfs subvolume, reaches actual `EDQUOT`, verifies a second quota-boundary
allocation refusal, releases the original fully allocated private 8-MiB reserve
using truncate and fsync, then writes a new rollback receipt. The retained file
is unchanged. Exact subvolume deletion, absence of its root-tree item and
absence of its qgroup accounting/limit items are all checked. Kernel accounting
remains consistent. Native time is 12.960 s; the whole driver takes 13.944 s.

The first attempt
`factorybox-storage-quota-native-e0e4f2b896e24bc082054236b1edb509` fails its
cleanup-wait check after the kernel has already cleaned the deleted root.
Read-only observation `factorybox-storage-quota-cleanup-observation-c53ab1c89d17460cb7f081cfb9702c7d`
finds an empty original volume and no corresponding root/qgroup items.
The separately built ordered correction accepts wait `ENOENT` only together
with these independent absence checks. It retains every original allocation,
rollback and data assertion. The original failed result/source/logs stay
unchanged; after the replacement passes, receipt
`factorybox-storage-quota-cleanup-05a24126f01447f2b29d06d6e68827ae`
records removal of only that reverified empty labelled outer volume.

This is a kernel quota/reserve primitive. It is not an integrated Factorybox
storage policy, complete lifecycle under `EDQUOT`, or proof against new nested
subvolumes, additional mounts, concurrent writers or quota-state changes.
`statfs` still reports the containing filesystem's roughly 998-GB size here,
even with the 64-MiB limit, so the existing filesystem admission counters cannot
substitute for explicit quota accounting. [Btrfs documents](https://btrfs.readthedocs.io/en/latest/btrfs-qgroup.html)
that limits require consistent accounting; [qgroup membership](https://btrfs.readthedocs.io/en/latest/btrfs-quota.html)
must also be established explicitly rather than inferred from directory nesting.
The prototype and its patch remain in artifacts and are not selected runtime
code. Hard store quotas remain release work.

## Actual guest storage-path observation and chat handoff

The production goal continues in one replacement chat after the prior chat's
upstream model requests repeatedly failed. Local handoff
`factorybox-chat-handoff-f58a26a3d19c47ba830a3a8cbeae16fb` freezes GOAL.md,
AGENTS.md, this progress page and the worker contract at commit `38fef09`.
Its context receipt retains the last eight human messages, twelve visible
assistant updates, failed turns, the complete original transcript's SHA-256
and exact result hashes. Hidden reasoning and raw credential-bearing tool
payloads are excluded. The original chat and frozen inputs are unchanged.
The replacement has the full production objective; no second implementation
writer or resumed old goal was started.

The last isolated quota result
`factorybox-storage-quota-native-afb36aa1faff4a01a8e4721fc8bf7bb3` completes its
bounded observation and exact cleanup. A new subvolume created from a distinct
user namespace accepts one MiB after the original 64-MiB leaf quota refuses
allocation. The aggregate therefore exceeds the original leaf limit. This is
a negative storage-design result, not a qualified bounded store or a Sysbox
guest result. Its source and original assertions remain in the ordered
prototype `factorybox-storage-quota-nesting-prototype-59e6daed31b84023a41707d59dee0bf7`.

The new standalone diagnostic `tools/storage_guest_native.py` uses the existing
delivered worker's exact core and pinned Sysbox, adding only a new test and
setup. It runs one real Sysbox guest with ordinary-write controls on its overlay
root and on a fresh, explicitly supplied Btrfs directory from its owned volume.
`factorybox-storage-guest-9f6b0bb6e9d14ba6acbaab6b7d53d3ca` passes this observation:
both ordinary writes succeed; subvolume creation on the overlay returns
`ENOTTY`; creation on the direct Btrfs bind succeeds and the child accepts exactly
one MiB. The worker verifies each created child's subvolume ID and UUID against
the guest receipt before deleting it, then verifies absence. Exact guest,
outer-container and volume cleanup pass; inputs and global sysctls are unchanged.
The outer diagnostic takes 4.563 seconds, including its test-binary preparation.

This establishes that ordinary Sysbox isolation does not itself close the
observed nested-subvolume accounting gap for a directly supplied Btrfs bind.
No host path outside the new owned fixture was exposed and no quota setting,
kernel component or existing runtime changed. No Sysbox guest escape is claimed.
A hard storage limit must account for this path; simple directory nesting or a
leaf qgroup is insufficient. The diagnostic does not change production code,
select a storage backend, refresh the full lifecycle proof, or grant production
qualification.

Portable checks pass in `factorybox-checks-092a88b9e27a4a3b91b8aa1733d4e862`:
Go tests with vet, the consumer build, Python tests, immutable reference/fixture
verification and whitespace checks. The new guest test and setup are separate
files; no imported assertion or overlay was edited.

A read-only alternative-backend capability inspection is retained separately
in `factorybox-bounded-store-capability-7717991c47904c59b57713794f3226dc`.
The existing worker image contains mkfs.ext4, e2fsck, losetup, mount, umount and
stat, and exposes the loop-control device node. Its inspection stops before
confirming the remaining capabilities. The throwaway host reporter then passes
an absent container ID to an inspector requiring an exact ID and raises
TypeError. Exact label/name-checked container cleanup succeeds; the original
failed result and output remain. No loop device, filesystem or mount is created,
and no backend is selected from this incomplete check.

## Fixed filesystem primitive and extracted storage component

The isolated fixed-filesystem prototype uses the existing native image's
formatter and the host's standard loop/ext4 support. It creates one owned
96-MiB image, one exact autoclear loop association and one private mount.
It installs no host runtime, module or kernel and issues no modprobe, quota
switch or global sysctl write. Opening the existing loop-control device in the
first probe autoloads the already installed loop module; the receipt records
the module's observed transition from absent to present. Later probes leave
that observed state unchanged. No unrelated loop association is detached.

The first native result
`factorybox-fixed-store-native-3dfd7bf0509244e9b541807f5f904969` fails its second
allocation canary: a refused one-MiB write leaves room for a smaller file.
Its complete failed image remains recoverable from the original verified
archive. The ordered correction in
`factorybox-fixed-store-prototype-6e41d3a5817c4c7388cf6d91a612f417/0001-fill-remaining-blocks.patch`
adds a 4096-byte filling phase, retaining every original canary, reserve,
retained-data, remount, backing-size and cleanup assertion. Its receipt records
exact before/after and patch hashes. Archived inputs are unchanged.

`factorybox-fixed-store-native-bff3b05e697842539a6f610faf01fd04` passes the
corrected primitive. Its backing file remains exactly 100,663,296 bytes;
the filesystem reports 90,116,096 bytes of capacity. After 79,687,680 written
pressure bytes, available space is zero and a second 4096-byte allocation
returns actual ENOSPC. Truncating and syncing the private eight-MiB reserve
permits a new rollback receipt. Old data and that receipt survive clean remount.
The exact original loop association is released through autoclear and the
private device node is removed. Native time is 3.192 s and outer time 4.235 s.
These are one primitive observation, not application/lifecycle performance.

The new runtime package `internal/storefs` extracts image creation and guarded
attachment from that behavior. Creation executes an exactly hashed formatter
descriptor against an exclusive image descriptor, uses an owner UUID, preserves
the 12-GiB host free-space floor and refuses existing files. Partial creation
remains for explicit reconciliation rather than automatic reformatting. Image
inode, owner, size, link count and superblock UUID are checked before attachment.
The image has its own exclusive lock; an independent bounded loop inventory
refuses an already associated image. Only an empty private target may be mounted.
Mount identity and capacity remain checked; cleanup uses exact autoclear rather
than forcing detach. The lock remains held if another descriptor prevents
release, and exact cleanup can finish on a later call after that descriptor closes.

`factorybox-storefs-native-d5b81f94c08c4df8999dad26f4659012` passes all four native
component tests with zero skips. They exercise real fixed-filesystem ENOSPC and
reserve rollback, data persistence across detach/reattach, duplicate owner and
orphan association refusal, hardlink/symlink backing substitution, preservation
of a substituted mount, last-reference lease retention and cleanup retry, wrong
formatter refusal before creation, and preservation/refusal of an uncertain
initialization. The tests build from frozen new component source with vet.
The existing worker image supplies only its tools/libraries; this does not
claim the new component is in that delivered worker or qualified for Sysbox.
The retained earlier three-test pass is
`factorybox-storefs-native-7580e8457ffa42b78b29f9f3d8fe8e5f`.

The first three native tests already pass in
`factorybox-storefs-native-1d88407107f94276b42405ce22492e2c`, but its host runner
then shadows the container name with a proof name. The exact ownership check
refuses that mistaken identity, so the parent remains failed with cleanup false.
Separate receipt `factorybox-storefs-runner-cleanup-5e86c22fa67b49068a9e46a2791d5a56`
verifies the stopped exact container, preserves the complete owned volume in a
verified archive and removes only those resources. The original result is not
rewritten. The caller-workflow regression in
`factorybox-storefs-runner-regression-e8fd4d9e6f4d4a1681d853c0b29a3522` reproduces
the precise name mismatch against the frozen original runner and passes against
the correction. This mocked caller regression is separate from native evidence.

Portable checks with vet, consumer compilation, Python tests, immutable reference
and fixture verification pass in
`factorybox-checks-1a81a2fe3f6a4d298ab77fb626a1d2a6`. The
[proposed storage decision](adr/0001-bound-worker-storage-with-a-fixed-filesystem.md)
keeps fixed store capacity distinct from host free-space admission. The new
component result alone does not qualify worker startup, lifecycle admission,
guest writable-path placement or committed-pause worker replacement. The
subsequent integration is described below. The prior selected runtime and
reference profile are unchanged.

## Opt-in bounded worker and actual guest capacity

The candidate now accepts an explicit `--bounded-root` and `--bounded-bytes`.
The outer private directory holds its own worker lease, fixed `store.img` and
immutable `bounded.json`; the mounted `data/store` contains the inner lease,
manager data and controller epochs. Initialization refuses an unidentified
nonempty directory or partial image and never reformats an existing store.
Worker readiness verifies the saved image, held outer/image/inner leases,
exact active mount and live services. The legacy default remains available;
this opt-in backend is not selected for production.

Loop attachment now requires direct I/O as well as autoclear, and independent
verification checks the actual kernel flags. This avoids adding a buffered
backing-image path for snapshot data; complete retained-memory qualification
still needs the full consumers. The first direct-I/O native run
`factorybox-storefs-native-77c7819cac9e40bface27d22de3876d7` fails because the
immediate final release check still sees its association after clean remount.
The exact failure and verified complete volume archive remain. Cleanup now
observes release for at most 500 ms without detaching anything; a still-held
external reference continues to refuse and retain the image lease. All four
native component tests pass in
`factorybox-storefs-native-048a59880a9e42a8aba2c9caca4e552d` (4.077 s native,
5.254 s outer), including independent refusal of an unheld image lease and a
substituted mount. Unsupported direct I/O refuses attachment.

Lifecycle admission uses a 64-MiB internal free floor plus 128 MiB of preflight
headroom for the bounded profile. These are observations, not preallocated
rollback blocks. The existing owned capture rollback reserve remains separate.
The host containing the image is checked independently for its 12-GiB floor
plus 4-GiB headroom. Controller and manager roots must be inside the exact image.
Admitted sandbox bundles, overlay layers and bind sources must be backed by it;
external bind sources, unknown mount types and initial descendant mounts are
refused. Read-only external binds also remain outside this initial profile.
Kernel memory filesystems remain governed by the sandbox's cgroup memory cap.

Exact delivered build `factorybox-build-worker-84e5cb48735547bfb0830e9ae4be1167`
passes `factorybox-bounded-store-55ef14ad81c34f4fb1a06e03f45f8d50` with no skips.
Its 536,870,912-byte image reports 510,873,600 bytes of filesystem capacity.
The test rejects an external writable bind before intent, starts the real
Sysbox RAM/file/watch actor, creates a complete checkpoint and independent fork,
mutates the branch without changing source RAM, deletes it, pauses and resumes
the source with identical private RAM. Pause records 4,612,096 snapshot bytes,
resident bytes falling from 4,612,096 to zero and anonymous source memory falling
from 4,608,000 bytes to zero. Other source cgroup memory remains 823,296 bytes;
this is not a claim that every retained worker byte disappears.

The actual resumed guest then writes until both large and 4096-byte writes
reach ENOSPC, and a second canary fails. Store available space is zero, the host
still has 880,782,774,272 bytes available and a new capture is refused without
changing live RAM. The old independently saved checkpoint remains usable after
release of the test pressure allocation. Worker readiness takes 0.564 s; the
native test takes 5.88 s and the entire caller takes 10.137 s. These are one
small fixture's observations, not consumer latency or a distribution estimate.

The initial fixture run `factorybox-bounded-store-daacb85b4005434ea80545053b2eb71d`
fails at guest `/dev` traversal. Matching the established native template's
0755 overlay directories inside its private 0700 bundle allows the lifecycle
to proceed without changing runtime bytes or assertions. Three subsequent
failures (`67675d5ce5c24f7abfcaf1d1698cab93`, `514c587e562148ecb228b11d73067f90`,
`1adfece54255450aa151557a9da3efe8`, all prefixed `factorybox-bounded-store-`)
preserve a separate allocation finding: after host-side unlink and filesystem
sync, the guest's separate idmapped overlay still sees the exact 390-MB pressure
inode with link count zero and its allocated blocks. Admission correctly stays
closed. The corrected fixture verifies that retained inode before truncating
only its own pressure allocation, then requires restored admission and the old
checkpoint. Retention must verify actual reclaimed capacity rather than infer
it from removed names. Every failed volume, input and result stays preserved.
Ordered fixture corrections with exact before/after and patch hashes are in
`factorybox-bounded-store-corrections-6c7f016ef68b4d8786f699b09e04b87d`.

Portable tests with vet, the consumer compilation and immutable reference and
overlay checks pass in `factorybox-checks-056b9122d78f42898c0b06f6bba16a6d`.
The following worker-replacement proofs extend this small-actor scope. All full
consumers, broader exhaustion/fault cases, final repetitions and upstream
qualification on this new profile remain unrun and block its selection and
production qualification.

Storage maintenance preserves the required artifact headroom. A fully decoded
and reverified closed passing archive repack reclaims 17,501,988 bytes in
`factorybox-storage-repack-090e6114be554ee294619d90b1033a30`. Exact byte-identical
source inputs of six closed upstream runs share one inode with receipts in
`factorybox-storage-share-bats-inputs-5c9697805d0e476db442e244115f0aa2`, reclaiming
493,015,040 allocated bytes without changing their contents or original results.
After the exact delivered bounded-store proof passes, only its optional compiler
cache is retired with manifest and source/package/image revalidation in
`factorybox-storage-retire-cache-14520b6090cc4d309595c0063dcdd452` (139,550,720 bytes).
Selected inputs, open failures and the historical experiment remain untouched.

## Bounded-store worker replacement and startup refusals

The exact delivered candidate `factorybox-build-worker-84e5cb48735547bfb0830e9ae4be1167`
passes planned replacement in
`factorybox-bounded-loss-0b294a8b8a224217bbe4b8d2c2158ab9` and abrupt replacement in
`factorybox-bounded-loss-24d9e2c07ac642d7bbb8b61dd8b3b657`. Each uses a separately
owned one-GiB image and the delivered CLI for every lifecycle action. The actor
holds 32 MiB of random anonymous ballast in addition to its normal process RAM.
The caller requires a completed pause, stops or kills the exact old worker,
checks exit status 0 or 137 respectively and removes the entire container before
starting its replacement. The same image inode/owner/UUID and host boot remain;
the worker/controller epoch changes. Mount numbers can be reused in a fresh
namespace and are verified against the current attachment rather than treated
as a cross-worker identity.

The new worker refuses a wrong saved checkpoint hash before publishing target
backing. Exact recovery restores private RAM and files, and replay returns the
same completed result. A checkpoint/fork of the recovered source has independent
RAM and private file mutations. After source deletion, that child pauses,
resumes, checkpoints and forks again. Its grandchild continues useful work after
the child's deletion. At most two sandboxes are live, and exact cleanup completes
in both runs. Original source pause releases 38,158,336 resident snapshot bytes
to zero and reports zero anonymous source bytes in each run. Other cgroup memory
remains recorded. The later descendant pause also reaches both measured zeros.
Total caller times are 19.672 s planned and 18.072 s abrupt; these are two small
fixture observations, not full consumer or timing qualification.

`factorybox-bounded-identity-506a093cb67e496083b0e12f148c7bd7` passes delivered-worker
startup refusals. A duplicate worker refuses the held outer lease while the
original live readiness identity remains unchanged. With the original stopped,
changed owner and changed fixed size each refuse before attachment. A complete
read-only Docker archive stream hashes all 536,870,912 image bytes before and
after each refusal; all hashes are identical. Valid reopen starts a new epoch on
the same image and reads the original private canary. An unidentified partial
54-byte image is refused and remains byte-identical. The native caller takes
12.378 s and exact cleanup passes. Its archive reader subsequently gains a
deadline that interrupts blocked reads as well as process waits. Separate real
child-process tests check the complete exact-member hash, changed member refusal
and termination of a producer that never writes a tar header.
The deadline-enabled caller repeats all native assertions successfully in
`factorybox-bounded-identity-17afd11d8c56438fa9932ec54e4db95a` in 12.245 s.
Final portable checks, including vet, those stream tests, consumer compilation
and unchanged imported reference/overlays, pass in
`factorybox-checks-a8739b29e0c84fdc9e753607ae5e628b`.

These replacement tests add no production runtime changes or imported fixture edits. At
that point checkpoint/epoch retirement remained unimplemented. Its proposed ownership,
selection and reclamation rules are recorded in
[the retirement decision](adr/0002-retire-saved-state-with-explicit-selections.md).

## Experimental checkpoint retirement and an unresolved interrupted sleep

Candidate `factorybox-build-worker-f09e41168c1240bba4422c263795a7f9` adds delivered
`select-checkpoint`, `release-checkpoint` and `retire-checkpoint` commands. Exact
source owner, checkpoint ID and canonical complete checkpoint hash bind each
request. Durable selections and an unresumed committed pause protect their
recovery input. Release cannot reactivate the same selection identity. Unfinished
sandbox/fork work blocks retirement. Completed descendants and resumed sources
must retain independently verified image copies and no filesystem dependency on
the retiring backing. The pinned restore adds `restore.log` and `stats-restore`
to those private copies. Only these bounded additions are allowed; every original
file's hash/length and every file's independent regular-file identity still match.

Retirement publishes a hash-bound compressed inventory and allocated one-MiB
completion reserve before removal. It verifies exact inode, mount, parent,
content and contained hardlink ownership. Only inventoried names are unlinked;
payload bytes are never truncated. Verified read-only immutable aliases are
unmounted without deleting their shared base. Metadata, original request
identities and completion remain. Retry rejects unknown entries, successors and
foreign mounts. Reports contain observed before/after filesystem capacity;
unlink is not evidence that every open reference has released blocks.

`factorybox-checkpoint-retirement-67b937096eb84f42b9df65f7cafb35e3` passes
`TestNativeCheckpointRetirement`, including selection/pause protection, live
independent resume/fork, foreign mount and successor refusal, increased available
capacity, actual controller SIGKILL during partial removal, unknown-entry refusal
on retry and useful descendants after source deletion. Its added
`TestNativeCheckpointRetirementBaseActor` also passes with unchanged private RAM,
same live PID, useful mutation and the shared immutable base intact. These are
individual passing tests inside an overall **failed required suite**.

The original `TestNativeCheckpointRetirementImmutableAlias` with `exec sleep 300`
still fails its live-source assertion. Instrumentation proves it exits before
retirement, directly after checkpoint. Freeze/thaw alone preserves it. The
actual waiting syscall changes from `clock_nanosleep` 230 to `restart_syscall`
219 after the control freeze. The selected Compel code converts an interrupted
restart block to `-EINTR`; the native CRIU log reports this conversion. The
original source exits normally with status zero while its request/remaining
timespec still contains about 299 seconds. Read-only disassembly of the exact
BusyBox 1.37.0 bytes shows its duration helper calling `nanosleep` once and
returning without a retry. Decimal sleep works. No transparent relative-wait
guarantee, admission policy or timer correction has been delivered. The failed
survival assertion remains required and blocks qualification.

The first failures and diagnostics remain frozen and fully archived. Ordered
fixture before/after hashes, patches, exact result hashes, pinned disassembly
and Compel excerpt are in
`factorybox-checkpoint-retirement-notes-890fbf4221d14ee3b7cf7a13859d6ea2`.
The latest isolated sleep failure is
`factorybox-checkpoint-retirement-4bf7974fb0ca4c0db3e37c000eda8b0a`.
The original required sleep case has not been replaced with the passing actor.
No imported reference or overlay was changed and vet remains enabled.

The same exact delivered candidate passes the existing complete bounded-store
capacity/guest-ENOSPC regression in
`factorybox-bounded-store-9774ed9fea4b48429793d5fdf4623ade`, planned worker replacement
in `factorybox-bounded-loss-43524087d2304a26b5b32410b27f0c2e` and abrupt replacement
in `factorybox-bounded-loss-d995efa00e21498a92c9f6cfa065e7ba`. These do not cover
checkpoint retirement across epochs, selection recovery across worker loss,
full-consumer bounded storage or epoch retirement.

Storage receipts preserve the 32-GiB artifact budget and required start headroom.
Fully decoded and byte-compared offline compaction of 38 closed passing payloads
reclaims 421,130,240 allocated bytes in
`factorybox-storage-compact-closed-8cbac24282e84710ab647c02a4d664d8`. Seven closed
diagnostic test executables are compacted with full reconstruction receipts in
`factorybox-storage-compact-retirement-probes-fc2862a7e3944cae991425f4b9dbf646`;
the current sleep failure stays directly usable. Additional closed probe receipts
sit beside their binaries. Closed probes now use the same full-verification
compaction policy as other compiled test outputs. Only optional compiler caches
are retired after the matching delivered capacity regressions pass, in
`factorybox-storage-retire-cache-6303c0c900174a01ae4da63920c7a8e0` and
`factorybox-storage-retire-cache-9b689f7d02594809925f8b685f505555`.

Final portable Go/Python checks, consumer compilation, enabled vet and exact
reference/imported-overlay verification pass in
`factorybox-checks-7fb683fad48548f2a5f7911f318502a4`. This does not change the
failed native retirement-suite verdict.

## Kernel restart-state diagnosis and a direct syscall regression

The frozen kernel stack only shows return-to-user signal/restart handling. It
does not identify the original syscall hidden behind `restart_syscall` 219.
`tests/timer-probe/probe.py` adds an opt-in, read-only diagnostic using one
temporary BPF task iterator on this exact AMD64 kernel. It filters the requested
PID in the caller's PID namespace, matches kernel birth time to the pinned live
process, and reads its actual restart block. No source task memory is written.
All iterator/link/program descriptors close, and querying the exact program ID
confirms its disappearance. No BPF object is pinned or persistent event hook
installed.

Native controls identify `hrtimer_nanosleep_restart`, kernel Clock 1, the user
remainder pointer and the original monotonic expiry. A separate PID namespace
with its own PID 1 returns its own `do_no_restart_syscall`, rather than adopting
the frozen sandbox's PID 1. This establishes readable restart state on this host;
it does not implement or qualify timer restoration.

The new required `TestNativeCheckpointRetirementTimerState` uses a tiny static
AMD64 task calling `clock_nanosleep` directly. Its request is verified in an
actually read-only mapping and its remainder pointer is NULL. The probe still
finds the original clock/expiry, so an optional userspace remainder cannot be
the sole source of recovery data. The task checks general registers, flags,
stack and unchanged request bytes after return, then writes a monotonic
completion timestamp. Source and fork must both finish at the original kernel
deadline within 250 ms. Restarting the original twelve-second duration is not
accepted. These downstream checks remain unrun because the original source
already exits prematurely after checkpoint.

The full four-test native suite
`factorybox-checkpoint-retirement-567a4d3dd96c45f4bd7112c5581af5ad` retains the
same two passing retirement/RAM-actor controls and both required wait failures.
This reproduces the defect independently of BusyBox. Source/fork timer restart,
read-only/NULL-pointer safety, real signal/error cases, repeated capture and later
pause/resume/recovery still need implementation and proof. Runtime components
and delivered candidate bytes remain unchanged. No timer correction, new
production BPF requirement or changed admission policy is selected.

Native observations, exact result hashes, ordered before/after fixture patches
and frozen current diagnostic sources are in
`factorybox-timer-restart-diagnosis-b0e8c3c680174fd4853859b109b97073`.
The complete retirement caller automatically includes all four required tests
and mounts the diagnostic helper. Its isolated diagnostic form is
`tools/bounded_store_native.py --worker-build BUILD --suite retirement-base-diagnostic --timer-probe`.
Portable checks with vet, consumer compilation and immutable reference/overlay
verification pass in `factorybox-checks-cc09595f4ea5461e9daf97b5c3ae3268`;
the full native suite remains failed and production qualification remains false.

The exact assembled regression ELF also passes an ordinary uncaptured control
in `factorybox-timer-abi-control-010baec04c184d808ab42d2663d4f6d8`. It reaches
its own general-register/flags/stack/request-byte checks, writes the complete
timestamp and exits zero after the requested twelve seconds. Caller time is
12.360 s including container startup. This validates the test task itself;
source/fork timer restoration is still unproved. The first control attempt
`factorybox-timer-abi-control-ef669d6780f14920909b2395f0c11f8d` retains its
output-permission failure and exact recipe hashes. Running the cap-free ordinary
control as its output directory's existing host UID/GID corrects that fixture
permission without changing the ELF or any assertion.

Immutable input sharing creates further artifact headroom without regenerating
or discarding payloads. Receipt
`factorybox-storage-share-sources-d11b4cd13f9f42808d7e4ad182146365` shares 31,620
byte-, metadata- and mtime-identical files of completed passing source inputs,
then rechecks every original source manifest and result. It reclaims
281,903,104 allocated bytes. Receipt
`factorybox-storage-share-cache-data-59a569db4a7249e6b481617b927cc69d` shares 1,480
identical content-addressed data objects in three completed standalone Go CLI
caches, reclaiming 265,650,176 allocated bytes. Action indexes and all original
source/package/binary/result hashes remain unchanged. No cache object is removed
or rebuilt, and the historical experiment stays cold.
Repeating source sharing against the already shared canonical inputs makes zero
file changes and revalidates the original manifests/results in
`factorybox-storage-share-sources-212967ac54764adb8616361fe2ba1ae8`.

## Timer continuation experiment and confirmed pause memory

The new experiment prepares one admitted, frozen native AMD64 task before
CRIU reads its mappings. Only the native test binary has the preparation path;
the delivered CLI has no timer hook or Python dependency. The sealed capture
request, original cgroup descriptor, PID birth and live pidfd identify the
source. A temporary read-only BPF iterator supplies the kernel deadline. The
experiment requires one thread and zero time namespace offsets. It adds one
private RX code page and one RW state page, preserves the original read-only
request, stack/red-zone bytes, flags and general registers, and returns the
raw syscall result. It never retries EINTR unconditionally.

For a NULL remainder, the continuation waits on the original absolute
monotonic deadline. CRIU's existing restart handling then preserves subsequent
captures without another preparer. The passing experiment
`factorybox-checkpoint-retirement-59f44517b59b464eb3b6114bbd63394d` includes the
prepared source capture, a second capture through the delivered CLI, private
fork, capture of that restored child through pause, snapshot RAM release and
resume. Both source and resumed child pass the unchanged direct-syscall
register/flags/stack/request assertions and finish at the original deadline
within the required 250 ms. No exit tracer participates in this final run.
The two private pages retain 8 KiB per prepared task until process destruction;
this is measured state overhead, not a demonstrated optimization.

Earlier prototype attempts that missed completion files remain preserved.
Natural-exit observation in
`factorybox-checkpoint-retirement-8608369301b84b0cbbe25839029abce6` shows both
tasks exit zero after completing their original assertions. The test now pins
each live owned task's actual root mount. Both
`factorybox-checkpoint-retirement-4a8dc9d3c3114deb9997bff2d0054d84` and the
final experiment demonstrate a completed timestamp through that guest view
while a repeatedly polled outer overlay still returns ENOENT. The required
task assembly and every original assertion remain intact; exact fixture
before/after hashes and ordered patches are retained with the experiment notes.

The pause extension also exposed delayed anonymous-memory accounting for the
small task. Closed failures
`factorybox-checkpoint-retirement-e33093d0a73a4594a0dc0a1123ef67c7` and
`factorybox-checkpoint-retirement-8ffa3aaab44045ae9e178df5c9b773f2` retain
20,480 to 24,576 reported anonymous bytes after populated=0 and zero resident
snapshot pages. In the latter, repeated read-only observations reach zero
about 1.6 seconds later. Pause now waits at most three seconds, also bounded
by its context, while revalidating the exact empty original cgroup. It records
the observed zero and refuses to commit if release remains unconfirmed.
The original assertions are preserved. No global flush or substituted counter
is used. The final experiment records 24,576 anonymous bytes before pause,
zero afterwards, and 32,768 snapshot bytes with zero resident bytes afterwards.

Candidate `factorybox-build-worker-2b11bea0a63c4136a18d41bbe211af80` contains
that pause confirmation change, with the same pinned Sysbox, CRIU and native
base. Its package SHA-256 is
`966b535b0d3ac33d806e3128634bacf9489bc6636616c060ecfa83a11be491f4`.
Capacity/guest ENOSPC, full checkpoint/fork and pause/resume pass in
`factorybox-bounded-store-3461693615cc4214a565d1cb260a2dca`.
Planned and abrupt bounded-store worker replacement also pass on these exact
delivered bytes in `factorybox-bounded-loss-8c1b2d07cad54b81a788430c2f58c9e0`
and `factorybox-bounded-loss-61fc2dcb72ea4c6f9e9de88f1e9604e3`.
The complete required retirement suite
`factorybox-checkpoint-retirement-abce758b443345b08446ba1593a6298a` still has
two passing retirement/RAM-actor controls and both original timer failures.
Production qualification remains false. The timer continuation has not been
integrated into the runtime. A non-NULL remainder, real signals and errors,
multiple threads, nonzero time offsets, safe continuation lifetime and
interrupted preparation still require implementation and proof.

The verified evidence index and exact, successfully reconstructed ordered
before/after source patches are in
`factorybox-timer-continuation-notes-c3b2393e0dc44ad19708a61cd15b2c02`.
The final timer experiment finishes 267,281 ns and 306,172 ns after the original
kernel deadline for source and resumed child respectively. These are individual
correctness observations, not latency percentile claims. Portable checks with
vet, consumer compilation, Python tests and immutable reference/overlay checks
pass in `factorybox-checks-3fe80cd2883b4bbb94968a57157e0392`.

## Go timer reader, guarded preparation and delivered integration

Candidate `factorybox-build-worker-16a6d038c23142d1802cec1cc1b05acb` now
integrates timer admission and preparation into the Go capture worker. Its
package SHA-256 is
`fb629eba620042a60690bd3697efa27d7d84ffade72f23f26eed62005e1f30c6`.
The native base, Sysbox and CRIU inputs are unchanged. No Python preparer is
part of the delivered runtime. This remains an unselected local candidate and
production qualification remains false.

`internal/timerstate` implements a bounded, read-only kernel task iterator.
It pins each exact PID or non-leader TID with its own birth time, filters the
caller's PID namespace and closes the iterator, link, program and allowed-task
map. Exact program/map ID lookups must confirm release, including on errors.
The permitted-task map is frozen before attachment. This reader uses raw BPF
syscalls and the installed kernel's BTF rather than a new dependency. Its BTF
decoder accepts the kernel's 24-bit member count; the older pinned indirect
decoder only accepts 16 bits. A regression puts 65,536 enum members before
`task_struct`, and malformed bounds must fail.

Native read-only component proof
`factorybox-checkpoint-retirement-fb72da3d3a5e4bc48c38abdbe30dfb1b` passes
for owned root PIDs and non-leader threads, mixed timer/non-timer tasks, a
different PID namespace, duplicate PID refusal, wrong-birth refusal with BPF
release, agreement with the independent Python reader and unchanged useful
actor RAM. It tested the component before the CLI linked it; this is explicitly
not delivered lifecycle qualification. The preserved preceding failure
`factorybox-checkpoint-retirement-33ecf09ab9bf4bba8b852682b31c6bae` is the
fixture's inherited background-sleep output pipe. Redirecting the entire
owned shell fixes that fixture without dropping assertions.

Capture now records a hashed frozen-thread/timer plan. A separate Go mutator
may trace only those original threads after an independent source guard has
acknowledged its owning OS thread. All source threads stop before preparation.
Remote mmap/mprotect calls reuse the verified original syscall instruction;
application text is never patched. Predetermined MAP_FIXED_NOREPLACE private
addresses, original registers, masks, request bytes and continuation state are
journaled before task mutation. Code becomes RX and state stays RW. Existing
relative continuations reuse their verified frame. Native monotonic/boottime
offset conversion is implemented but not yet qualified with nonzero offsets.

The source guard traces the mutator's OS thread with TRACEEXIT. Even SIGKILL
stops that thread before the kernel automatically detaches its tracees. The
guard confirms the exact original cgroup is frozen before allowing exit. An
unconfirmed hold keeps the guard alive rather than releasing the tracer.
The original controller is also pinned and polled independently, because the
capture worker temporarily holds a duplicate control-socket endpoint. This
prevents that duplicate from hiding controller death.

The controlled native exit proof
`factorybox-checkpoint-retirement-d74c9b82fc9649f7868e1eb69ee5a510` passes
normal exit and real pidfd SIGKILL. In each case the exact source was observed
with a thawed cgroup and the actual test tracer before exit, then held and
untraced after the independent guard's recorded EXIT event. No preparation
completion is invented. This tests the production guard against a controlled
test-only tracer; it does not repair an interrupted real mutation or qualify
controller loss during that mutation.

The full required retirement suite now passes all four tests in
`factorybox-checkpoint-retirement-906db4b97dd649bd93d9cc00febb2017` on the
delivered candidate. The previously failing BusyBox sleep stays alive. The
unchanged direct-syscall task with a read-only request and NULL remainder
preserves registers, flags, stack and its original deadline in source and
fork. Original checkpoint selection, interruption, retirement and RAM-actor
assertions also pass. The retained historical failures above are unchanged.

Repeated capture and restored-child pause/resume additionally pass in
`factorybox-checkpoint-retirement-ed4891a88d914f438f4215ef8c052bd3`.
The existing experiment fixture now delegates preparation to the Go runtime
when the sealed request contains a timer plan. Its original assembly and
assertions remain intact. It still verifies a second delivered CLI capture,
private fork, child pause with actual anonymous and snapshot resident bytes
both zero, resume and both original completion deadlines. The Python
prototype inputs remain frozen comparison evidence.

Other exact delivered-candidate regressions pass:

- Capacity, actual guest ENOSPC, preserved prior checkpoint, independent RAM
  and pause/resume in `factorybox-bounded-store-947cd46eaaaa41b7a42c6f3f69b78927`.
- Planned and abrupt bounded worker replacement in
  `factorybox-bounded-loss-1e3937a9fc4f4291be3b017dae81144a` and
  `factorybox-bounded-loss-394aeedf45204fb09443c613e75368b8`.
- Actual capture-worker SIGKILL after completed CRIU while the controller is
  stopped and the private filesystem is full, for checkpoint and pause, in
  `factorybox-storage-admission-bb2592f6bb484980abfbb4edcce6a1fa`.
  The original source remains useful before pressure removal, and its prior
  checkpoint remains independently usable. This is a later capture-copy
  interruption, not an interrupted timer preparer.

Real signal races at the continuation boundary, non-NULL remainder error
cases, actual partial-preparation repair, repeated multi-thread mutation,
nonzero time offsets and continuation lifetime still need direct proof or
correction. Pending caught unblocked signals are refused before and after
preparation; that check alone does not close the later signal race. The
current helper retains two private pages per newly prepared task until process
destruction. No general signal transparency or bounded lifetime is claimed.
The complete consumer/native/upstream suites are still required on final bytes.

Artifact maintenance retains the historical experiment cold. Closed failed
native inputs are eligible for identical-source sharing only with confirmed
cleanup and unchanged source manifests; failed builds and dumps are excluded.
Receipts `factorybox-storage-share-sources-2daaeb59c60e4072853fb75a0772a76e`
and `factorybox-storage-share-sources-ba2d843a20804968ad546e9bf42e70c1`
reclaim 64,364,544 and 5,169,152 allocated bytes respectively. Only optional
compiler caches of three superseded v0.1 packages were retired against the
existing native-qualified successor, preserving every runnable package and
source. Receipts end in `7b197a5c21304750ba554cb46b81bbf0`,
`73e0bc83a94a4f598871b2ef6185daa9` and `a49b4b60b3f94f36b28768d722d2dafc`;
actual reclaimed allocations were 4,153,344, 6,725,632 and 138,264,576 bytes.

Receipt `factorybox-storage-share-inputs-f4889fc232ec43bf8d3a9c9f3f0c5d19`
shares 200 identical completed compiled inputs, including completed worker
component bundles, reclaiming 2,468,044,800 allocated bytes. All original
source, result, runnable bundle, package and image identities are reverified
before and after. Executable bytes, modes and ownership stay identical;
original and shared inode mtimes are recorded. No file contents are rewritten.
The current successful build optionally discards only its own new compiler
scratch after verification, retaining its full inventory receipt. Failed
build scratch is preserved.

Exact evidence hashes and successfully reconstructed ordered before/after
source patches are in
`factorybox-timer-integration-notes-331047eab8a04eed9fac29654251cddc`.
All six portable gates, including enabled Go vet, Python tests and byte-exact
reference/imported-overlay verification, pass in
`factorybox-checks-59eb1ebbe6ab43b498fa7e6d07b29503`.

## Caught signal at the timer continuation boundary

The first integrated continuation has a demonstrated signal defect. Native
`factorybox-checkpoint-retirement-b6e4d85232654573957faa728f33999b` queues a
real SIGUSR1 after preparation completes, while the original source is frozen
and its exact capture worker is stopped before CRIU starts. The source status
records the pending caught signal. After pause/resume the handler runs once,
but the original relative wait does not return EINTR: it enters the helper's
absolute sleep and continues waiting. The uncaptured and already-active helper
controls pass the same raw-return/register/flags/stack/request checks.

The negative control repeats against the old worker's frozen non-test source
in `factorybox-checkpoint-retirement-2e2e8d435613420a941d1dfa2c54b73d`.
The caller's `--frozen-core` option retains the complete old source/test tree
and overlays only current test files into a new private fixture. Its full
non-test manifest must still equal the selected delivered worker. This is a
current regression against exact old bytes, not a qualification exception.
The same signal boundary fails again, with one handler and the original wait
still active. Both old failures and their complete owned stores remain saved.

Candidate `factorybox-build-worker-b675cca751d04fa4b06284f36a1b2324` changes
the private continuation. Its package SHA-256 is
`9732f44b9c7f46b552376bc63459eebc9a3d6f0a6db921f29a62d06341df18b7`.
The mutator retains a blocked entry mask and saves the original application
mask in its private frame. `ppoll` atomically installs that original mask for
the wait. A caught signal returns raw EINTR and is never retried. Only timeout
completion loops, rechecking the original monotonic/boottime deadline in
at most 25-ms slices, so time spent frozen does not restart a full duration.
Before returning to application registers, the helper restores the original
mask. Non-NULL remainders use the original deadline and a bounded self
`process_vm_writev` copy rather than writing through an unchecked pointer;
that error/remainder path is implemented but still needs native proof.

The source `internal/lifecycle/timer-code/continuation.S`, exact assembler
tool identity and 571 extracted code bytes are frozen in
`factorybox-timer-code-build-7b2dfc58013e49d0bf249e67a42759a7`.
Code SHA-256 is
`f82e0d96e439a58077bcebe3a64caabd4117c07c2a74e82e8dc09e8de562d287`.
The Go array is verified against those bytes. No assembler executes at runtime,
and the old Python experiment and its archived inputs remain unchanged.
Capture-worker outcome recording now also covers preparation errors.

Native signal proof
`factorybox-checkpoint-retirement-8ad1cd85d5a34053861c2babf4029389` passes
all four cases: uncaptured control, signal during an active prepared wait,
signal at the first prepared entry before CRIU, and signal during a second
capture of an existing `ppoll` continuation. Each requires exactly one handler,
raw EINTR and the preserved register/flags/stack/read-only request contract.
Both capture cases complete pause/resume using the delivered CLI.

Two fixture failures remain explicit. In
`factorybox-checkpoint-retirement-966f26a62069494a86fa47c04ac0e840` the new
task can finish before resume publishes its required live-process receipt.
The separate signal fixture now waits only after its original ABI checks,
completion marker and fsync; failure exit 79 is retained. The held-success
fixture passes the initial three cases in
`factorybox-checkpoint-retirement-d5e2ed60f004444e974fba2ac2e9ef81`, and the
old-byte negative control still fails with that same successful-task hold.
The first recapture placement in
`factorybox-checkpoint-retirement-3ddab97d4bac4176ab43bacd916eff6c` freezes
the task before invoking pause, so normal OCI status correctly rejects it as
already paused. The final fixture stops its own controller after the normal
guard entered instead, confirms the exact frozen/untraced source and absence
of a CRIU/copy child, then queues the signal. No production fault switch or
diagnostic source thaw is used in the passing assertions.

The bounded poll has a measured cost. For one prepared sleeping task, the
old control records zero charged CPU microseconds over 2.001542017 seconds
and 24,576 anonymous bytes. The final new signal run records 6,140 charged
CPU microseconds over 2.001569880 seconds and 20,480 anonymous bytes. An earlier
new run records 4,941 microseconds and 24,576 bytes over about two seconds.
Raw cgroup counters are retained. These individual observations do not
establish a distribution, a memory reduction, or an acceptable pressured-worker
cost; chunked-wait timing and CPU behavior remain qualification work.

The new delivered bytes also pass all four required retirement cases in
`factorybox-checkpoint-retirement-3e22137468c644daaf38d2591ce83f1c`, repeated
source capture and restored-child pause/resume in
`factorybox-checkpoint-retirement-53df7c82bcf04f4ab49eb4135a450639`, and
capacity/actual guest ENOSPC in
`factorybox-bounded-store-b7abe41ff7744b4cb9d8c7770c675394`.
Planned and abrupt bounded worker replacement pass on the same delivered bytes
in `factorybox-bounded-loss-b8468034c7e14f1ca5b8b51436ef69de` and
`factorybox-bounded-loss-86be80b052594da0ae64f3276f8cb8c1`.
The stopped-controller, full-filesystem capture-copy SIGKILL cases for
checkpoint and pause also pass in
`factorybox-storage-admission-b2cbad55a2b84511a24e5edee0316d0a`.
The controlled normal/SIGKILL tracer exit guard passes on these bytes in
`factorybox-checkpoint-retirement-7e2090fc82844a74b0fa585c59a6ab45`.
Portable checks including vet and immutable reference/overlay verification pass
in `factorybox-checks-32b3b972bc9b42a08d1befdb561a926b`.

Exact native results and reconstructed ordered source/fixture patches are
indexed by `factorybox-timer-signal-notes-3e37040c96a54eacb304703e393eaa63`.
Production qualification remains false. Non-NULL remainder and fault cases,
broader signals and mask semantics, interrupted real preparation repair,
multi-thread mutation, nonzero offsets, continuation lifetime and final
consumer/upstream qualification remain open.

## Non-NULL remainder before a delayed signal handler

The next direct-syscall regression distinguishes interruption time from handler
completion. It supplies an actual writable remainder pointer, retains the
register/flags/stack/read-only request checks and requires raw EINTR with one
handler. The handler performs a real one-second wait before returning. The task
records its own monotonic time immediately before the original twelve-second
wait, allowing the normal control to run without a diagnostic freeze. Both
remainder comparisons retain the original 250-ms deadline bound.

The first fixture attempt
`factorybox-checkpoint-retirement-8f464e82e8304498824b506d7f7ee354` fails its
uncaptured control: freezing it to read the kernel restart block perturbs the
observed remainder by about 0.5 seconds. That is retained as a fixture failure.
The final no-freeze control passes in
`factorybox-checkpoint-retirement-7bf2eb8700a34a429a77852d4274542e`, differing
from its interruption-time estimate by 35,613 ns. The prepared b675 candidate
passes the same raw EINTR and ABI checks but returns a remainder
1,000,158,168 ns too short. The actual handler lasts 1,000,365,734 ns.
Its helper recomputes the remainder after that handler, incorrectly charging
the handler's elapsed time.

Candidate `factorybox-build-worker-2fca9b438e664527a211a44df38bd0f7` records
the full original remaining duration and the bounded poll-slice duration at
each slice entry. On EINTR it uses the kernel-updated remaining slice,
subtracting only the elapsed part of that slice from the original remaining
duration. It performs no new clock read after the handler. The existing
bounded self-copy and raw error propagation remain. This corrects the measured
time accounting, without claiming all copy/error ordering is qualified.
The package SHA-256 is
`10c0fe52377676d854272bc17f29333a0856b8f668ecf080d641afd837909f83`.

The same final native fixture passes both cases on these exact delivered bytes
in `factorybox-checkpoint-retirement-03381a333fc644c687b64594c2d3d2d6`.
The uncaptured remainder differs by 33,727 ns and the prepared remainder by
31,282 ns. Both real handlers last about one second. These are individual
correctness observations. The four signal-boundary cases also pass in
`factorybox-checkpoint-retirement-f1fc7285c7dd47c594245bdfd5fdbfc6`.
The full four-test retirement suite passes in
`factorybox-checkpoint-retirement-74c6a3ee31134c6990c06f54c6f20469`.
Capacity and actual guest ENOSPC pass in
`factorybox-bounded-store-0e74bfd161a44df29db80e9642fcffdd`.
Planned bounded-worker replacement passes in
`factorybox-bounded-loss-d6bc10b605704b0eafd9dc84f8af9378`.
Abrupt bounded-worker replacement passes in
`factorybox-bounded-loss-f50f1c31487346c2b29f1f9afb962b9e`.
Repeated timer capture and restored-child pause/resume pass in
`factorybox-checkpoint-retirement-cf2e8c92938541158bc5d8b1ddc0b06b`.
The full-filesystem capture-copy SIGKILL cases pass in
`factorybox-storage-admission-e06b026ba59f4be6894344c18f0e7858`.
The controlled normal/SIGKILL tracer exit guard passes in
`factorybox-checkpoint-retirement-e828671fe7b64ee6a16931658b59b13d`.
The latest individual two-second sleeping-task observation records 5,706 charged
CPU microseconds and 24,576 anonymous bytes; raw counters are in the signal proof.

The new assembly's 574 bytes and compiler/source identities are frozen in
`factorybox-timer-code-build-3f8241901b3c417088b85c9403912ea4`; code SHA-256 is
`826c5d482dc4ab1a529ea7394ce044c3b53a15770732a8c12802bf94bedabb23`.
Portable checks with vet and exact reference/overlay verification pass in
`factorybox-checks-00aa0eef7f3f4e8b9f3dd3846503dd45`.
Evidence hashes and exact reconstructed source/fixture changes are in
`factorybox-timer-remainder-notes-99afbe6c2e694fc79b0eadf57d2b97dd`.

Production qualification remains false. The next section records the later
copy/protection ordering verdict. Broader masks and
signal actions, pending signals through pause/recapture, nonzero time offsets,
actual partial-preparation repair, multi-thread mutation and continuation
lifetime also remain required.

## Remainder mapping changed by the signal handler

The delivered 2fca candidate has a confirmed copy-order defect. A separate
native fixture places the remainder on its own page. The one-second handler
then makes only that page read-only. The native control returns raw EINTR,
with a valid remainder already written before the handler. The delivered
prepared wait returns EFAULT because its self-copy happens after the handler.
The first negative is
`factorybox-checkpoint-retirement-4df5d445d898415a8d7c163e0c38bcfb`.
The final fixture confirms the same result in
`factorybox-checkpoint-retirement-98bebdb146ae463e87146c0738fa442f`.
All original required timer, signal and delayed-handler tests stay unchanged.
The new task retains their register, flags, stack, read-only request, one-handler
and raw EINTR assertions. Symbol and mapping checks verify the dedicated page.

A test-only activation experiment uses the existing 323-byte native-wait code
from the unchanged `tests/timer-probe/restart.S`, SHA-256
`02f23238267d39195a106632b38e3b1c4ba58fa6d7d95adeae531e5ad553e15f`.
It operates on one exact task after a real delivered checkpoint, fork or
resume. The production source guard independently watches its tracer exit.
It journals the original registers, mask and private frame before mutation,
verifies the runtime's private RX page, and replaces only that page. It keeps
catchable signals blocked while stepping to the native clock_nanosleep entry,
then installs the original mask at that kernel boundary. Syscall tracing
prevents a return or signal-delivery stop from running application code.
It confirms an actual sleeping kernel wait, interrupts it, reads its exact
restart block and deadline, and freezes the source before detaching. Temporary
BPF objects must be released. Application instructions and the request stay
unchanged. This command and its switches exist only in the test executable.

An earlier syscall-entry-only experiment passes in
`factorybox-checkpoint-retirement-757d28e396e94dbfa0309d9fd7b36ff9` and
`factorybox-checkpoint-retirement-6eeaddb9e0844d69862f08877fa116a9`.
The final experiment additionally verifies the armed kernel timer. Its first
observation attempt, `factorybox-checkpoint-retirement-f07f989fdb884be1aa876658e6a81df2`,
reads a restart block before interrupting the wait. Linux has not yet filled
that block, so the experiment correctly refuses the observation. The next
attempt, `factorybox-checkpoint-retirement-35a9bda8b37d4270ab170bbcd2e61e87`,
passes source, fork and resume but fails a new queued-signal timing assumption.
The remainder was written at the tracer interruption, about 399 ms before the
signal was queued during the hold.

The final native frozen control establishes the correct comparison. It freezes
an uncaptured native wait, reads the actual kernel-written remainder, waits
600 ms, queues SIGUSR1 while still frozen, then thaws. After the handler, the
remainder equals the earlier kernel-written value byte for byte. It differs
from the later signal-time estimate by 602,112,981 ns. The activated queued
case preserves the same native behavior, with a 403,623,241-ns difference from
signal time and exact equality to its recorded interruption-time remainder.
These cases do not relax the existing 250-ms checks. Both actual interruption
observations must still satisfy that bound, and the held remainder requires
exact equality. The initial frozen-control attempt accidentally thaws before
queuing the signal and fails in
`factorybox-checkpoint-retirement-d1e5720cbe6b4d9880bfdfbc1ad7315d`.
Its frozen source and failed result remain preserved.

The final six cases pass in
`factorybox-checkpoint-retirement-7c294dbd14b34613823c82854cb26126`:
ordinary native control, frozen native control, activated source, activated
fork, activated resume and signal queued during the activated hold. All return
raw EINTR and preserve the ABI and protected remainder. Active source, fork
and resume remainders differ from their interruption-time estimates by
483,574 ns, 534,352 ns and 378,603 ns. The four armed kernel deadlines differ
from the saved application deadline by 355,841 to 577,522 ns. These are
individual observations. Fork and resume have measured monotonic namespace
offsets of -1,511,617,658 ns and -2,573,720,735 ns. Comparisons use the guest's
clock, accounting for those offsets. No general time-namespace qualification
follows from those two observations.

The final negative and positive runs use the exact same test executable,
SHA-256 `a2f4f9c4a8b45ccf918d3361af2e84d9dc6a8c856592a6d40b4bd5302c916e63`.
Every run confirms exact owned cleanup, unchanged inputs and unchanged global
sysctls. Portable Go checks with vet, Python checks, consumer compilation and
immutable reference/overlay verification pass in
`factorybox-checks-e49f48c645424d79bcac5e718062d3ec`.
The verified evidence index, unchanged required-test hashes and reconstructed
source/fixture patches are preserved in
`factorybox-timer-copy-order-notes-1b608f92b4bf4ac09019e7147f6550d4`.

The delivered runtime remains unchanged and fails this required behavior.
The experiment activates restored tasks only after the existing CLI has already
exposed their ppoll continuation. Production integration must hold restored
tasks before any application continuation, activate before thaw, and keep
checkpoint, pause, fork, resume and worker recovery consistent. The capture
guard currently admits one watched mutator per transaction, and FD 9 closes
when preparation returns. A second mutator cannot simply reuse that protocol.
Keeping one watched child across capture is a possible source-side design;
it is not implemented. The pinned CRIU source runs its pre-resume notification
before final detach. A restore hold at that boundary still needs a separate
native proof, including controller/tracer loss and exact cgroup ownership.
Broader signal actions, interrupted mutation repair, multiple threads, repeated
activation, private-page lifetime and all remaining release gates stay open.

## Holding restored tasks before application continuation

The optional sealed restore/resume protocol can now hold the exact restored
cgroup before CRIU detaches its tasks. A late libcontainer callback first
checks the persisted operation, cgroup descriptor, restored PID and birth,
and the actual CRIU tracer. It freezes the target, sends the controller a
request-hash-bound held identity over an inherited packet socket, and requires
the exact acknowledgement. A missing or foreign acknowledgement fails restore.
After successful CRIU completion, the adapter records `restore-held.json` or
`resume-held.json`. These are separate from ordinary completion receipts.
The public lifecycle does not yet request this hold or activate timers.

The first native attempt exposes an important placement detail. Sysbox moves
the temporary CRIU child into the target's `init.scope`. Freezing that cgroup
also freezes CRIU while it is receiving the notification reply. The counter
stays unchanged, but restore never completes. The first failure is retained in
`factorybox-checkpoint-retirement-5c2ee77da5e04836a0b54208d3755424`.
The diagnostic repeat
`factorybox-checkpoint-retirement-0340a1e7bcd84979ad807ec7c8ee37fd` records
CRIU inside the target cgroup, blocked in `get_signal` around `recvfrom`, both
before and five seconds after the acknowledgement. The application counter
remains exactly 140,573,640 and the target is populated and frozen.

The ordered follow-up pins the prepared launcher's original worker cgroup
before starting CRIU. At the late callback, it verifies the original worker
descriptor, the exact CRIU PID from libcontainer, a live pidfd, the tracer's
executable inode and its temporary target membership. It returns only that
CRIU process to the original worker cgroup and verifies its new membership.
The application tasks remain in the target. The subsequent freeze therefore
holds the application while CRIU can finish detaching. No CRIU binary change,
host runtime installation, global cgroup configuration or sysctl is involved.
The first patch and the corrective patch are both retained as ordered inputs.

Worker `factorybox-build-worker-d8cc5e7841bb4b6190fbb6331ff7fc5f` packages
Sysbox build `factorybox-build-sysbox-510db28d7f1d40aaabbc10f9538da066`.
Its package SHA-256 is
`178b275c24ca9246fe63c4af1c1c8d4d167c7716377e3f67025d8e8b8d21f652`.
Sysbox-fs and Sysbox-mgr, the toolchain and all APT package/source identities
remain byte-identical to the prior component build. Only Sysbox-runc changes.
The existing retained CRIU binary is reused after checking its exact frozen
base lock and the complete unchanged current CRIU component recipe. The worker
keeps that base lock and its hash. Default candidate verification remains
strict; changed CRIU recipes or mutated base locks are refused by new tests.

The final native hold proof
`factorybox-checkpoint-retirement-47c230423bc2402f939e1039b1f3723d` passes
five cases. Fork and resume hold a continuously incrementing native counter
unchanged while awaiting acknowledgement and after CRIU has detached. The
counter advances only after the explicit exact-cgroup thaw. Both restored
process births and request hashes match their held receipts. Socket disconnects
in fork and resume, and a foreign acknowledgement in fork, reject restore.
In these three runs the rejected restored tasks have exited. Their unresolved
operation records are preserved and deletion is explicitly refused without
changing those records. All owned outer resources are then removed.

The first version of that cleanup fixture incorrectly expects ordinary Delete
to discard an unresolved creation; it fails in
`factorybox-checkpoint-retirement-0b469a0e10fe48d881a61863a701ae74` after
the positive fork and resume cases pass. The corrected new fixture verifies
the refusal and retains the pending record and mounts until removal of its
unique outer worker and volume. The shared reader constructor gains an
explicit cleanup callback used only by the new rejected-resume fixture.
Every existing caller keeps the original deletion and unmount assertions.
Before/after hashes and exact reconstructed patches are recorded in
`factorybox-restore-hold-notes-58e4c5e2cc7f4621a903411f62fe861a`.
The backend's redundant-unregistration messages in rejected fork cleanup are
preserved; no general cleanup/recovery qualification is claimed.

The unchanged full four-test retirement suite also passes on this worker in
`factorybox-checkpoint-retirement-98f1642fb342416eab5847d880695670`.
Portable checks with vet, consumer compilation, Python checks and immutable
reference/overlay verification pass in
`factorybox-checks-d16ed43370d14442ab753922ed8f8aac`.

The build initially encounters a missing Docker bridge and then its stale DNS
address. A build-only resolver file is now recorded, hashed and mounted
read-only at each dependency RUN. The host resolver is unchanged. The failed
bridge, DNS and attempted read-only resolver-write builds remain archived.
The successful builds use host networking only for dependency retrieval and
retain the 3-CPU/3-GiB builder limits and the same dated APT inputs.

Production qualification remains false. The timer self-copy defect remains
in the public path. Next, request the proven hold in lifecycle restore/resume
and activate the saved continuation before thaw, under an independent exit
guard. Source capture also needs activation before source release. Reconcile
interrupted activation, preserve repeated capture and worker recovery, and
qualify pending signals, multiple threads and private-page lifetime. Capacity,
worker-replacement, full-consumer and full upstream suites have not all been
rerun on this optional-hold candidate.

## Activating native waits before source and restore release

Candidate `factorybox-build-worker-edd5cff5a26a4488b2fd11de136434ac`
integrates the verified backend hold into the delivered fork and resume paths.
Its unselected image is
`factorybox-worker-edd5cff5a26a4488b2fd11de136434ac@sha256:94c31a712db57280c16a6747baaf5fc822f3ac73b85bc4b9a2dd06077cd5a048`;
package SHA-256 is
`025808f13a2e54853f43dacadbf4e230700160a3b1d90385095378d1b9b83af8`.
It reuses the exact pinned Sysbox and CRIU components from the preceding hold
candidate. No host runtime, kernel, global configuration or sysctl changes are
needed. Production qualification remains false.

Ordinary restore/resume requests now require the sealed activation hold. The
controller verifies the original request hash, process birth, CRIU tracer and
frozen cgroup before acknowledging detach. It accepts the separate held receipt
only with the same paused runtime identity. A new capture guard pins that exact
restored task and record. A sealed activation child runs while every application
thread is ptrace-stopped, then returns the original group to its frozen,
untraced state. Only after activation succeeds does the controller thaw and
publish the ordinary completion receipt. An absent receipt still refuses a
second restore. Earlier persisted resume intents that name a different request
recipe remain refused; no silent rewrite or migration of archived inputs occurs.

Retained-source capture also activates before thaw. Its control descriptor now
lives for the complete capture worker. The independent guard can drain the first
preparer's exit watch and arm a second, distinct activation watch with separate
readiness, intent, completion and exit receipts. The second child is watched
before it can ptrace a source thread. An unconfirmed mutation remains held;
this change does not claim automatic repair of a partially interrupted activation.

The activation child replaces only the exact verified private RX page and frame.
The original application text, read-only request and syscall ABI remain intact.
It uses the same 323 native-helper bytes as the prior independent experiment,
SHA-256 `02f23238267d39195a106632b38e3b1c4ba58fa6d7d95adeae531e5ad553e15f`.
Their current source, compiler and extraction hashes and exact embedded-byte
comparison are retained in
`factorybox-timer-kernel-code-build-5a7261055d9c47fdbc115cdf65d4cc43`.
The child's syscall stops install the original signal mask at native relative
`clock_nanosleep` entry. A real kernel wait or its stopped kernel return is
confirmed before release. Interrupted native waits retain a verified restart
block and deadline, with measured namespace offsets and the existing 250-ms
bound. A deadline already elapsed at the clock-return stop restores the saved
application return context while the task remains stopped.

A recapture of an active private native wait verifies its exact code, frame,
syscall boundary and remainder pointer. It reuses those two pages and the
original saved application context. The first integrated candidate
`c72e64f64c4f45c79b054255ab671799` still rejects a newly queued caught signal at
that recapture boundary. Its failure and unresolved pause/delete refusal remain
in `factorybox-checkpoint-retirement-e9c8bde87ea942f48de90a9192a55a3e`.
The correction admits that signal only after every source thread is stopped and
the exact previously activated private wait has been verified. Its blocked staging
mask retains the signal until the next native syscall-entry handoff. Unidentified
application waits retain the conservative caught-signal refusal.

On the final package:

- `factorybox-checkpoint-retirement-4e8110f84c2e4994a1a16c83623f016b`
  passes the protected-remainder control, source checkpoint, fork and resume.
  The real one-second handler makes the dedicated remainder page read-only;
  every raw EINTR, register/flags/stack and read-only-request assertion remains.
  Differences from the original interruption-time estimate are 40,711 ns for
  the control and 410,173 to 528,136 ns for the three lifecycle cases. These
  are individual observations, not distribution or platform guarantees.
- `factorybox-checkpoint-retirement-864067ea35604aea9b0d782db0a24618`
  passes all four signal cases, including a signal queued after preparation
  and one queued during an actual frozen native-wait recapture.
- `factorybox-checkpoint-retirement-617e8a2a5ed04dcc8375ca4cacab9d35`
  passes the unchanged full four-test retirement suite, including the original
  direct-syscall NULL-remainder regression and the actor case.
- `factorybox-checkpoint-retirement-57301903c3474da49300796ed814fe42`
  passes both original one-second-handler interruption-time remainder cases.
- `factorybox-checkpoint-retirement-29b44797aa144931aae589448eeceaa5`
  preserves the native normal/SIGKILL tracer-exit hold proof. It exercises the
  shared watch implementation; it is not a partial activation repair proof.
- `factorybox-checkpoint-retirement-91172c0cd23f4ba0a5b88d429aef8494`
  passes all five raw backend hold cases again: fork, resume, their socket
  disconnects and a foreign acknowledgement. Ordinary deletion continues to
  refuse the exact unresolved negative records without changing their bytes.

The three signal/remainder fixtures now observe the verified private native
wait instead of ppoll. The protected-remainder suite additionally requires fork
and resume. Exact before/after hashes and reconstructed ordered patches are in
`factorybox-timer-activation-notes-e5db5e8880b4482097744cd35aadf8f2`.
Their synthetic applications and every existing ABI, copy-order, protection,
deadline and handler-result assertion remain. The original required timer test
is byte-identical. All final native runs use the same test executable,
SHA-256 `da68f4cf7447b0e329c7f36cb23929216e45879379c4ecd74c649712425bf5b8`.

The notes also reverify the original chat handoff, its eight human decisions,
twelve visible assistant updates and frozen document/evidence hashes. Closed,
identical immutable component inputs share storage with the verified receipt
`factorybox-storage-share-inputs-6bd3523ead1e46289e090e8bd1337374`, reclaiming
443,244,544 allocated bytes. Source, results, logs and selected inputs remain.
Closed native probe executables have complete verified decompression receipts.
Portable Go tests with vet, consumer compilation, Python checks and immutable
reference/overlay verification pass in
`factorybox-checks-5e6c6f46b23147af8a21f83714f59507`; final documentation diffs
also pass whitespace validation. Every final native run confirms owned outer
cleanup, unchanged source inputs and unchanged global sysctls.

The tested public-path copy-order defect is corrected. Broader signal states,
partial mutation repair, controller/tracer loss at each activation boundary,
multiple simultaneous native thread waits, completed private-page lifetime and
full-consumer repeated capture/recovery still require qualification. The current
two-page reuse covers an active recaptured wait; it does not prove bounded memory
over arbitrarily many different completed waits. Capacity, planned/abrupt worker
replacement, full consumers and required upstream suites must pass on final
delivered bytes. Epoch retirement and every remaining release gate stay open.

## Component and replacement gates after timer activation

The exact `edd5cff5a26a4488b2fd11de136434ac` package additionally passes:

- Planned bounded-worker removal and committed-pause recovery in
  `factorybox-bounded-loss-6196530fda0c4267a18c688966d780e8`.
- Actual SIGKILL/removal and the same recovery assertions in
  `factorybox-bounded-loss-3e40ffe4477b4fc7a95855c22347d8ec`.
  Both preserve a complete 32-MiB private-RAM pause, verify zero resident
  snapshot data and source anonymous memory, refuse a wrong hash, import into
  a new epoch, verify exact replay and independently fork a resumed descendant.
- Actual bounded guest ENOSPC and the full existing capacity assertions in
  `factorybox-bounded-store-62e7865e3c5d4ad9979f54f1e40d5c24`.

These runs verify exact cleanup, unchanged inputs and unchanged global sysctls.
They retain their original ceilings and same-host/same-boot scope. They do not
qualify full consumers or recovery after host restart.

The pinned Sysbox component also completes the full selected Go-module command
in `factorybox-upstream-dcdb017435b94b64a0ebcfdf56ef8f2d`: 722 passed cases,
comprising 399 top-level tests and 323 subtests, with 18 original skips and no
failures. The skips remain named in the result. They include cgroup v1,
systemd, shiftfs and optional integration cases; they are not counted as passes.
The native fixture runs offline after dependency preparation. A first attempt,
`factorybox-upstream-b359c23021cd43d9bf29292bbd367142`, times out before any
test starts. Read-only inspection identifies `go mod download` in sysbox-fs
using the stale `172.17.0.1` resolver. The separate preparation recipe now binds
the component build's immutable `build-resolv.conf` read-only at `/etc/resolv.conf`.
The retry finishes dependency preparation in 9.4 seconds. No component binary,
upstream assertion, frozen context or host resolver configuration changes.

`factorybox-criu-core-e22b2699bd3e4f60aa60720dabe05833` passes all 18 declared
unchanged ZDTM C cases, with two actual dump/restore cycles per case and their
original final assertions. The native fixture verifies 1,030 upstream test files,
4096-byte pages and unchanged zero global hugepages. The binary SHA-256 remains
`45ba3635a5f4da5becb3c49298e00e896d6c5366d1ca3be53e813d8306b4879c`.
This is the selected host-flavor core gate, not the complete ZDTM matrix.
The driver now accepts an explicit `--criu-base-lock`, restricted to retained
candidates. It verifies the same immutable old base SHA and complete unchanged
current CRIU recipe used by the worker build, freezes that lock and its own
runner, and records their hashes. The default strict provenance path remains.

The existing upstream Bats selections pass with the exact Sysbox component
bytes: seven executed lifecycle cases without skips in
`factorybox-bats-8f4d454610b24427b17cd683595dca6f`, then four executed systemd
cases and the original bionic/archlinux skips in
`factorybox-bats-7a3ebaba95e3477895096463a1b1e079`. Their existing explicit inner
Docker networkless profile, fixture compatibility changes and test bodies stay
unchanged. No bridge/NAT, complete umbrella matrix or skipped case is qualified.
Both runs verify unchanged components, inputs and observed global sysctls.

Portable checks, including vet and all immutable reference/overlay checks, pass
in `factorybox-checks-742767ed18b04840992287bbd4ffed37`. Exact ordered patches,
the failed preparation receipt, all current results and package provenance are
indexed in `factorybox-activation-gates-notes-f536ae18444e48369e4f946b96d31372`.
The production runtime source and selected pilot profile are unchanged during
this additional gate pass.

A review of the remaining error paths identifies a concrete gap. Capture stages
the timer before CRIU and filesystem capture. The new activation runs only when
`sysboxcapture.Capture` succeeds. Existing confirmed abort, orphan and delegated
cure paths can mark the capture safe and thaw the prepared source without that
activation. The current successful recovery tests use the Go actor and do not
establish the protected-remainder behavior in this failed-capture path. A new
native regression must combine real post-admission ENOSPC, confirmed rollback
and the same actual handler protection/ABI assertions. This remains a release
blocker; the preceding success results do not cover it.

## Native timer activation after confirmed capture abort

The preceding review identifies a real error. The new native regression uses
the existing protected-remainder assembly byte for byte, with an uncaptured
control and a separate captured source. Its controller lives on a private
128-MiB tmpfs. A 160-MiB nonzero file in the owned source makes the filesystem
copy reach actual ENOSPC after admission and completed process capture. The
test requires confirmed rollback and the same running source PID before sending
SIGUSR1. The original handler waits one second and protects its remainder page;
the assembly retains all original register, flags, stack, EINTR and deadline
assertions. The tolerance remains 250 ms.

`factorybox-storage-admission-eac13a264f8544c5892553b18ab5204d` fails on the
previous `edd5cff5a26a4488b2fd11de136434ac` package. Its uncaptured control
passes. The recovered source is still in syscall 271 before the signal and
returns raw -14, EFAULT, instead of -4, EINTR. The failed fixture, binary
failure record, source, logs and fully verified compressed volume are retained.
Exact cleanup, unchanged inputs and unchanged observed global sysctls pass.

Candidate `37dd732100cd45be93cc7a123ee39f1a` corrects the confirmed-abort
path. After the original process/network restoration proof, the caller starts
the same independently watched activation before publishing abort intent or
requesting release. A separate `timer-activation-worker-intent.json` identifies
the actual activation parent without overwriting the dead capture worker's
original evidence. Restored-task activation uses the same helper. An exact
completed activation receipt permits replay without another mutation.

Both SourceGuard completion acknowledgements, orphan completion and delegated
cure also refuse release if preparation or activation intent exists without
its completed, token-matching frozen/detached receipt. Invalid and redirected
receipts refuse. Ten portable cases cover this metadata rule; callers continue
to verify the original live kernel anchors separately. A lost controller or
guard with an unfinished timer mutation is held for reconciliation. This
change does not implement automatic repair of that unfinished mutation.

`factorybox-storage-admission-eaaf2fc2f71d44cba5ce1a62426817a1` passes the
same two native cases on the new package. The failed task SHA-256 is
`8942944968708d04b484748e343550a2c614bfc53c007cb2c915a0b2e22ade6c`;
the successful task SHA-256 is
`0fb25d2021941b423046668b92b46d2b75ce53c6be7b9f90240a5eca0a7b44dc`.
The six differing bytes are GCC's temporary object filename in the unloaded
ELF symbol string table. All loadable segments and the assembly are identical.
Assembly SHA-256 is
`13d87314ca5958839fef3d8eee3e90175dd63d803ab810d9edfa2a8a6a9d9cc3`.
The package SHA-256 is
`e6ef06d8b41a41bab9c632bf3d821eac7145514f32945793d6774620ca13a641`.
Sysbox, CRIU and native-base component identities remain unchanged.

All eleven preceding native gates pass again on this package: full retirement,
protected remainder, signal boundaries, delayed remainder, tracer-exit guard,
backend restore hold, bounded guest ENOSPC, planned/abrupt worker replacement,
storage admission and capture-worker loss under zero available space. Each
verifies cleanup, frozen inputs and observed global sysctls. The probe SHA-256
is `6611376ee6af27504317afa541c9af0b5b633bfdacf7d5f2fd144b2c4abaff2d`
throughout these runs and the positive abort regression. The first batch wrapper
mistakenly requests an absent metadata field after successful retirement and
stops; its failure remains recorded. The corrected wrapper compares the actual
before/after sysctl dictionaries and completes the remaining nine gates.

Portable checks including vet and reference/overlay manifests pass in
`factorybox-checks-bedf60f37e9e4cc2b1870ade6283c777`. Exact ordered source
patches, negative/positive native observations, both wrapper outcomes, build
provenance and retention receipts are verified and indexed in
`factorybox-timer-abort-notes-ba5e6e083bba490185420f3d0a2b1c12`.
The existing Sysbox/CRIU/Bats component gates remain applicable to their unchanged
component bytes; they do not replace final full-consumer qualification.

Production qualification remains false. Broader orphan/delegated timer holds,
partial preparation/activation repair, multiple native thread waits and completed
private-page lifetime require further native evidence. The regression exercises
the delivered native core through the probe API; it does not replace the full
consumer and endurance gates on final delivered bytes.

## Native orphan hold after timer preparation

`factorybox-checkpoint-retirement-976ff0a2a1ae41b089a2e4843d0fd1f6`
passes a delivered-CLI pause fault on the unchanged `37dd732100cd45be93cc7a123ee39f1a`
package. The original protected-remainder assembly is unchanged. The fixture
observes and stops the exact owned GNU cp child after timer preparation and
completed process capture, then stops the original guard and controller.
Actual SIGKILL ends the capture worker and controller. The isolated probe acts
as a child subreaper and reaps that exact worker before allowing the original
guard to decide. A live worker or zombie therefore cannot account for refusal.

The exact source receives real SIGUSR1, confirmed in its pending-signal mask.
The guard refuses with `timer mutation remains unconfirmed` and records
`original_source_held_for_capture_reconciliation`. The original source PID and
birth remain frozen, populated and untraced for 1,250 ms with no signal-handler
counter progress. No activation or safe-abort receipt appears. Deletion of the
unresolved pause refuses and leaves its record unchanged. Synthetic guard
receipts are copied before the unique outer worker and volume are removed;
there is no diagnostic thaw or claimed automatic timer repair.

The first fixture run, `factorybox-checkpoint-retirement-bf5a789d5eb7490399a62c23815fa834`,
hangs in the test's controller Wait because the stopped guard inherits stderr.
Read-only process observation shows the dead worker, stopped guard and stopped
test progress. The exact probe is terminated through a verified pidfd; the run
remains failed with exit 137 and its compressed volume intact. A two-second
WaitDelay in the test fixes this fixture deadlock. The first successful retry,
`factorybox-checkpoint-retirement-43f354e7b70245b889ff082609ebedef`, exposes
root-only permissions on copied synthetic JSON output during later compaction.
A separate receipt verifies unchanged file bytes while making only those
outputs readable. The final run above passes both assertions and automatic
verified compaction with the corrected output modes.

Portable checks with vet pass in
`factorybox-checks-901eba90690d43eab4cb541e7b5e536f`. All three attempts,
output-access maintenance, exact patches and unchanged runtime/component bytes
are indexed in `factorybox-timer-orphan-notes-77fc993985844b24af1e9a0695dd1311`.
This covers one completed-process/orphan decision boundary. Guard loss,
incomplete mutations, broader thread/signal states, private-page lifetime and
every other production gate remain open.

## Native guard loss with a surviving timer controller

`factorybox-checkpoint-retirement-3fb1172a48bc43dcbdc12c41d9d125ec` passes
the complementary delivered-CLI pause fault on the unchanged `37dd732100cd45be93cc7a123ee39f1a`
package. The source has its exact staged 574-byte continuation, completed timer
preparation and actual completed process capture. At the observed owned GNU cp
boundary the real guard and capture worker receive SIGKILL. The original
controller continues, reaps both children and reports the unconfirmed capture.

Its controller outcome preserves the exact original intent and records a frozen
source with capture still in flight. No retained-cure request exists at this
completed-process boundary, so delegated cure refuses with the actual ENOENT.
The source retains the same PID, birth, staged-code bytes and pending SIGUSR1
while frozen, populated and untraced for 1,250 ms. The signal handler makes no
progress. Repeating the exact pause refuses without changing the source record;
deletion also refuses and preserves that record. No activation, safe-abort or
ordinary guard completion receipt is manufactured. Synthetic evidence is copied
before exact outer teardown; no diagnostic thaw is used.

The new case shares the observed-copy fixture with the earlier Orphan case.
All original Orphan assertion calls and its complete fault/observation body are
preserved. Only setup paths and cleanup lifetime move into the helper, and Wait
is wrapped to retain its completion state. The original Orphan case passes
again in `factorybox-checkpoint-retirement-1526752d1c444792a1aa2eb344613c90`.
Both final native runs use probe SHA-256
`eff43c484eeca355ae119f209804e3ffc30fc63c461d7ccd616cbff7de6421de`.

The first portable attempt has a missing local fixture alias and an unused
import; it remains failed. The first native guard-loss attempt,
`factorybox-checkpoint-retirement-0a36a9fff67a4168b7127ec37573ec62`, fails
because the new fixture expects a filename in the raw ENOENT error. It records
`passed: false` and the actual `no such file or directory`; later assertions
remain unrun. The corrected oracle requires exact ENOENT and independently
requires the grant file to remain absent before and after the decision.
This is a fixture correction, not a runtime change.

Portable checks including vet and all reference/overlay checks pass in
`factorybox-checks-e6b774a543874cbab588283083dab8bf`. The failed attempts,
both final native results, unchanged original assertions, exact ordered patches
and preceding immutable-probe sharing receipt are verified in
`factorybox-timer-guard-loss-notes-df133f48e20842a3b4327b955e72be8e`.
Grant-in-flight/delegated timer recovery, partial mutation repair, broader
thread/signal states, completed-page lifetime and every other production gate
remain open. This proof establishes a safe hold at this declared boundary.

## First full minimal consumer on the bounded store

`tools/consumer_worker_loss.py --bounded-bytes` now runs the existing consumer
fixtures inside the delivered bounded store. It accepts an explicit whole-GiB
capacity from 4 through 32 GiB and verifies the actual store/image owner,
direct-I/O attachment, bounded device, independent host device and capacity
policy before and after the native probe. The successful source Start must use
the admitted bundle and rootfs under that store.

Only new owned fixture copies have their path literals relocated. The three
original Go test files and original setup script remain unchanged. The exact
before/after hashes, both ordinary and empty-features setup variants and patch
reconstruction are declared in `tests/compatibility/consumer-bounded-paths.json`
and frozen in `factorybox-consumer-bounded-fixture-inputs-aa12d887090b4a95b9f20047b075a66e`.
Every original assertion, the 420-second deadline and resource ceiling remains
in force. Six portable cases cover both reconstructions, changed/hardlinked or
closed inputs, exact capacity and source-placement refusals.

The first full minimal run,
`factorybox-consumer-worker-loss-d2cccba26a6f41a786881468dfc1bb7c`, executes
all eight original useful-work phases successfully in 55.75 seconds on the
unchanged `37dd732100cd45be93cc7a123ee39f1a` package. Its actual direct-I/O image
is 34,359,738,368 bytes; the filesystem reports 33,501,757,440 usable bytes and
available space decreases from 33,484,865,536 to 28,581,412,864 bytes. The store
and host devices are distinct. This is one functional observation, not a
performance distribution or full bounded-consumer qualification.

The overall result is failed: the worker does not exit successfully and its
final log reports `device or resource busy`. Its first archive also fails
because the 32-GiB loop image plus tar metadata exceeds the archive pipeline's
default 32-GiB decoded-stream observation bound. The old error incorrectly
names 64 GiB; the original failed result remains unchanged.

The independent stop minimizer,
`factorybox-bounded-base-stop-13bfb7df062040409deb49f7da6c0dc6`, reproduces
the failure with two sequential delivered workers and 512-MiB stores, without
a sandbox: the empty control exits 0; one public `prepare-base` on a single
text file makes Stop exit 1. The extra mount is the canonical read-only
self-bind identified by the completed base manifest. Both minimizer cases
are cleaned up, with the failed store completely archived and verified.
No production fix is selected by this result.

For retention, the archive pipeline accepts a caller-declared decoded-stream
bound, capped at 64 GiB. Only known bounded consumer stores select their image
size plus 1 GiB of tar allowance; the default remains 32 GiB. This changes no
physical artifact or host-free-space budget. The encoder still has its original
physical cap, and the complete stored compressed file is decoded, counted and
hashed against the original tar stream before volume removal is allowed.
Portable tests exercise default refusal, an explicit larger observation bound,
physical limits, corruption and archive-before-removal ordering. A candidate
raw-hole scan was rejected after an actual preallocated-file test showed that
GNU tar still included the allocated zero extents.

The exact stopped consumer volume is subsequently archived in independent
reconciliation `b363fd99aee146009a4098648245ae8d`. The complete
34,360,786,944-byte tar stream has SHA-256
`7f54acdc6f471c40f10a7cd29fee0a954b88023e4dbcf909f1ef1ea47f91f1cd`;
the stored compressed file is fully decoded and matched before removal of the
exact owned volume. That reconciliation passes cleanup while the original
native result remains failed with its original cleanup=false record intact.

`factorybox-storage-share-closed-inputs-e97bc649657d4b74872cc1d1646f6957`
fully verifies 570 compiled inputs and shares 292 identical closed-run inputs,
reclaiming 1,364,025,344 allocated bytes. Original results, logs, archives,
encoding receipts and input contents remain intact; original and shared mtimes
are recorded. No current unresolved failure or frozen source is removed.

Portable checks including vet and all immutable reference checks pass in
`factorybox-checks-1b2245e284e3423181a3cc3ad81546a8`. The fixture bridge,
native negative results, retention and ordered source patches are indexed in
`factorybox-bounded-consumer-notes-f11aa1b429604122afcad7bad16e89cc`.
Clean bounded-store shutdown and the complete Factory/Theia/agent and worker-loss
acceptance runs remain required. Production qualification remains false.

## Confirmed deletion before bounded worker exit

The bounded Stop candidate fences the old controller epoch under its operation
lock, withdraws readiness, and releases only verified canonical base mounts and
their completed checkpoint aliases. Every sandbox must already have confirmed
deletion, with its runtime and cgroup absent. Backing data remains intact.
Paused epochs and recovered base layouts still require a separate drain path.

The canonical-only build `6e9b9de9cd9b460e8a3d41bdc8d305b1` passes the seven
small Stop cases but fails the full consumer with EBUSY because checkpoint
aliases still cover the store. The next build `a9243efd79ac46e4a8e5ae32fbabb672`
also inventories those aliases, but its full consumer fails on ENOENT.
Read-only inspection of the fully verified stopped store confirms all four
published checkpoint manifests and their listed filenames. A first inspection
incorrectly reads the logical 32-GiB image into memory and fails; its corrected
streaming version verifies the whole image before and after read-only debugfs
inspection and removes only its temporary duplicate.

File-syscall tracing after all eight useful-work phases, in
`factorybox-consumer-worker-loss-799a935e8257433883e2931801e0e5c5`, identifies
the ENOENT precisely. Fork and resume timer activation leave additional guard-only
generations under `captures/`; these have no published `checkpoint.json`.
The new exit inventory incorrectly interprets them as checkpoints. The signed
pinned Ubuntu strace inputs are diagnostic files only. An initial DNS failure
and a Python 3.10 compatibility failure are retained; an independent small
container then reproduces the old helper failure and proves actual tracing
with the corrected helper. No trace tool is installed in the delivered worker.

The correction accepts only the exact guard-only generation layout, original
source identity and record hash, completed timer activation and matching
successful guard disarm. A real root-owned metadata regression first fails on
the original core, then passes all 13 cases on the corrected core, including
12 refusals for changed identities, uncertain completion or extra payload.
Published checkpoint RAM and filesystem metadata verification is retained.

Build `factorybox-build-worker-f4f3b99388ca476d9b2d18592bd033d6`, package
SHA-256 `3b89f000806cd99cc32dc414934a0db7a118bde01c8d65a223acf2d30d6d840e`,
passes the complete bounded minimal consumer in
`factorybox-consumer-worker-loss-febe899836204f8db27c7728b37d4486`, with
all eight phases, exit 0 and cleanup. The original workload test takes 38.59
seconds in this single observation. This is no performance distribution or
production qualification.

The seven Stop cases pass on those same package bytes in
`factorybox-bounded-base-stop-33ff7cd453d44c0ba210c00df5042417`, including
refusal for changed mount identity, a descendant mount, incomplete base/object
records and a held operation lock. Fresh workers confirm unchanged base bytes,
inode metadata and original base records. The full retirement suite passes in
`factorybox-checkpoint-retirement-3ba6a35b22f54d2896881d7da25ba9bc`: all four
original tests plus the new internal-guard regression. Its timer case remains
required, and completed checkpoint retirement is accepted by the new Stop path.
All six portable checks pass in
`factorybox-checks-6f6c6fa754b04a6aa43ef1614cf9815c`.

The ordered source changes, intermediate failures, diagnostic inputs and storage
maintenance receipts are indexed in
`factorybox-bounded-base-exit-notes-bb1b671b490b4a38a0457ec1d3694b2a`.
The subsequent full bounded Factory/Theia/OpenCode-fixture run,
`factorybox-consumer-worker-loss-c0d0e22228e4497a8898c2dbb4fdc265`, passes
all eight useful-work phases and retains its real external Theia connection
through checkpoint/forks/pause/resume in 132.84 seconds. Its overall result
still fails. Revalidating the larger saved RAM generations reaches the new
Stop path's internal five-second deadline during the branch checkpoint;
the final store unmount consequently reports EBUSY. The full stopped store is
retained and verified as tar SHA-256
`7fb9c231c7496d6b1e58f93a736c595f860c690906cc4615e0846891d1c75114`.
This run uses the local model fixture, not an external provider acceptance.
The 12-second candidate `factorybox-build-worker-023b284bde7c49408c9dcd4579342b69`
again passes every functional phase in 199.21 seconds, but reaches the same
deadline while verifying the final branch checkpoint in
`factorybox-consumer-worker-loss-c6d451d55cc342a4ba805831ecb7db64`.
Its measured Stop call takes 14.516 seconds and exits 1. The complete stopped
store is retained as tar SHA-256
`50597fd21c667bba6150a4aaa1264d3c0c4f21a24e327f5fd5f3d078892da056`.

The next candidate budgets 30 seconds for complete exit verification and up to
45 seconds for verification plus both service shutdowns. The consumer harness
caps that Stop call by the remaining original 420-second lifecycle budget.
No RAM verification, original assertion or resource ceiling is removed. This
Candidate `factorybox-build-worker-10aaf182ad584ceaabcd8411cdaadf82` is built
with package SHA-256
`322ef3e414afbacbf83ea9c874dce01e88827b33c584231c48295173c974d130`.
All six portable checks pass in
`factorybox-checks-96f1603b268545cfb2d20a306733fb09`.
The full bounded Factory/Theia/OpenCode-fixture lifecycle now passes in
`factorybox-consumer-worker-loss-c62a4d9796a94bdb97c59a4ad962b429`, including
all eight useful-work phases, the continuing external Theia connection,
worker exit 0 and complete cleanup. The original test takes 196.47 seconds;
the caller measures a 13.463-second Stop with 218.799 seconds left from the
original lifecycle deadline. These are individual observations, not a
performance distribution. All 13 native core regression gates pass on these
same package bytes in
`factorybox-bounded-exit-regressions-7f0c771b110c46f69403ad08bd5ab1b1`:
retirement, signal/remainder/protection, exit guard, restore hold, orphan and
guard loss, capacity, planned/abrupt actor worker replacement, admission and
controller loss. Every gate retains its original assertions, reports unchanged
inputs and global settings, and completes cleanup. Full-consumer replacement
with a prepared base remains a separate open qualification.

The seven Stop protection cases pass again on these same package bytes in
`factorybox-bounded-base-stop-26a1c0a3bfc342f88524328823abe51f`.
The complete bounded minimal consumer also passes again in
`factorybox-consumer-worker-loss-3c22440d8547430896c77a9b72e3f01c`, with
all eight original useful-work phases, worker exit 0 and cleanup. Its original
test takes 38.66 seconds in this individual observation.
Independent offline rebuild
`factorybox-build-worker-c12b953a38f347f694fe6b1f89c38e7b` reproduces the
same package SHA-256, image identity and complete image layer list. Both builds
retain their frozen sources and recipes; this does not qualify a reduced image
or a general platform/reliability claim.

After the passing complete bounded minimal replacement proof, four superseded
minimal store-image payloads are retired with complete stream verification,
member hashes and durable receipts. Their full images are intentionally no
longer reconstructable. Original sources, native results/logs, CLI manifests,
the diagnostic trace and selected build inputs stay intact. Both current
Factory deadline failures remain fully retained. The retirement receipts are
`factorybox-storage-retirement-7447b96b212740e198b493e4e27dca1d`,
`factorybox-storage-retirement-5170a9d9574b4b2c9877b363e5e41190`,
`factorybox-storage-retirement-394acaf0496c4365b825b9817e5fb324` and
`factorybox-storage-retirement-46dcb371a23e4e5c80960d7de95f3ab6`.

The remaining native regressions, bounded paused worker replacement, external
provider acceptance on the selected bytes and all other release gates remain
required.

## Bounded committed-pause drain and recovery

The complete minimal consumer with a prepared base initially confirms its
pause, then fails planned worker Stop in
`factorybox-consumer-worker-loss-71d2ec0da9c148a69c8a5056365f53b7`.
Candidate `10aaf182ad584ceaabcd8411cdaadf82` requires every source to be
deleted, while the source is correctly still paused. The recorded topology has
the canonical read-only base, its checkpoint alias and Sysbox's stopped-runtime
FUSE mount. The consumer already released its own source overlay and lower
alias. The complete 32-GiB image is retained, losslessly repacked in
`factorybox-storage-repack-0abc80b21faf4815a08f4905b9bd2f32` after full
raw-payload verification.

Intermediate candidate `af7bece39e1c4d098631df587ae504ea` stops that old
worker with exit 0 and recovers the actual consumer. All replacement assertions
pass, including exact replay, removal of old source files, another checkpoint
and fork, independent mutations and source deletion. The overall run
`factorybox-consumer-worker-loss-6056f81a5d7e4fcbae0dbb015c21363c` still
fails its final Stop. Its complete image and logs retain the cause: the newly
imported canonical base and two read-only aliases remain outside the ordinary
base registry.

Candidate `c2466d1f9df74c30be742170ac867948` passes the complete planned
bounded minimal worker replacement in
`factorybox-consumer-worker-loss-805b260a3a1e4d3d97ae935cda88fb1d`.
Its package SHA-256 is
`669b9270350b5804ce7b9415807d4c6252faba7a5e9580cd05f13f6eb33122b1`.
The old and replacement workers both exit 0, cleanup succeeds, and observed
global settings remain unchanged. This individual run takes 38.27 seconds for
preparation and 36.15 seconds for recovery; the respective Stop observations
are 1.62 and 0.88 seconds. These are not latency percentiles.

The exit fence now binds each complete, released, unresumed pause before
non-forced deletion of its exact stopped runtime and retirement of its empty
cgroup. The pause record and checkpoint remain unchanged. A dedicated recovery
reader can read that exact fenced selection after independently confirmed
worker fencing; ordinary old-epoch operations remain closed. Recovery input
bases are discovered only from complete, matching import and preparation
records. Their exact aliases are uncovered before their verified canonical
mounts. Partial imports and unknown objects refuse release. No backing is
deleted by this path, no caller source mount is unmounted and no force/lazy
unmount is used.

Portable checks pass in
`factorybox-checks-9fc1c0927c0d4b8eaaad27fe3a819921`. The first full native
retirement attempt on this candidate remains failed in
`factorybox-checkpoint-retirement-6da013b5e80a49edb30ba0b6f7195746` and
batch `factorybox-paused-epoch-regressions-c523cac908374730945224263c11d0dc`.
The four original tests and internal-guard regression pass, but the new fence
fixture's temporary controller lacks required mode 0700. The runtime correctly
rejects it. The isolated correction in
`factorybox-worker-exit-fence-metadata-0959a558f1f547a1b20cc0d0205c49e8`
exposes a second fixture error: its orphan-completion case still creates an
intent. The corrected isolated test passes all 14 cases in
`factorybox-worker-exit-fence-metadata-695753d32a7c4eb8b5c290bcac0e0f8f`.
Both ordered fixture corrections retain exact before/after and patch hashes;
every assertion remains and each negative case now requires its actual refusal
reason. Production code and its permissions checks are unchanged by these
fixture corrections. Cases cover interrupted intent, complete intent,
malformed/changed records, file rights, links and held locks. Synthetic metadata
cases alone do not qualify interruption of a live drain. Complete native
regression, abrupt/full Factory replacement and final delivered-byte
qualification remain required.

The package rebuilt with the finished fixture is
`factorybox-build-worker-c8578b8ae98a47f392bc050c79274346`, package SHA-256
`09fa2a3fb08e576fa76cdf8c6a136c839de919721e1856f928bf70de45beba13`.
Its CLI SHA-256 remains
`2c2f3887d54cc3c304d5e62440d284fba11eee31dd5719d0b589c6830e96d5dd`,
identical to `c2466d1f`; the packaging metadata binds the corrected test source.
All 13 native core regression gates pass on this package in
`factorybox-paused-epoch-regressions-8d6c41b5f4da48c8b37e6e056b9a6121`.
The full retirement gate
`factorybox-checkpoint-retirement-6f0b3e062e5647e2835d7ba202886725`
includes the four original tests and both guard/fence regressions.
All eight Stop protection cases pass in
`factorybox-bounded-base-stop-7e60812bb05446ad93808c9275f2903c`.
An earlier attempt, `factorybox-bounded-base-stop-49f3f79275864da29c0cdb17f998cc4b`,
passes every individual case but remains failed because the driver still
requires exactly seven results after adding the eighth case. The corrected
driver requires all eight; no original case or assertion is removed.

Independent offline build
`factorybox-build-worker-0d3f304edb76467283a158b0aea1a67a` reproduces the
same complete package, image identity and layers. The exact final package also
passes planned minimal replacement in
`factorybox-consumer-worker-loss-fe8c7e52724f41c4a4c5006b07336308`,
abrupt minimal replacement in
`factorybox-consumer-worker-loss-a07a4c9c7743423e9a82cd784e1f45a8`, and
all eight ordinary minimal lifecycle phases in
`factorybox-consumer-worker-loss-45c31f148afb48cabad190666ccc5969`.
Each passes exact cleanup and its expected worker exit. Final portable checks
pass in `factorybox-checks-ab1bc5f76a50442c90d02725ed315834`.
Full planned Factory/Theia/OpenCode-fixture replacement also passes in
`factorybox-consumer-worker-loss-91e53244af4049688b11dc6c94d7a7cb`.
The first consumer batch
`factorybox-paused-consumers-77525818087e4e48a87c007befea4cd4` remains
failed because its collector expects exactly one generic evidence message;
Factory emits both separate Theia admission and full-consumer evidence.
Continuation `factorybox-paused-consumers-72cb8cb207884bc4a22bfdb4c8444908`
selects only exact full-consumer artifact paths. It revalidates and pins the
four complete passing leaf results rather than rerunning unchanged cases.
All eight cases in that continuation now pass on the exact final package.
Abrupt Factory replacement is
`factorybox-consumer-worker-loss-1f5c57ffb12144c9b281dc60eb9a8e25`,
ordinary full Factory lifecycle is
`factorybox-consumer-worker-loss-105b6749f2544ae5ba61ff1cab8f1e13`,
minimal incomplete-pause refusal/recovery is
`factorybox-consumer-worker-loss-ddc2c441eda74439b8b79de07bdb46f1` and
the matching full Factory case is
`factorybox-consumer-worker-loss-6c11ef17483d4b3cbd4c6233d15722b2`.
Both incomplete pauses are refused twice, their earlier committed pause
metadata remains unchanged, and that earlier complete state is recovered
through the full original consumer assertions. Every case has expected worker
exits, cleanup and unchanged observed global settings. No fixture or assertion
changes for the collector fix. Factory uses its separate Theia peer and local
OpenCode model fixture throughout; this is not external-provider acceptance.
Actual interruption during a live exit fence, replacement with a different
loop assignment and large exit-ledger admission remain distinct checks. The
full production goal remains active and unqualified.

After the passing full bounded Factory lifecycle on `10aaf182`, the superseded
five- and twelve-second deadline failure images are intentionally retired with
complete decompression/member verification in
`factorybox-storage-retirement-d696554320e749f1b7405d5bb50028bf` and
`factorybox-storage-retirement-0a695bb908c743c28327110868608803`.
Their original source, results, logs, CLI manifests and selected inputs remain;
their complete images are no longer reconstructable. After the passing planned
minimal replacement, the superseded recovered-base failure image `6056f81a`
is also intentionally retired with complete stream verification in
`factorybox-storage-retirement-53d1589b40ee481da804f11e1dde5d54`.
Its original source, result, logs, CLI manifests and selected inputs stay intact;
its full image is no longer reconstructable. The selected initial paused-base
failure image `71d2ec0d` remains complete. Sharing byte-identical completed inputs
is recorded in
`factorybox-storage-share-closed-components-b3cb585d4367440aa05ce16c3945f518`.
The 32-GiB artifact budget and required 8-GiB starting headroom remain enforced.

## Retained store remounted on another loop device

The bounded consumer driver can now reserve the released old loop slot with a
separate empty owned worker and a 512-MiB store. The helper has the same
3 CPU/3 GiB ceiling, creates no sandbox, and checks exact container/volume
ownership before cleanup. The retained consumer image must remain identical
while its new loop number and block device both differ. The original consumer
assertions and 420-second lifecycle deadline remain unchanged.

On the previous qualified intermediate package `c8578b8a`, the complete minimal
planned case fails in
`factorybox-consumer-worker-loss-00f48559850c4e1ea7a56ebb0bf03ecb`.
The source pause and clean Stop pass; the helper holds loop 0, and the exact
same retained image attaches to loop 1. Recovery refuses with
`persisted base inode or metadata changed`. Both workers and the empty helper
are cleaned up and the complete failed 32-GiB image is initially retained.

Offline inspection
`factorybox-recovery-device-inspection-29143efd2f45471e9a4b618a8e7b1adb`
verifies the full archived tar and reconstructed sparse image. Read-only
`debugfs -c` confirms unchanged filesystem UUID, inode, mode, UID/GID, mtime and
ctime. Only the recorded kernel device changes from 1792 to 1793. The initial
inspection `factorybox-recovery-device-inspection-74ba170eca834da0a344035d975fe72d`
also verifies the entire tar/image but cannot open allocation bitmaps under its
resource limit; it remains failed. The corrected inspection uses the installed
manual's mode which omits those bitmaps and forces read-only access. Both
temporary copies are removed; canonical evidence and receipts stay unchanged.

Candidate `factorybox-build-worker-3d7bacfa1646433da4f0ce802fef80ac`, package
SHA-256 `5d75c9e1b43f3567966c69998c566d545403ac6949bb61c23a74ddf444c8f462`,
binds a changed-device base import to the persistent source worker envelope.
The source boot, epoch, recipe, store owner and complete image identity must
match the currently verified retained store. Only then can the exact committed
base's old device be translated. Inode, all metadata, covering-mount checks,
complete content verification, private independent copy and fresh read-only
identity remain required. Standalone recovery retains its strict device check.
The old envelope's process/peer information is never adopted and does not
establish worker fencing.

The 26-case source-identity unit test and all portable checks pass in
`factorybox-checks-fd8e28f690fc42b68a7b690541bbecc1`.
Both original and new native base tests pass in
`factorybox-base-recovery-b17c9c2073374627bec894a2c692b22e`; the new case runs
on the owned bounded ext4 store, retains default refusal, rejects an independent
verifier's refusal, preserves foreign-mount and corrupted-content rejection,
and proves the recovered copy survives original mutation/deletion.
The complete minimal planned case with loop 0 held now passes in
`factorybox-consumer-worker-loss-585c0ee025944ea8857a99ca0b9b2490`, including
replay, new checkpoint/fork, independent useful work, exact cleanup and both
clean worker exits. The independent build
`factorybox-build-worker-60942f30d86d4279b9bbc94ccbab13b5` reproduces package,
image and CLI exactly. All 13 native core regression gates pass in
`factorybox-device-recovery-regressions-53caaaf7efa042b5a6245ea5be6b9532`, and
all eight Stop protection cases pass in
`factorybox-bounded-base-stop-25bc622b5f4c4da8ad361672aa5bc742`.
The twelve-case batch
`factorybox-device-consumers-27d3a6e63927443dac6f0b153e7bfd40` passes all eight
original consumer cases plus minimal/Factory planned/abrupt forced-loop
replacement. Each helper verifies the exact retained image and changed device,
and ends with Exit 0 and exact owned cleanup. Factory preserves its separate
Theia and local OpenCode model/tool fixture; this does not qualify external
inference. No p95 or platform claim is made. The ordered proof index is
`factorybox-device-recovery-notes-84957ba0430f45e0810ff6a869735fe3`.
Live-drain interruption, large exit-ledger admission and the remaining production
requirements stay open. Production qualification stays false.

After all twelve consumer cases pass, the superseded opaque store image is
intentionally retired in
`factorybox-storage-retirement-6ab725eb0b084e5fae1ac6ddaa93beb0`.
Complete decompression and every member hash are verified before removing its
1,329,253,474-byte encoded payload. Source, results, logs, CLI inputs, the member
index and both offline inspections remain unchanged. The full old image is no
longer reconstructable. Selected unresolved failures and frozen inputs remain.

## Actual interruption after the worker exit fence

The optional `--drain-interrupt-inputs` consumer mode uses a frozen host observer
after the original prepare probe has passed. It checks the exact Docker-owned
host PID/birth, delivered CLI digest, live ready envelope, host boot and private
controller directory on the bounded device. Only an inotify event for the exact
committed-pause exit fence can trigger pidfd SIGKILL. After confirmed process
death, the unchanged intent must remain while completion is absent. The observer
closes every directory, mount, inotify and process handle and exits before a
replacement can start. No runtime hook or original assertion changes.

The pinned host Python lacks the high-level pidfd APIs. The first owned-child
preflight fails before any signal in
`factorybox-drain-watch-inputs-210267eb2f1d486aa43f8621357cf3ba`.
The corrected input
`factorybox-drain-watch-inputs-6fb5b0ca979c4d66846194fb1c930fb2` binds the Linux
AMD64 syscalls checked against the installed header. It rejects wrong birth,
wrong executable, stale process and invalid/init/group PIDs, and proves a
positive pidfd signal reaches only its own child. That is diagnostic preflight;
the full consumer separately establishes the actual drain boundary.

The complete minimal case passes in
`factorybox-consumer-worker-loss-199ba9bdd9aa436e93ae4dc8c43beee9`, and the
Factory/Theia/local-OpenCode case passes in
`factorybox-consumer-worker-loss-b7b04619f3a247c28f80b5e1cfc944d8`.
The source probes exit 0, their workers exit 137 from the selected fault, and
full recovery, exact replay, independent useful work and a later checkpoint/fork
pass. Recovery workers end with 0 and cleanup. The minimal combined case with
the released old loop slot held also passes in
`factorybox-consumer-worker-loss-ee014cf8addd4febaac0a1fd38c3f981`.
All six portable checks pass in
`factorybox-checks-178c1c30be494fe6a7828cc7157656d4`.
Runtime, Sysbox and CRIU bytes are unchanged from the independently reproduced
3d7 package. The final combined Factory case also passes in
`factorybox-consumer-worker-loss-4e8c91618e2f43c88c0f9a343cdbfe1e`.
All four cases are bound by
`factorybox-interrupted-drain-consumers-d128a70160f44b85a831aae4fddb5d50`.
The ordered index
`factorybox-interrupted-drain-notes-ffc74b3b9e414ac1bc8f0948c613462a` verifies
the exact observer/driver bytes and incomplete fence, both combined device
changes, the unchanged prior core acceptance and preserved chat context.
No power-loss, host-restart, migration or production qualification follows.

## Large exit-ledger admission

The public-API case
`factorybox-exit-ledger-e4281649832b494988c7108edc0c13ab` imports 540 real tiny
bases in an owned 512-MiB store. Valid longer canonical source paths enlarge
their ordinary base records without rewriting runtime metadata. The minimum
encoded ledger is 4,200,641 bytes, above the 4,194,304-byte reader/publisher
limit. The exact size refusal occurs before any canonical-base umount2. A
targeted supervisor trace also sees the later bounded-store Close fail with
EBUSY, proving the observer stayed active through cleanup. All 540 payloads,
inode/metadata and base intent/record hashes survive fresh worker replacement;
both fence files are absent, and the empty replacement stops with 0.

The narrow completion case
`factorybox-exit-ledger-83c2bfe08ac147a0baf6d85f2fa73b6a` remains failed.
Every base again comes from the public CLI. Its actual intent is 4,194,289 bytes,
only 15 bytes below the limit. The trace observes all 540 canonical unmounts and
a successful store unmount; then completion publication refuses its larger
record. Worker exit is 1, intent remains, completion is absent. All persistent
backing and base records remain unchanged. The complete failed store archive,
trace, source, original result and diagnostics are retained. This is a late
admission failure, not evidence of lost payload data.

The candidate checks a copy of the complete exit ledger with maximum-width UTC
RFC3339Nano completed_at before publishing the fence or releasing anything.
It uses the same encoded-size checker as the actual publisher. JSON encoding,
the original 4-MiB limit and all existing assertions stay unchanged. Portable
checks pass in `factorybox-checks-8ac506fd3f1149d9b1453201fbc54dcf`.
Worker `factorybox-build-worker-5c281a6c3d6d4f08808be45deb0b75f1` contains
this fix; the separate build
`factorybox-build-worker-231c41a288f648209bf1aecc88004cf5` reproduces its
package, image and layers exactly. The package SHA-256 is
`2b02541fcbf99b7babd70dfeb842390846ba53d6fb648806572bde77350e3b07`;
the CLI SHA-256 is
`74ee224a033a88bc557ebd0d32fd150eb8068b37ed3287fcd1d575f50b179073`.

The corrected narrow case
`factorybox-exit-ledger-9dcf6a311184455fb6ce2ea9b065a87a` again prepares
540 real bases. Its minimum ledger is 4,194,260 bytes; adding the maximum
request timestamp still leaves only 14 bytes below the limit. Completion
preflight refuses before either fence or any canonical-base unmount. The
oversized case
`factorybox-exit-ledger-b052c57114584ae4890c1c0864565c27` also passes on
these exact delivered bytes. Both require the later EBUSY store-close trace,
preserve every base's contents and identity through fresh worker replacement,
and finish with exact owned cleanup. All 13 native core regression gates,
eight Stop protection cases and both base-library tests pass on the new
package. All twelve full consumers pass in
`factorybox-ledger-consumers-bb5cc13ae07f4b1a8e7faa1d1ac1fb12`, including
the four forced-loop combinations, both incomplete-pause refusals and both
normal lifecycles. All four actual interrupted-drain cases also pass in
`factorybox-ledger-drain-consumers-65704437d09c4d97bc23e90d7ee10ed1`.
Every consumer case is newly run on the new package; no earlier package result
is substituted. Factory retains separate Theia and the local OpenCode fixture.
The ordered index
`factorybox-ledger-notes-cd1266fe9219423dbd9b5651d14e85fd` binds all seven
completed groups, both native size boundaries, the independent rebuild,
diagnostic reconstruction and exact implementation/documentation patches. It
fully decodes and verifies the preserved narrow counterexample archive and
rechecks the immutable reference and old-chat context. This is safe size
refusal; bounded admission of new work, epoch retirement and the complete
production goal remain open.

The diagnostic recipe preserves exact before/after hashes and a patch for its
copied umount2 filter and public-base scope label. Imported sources and frozen
diagnostic executables remain unchanged. No latency claim follows from tracing.

## SHM startup raw-clone failure under GC pressure

The read-only source inspection
`factorybox-startup-source-inspection-cc86e7c6141c4e959af935c7f911d814`
preserves the six matching source files and original small logs for the
historical `factorybox-devx-lifecycle-f7c02247159d4e13bf768da0ad1d39c5`.
Its filesystem log ends after FUSE dir and its process listing shows a wait.
The historical child identity/stack was not recorded. The initial raw-clone
hazard was a source hypothesis, not a captured historical mechanism.

The isolated diagnostic compiles the unchanged public SHM probe and every
original module test with the component's actual Go 1.25.14, using the retained
offline AMD64 builder. Vet stays enabled. All 32 plain calls pass in
`factorybox-shm-pressure-c84eae98dd0b46069ce1fa617357c134`.
With a declared private 32-MiB retained heap, one allocation/GC goroutine,
GOGC=1 and GOMEMLIMIT=64MiB, the first probe exceeds the original 60-second
limit in `factorybox-shm-pressure-cbe052c247664594866fe352ec1029f4`.
The parent waits for its exact namespace child. That child's captured Go stack
is in runtime.exitsyscall0/stopm immediately after the raw SYS_CLONE in
CreateUsernsProcess; its only live kernel thread waits at the matching futex.
Namespace IDs, births, executable identity, maps and task state are retained.
All diagnostic signals follow the already failed deadline and use exact pidfds.
The first diagnostic `factorybox-shm-pressure-55b7e8215bb84d2491a11183ca23dd82`
allowed a brief extra probe after the diagnostic child exit. It stays failed
and unchanged. The corrected helper publishes a failure marker before signals;
the test checks it before and after every call. The later failed proof retains
the same mechanism without that follow-up.

The exact selected Sysbox-fs binary, SHA-256
`bf5493e6afa11a4756efeba7c9ca3f8588cdcb86d3cb49568ff1be8910bc267d`,
then reproduces the same failure during its actual handler startup. With
GOGC=1 and a process-local 1-MiB Go soft limit,
`factorybox-startup-manager-c368af884a454642a1de455ba6fe23d0` fails its first
start after 60 seconds. A parent thread is in Wait4(child=33); the exact child
is parked in exitsyscall0/stopm after CreateUsernsProcess at linux.go:301,
through ShmSysctlUserNamespaced and handlerDB.go:148. Before diagnostic signals,
the log ends at the same FUSE-dir boundary as the historical failure. The
earlier three-second observation and final thread capture agree. The child
signal later unblocks startup into its unsupported-SHM fallback; readiness
remains failed and no later start is attempted. This proves the failure
mechanism on the exact binary, not the missing historical child's exact heap
or GC state. All sixteen starts with a 16-MiB soft limit pass in
`factorybox-startup-manager-6f36fbc3eb73434fae8dc5625c65ef2c`.

The existing unselected candidate replaces the raw-clone SHM callback with a
fresh executable through Go's normal fork/exec path. The empirical open-only
check, private user/mount/IPC mappings and three-second helper bound remain.
Its direct proofs `factorybox-shm-reexec-5f3724c56015453c98c10cdf2b22bd8a`
and `factorybox-shm-reexec-1ef30470d475488a82e5e4ca9d8fb5fb` both pass all
32 diagnostic calls, the separate 32-call public regression, unchanged original
module assertions and the mapping/shared-namespace refusal cases, without and
with the same GC pressure. Parent and global SHM values remain unchanged.
Copied diagnostics have exact ordered patches and reconstructed source hashes;
reference and imported overlays remain untouched. No timing inference follows.

`tools/build_sysbox.py --shm-startup-reexec` explicitly applies the declared
patch only to a newly copied private component context. Existing test files
must retain their hashes. New worker packages retain the exact patch and full
before/after source declaration; package loading binds both to its recipe.
The complete upstream runner requests the added native namespace cases and
requires their actual pass events. Component build
`factorybox-build-sysbox-a3d550a7cb8a4760aa41231b8c8c12fb` passes, and clean
independent build `factorybox-build-sysbox-83818bfe47184a5f9bb3f67e6ce95f75`
matches all three binaries, package inventory, full context and patch/source
declaration. The new daemon passes all sixteen starts under the original
one-MiB pressure conditions in
`factorybox-startup-manager-d764f91d4f1e4259b02aed9b7fe3988f`.
Worker `factorybox-build-worker-2a83453ad0f54b6a9580dc194bf5cdba` builds with
package SHA-256 `dc64fdbbffc5e2a3cc543b2548c3e474d564bad1006d62ab72222e66cce30b1d`.
Independent worker build
`factorybox-build-worker-0c7cbf383ea54d7b97541719b7f8c01f` matches the full package,
bundle and image. The complete upstream module run
`factorybox-upstream-662ad4adabc74a25b2895048dae22cf6` passes 727 cases, including
all five added native SHM cases, with the same 18 original skips.
The seven lifecycle Bats cases pass in
`factorybox-bats-fa352808c7c6428aafb43fa36aa9240a`; the four executed systemd
cases and original two skips are preserved in
`factorybox-bats-dd234e09657d459cb5493021e62ccc70`.
The complete ordinary minimal lifecycle passes in
`factorybox-consumer-worker-loss-27a074aa7ebd4fd7a8cb8eabe6c8a234`.

The next complete Factory case,
`factorybox-consumer-worker-loss-87d3d62a3d544fa8b2a0bba588c90b38`, fails at the
checkpoint of its independent branch. Timer admission refuses with
`relative timer has an unprocessed caught signal; source continuation remains
unconfirmed`. Its original 64.50-second native verdict remains failed; no
retry is substituted. Its full bounded-store archive has 34,360,786,944 decoded
bytes and SHA-256 `cb078327e4506209e6c3f1cc419d4bf758e1da589f2cee15c6387b3cea81900a`.
Exact outer cleanup, unchanged source and global sysctls pass. The serialized
component batch `factorybox-shm-component-gates-1d050c87d95b46b592232e2fa031b354`
remains failed with three passing steps and that failed fourth step. This is
an additional required lifecycle counterexample, not a successful package
integration or a demonstrated regression caused by SHM. The Go CLI is unchanged
from the preceding ledger-fix package. Its pending signal and source state need
inspection. Evidence is indexed in
`factorybox-shm-reexec-notes-7635fa90a366427484db37876b21a2ae`; a verified index
does not turn that failed integration into a pass. The original historical and
new failed results remain preserved. Production qualification is false.

## Pending caught signal before timer admission

The new native `TestNativeTimerAdmissionSignal` reduces the Factory failure
class to one static syscall/ABI actor and one real SIGUSR1. It queues the signal
through an exact pidfd after the original source cgroup reaches its first
checkpoint freeze, before timer preparation or CRIU. The existing signal
assembly and every original raw-EINTR, register, flags, stack and read-only
request assertion are retained. Successful checkpoint, exact replay and an
independent fork with one handler result are required.

`factorybox-checkpoint-retirement-aafd2e4cf6aa49f5ba18f46ec68f04c5` fails on
the delivered `2a83453ad0f54b6a9580dc194bf5cdba` worker in 1.28 seconds with the
same caught-signal admission error. Its actual proc status records SIGUSR1
shared-pending and caught, both mask 0x200, with no signal blocked. The source
cgroup is populated and thawed after refusal. The second preserved failure,
`factorybox-checkpoint-retirement-b90efd68208843aeb5f73bbddce505ff`, has the
same pending/caught state and thawed source on the earlier `5c281a6c...` worker
with the original Sysbox components and byte-identical Go CLI. The SHM change
is therefore unnecessary for this failure class. Both retain the complete
one-GiB private store and pass exact outer cleanup, unchanged inputs and global
sysctls. The full Factory failure's exact signal was not captured and is not
retroactively identified as SIGUSR1.

The candidate gives this specific pre-capture condition a bounded opportunity
to settle through ordinary source execution. Every new freeze repeats
descriptor, namespace, filesystem and timer admission. Only a typed pending
caught-signal refusal from timer admission can enter that path. The source must
be unprepared, have no completed timer plan and pass its existing original-task
and untraced/frozen-tree checks before release. A two-second settling window,
at most 32 inspections, backoff and the caller context bound the work. The
pending-signal check during actual timer mutation remains strict. A separate
private receipt records attempts and the first/last pending masks. Safe
pre-capture timer rejections also use the existing guarded replay path.

Portable checks pass all six gates in
`factorybox-checks-7d9d571fb97745d4afe9a035b11930af`. Candidate worker
`factorybox-build-worker-2d4ef2ecd68848979f86a6b8cf2c0f97` builds with Go CLI
SHA-256 `eb031e5f1f23ba24ebaa5979fe50a2973eec58a4c9f12a3ec26af75ab83b2094`.
Its native regression, independent reproduction, original timer/fault suites
and full Factory retry remain required. The original failures stay failed.

The separate native regression now passes on this candidate in
`factorybox-checkpoint-retirement-8b644cb24d294615831670cb311e25e0`.
Its first admission observes the actual pending SIGUSR1 mask 0x200; the
second admission succeeds. The original actor processes its handler exactly
once, retains its raw EINTR and ABI assertions, and preserves the result
through exact checkpoint replay and an independent fork. Source release,
exact outer cleanup, unchanged inputs and unchanged global sysctls pass.

The complete Factory lifecycle also passes in
`factorybox-consumer-worker-loss-ca69a93a251944cb9e4a393d35ddcc3c`, including
fork of fork, pause, resume, fork of the resumed source, the separate native
Theia connection and complete owned cleanup. The native test takes 192.12 s.
This single shared-host observation records prepared first useful work at
28.342 s, fork at 15.292 s, pause at 16.248 s and resume at 12.184 s. Preparation
and the separately completed source checkpoint are excluded from the
corresponding first-use values. It is not a matched performance comparison or
a latency distribution. This green run addresses the full pending-signal
counterexample; it neither explains the separate historical PID collision nor
replaces final package qualification. Independent reproduction and the
original timer/fault gates remain required.

All thirteen original serialized native regression gates now pass in
`factorybox-signal-admission-regressions-a7b9e441db764ece9fb50bf67fa5a04b`.
These cover checkpoint retirement; post-preparation signal, timer remainder and
protection; real tracer exit, restore hold, orphan/guard loss; bounded guest
capacity; planned and abrupt worker replacement; admission and controller loss.
Each step passes exact cleanup, unchanged inputs and unchanged global settings.
The new pre-admission signal proof above is additional to these original cases.
The separately defined full native filesystem-admission suite and final release
matrix remain required.

Independent build `factorybox-build-worker-a9de0a87414646118eff83338265c5ac`
reproduces the complete package, bundle and image exactly. Package SHA-256 is
`f5b815ee321c27b58c447a3d5512b08d87f6ea90bbc2e0e904ad1332255a6070`;
image identity is
`sha256:db9309a97fb850efc17994eebe39459169157b12e95e36f3804a7be26880818c`.
The separate proof index and ordered source reconstruction are
`factorybox-signal-admission-notes-584c2b7b4ed24c44b8649ef8e0865b16`.
Production qualification remains false.


## Native admission fixture coverage on ordinary worker bytes

The first renewed native-admission invocation,
`factorybox-runtime-32aa77839b4840c08b406d13ab752f68`, stops before any native
container starts: its older driver does not accept the retained CRIU build's
exact frozen base lock after unrelated Sysbox recipe changes. The driver now
accepts the same explicit frozen lock as the worker builder. The original
loader still verifies its exact hash, all CRIU inputs and equality of the full
current CRIU component recipe. The frozen lock is retained in the new run.

The next full run, `factorybox-runtime-2acafe42a6e5422f885556619db6e6a6`, passes
all seventeen tests and 78 subtests but remains failed. Its blanket adapter
invocation also discovers the newer `TestFilesystemBaseDeviceRecoveryNative`;
that test requires a separate bounded ext4 fixture and is skipped here.
This result is not reclassified as a pass. The original zero-skip requirement
and all original assertions remain unchanged.

The private native-script copy now explicitly selects all eleven original
adapter tests, with an ordered patch. Original script SHA-256
`598bfbbf547168fbfab2af17827deecddd06cb9c0ab49083938f1369208859b9`
becomes `1324326d46c4d1624ae5b9ee4320b736b0be83a871551709daab474730bc6b34`;
patch SHA-256 is
`29b99ca106fe7462c10ce803b4832a7391f0a0d21f21727e656c5740a2e38138`.
No original repository fixture or Go test is edited. The additional bounded
device test remains required in `tools/base_recovery_native.py` and passes,
together with its original strict base-recovery case, on the actual delivered
512-MiB ext4 worker in
`factorybox-base-recovery-55a2ee7163af4206ace8d53a944ee394`. Exact cleanup,
unchanged inputs and unchanged global sysctls pass there.

`tools/lifecycle_native.py --ordinary-build`, with the explicit AMD64/native
base and frozen CRIU lock, now passes the complete unchanged admission suite
in `factorybox-runtime-f9fc7488833a48fe9ce36ca80c9d3341`: seventeen tests,
78 subtests, zero skips and complete owned cleanup. Its CLI SHA-256 is
`eb031e5f1f23ba24ebaa5979fe50a2973eec58a4c9f12a3ec26af75ab83b2094`,
byte-identical to the independently reproduced worker above. Both foreign-proc
cases and their original immediate unmount assertions pass. The historical
busy-overlay failure is still unexplained; one green full suite is not a fix.
All six portable gates pass after these driver changes in
`factorybox-checks-e97c9757747343c98f9fd11304683095`.

The unchanged transient foreign-proc-directory case then passes twenty fixed
diagnostic repetitions in
`factorybox-proc-cleanup-diagnostic-7952e9bb33dc4bd78125a0515d93f61a`.
The first-failure stop flag is set before the run. Every original assertion
and immediate overlay unmount is retained. The frozen ordinary test binary
is reconstructed into the new artifact and verified in full, without writing
into its closed source artifact. A separate ordered shell patch narrows only
the selected case and sets the repetition count. The eleven original adapter
cases still execute.

The next declared diagnostic changes only the test executable's GC settings
and those inherited by its short-lived CLI children: GOGC=off and a 1-GiB soft
limit. Sysbox daemons retain their original environment, the actor retains its
explicit environment and the outer 3-CPU/3-GiB ceiling remains.
`factorybox-proc-cleanup-gc-off-0eba53968ae14ecfbc331d8ad7c8c3ab` also passes
all twenty original cases. Both diagnostics pass exact cleanup, input checks
and unchanged global sysctls. Neither reproduces the historical busy mount or
identifies its owner, so no cleanup repair or release qualification is claimed.

The component-control diagnostic
`factorybox-proc-cleanup-old-sysbox-99ad111a9fcd4d838c4b53602d83b0ee`
also passes the same twenty original cases. It substitutes only the verified
old Sysbox component set from worker `5c281a6c...`, including the matching
runtime digest. CRIU, the ordinary Go CLI, the test binary, GC settings and
resource limits remain unchanged. Exact cleanup, all pinned input bytes and
global sysctls pass. Sixty targeted observations have not reproduced the old
unmount failure; neither a component regression nor a repair is inferred.

## Reduced live-PID-2 restore topology diagnostic

The original collision's selected `core-4463.img` identifies an alive `sleep`
task, not a zombie, with own-namespace PID 2. Its process-tree image places it
under nested init 1328, after a later exec-root process with own PID 132.
The preserved four selected images remain the authoritative source; the
conflicting restore occupant is still unknown.

Private diagnostic `factorybox-pid-collision-topology-75012c51151542e7a983a3b6f300bb44`
adds a new test to a copy of the ordinary worker source, leaving every existing
Go file unchanged. Its first setup fails before any checkpoint because the
diagnostic reads only the direct cgroup and assumes an absolute PID-namespace
depth. Its complete 47,185,920-byte source archive is preserved. The next
ordered version, `factorybox-pid-collision-topology-c3cee4769ba6451baac19955b11df6ff`,
uses the production bounded recursive cgroup reader and records actual namespace
vectors and namespace identities. It observes a nested init and its live PID 2.

That second setup stops before any checkpoint for a separate harness error.
CombinedOutput waits for a detached actor's inherited output pipe even after
the runc command exits. The retained `detached-exec-pipes.json` identifies the
same pipe in the test reader and both the nsenter parent and actor writers.
The exact owned fixture is deliberately stopped, with its ownership/stop receipt
and complete source archive retained. Generated initial-status output ownership
is transferred to the invoking user with its own receipt; no input bytes change.
The next fresh version writes runc startup output to a regular file and redirects
the actual workload's standard descriptors to its own `/dev/null`, as the
original native source fixture does. Its generated JSON is readable by the
driver. These setup failures are not the historical PID collision and do not
qualify any restore correction.

The next version, `factorybox-pid-collision-topology-12853f91f2b547d1806b7378e7defc3b`,
reaches a real checkpoint and restore. Its first restore fails before resume
with `Can't find a session leader for 163`, which differs from the historical
EEXIST. The exact selected log and process-tree image are retained in
`factorybox-failure-inspection-2535c54a71ca43468dd6bc6e3831581e`, after full
archive decompression verification. Its runc-exec helper is outside the captured
root ancestry. This is a diagnostic topology mismatch, not the original failure.

`factorybox-pid-collision-topology-c668d7b2d2844cba9fc451bf74d39cbf`
instead starts both helpers below the source root and establishes real inner
init and actor sessions. It passes the complete reduced lifecycle in 18.81 s,
including private 32-MiB actor RAM, original deletion, fork of fork,
pause/resume and fork of the resumed source. The actual inner actor PID is 132,
alongside live sleep PID 2. No production Go source or existing test changes.

The next control reproduces the saved child-list order 132-before-2 by using
ordinary clone3 set_tid inside the fixture's own nested PID namespace. The
single-threaded C init holds the first child until live PID 2 exists, then
releases the unchanged Go actor. It writes no PID counters or sysctls. The
actual kernel child order, sessions, private RAM, source hashes and exact
owned cleanup are checked. This full reduced lifecycle passes in 16.16 s in
`factorybox-pid-collision-order-453605e743f94468944557244ed3a6a3`.
All ten predetermined serial cases then pass in
`factorybox-pid-collision-order-308659aacd1f4bd89289bb15e6b96dba`, with separate
proofs and unique source owners, zero skips, unchanged inputs and global
sysctls, and first-failure stop enabled. These are targeted diagnostic cases;
they do not replace either example's final regular repetition series.

`factorybox-pid-proc-topology-13b652ae57394f4f99acb868489010a7`
adds only a private mount namespace and a real proc mount belonging to that
exact inner PID namespace. Both kernel namespace checks and the same complete
reduced lifecycle pass in 16.69 s. Neither child order alone nor this one
private-proc observation reproduces the old collision.

`factorybox-pid-proc-children-a484607870d34fe88604563a179dc3cc`
adds one multithreaded child through the unchanged original actor's
`make-memory-child` route. Each actor has independently checked private 32-MiB
RAM, including after source changes and deletion, fork of fork, pause/resume
and fork of the resumed source. The same controlled child order and real inner
proc mount remain. This complete reduced lifecycle passes in 18.85 s with zero
skips, exact cleanup and unchanged inputs/global sysctls. A second actor alone
does not reproduce the old PID collision; this diagnostic is not a repair or
one of the final regular consumer repetitions.

The original failure archive is inspected again for a concrete missing fact:
the four inner process groups' saved process/thread PID requests. Full
decompression verification in
`factorybox-failure-inspection-070e2c70aee946e48e69ca2f9be672b8`
retains only 25 bounded metadata files. Their separate parsed analysis,
`factorybox-pid-image-analysis-f5635ece19a240268fc6a1e52f9e781c`, checks all
21 core images from groups 1328, 4452, 4453 and 4463 against their exact hashes.
All four namespace IDs are 24 and these selected saved own-PID/TID requests
are unique. The sleep request is alive PID 2. This does not identify a live
restore-time occupant or prove anything about other retained references.
All 2,224 files of the original and current CRIU source contexts are also
compared: only `criu/cr-restore.c` differs, by the bounded read-only EEXIST
logging patch. No intervening CRIU repair is inferred. The historical PID
collision and busy-overlay defect remain open.

The saved complete 680-byte process-tree image also establishes a job-group
difference missing from the earlier control: child 4453 has its own group 4453
inside parent session 4452, mapping to inner child PID/group 144 and session
132. `historical-sessions.json` retains all sixteen parsed entries and exact
source/parser hashes without expanding the original archive again.

`factorybox-pid-child-pgrp-6dc199348f1d484b9fc992abef8a1492` adds only that
own-group relationship to the reduced test. Its private copy of the original
actor inserts `Setpgid: true` before the existing memory-child start; every
original line and assertion remains. The original actor file itself is unchanged.
The private fixture SHA-256 changes from
`25091444a47038125cab13a1989e84557b3097997e0d17daa2afd1ca0f053667`
to `ce833d469b157f38d8ee6ff1a089c6d69010dcd250f3f16f1ba723a48fd76bed`;
ordered patch SHA-256 is
`72735ecd0b4b4c9072412b123a2c5b037a42633c2214d6820976d2ec9e35407a`.
The real kernel reports child PID/group 17 and session 132. The complete reduced
lifecycle passes in 18.46 s, including both independent 32-MiB actor states.

`factorybox-pid-high-thread-range-4b59ed39086142bd912afc5a1c8ba599` keeps that
fixture and changes only the allocation range: ordinary temporary fork/reap
advances the owned inner namespace through PID 137 before actor 132 starts.
Every temporary child is reaped before capture. No PID counter or sysctl is
written. The actual child now has PID/group 143, above its parent 132, in session
132. This complete reduced lifecycle also passes, in 17.06 s, with zero skips,
unchanged inputs/global sysctls and exact cleanup. It does not claim the original
exact PID 144 or complete Factory process topology.

`factorybox-pid-group-notes-3c222caa641e407593015e598c594ef5` verifies both
results, the original bounded image, the declared actor-fixture change and exact
reconstruction of every ordered diagnostic patch. The production Go sources,
ordinary CLI and CRIU stay unchanged in both controls. These two negative
observations still do not identify the historical restore occupant or qualify
a PID correction. The PID collision and busy-overlay defect remain open.

## Canonical base admission before exit metadata exhaustion

The preserved 540-base failure shows that safe refusal at worker exit is too
late for normal admission. The new bounded-store policy assigns canonical base
records at most 2,097,152 encoded bytes, half the unchanged 4-MiB exit-ledger
reader/publisher limit. It does not allocate filesystem blocks or claim to bound
the remaining checkpoint, fork, recovery or pause metadata.

The new public-CLI regression first fails on the preceding delivered worker
`2d4ef2ecd68848979f86a6b8cf2c0f97` in
`factorybox-base-admission-d8d44e664cc34726be42766cdeba467e`.
It accepts 270 real tiny bases, using 2,102,383 bytes of compact base entries.
That counterexample exceeds the newly declared admission policy; the earlier
540-base artifacts separately preserve the original exit-size failure.
The complete 537,919,488-byte failed-store archive is retained and verified,
SHA-256 `faeef55ce97713654d826d8838faa7b2ce39a35b121e9158bdbd205d8ef5fcfd`.
The driver and original result remain unchanged.

The candidate checks existing complete base records plus a prospective record
before creating any new directory, intent, copied payload or canonical mount.
The prospective record uses the actual paths and maximum JSON numeric and UTC
timestamp widths. A partial or changed existing import blocks new imports.
Completed immutable replays still run their original complete verification.
Unbounded explicit-controller fixtures retain their existing behavior; the
delivered bounded-store profile always enables the new policy.

Worker `factorybox-build-worker-ef7dcbe22aae4169a4642bb8df935f53` passes this
same regression in `factorybox-base-admission-de59cb25360a4bf090703a81d11ff396`.
It retains 269 bases using 2,094,871 bytes; the 270th import's conservative
8,079-byte bound refuses before any intent. The first and last complete base
replay exactly at this boundary. All prior payloads and metadata stay unchanged.
The source worker stops with exit 0, independently starts a fresh epoch, verifies
all 269 bases and both complete exit ledgers, then stops with exit 0 again.
Both ledgers remain below their unchanged 4-MiB limit. Owned cleanup, unchanged
inputs and unchanged global sysctls pass.

The CLI SHA-256 is
`c6ccfebf32e7cf9de6d53c764eb7ec434607b6fafb706f011032aba166bea74f`;
package SHA-256 is
`3aca6a495a0152a52b01478d1067a12b25281f259a181793df93d4d2b347feec`.
Separate build `factorybox-build-worker-0d90e0cd8142498aa6f5d798c16252db`
reproduces the package and image exactly. All six portable gates pass in
`factorybox-checks-f6044a26b0234cc38516beec6bfb65ec`, including the unchanged
reference/imported-fixture checks with vet enabled.

All thirteen existing native regression groups also pass on these exact worker
bytes in `factorybox-base-admission-regressions-44e42f4f65114436ba3601a65c5895fd`:
checkpoint retirement and immutable aliases; the original signal, remainder,
protection, exit-guard, restore-hold, orphan and guard-loss cases; actual bounded
capacity/guest ENOSPC; planned and abrupt worker loss; storage admission and
controller loss. Every original assertion remains, including exact owned cleanup,
unchanged frozen inputs and unchanged global sysctls.

The complete Factory lifecycle with separate Theia and the local OpenCode model
fixture also passes on this package in
`factorybox-consumer-worker-loss-21efb535c9f74b168f9f0e961b2ba5fd`.
All RAM/filesystem independence, source deletion, fork of fork, pause/resume and
fork of the resumed source assertions remain, with clean Stop and exact cleanup.
This one development observation is not a final regular repetition or an
external-provider test. The ordered proof and source reconstruction are indexed
in `factorybox-base-admission-notes-5287e67f04804fef81586bba7427f4d9`.

The documented manual response is to finish or commit pauses, drain normally,
require clean exit and replace the worker in a fresh epoch on the same owned
store. This resets the new epoch's metadata budget while retaining old backing.
It does not reclaim old physical storage. Other new-work budgets, verified epoch
retirement, the two unexplained historical defects and final-package acceptance
remain open. Production qualification remains false.

## Predetermined complete Factory PID diagnostic sequence

`tools/restore_collision_probe.py` now accepts an explicit frozen CRIU base
lock, verified already-tested consumer helpers and the bounded-store setting.
Its unchanged CRIU loader still requires the complete current CRIU recipe to
match that frozen base. The lock and runner are copied and hashed in the new
sequence. The original one-to-ten bound, first-failure stop, all full lifecycle
assertions and independent owned resources remain. The result additionally
checks the delivered CLI and requested store capacity. No production runtime
or CRIU behavior changes.

`factorybox-build-consumer-tests-7e4500546355429997281f677807f25d` freezes the
exact helper bytes of complete passing native proof
`factorybox-consumer-worker-loss-21efb535c9f74b168f9f0e961b2ba5fd` for worker
`ef7dcbe22aae4169a4642bb8df935f53`. Compressed helpers are fully decoded and
hash-verified into this new immutable build. Each future case verifies the
complete original source tree and shares only these read-only inputs; workload
stores and processes remain independent.

Sequence `factorybox-restore-collision-probe-f112407045fc455fa559fd7cf54b454f`
declares ten full Factory lifecycles before its first case. It uses the same
32-GiB bounded worker, separate Theia, local OpenCode fixture and read-only CRIU
collision diagnostics. The 3-CPU/3-GiB worker, 1-CPU/768-MiB Theia, two-sandbox
limit and 420-second native lifecycle deadline remain. It stops on the first
failed case and retains that failure; successful cases do not identify the
historical occupant or qualify a repair. This is a diagnostic sequence, not the
final regular acceptance series or a latency comparison. All ten cases now
pass with cleanup, including the first three:
`factorybox-consumer-worker-loss-59c7b2a7f6fd42cabea6cdbcd2ecab5c`,
`factorybox-consumer-worker-loss-5186cc6c2bfa418aa4c977d5ce8ca0fc` and
`factorybox-consumer-worker-loss-9f2aab9132f84c8c839f7f564a69a43b`.
The sequence terminates with exit zero; its result/rows identify all ten cases.
All six portable gates pass for the driver change in
`factorybox-checks-4271fe4b28d141b487b270ba9c5914c8`.

Offline inspection
`factorybox-failure-inspection-164efc81407543f4bce00de2221e8ebf` verifies all
12,155,092,992 decoded bytes of the original collision archive again and retains
only `ids-1169.img` and `core-1169.img`. They identify an alive multithreaded
`MainThread` init with own TID 1 in PID namespace image 17. The known sh init
1328 has own TID 1 in namespace image 24. Thus the original has two distinct
inner PID namespaces, a feature absent from the previous reduced control.
`factorybox-pid-sibling-namespace-fc8fb432e79f46ef9033f2c20a9c29a9` contains a
private extension prepared before the sequence ends. It runs only after the
complete sequence terminates. Its first execution fails before capture because
the sibling init has five threads but the exact own-TID-2 precondition is false.
This is a setup failure, not the historical restore EEXIST. Its complete
48,234,496-byte failed-store archive is preserved and fully decoded/verified,
SHA-256 `f99e2c52da224c1253f6499e1d60e2e69f7755a1ce3d369c5830c25981d78b6b`.

The new private control
`factorybox-pid-sibling-before-proc-aa3a349cc0bd4994b07d630a23480e44`
creates and synchronizes the four auxiliary threads before the proc mount.
Their own `gettid` reports are exactly 2, 3, 4 and 5, corroborated by worker-side
per-thread status. The original control's exact TID-2 precondition and all other
assertions remain. Its complete independent 32-MiB actor lifecycle passes in
18.45 s, including fork of the resumed source, no skips, exact cleanup and
unchanged inputs/global sysctls. No production Go or CRIU behavior changes.

`factorybox-pid-sequence-notes-6fcda25c61c84018ae57c2a147fc01b8` checks all ten
full results against the same frozen CLI, CRIU, worker image, core/test source,
consumer and local-agent inputs. It checks every complete observation and pause
receipt, fully verifies the failed sibling archive, and reconstructs the exact
ordered sibling-source patch. Reference verification still checks 232 files.
Both controls are negative observations: neither explains or fixes the original
rare restore conflict, and neither replaces the final regular acceptance series.

Before this sequence, maintenance
`factorybox-storage-compact-pid-tests-f897c0dc608b4e8f8d58286a33dda4c4`
reclaims 59,478,016 allocated bytes by completely decoding/verifying seven
closed generated test executables before replacing their raw encodings.
All source, original results/logs, selected unresolved failures and reconstruction
receipts remain. The initial-release goal and both unexplained failures remain
open; production qualification is false.

After both sibling outcomes close, maintenance
`factorybox-storage-compact-closed-pid-b938ba737fb348609b55d217ecf1a071`
reclaims another 42,496,000 allocated bytes by fully verifying/decompressing
five explicitly selected closed private test executables before removing their
raw copies. It also fully decodes/verifies all four corresponding failed-store
archives and preserves them. Native results, source, logs, current unresolved
failures and selected runtime inputs remain unchanged; reconstruction receipts
retain every executable's original bytes. No native worker overlaps maintenance.

## Shipped agent interruption source inspection

`factorybox-agent-cancel-source-b5c4814456124ffdadf7caeb4f8089dd` verifies eight
small source files from the exact sealed consumer rootfs. Its four chained
receipts pin every file and the 1,801,467,904-byte input archive. This is bounded
read-only inspection, with no consumer image or active native fixture changes.
Its successful result explicitly has `native_executed: false`.

The shipped OpenCode 2.0.27 protocol exposes POST
`/api/session/:sessionID/interrupt`, with optional boolean `resume`. The actual
session operation delegates through an uninterruptible effect to execution,
which defaults the reason to `user`. Settling that interruption calls
`jobs.cancel(sessionID)` unless the reason is `shutdown`. Source inspection
therefore distinguishes user cancellation from server shutdown, but proves
neither a tool's actual exit nor absence of an earlier external effect.

The original held-tool client has no interrupt operation. The previous external
disconnect proof loses a completed provider response after tool completion;
it does not cover interruption of a running tool or partial tool-call output.
`findings.md` records those precise gaps and the native cases still required,
including cancellation through pause/replacement and an effect counter outside
the captured environment. That source inspection alone makes no native
cancellation/error qualification claim.

## Acknowledged agent cancellation through worker replacement

The additive `agent-cancel` probe uses the actual shipped OpenCode 2.0.27 API,
POST `/api/session/:sessionID/interrupt?resume=false`. A private copy of the
local model changes only its held command to a launch-counting wrapper around
the unchanged 16-MiB held tool. Every original model assertion remains. Exact
before/after hashes and reconstructable patches declare this change and the
copied setup's selection of the additional native test. The original model,
ordinary held-tool client and existing native assertions remain unchanged.

The first native attempt,
`factorybox-consumer-worker-loss-20eb6232b8d946349f3bdadaa831b19e`, reaches actual
tool interruption and exit, then fails the new fixture's incorrect assumption
that the error is a string. Inspection of the exact exported schema in
`factorybox-agent-cancel-schema-5d83e3037ecc4cecbbb82adce4760a4c` confirms an
error object with `type` and `message`. The corrected fixture also requires the
durable session outcome `interrupted`. The original failed result stays intact.
Its complete 34,360,786,944-byte failed-store stream was fully verified,
SHA-256 `bf9e3a7691fb65b682756927535d5097e951d954dceddb4445a22eb315a80148`.
Lossless repack `factorybox-storage-repack-3ae270ef71874e7a905d164d87b74881`
verifies both complete decoded streams and reclaims 824,267,368 encoded bytes.
After both complete planned/abrupt replacement proofs pass, qualified retirement
`factorybox-storage-retirement-4002510a2b2d499da2309fe1734eb975` again verifies
that complete stream and retires its 641,382,782-byte encoding. Host observations,
source, results, logs and outer-archive member metadata/hashes remain. The old
mutable filesystem is intentionally no longer reconstructable. The separate
later ESRCH failure's retention and subsequent qualified retirement are
described below.
The repacker now hashes large compressed files as streams instead of loading
an entire archive into memory; its receipt/decompression guards remain.

The next attempt,
`factorybox-consumer-worker-loss-cef1d7f3efe746979730cb2817f0298e`, passes actual
cancellation and committed pause, but its new driver initially schedules only
preparation. Its original `passed: true` is preserved as a historical partial
result and is explicitly rejected as worker-recovery evidence. Regression
`factorybox-agent-cancel-phase-red-01fda6f814b84ea39879393a08f15c24` catches the
missing second phase. Probe choices and execution now use one explicit phase
plan, and final cancellation admission independently requires both completed,
closed workers, different epochs, identical cancellation data, recovery replay
and an independent descendant after source deletion. All six portable gates,
including the new missing-phase/changed-epoch/changed-state regressions, pass in
`factorybox-checks-97213398c5e241e38a4437a073fd4574`.

The corrected complete native cases both pass on worker
`ef7dcbe22aae4169a4642bb8df935f53`:

- Planned replacement:
  `factorybox-consumer-worker-loss-a8cba0f395194a0ba940599aaeca0d30`.
- Abrupt replacement:
  `factorybox-consumer-worker-loss-1c3fa52e05f84c908945c0c13c553c3c`.

Each confirms the original tool's exit and one launch, durable
`aborted: Tool execution interrupted`, an idle repeat with no execution,
committed pause with zero source anonymous and resident snapshot bytes, and
the same cancellation after independent recovery in a new worker epoch.
Recovery replay preserves its target. A new checkpoint/fork retains the
cancellation while the application changes independently; deleting the
recovered source leaves its descendant useful with one tool launch. The old
worker exits 0 or, at the deliberate abrupt boundary, 137. Both cases close
all owned resources and preserve global sysctls. They use the same exact
native test executable, SHA-256
`bc574e92d9bc01c46005473d4b5f1ab0f114428d6c0a3c35709266f891f57048`.

`factorybox-agent-cancel-notes-f7c07e35c6764827b0b90c31e20b3c36` binds both full
results, reconstructed fixture patches, verified encoded native inputs and
complete first-failure archive; it checks the historical partial result is
refused and reference verification still checks all 232 files. These are
specific local-model cancellation observations. The next section adds explicit
new user prompts after cancellation. Provider errors/partial tool-call output
and uncertain external effects with a counter outside the captured environment remain required.
The final package's external gateway/model proof and all other first-release
gates also remain open. Production qualification is false.

## Explicit new agent prompts after acknowledged cancellation

The additional cancellation model reply accepts only an explicit new prompt
with a fresh 128-bit token and the actual original tool-call ID/error. The
ordinary model's result and duplicate-tool assertions remain unchanged in an
ordered private patch. The client submits each mutating prompt once, requires
its own durable user message, matching assistant answer and successful idle,
and verifies the original canceled tool part is byte-identical with one launch.
An uncertain submission fails instead of silently resubmitting.

The recovery test submits different prompts in the recovered source and its
independent fork. Before continuing the fork, it verifies source continuation
did not change the fork, deletes the recovered source and rechecks the useful
application/cancellation. Both responses must ingest the original error and
reach successful idle without another shell. Completion admission rejects a
historical cancellation-only proof or either missing new-prompt receipt.

The first expanded planned case,
`factorybox-consumer-worker-loss-e6991b5b97814ea28ded1417f6ec3fd9`, passes.
The corresponding abrupt attempt,
`factorybox-consumer-worker-loss-e461286146df4396b17c7d7c05c2b79a`, fails before
worker loss: the original tool is already durably aborted/interrupted, but a
proc-stat read races its exit and returns ESRCH. The new client accepted only
ENOENT as absence. Its actual failed observation remains preserved. Its complete
34,360,786,944-byte store was fully verified, SHA-256
`cfcbd861c998977b3028164e39c1d9cb5151332725d38b67d29e70d4a6a58344`.
Lossless repack `factorybox-storage-repack-d7ff8b09ee964622aaed88d0a26a1ded`
verifies both complete decoded streams and reclaims 711,715,132 encoded bytes.

The deterministic regression in
`factorybox-agent-cancel-esrch-red-47cd87e7eeff4c0a85711cf8c93ef640` exercises
the actual unchanged client identity/gone functions and fails on ESRCH. The
correction accepts ENOENT or ESRCH from that exact proc read; EACCES, EPERM and
EIO still propagate, and a live process with the same birth remains live.
All six portable gates pass in
`factorybox-checks-3c21206990304f5b8f34238a99d5e60e`, including those negative
identity assertions and all 232 immutable reference files.

Both complete native cases on the corrected exact inputs now pass:

- Planned replacement:
  `factorybox-consumer-worker-loss-81443edb880e4f478beaa97e427027b5`.
- Abrupt replacement:
  `factorybox-consumer-worker-loss-9695d3ad05bc470299cd27968645d550`.

They share the same complete source/agent manifests and native test binary,
SHA-256 `e928db57fd44f2b44070ed3110d1e2c20248634829eeaa7ddff9821d62d14457`.
Each uses distinct old/new worker epochs, preserves settled cancellation and
recovery replay, completes a new prompt in the recovered source and a different
new prompt in the independent fork after source deletion, and observes one
tool launch in each environment. The original tool error remains unchanged.
The intentionally killed old worker exits 137; every other worker exits zero.
All owned resources close and global sysctls remain unchanged.

`factorybox-agent-cancel-continuation-notes-02ca6c47ce304523824f248fa1630bf4`
verifies both complete results, direct prompt/cancellation receipts, every
original model assertion, reconstructable fixture patches, identical current
inputs, fully decoded test executables and the complete ESRCH failed-store
archive. It explicitly refuses the historical cancellation-only proof for this
new-prompt requirement. Provider errors, partial tool-call output, uncertain
external effects, the final external gateway/model proof and other release
requirements remain open. Production qualification is false.

After both corrected complete cases and their aggregate verification pass,
qualified retirement
`factorybox-storage-retirement-5e81e478652b4f1f926e6e7de67a2da6` verifies that
entire ESRCH failure stream again and retires its 771,061,835-byte encoding.
All host observations, source, results, logs and outer-archive metadata/hashes
remain. The old mutable filesystem is intentionally no longer reconstructable.
The original runtime PID, busy-mount and selected unresolved failures are
unchanged.

Maintenance `factorybox-storage-share-cancel-inputs-6bc75493540849618af8b81bad53b469`
fully decodes/verifies 24 compacted test inputs before and after sharing 17
byte-identical encodings, reclaiming 109,199,360 allocated bytes. Original
results, source, logs, encoding receipts and all failure filesystem archives
are unchanged. Every old and shared mtime is recorded. This is artifact
maintenance, with no runtime optimization or production qualification.
`factorybox-storage-share-sources-03d128a770db40a4b4f6f78ce7210bd6` additionally
verifies 473 closed source trees and all original results before/after sharing
30,763 byte- and metadata-identical immutable source files, reclaiming
315,813,888 allocated bytes. Active runs and failure filesystem archives are
not eligible; every original source manifest remains identical.

## Provider faults and an outside-world effect oracle

The optional test gateway now has two separate modes. Its unchanged default
inference path listens on loopback 4097. Explicit `--effect-only` listens on
4098 and admits only bounded POST `/fixture/effects/apply` and
`/fixture/effects/status`, without query strings. Each mode refuses the other's
paths. Inspection/stop must specify the same mode, exact executable, nonce,
birth and target network namespace; a successful stop still confirms pidfd exit.
These helpers remain optional test-controller processes outside captured PID
and mount trees, with no standalone-runtime dependency.

Both native mode proofs pass on worker `ef7dcbe22aae4169a4642bb8df935f53`:

- Ordinary inference gateway:
  `factorybox-provider-gateway-35bafc7a2ee54a5c854a9907102f7943`.
- Separate outside-effect gateway:
  `factorybox-provider-gateway-4393c2866fcc4406b22b4656e9a0af72`.

Their independent builds produce identical helper bytes, SHA-256
`a1dc9d8f684562b1da37bbdf329813178538bcf4f674162e902e9c4253470175`.
Both reject wrong-mode inspection and wrong-birth stop while preserving the
original helper, forward the real bounded HTTP exchange and confirm clean exact
stop/worker exit, cleanup and unchanged global sysctls. An effect-only proof
cannot qualify external inference; its admission regression rejects it before
any provider discovery or copying.

The effect oracle is a host-controller Unix HTTP server. Only its socket is
exposed to the worker; its process and fsynced hash-chained effect ledger stay
outside the captured environment. The native effect case observes zero effects,
commits one, deliberately loses its response and receives HTTP 502 through the
gateway, then reconciles that same operation twice with exactly one application.
The ledger does not deduplicate: the real HTTP unit regression repeats an apply
with the same operation ID and observes two effects. Changed ledger data and
invalid paths/operation IDs are rejected. This qualifies the oracle/transport,
not an agent's handling of an uncertain external result.

The additive `opencode_tool_probe.py --provider-faults` uses the actual shipped
OpenCode 2.0.27 in its pinned consumer image, without external inference. Its
first run, `factorybox-opencode-tool-13646d0b76ee487894894696d818f3d2`, remains
failed with complete output. It exposes OpenCode's real synthetic continuation
after a partial-stream transport failure: the original counterpart mistakenly
treated that last synthetic user message as an unrelated request. The actual
partial shell was already `executed: false`, `state: error`,
`error.type: provider.transport`; no launch file existed.

The corrected counterpart retains the latest explicit fault prompt across that
synthetic input until an explicit new user prompt. Its real HTTP regression
rejects the frozen old counterpart and passes the correction. Native repeat
`factorybox-opencode-tool-32f2b5b28b6643b8bf861e5e10dfeda5` passes: an HTTP-400
provider rejection produces `provider.invalid-request` and failed idle without
an inference retry or tool call. A streamed incomplete shell call is marked
not executed with a durable transport error. OpenCode retries inference once;
the controlled retry rejection settles failed idle. An explicit new prompt in
each same session then receives its exact token answer and successful idle.
Actual tool launches remain zero. The original ordinary model/client assertions
are unchanged. Expected injected errors qualify only these fault assertions,
not availability or successful inference from an external provider.

All six portable gates pass in
`factorybox-checks-8eb2c9eb08564967a5d9851562d61217`.
`factorybox-agent-provider-fault-notes-63d30d5c48504df4ad6f1fc7b8bbad10`
binds both gateway results, identical current helper/agent inputs, the outside
ledger, actual failed/continued agent transcripts, the preserved first failure
and a fresh red HTTP replay of its old counterpart. Reference verification
still checks all 232 files. At this component stage, composition of the actual
agent with an outside effect through pause, replacement and fork was still
missing; the following section supplies that candidate proof. These component
fault observations alone do not prove preserved provider-fault state through
that lifecycle on final bytes. Final external model and all other release
gates remain open; production qualification is false.

## Uncertain outside effect through complete worker replacement

The additive `agent-effect` probe runs the original
`TestNativeConsumerWorkerLoss` and every existing consumer assertion. Only new
private model/client copies are patched, with exact before/after hashes and
reconstructable diffs. The original ordinary client is retained byte for byte
as the delegate and executes its normal arm/state/watch/advance/finish checks.
Every original model assertion remains. The original held tool still owns its
16-MiB private RAM, nonce, filesystem watch and nonzero result/exit semantics.

Its wrapper submits one outside request. The host ledger fsyncs the effect,
drops the reply and the tool observes HTTP 502 without acknowledgment. The
connection fully closes before the held tool starts. Its uncertain receipt and
stdout marker remain inside the saved environment; neither records a successful
outside result. The optional gateway's exact exit is confirmed before pause.
The host-owned ledger/controller survive independently of the old worker.

Both complete cases pass on worker `ef7dcbe22aae4169a4642bb8df935f53`:

- Planned replacement:
  `factorybox-consumer-worker-loss-7a3b38c00ff24d85928ac58deee1297e`.
- Abrupt replacement:
  `factorybox-consumer-worker-loss-961c7a37429e45f09a0e0ea22b7d51c9`.

Their complete Go/agent input manifests match, including the unchanged native
test executable, SHA-256
`e928db57fd44f2b44070ed3110d1e2c20248634829eeaa7ddff9821d62d14457`.
Both confirm zero source anonymous and resident snapshot memory after pause,
distinct worker epochs, exact recovery replay, removal of the old saved source
files, another checkpoint/fork and independent RAM/filesystem/application
mutations. In each descendant the operator client performs a read-only outside
lookup and records the confirmed application with `do-not-retry` before
releasing the held tool. The actual agent must ingest its original uncertain
output, result/exit, outside operation and verified commit ID. The independent
fork does this after its recovered source has been deleted. The same uncertain
receipt hash remains in both descendants.

The outside ledger contains one application. Its operation, committed hash and
controller identity are identical at preparation, replacement recovery and
completion; the one application has no delivered response. The oracle does not
deduplicate, so a replay would increase that count. All three gateway leases
per case close by exact pidfd, including before the intentional worker SIGKILL.
That old worker exits 137; every other worker exits zero. Cleanup and unchanged
global sysctls pass. The whole-case 420-second bound, sandbox ceilings and all
original assertions remain unchanged.

All six portable gates pass in
`factorybox-checks-97734a0486b14de2aec2a6ec7376de2c`. Negative completion tests
reject omitted recovery/descendants, reused epochs, unclosed workers, repeated
outside effects, a changed outside controller, an automatic-retry decision,
changed uncertainty and missing agent acknowledgment.
`factorybox-agent-effect-notes-78555d1df6624ee5aa2f2d950470ac4a` verifies both
complete results, current inputs, reconstructed model/client patches, every
original assertion, fully decoded native test inputs, direct result receipts,
the outside hash chain and all gateway/worker closures. Reference verification
still checks all 232 files.

The [agent recovery procedure](agent-recovery.md) describes holding an unresolved
job, looking up the actual outside operation and recording the no-retry decision
in every descendant. There is no new generic runtime reconciliation command.
This local-model observation is not a general exactly-once guarantee, support
for host restart or migration, or final-package qualification. The PID conflict,
busy mount, remaining storage admission/manual retirement, final external model,
operations and final acceptance requirements remain open.

## Timing boundaries on the canonical-base candidate

The single full passing Factory observation
`factorybox-consumer-worker-loss-21efb535c9f74b168f9f0e961b2ba5fd` is on worker
`ef7dcbe22aae4169a4642bb8df935f53`. The existing strict
`benchmark_lifecycle.observations` extractor verifies its complete three
checkpoint timings, eight useful-work observations and pause receipt.
Prepared useful work is 22.279 s. Separate preparation is 34.241 s and worker
startup is 0.740 s, making cold worker-to-useful work 57.260 s in this observation.

Fork useful work from an existing checkpoint is 10.878 s. Source checkpoint
creation costs another 11.241 s, so capture plus useful fork is 22.119 s.
The corresponding resumed-source capture plus useful fork costs 27.028 s.
Pause is 9.523 s and useful resume is 6.812 s. These boundaries must remain
separate; fork latency alone is not capture-plus-fork latency.

The pause receipt records source anonymous memory falling from 208,478,208
bytes to zero, and 549,224,448 resident snapshot bytes falling to zero. Residual
source cgroup memory is 10,485,760 bytes. This neither says the complete worker
uses zero memory nor establishes a latency distribution, matched optimization
gain or final-package performance qualification.

## Initial local release scope agreed on 2026-10-23

The user approved a controlled first release on the existing Linux AMD64 host,
with separate workers per customer/trust domain and recovery only on the same
host and boot. `GOAL.md` now defines its acceptance checklist. Before this
change, the complete previous plan was frozen byte for byte in
`factorybox-first-release-scope-de79043f4de849b9ac94bd502716abf3/previous-GOAL.md`,
SHA-256 `5965e225dafd4eadca30869d75502ff7405a0aed6e3ac1a9e2772ea2a444a05f`.

The first release still requires the final package's independent reproduction,
all required existing assertions, the rare restore-PID correction, attribution
and correction of runtime cleanup defects, bounded admission and a verified
manual retention/capacity procedure, shipped-agent failure handling and checked
operations/recovery/rollback instructions. Ten complete regular lifecycle runs
per example are fixed before the final repetition series, in addition to the
required targeted fault and upstream cases. No failed case is discarded.

The 100-run series, 24-hour soak, further performance optimization, automatic
retention and further image minimization move to the later programme. This is
a change to release scope, not successful evidence for those unrun items.
The first release makes no long-term stability or latency percentile claim.

## Remaining work in the original wider programme

Broader cancellation and uncertain external side-effect handling; resolution
of the PID collision and new admission-fixture busy-mount cleanup; a predetermined
repeated performance comparison; verified base reuse in the supplied
operational path; external-provider bounded-store recovery and broader low-disk cases;
broader signal/error and interrupted timer preparation handling, final-consumer
checkpoint retirement qualification and epoch retirement;
explanation of the preserved Sysbox-fs timeout;
repeated lifecycle/pressure/fault cases and bounded 24-hour soak; a reduced,
independently reproducible worker package and operations instructions; required
native and upstream suites on the selected delivered bytes. Missing or failed
required assertions continue to block production qualification.

## PID reservations without a live process leader

On kernel `7.2.8-9-omarchy`, isolated native control
`factorybox-pid-reservation-control-5ccfd1a12f12455caec64ac95ba31311`
demonstrates a blind spot in the existing EEXIST diagnostics. A reaped PID-2
leader has no `/proc/2/status` and `pidfd_open(2)` returns ESRCH, while an owned
PID-3 child retains its process group, session, or both. Each exact clone3
request for PID 2 returns EEXIST until that last holder exits and is reaped.
The same exact PID request then succeeds. All three variants pass. This is a
kernel mechanism control, not identification of the historical restore occupant.

Its first attempt,
`factorybox-pid-reservation-control-854e9f2d56ad43d984fc1f617f7bcdf7`, fails
before container creation because Docker rejects the new runner's invalid
`--pid=private` argument. Its original failed verdict and output remain.
The corrected command uses Docker's default private PID namespace and verifies
its actual namespace inode differs from the host. No workload from that first
attempt starts; a subsequent exact-name inspection finds no container.

`factorybox-pid-reference-diagnostics-134619240f274ed0b22c693886a34641`
executes the exact proposed CRIU reference-scanning functions with those real
kernel states. All six cases pass: each of the three reference types through
both a local proc view and an inherited ancestor proc descriptor. The latter
maps visible proc IDs 10, 13 and 16 to actual local holder PID 3. Namespace
inode checks and the final NSpid component precede local getpgid/getsid queries.
Each case identifies exactly its remaining holder and preserves EEXIST; after
owned pipe release and exact reap, PID 2 is available. The scan also visits
non-leader thread entries, although these six cases use single-thread holders.
The inputs and global sysctls stay unchanged; exact owned cleanup passes.

The additive ordered
`criu-pid-collision-references-candidate.patch` retains the original diagnostic
patch and runs only after nested clone3 EEXIST. It observes task/group/session
references in the caller's actual PID namespace. For a two-ID CLONE_NEWPID
request it examines the caller-level ID, rather than the new namespace's PID 1.
Different active and child PID namespaces cause an explicit diagnostic refusal.
The racy snapshot is bounded to 4,096 examined entries and 32 matches, with a
250-ms elapsed-budget check between entries. It is never cleanup authority.
It sends no signal, writes no PID counter, retries no fork and preserves errno.

Candidate build `factorybox-build-criu-retained-a3a1bd4425e9455fb58416e2af4d5315`
passes, binary SHA-256
`6263dd24728ecb8350486ca829c8776e27f62090bd493f6c733d02d0e7f836d0`.
Its original source/tests remain intact; only the ordered cr-restore diagnostic
changes from the previous CRIU candidate. The default builder path and selected
worker remain unchanged. The builder fixture's complete original-to-current
patch and exact hashes are updated in the production compatibility declaration;
all original assertions and build/resource checks remain. Frozen before/after
inputs are in `factorybox-pid-reference-inputs-c6336b5a30684db9a781b74afc2d7262`.
All six portable gates pass in
`factorybox-checks-310e057a779b47b78d180fc03816f109`.

The original full PID-failure archive is unchanged and retained. Its saved
sleep's own PID is 2, but the observed mechanism alone does not prove a group
or session reservation caused that restore failure. The next reproduction
needs the actual requested-ID, namespace and holder observations. No restore
repair, final native qualification or production readiness is claimed.

## Historical replay stops at lost archive metadata

Inventory `factorybox-pid-replay-inventory-d7902aad38274a7fbd33de0c09d3aad9`
fully decodes and verifies all 12,155,092,992 bytes of the original PID-failure
archive. Its 216,799-entry index identifies a closed selection of the failed
fork, target record and original immutable base: 2,719,848,203 logical payload
bytes, with every selected hardlink target inside that selection. No original
archive, source, result or failure verdict changes.

Three isolated historical replay attempts remain failed:

- `factorybox-pid-historical-replay-3350530c1e2e4fd0b55568f994be3fcf`
  stops at unpacking because the old image has libzstd but no zstd executable.
- `factorybox-pid-historical-replay-9e6686e71400455c8064d923f768ca7d`
  fully verifies the encoded and decoded original archive through its pinned
  libzstd, then fails before the test because the new fixture made the worker
  root read-only. The original manager needs its container-local sub-ID files.
- `factorybox-pid-historical-replay-66a6a85f253c49cbba78c88eeff325e5`
  corrects that fixture setting and starts the original Sysbox services. The
  unchanged original filesystem digest rejects the reconstructed lower tree
  after 8.68 seconds. No restore is attempted. Its fresh mount/cgroup-record
  setup exists only in a disposable test copy; no production retry or identity
  rule is relaxed.

The archive itself proves one concrete metadata loss. The immutable-base
record saves its root mtime as `1792654540440218967` ns. The original tar header
contains `1792654540000000000` ns and no PAX headers. This difference is an input
to the original tree digest. The archive preserves its recorded tar bytes, but
those bytes cannot establish reconstruction of the original filesystem metadata.
Whole-stream compression verification never established that stronger property.
The original PID-failure archive remains complete and unchanged; its lost
subsecond metadata cannot be recovered from that tar.

The stopped-volume archive command previously omitted PAX, xattrs and ACLs.
Its existing real-GNU-tar round-trip regression now includes nonzero nanosecond
mtimes, hardlinks, a symlink and a binary user xattr, retaining every original
byte/size/mode assertion. The unchanged old command fails three timestamp
subtests and loses the xattr in
`factorybox-archive-metadata-red-9d25e0e917034802b13d8de695d0abbc`.
The corrected command passes the same test in
`factorybox-archive-metadata-green-e78f256ffaa645c8b3718efd68075adf`.

New archives use PAX with numeric owners, sparse files, all xattr namespaces
and POSIX ACLs. The exact-owned, stopped, read-only volume helper retains its
1-CPU/1-GiB limits and gains CAP_SYS_ADMIN to read trusted xattrs. There is no
host configuration change. The complete archive stream is still hashed and
verified before owned cleanup, with the existing disk caps unchanged.

Native probe `factorybox-archive-metadata-native-f70b0ab177b54e509bf356eddda60436`
passes through the actual container archive helper and a separate fresh volume.
All eight entries have identical content and metadata, including fractional
mtimes, numeric UID/GID, modes, hardlink topology, symlink, FIFO, character
device 0:0, an 8-MiB preallocated store, user/trusted xattrs, file capabilities
and access/default ACLs. Exact cleanup and unchanged global sysctls pass.
The first native fixture attempt, `2dbc8bceeb784c139a18759602faa5ae`, remains
failed: it used a hash helper absent from the image's Python. The corrected
fixture streams the same hash and corrects its own eight-entry count.
This covers the named file types and metadata, not Unix socket-node export or
preservation of kernel mount/cgroup/inode identities. This diagnostic tar is
not a newly qualified production recovery format.

All six portable gates pass in
`factorybox-checks-d9f38c3cacae4015b995ddf37c104130`.
`factorybox-archive-metadata-notes-d5fc7f4fe63e4ae78c7a74a695a70888` binds the
red/green regression, native metadata match, failed replay inputs and complete
archives, and the actual original tar-header mtime after another full decode.

Lossless repacks `6b3bffb6ccf543e6a59f28aa9d134ba1` and
`a3ad1fbd6c0443eca751dc378b45b6e2` preserve both reconstructed failed stores.
Maintenance `a5ad0d1fe3d2408ca75959a68cd3c584` and
`fd6c94f692a84380a729802821d0136a` also verify complete decompression before
encoding closed generated test executables and offline metadata, including the
inventory index. All four operations reclaim 1,253,218,748 allocated bytes
without retiring any archive payload or changing a result. Encoding receipts
reconstruct every original byte; future inventory readers must use that receipt.

The rare restore PID collision is still unexplained. A future original-state
claim needs a faithful new capture; an explicitly metadata-limited diagnostic
of the old process images would not qualify the complete original lifecycle.
The selected worker and all other first-release requirements remain unchanged.

## Historical process-image diagnostic and stale-observation refusal

`factorybox-pid-image-diagnostic-d8c668af512b4e5bbf2197dfb751ef05`
reaches CRIU and restores the preserved process images successfully in
1.992 seconds. It verifies all 330 original prepared image/metadata files,
515,577,040 bytes, against the original preparation record before restoration.
The original Go runtime and old Sysbox components run in a new owned private
worker; only CRIU gains the previously built read-only PID diagnostics. The
original boot is checked. The restored target is observed, deleted by its exact
runtime identity, and its owned overlay and alias are unmounted. Exit 0, no
skips, exact outer cleanup, unchanged input hashes and unchanged global sysctls
pass.

This diagnostic deliberately does not reconstruct the original complete
filesystem metadata. The unchanged original digest still rejects its lower
tree, and that refusal is retained in the result. Fresh mount/cgroup/start
identities exist only in the disposable record. The original disk manifests,
strict replay, archived source and production guards are unchanged. Successful
restoration here does not reproduce or fix the original PID collision and does
not qualify a complete lifecycle.

Earlier observations remain visible, including one rejected green result:

- `factorybox-pid-image-diagnostic-92bde3ad71324e7ca4de4322a4f62e4e`
  fails before restore. Its new test hashes the formatted intent file rather
  than `json.Marshal(intent)`, which the original runtime uses. Corrected
  attempts retain the canonical intent check and all 330 file assertions.
- `factorybox-pid-image-diagnostic-d786db92682440f99d39ed06ce220d19`
  records a passing result and claims reproduction, but that verdict is
  rejected. The runtime actually refuses an uncaptured external bind before
  CRIU. The test mistakenly reads the historical `restore.log` copied with the
  images. Its bytes are the original 1,215-byte failure log, SHA-256
  `182835efdd4a361bec21b2b4a34273600d6b05391352db8bc8312d3459205065`.
  The saved result is not rewritten or counted as successful reproduction.
- `factorybox-pid-image-diagnostic-b31be3a5bb644294b0fa7eac56ca35d7`
  fixes the fixture's Sysbox-fs path to the original `/var/lib/sysbox/store/fs`.
  It saves the copied historical log outside the image directory, removes only
  that private copy and requires absence before attempting restoration. It
  correctly fails with `new_restore_log=false`: the unchanged runtime refuses
  the copied old `restore-work` directory before CRIU can create a new log.
- The successful `d8c668af` diagnostic preserves that old work directory by
  renaming it inside the new owned copy after verifying it is not a separate
  mount. It requires the original path to be absent and a new restore log to
  exist. That fresh log has no PID collision. Nonfatal address-configuration
  messages and image-readability warnings remain visible; success is established
  by the unchanged runtime's completion and observed live target, not by
  filtering those messages.

`factorybox-pid-image-input-proof-3e8998c578e34c009ba0278c6bbbab22`
binds only the corrected daemon-startup and 330-file preparation phases of the
failed `b31be3a5` attempt. It explicitly does not call that complete attempt a
passing replay. This narrowly verified replacement permits retirement of two
superseded fixture-only volume copies: the old read-only-root startup failure
`9e6686e7` and the formatted-intent-hash failure `92bde3ad`. Receipts
`factorybox-storage-retirement-d1674351b3b2477d88505680d5838747` and
`factorybox-storage-retirement-ce21098678b34934bfbf2958d2a2bbd1`
verify complete decompression, retain every member header and regular-file
hash plus selected small controller/image diagnostics, and retain all original
source, results and logs. Their old full diagnostic volumes intentionally
become unreconstructable. The original PID failure, strict metadata-loss replay
`66a6a85f` and latest fresh-log refusal `b31be3a5` keep their complete archives.

Storage maintenance also retains byte-for-byte reconstruction receipts:

- Repacks `852d9858fd79408798c771c48f69a0f2` and
  `a0d065902d8c499299723f75124ceba0` verify the complete new failed-store tar
  streams before replacing their encodings. The first is subsequently retired
  with the explicitly superseded fixture above; the second remains complete.
- `factorybox-storage-compact-historical-tools-3ad821d782374a53b3b978829e58dd6d`
  compacts 46 groups at 247 closed unselected paths, verifies every full decoded
  payload and shares identical encodings. Original results are unchanged;
  selected failure inputs and component builds stay directly available.
- `factorybox-storage-compact-test-caches-c20d9f82a26d4139a3a154d86ab44af1`
  archives seven closed OpenCode output caches. All 4,910 entries, file hashes,
  numeric ownership, modes and nanosecond mtimes are checked before removal;
  the full 155,432,960-byte tar is verified. All cache bytes remain
  reconstructable. Earlier maintenance errors are recorded, including a partial
  removal of 697 already-verified leaves before a directory-permission failure;
  the resumed operation revalidates the archive and remaining files. This changes
  no consumer input, source, result or session observation.
- Closed diagnostic executables and the cache manifest are encoded with complete
  verification. Two more completed source-sharing plans are encoded in
  `factorybox-storage-compact-source-plans-ea437591e58540d288fde34ea3f9c38d`.
  Readers must use their encoding receipts, as for the earlier inventory index.

Unselected worker `factorybox-build-worker-2d7d95abaad440e4bffefc96ab2d9f08`
assembles CRIU `a3a1bd44` with the current Sysbox SHM correction. Its package
SHA-256 is `2044248c688e2044d364d8bd23465d2746ae93e5e8861a25240f5b605fef02dd`
and image ID is
`sha256:7c2b3df2900fa63464c2a2ede733a9e69352a7aad7b83538ece44d769be2e7f7`.
Its CLI is byte-identical to selected worker `ef7dcbe2`; the CRIU diagnostics
change the package and image identities. It is an instrumented candidate, not
the selected release or an independent reproducibility proof.

Full bounded Factory/Theia/OpenCode lifecycle diagnostic
`factorybox-consumer-worker-loss-0d9e6e93ae2044f0b953efe2e4c8bdfd`
passes all original lifecycle assertions on these bytes. The native developer
test takes 153.69 seconds, including a fork of the resumed source and retention
of the same external Theia connection. Its earlier independent Theia admission
`factorybox-theia-admission-863808c27e684531aa8872589ec28ee7`
passes. Exact cleanup and unchanged inputs/global sysctls pass. This one
diagnostic has a 32-GiB bounded store and the existing resource/deadline limits;
it is not a worker-replacement case or a member of the final predetermined
acceptance series. No PID collision is observed.

`factorybox-pid-image-diagnostic-notes-7a21ef109db64613a7e252ff65e74b08`
binds the failed/rejected/successful observations, source and artifact hashes,
retention receipts and new unselected worker. The original PID cause, busy
mount attribution, remaining admission/manual retirement, final external-agent
and operations/package acceptance requirements remain open. The goal remains
active and production qualification remains false.

## Sandbox metadata refusal before transition

The sandbox reader was limited to 1 MiB while its atomic writer had no encoded
size bound. Repeated rejected pause history could therefore replace a readable
record with one that the same runtime could not read. Portable red evidence
`factorybox-record-metadata-red-502576ebc5c3421dab99d03b1cadd8d0`
executes the real old writer and fails both required assertions: an oversized
309-entry history is accepted, and a one-byte overflow including the publisher's
final newline is accepted. That original failed result and source stay intact.

Native red `factorybox-checkpoint-retirement-0f10716ea09742f3a8bbb1f12a8f25d9`
prepares an actual committed stopped pause, injects readable historical
rejections and calls the delivered old CLI. Resume fails late with
`write controller guard socket: message too long`, after starting its transition.
This identifies a smaller transport boundary as well: the guard packet reader
accepts 16 KiB and the sealed capture/timer readers accept 64 KiB. The original
complete failed store is retained. Two earlier fixture attempts, `d04d4ebb`
and `ee60ee01`, fail on invalid fixture CLI verbs `status` and `inspect` before
the tested operation. The actual CLI verb is `state`; those failures stay failed.

The candidate uses one shared 1-MiB record limit for reading and atomic
publication, including the final newline. Before new start/checkpoint/pause or
resume work, it bounds encoded successful and failed outcome records and the
full guard/capture request. Resume also reserves its following pause. Maximum
numeric/timestamp widths and JSON escaping count; existing record errors and
history are preserved. New durable error summaries are bounded to 4 KiB raw
text with the full original byte count and SHA-256; the full original error
still reaches the caller. The existing 16/64-KiB transport limits are unchanged.

All five new portable checks pass. They cover preserving the last readable
history, the exact final-newline boundary, immutable prospective admission with
existing escaped errors, the smaller guard packet and error-summary identity.
All six portable gates, including enabled vet and all 232 reference files, pass
in `factorybox-checks-907340352049465e88c4ff15c1825147`.

Candidate worker `factorybox-build-worker-b8724db2ca594eb9b86989129604a411`
and independent reproduction
`factorybox-build-worker-6ae8b7582e4b4f9da6e1dddd0a72981d` both pass and have
identical package SHA-256
`a9de2a5ecd3d2653ee3679aa7079797bf76631c469c00f184e1de73027889186`
and image ID
`sha256:28e128750c181c3d0b2c02c18f40734176aed7f81f0efb2676fbe46e9b9f5545`.
Their CLI SHA-256 is
`4b3e0334cc68d86b8e82b41197f30503884ea085f50848dc6bb70936d3f7bbdc`.
They include the existing SHM correction and read-only CRIU PID diagnostics;
neither build is selected or production qualified.

Native green `factorybox-checkpoint-retirement-eb1864a669af4782af26d4cea0869158`
passes on those delivered bytes, with exit 0, one required test, no skips,
unchanged inputs/global settings and exact cleanup. Its actual pause has
1,048,426 record bytes after 980 explicitly injected, synthetic history entries.
Resume refuses before transition/backing; the stopped source's record and
runtime creation identity remain unchanged. An independent fork of the prior
checkpoint passes the original 32-MiB private RAM and file actor assertions.
The injected entries are not a claim that 980 real pause cycles occurred.

The preceding green test body in `2b60bd1b` still has a failed overall result:
its root-owned mode-0600 proof file was unreadable by the host verifier.
Compatibility receipt
`factorybox-record-metadata-fixture-compat-5de071a222fc42de8e2828a66fadec9e`
binds the only test change, output mode 0600 to 0644. Test SHA-256 changes from
`acd35a6338f79725e9dc5c0bfd302c3c58450c08fe15732fc2a7e8a15cc529fa`
to `fb67af994ce8a2bf956e54db01ba23cce2f59d376abf8e5e63484a43c4035bc9`.
Every assertion remains byte-identical and vet stays enabled. The separate fresh
green run supplies the passing overall result; the old failure is not rewritten.

All thirteen original native core groups pass on this package in
`factorybox-record-metadata-regressions-bb07f2d95ff04a9aa15d2c5f916324cb`,
including six checkpoint retirement cases, timer/signal and interruption cases,
actual capacity pressure, planned/abrupt bounded-worker loss and storage/exit
admission. Every leaf has exit 0, confirmed cleanup and unchanged inputs/global
settings. The frozen batch recipe differs only in its worker build selection.

The ordinary native admission suite passes on the same CLI and all four exact
component bytes in `factorybox-runtime-ef76c4cb097540f99c106ccf057f6b66`:
seventeen tests, 78 subtests, zero skips and complete cleanup. Every original
test assertion remains intact. The full existing six portable gates also pass
after the fixture-mode and maintenance-tool changes in
`factorybox-checks-e243fc3ff3a748bcafaa82030b587ecb`.

Full Factory/Theia/OpenCode local-model lifecycle
`factorybox-consumer-worker-loss-347e0a47681843558033dd4f8e83603b`
passes every original assertion on the candidate package. Its native developer
test takes 168.73 seconds, including a useful fork after resume, the same external
Theia connection and independent edits through both real Dev Containers.
The separate Theia admission `factorybox-theia-admission-0a8998559e504ad79ede9e2032b80fb8`
passes. Complete worker exit, owned cleanup and unchanged inputs/global settings
pass. This is one development regression, not worker replacement or a member
of the final predetermined acceptance series.

The existing strict lifecycle timing extractor checks all three checkpoint
timings, eight useful-work observations, separate IDE observations and pause
memory receipt. This new observation records preparation 36.881 s, worker
startup 0.749 s and prepared useful work 22.036 s, totaling 59.666 s cold.
Source checkpoint is 10.393 s and useful fork 7.433 s, totaling 17.826 s.
Pause is 17.513 s, useful resume 12.456 s and resumed-source capture plus useful
fork 36.192 s. Pause/resume are slower than the earlier single observation;
these two different package observations do not establish a cause, matched
regression or latency distribution. No speed improvement is selected from them.
The new pause releases 169,193,472 source anonymous bytes and 507,768,832 resident
snapshot bytes to zero, leaving 28,876,800 source cgroup bytes. This is neither
zero whole-worker memory nor final-package performance qualification.

The [operator procedure](worker-contract.md#sandbox-record-admission-candidate)
describes the refusal and the tested independent fork from an intact confirmed
pause. This change bounds one sandbox record and its complete capture messages.
Aggregate worker-exit metadata admission, epoch retirement, the PID and busy
mount causes, final agent/operations evidence and final package acceptance
remain open. The goal stays active and production qualification stays false.

After the earlier historical-image report, retirement receipt
`factorybox-storage-retirement-74531a3d45454d1fa05bb029759aba9f`
fully verifies the old `b31be3a5` volume before retiring its 520,922,979-byte
encoding. Passing fresh-log process-image diagnostic `d8c668af` supplies the
explicit replacement proof for that copied-work-directory fixture. Every member
header/hash, selected small diagnostics, original source, result and logs stay
available. That old diagnostic volume is now intentionally unreconstructable;
the original PID failure, strict metadata-loss replay and current metadata red
failure remain complete. This later receipt supersedes the earlier report's
statement that the `b31be3a5` complete archive remained retained. It changes
neither the original failure verdict nor the limited scope of the replacement.

Completed-input sharing
`factorybox-storage-share-inputs-e2fae2f0f1ad44fe8536611308bc2963`
reclaims 342,036,480 allocated bytes across thirty verified identical inputs,
including the independently completed candidate/reproduction tar pair. The
maintenance command now explicitly accepts
`--include-completed-worker-packages`. It verifies every complete frozen build,
all original result hashes, package/bundle/source/image identities and matching
file bytes/modes/ownership before sharing; it rechecks builds/results afterward.
Every path remains directly usable. The receipt records both original and
adopted file mtimes. No package bytes, build verdict, archived dump or historical
experiment change.

Index `factorybox-record-metadata-notes-bd79f66a57624970ba03bbb7b5309a9e`
binds these source deltas, exact fixture declaration, red/green cases, unchanged
original tests, independent packages, all native/portable results and the new
performance observation. It also checks the stored encoded bytes of the
preserved PID, signal, metadata-loss replay and busy-mount archives against
their earlier complete-decode receipts. Its first attempt, `76f09ec8`, remains
failed because the verifier expected a top-level native log for the bounded
worker-loss suite; that suite has separate `Prepare.stdout`/`Recover.stdout`.
The second attempt, `90dd94d3`, fixes those paths but expects the worker-loss test
name for the ordinary lifecycle probe. The actual complete probe runs
`TestNativeDeveloperLifecycle`; that index also remains failed. The corrected
index checks the actual logs/test and retains both failed index results.
These verification changes do not alter any native outcome or assertion.

## Reproducible aggregate worker metadata overflow

The new public-API regression
`TestNativeWorkerMetadataBaseAdmission` reaches a different admission gap on
unchanged candidate `factorybox-build-worker-b8724db2ca594eb9b86989129604a411`.
Three actual stopped committed pauses and 200 actual tiny public base imports
are prepared first. Only those owned records' historical rejection text is
then injected, with every individual record below its 1-MiB reader limit. The
existing record identity, real pause checkpoint, source runtime and RAM/file
backing remain those established through the ordinary delivered CLI. This is
explicit synthetic legacy history, not a claim of repeated real failures.

Native red `factorybox-checkpoint-retirement-2bc36639faee49c39e7f1b6701fe98b6`
completes the test in 39.94 seconds and fails the intended assertion. The
complete exit record with both maximum-width timestamps occupies 4,192,256
bytes, 2,048 bytes below the unchanged 4,194,304-byte bound. The next public
`prepare-base` succeeds and raises it to 4,199,969 bytes. All 201 canonical
bases together occupy only 1,550,161 bytes, within their separate 2-MiB budget.
Even shortening both timestamps to their minimum 20-byte UTC forms leaves the
completion over its reader limit. The current base-only guard omits the other
arrays in the eventual exit record; this is not a timestamp-only edge.

The minimized variant uses four serially prepared, actually stopped pauses and
one prior public base. Four readable sandbox records are needed to approach
4 MiB without the large canonical inventory. The additional prior base makes
the backing-preservation assertions nonvacuous. Each new actor is paused before
starting the next; only one sandbox runs at a time. All RAM/file assertions of
the original bounded-loss helper remain unchanged.
`factorybox-checkpoint-retirement-9729b94e2059415b98cc203679d9a59f`
reproduces the same refusal failure in 13.21 seconds. Its second public base
raises the completion bound to 4,199,967 bytes while both canonical entries use
only 15,422 bytes. The ordinary three-pause/200-base scenario remains available;
`--suite worker-metadata --worker-metadata-minimal` selects the reduced case.

Both native results remain failed, with unchanged inputs/global settings and
confirmed exact cleanup. Their complete failed 1,074,790,400-byte store archives
are retained with whole-stream verification. No runtime admission correction,
clean-drain proof or production qualification is claimed here. The next change
must account for the complete aggregate and prospective growth before new
backing or intent, including paused/active sandbox records, capture/fork aliases
and recovered canonical bases. Complete replay and the final strict worker-exit
preflight must retain their existing ownership and integrity requirements.

Offline maintenance
`factorybox-storage-share-test-encodings-88fd07e5b1cf4574a7a3ce21ed8ba3ad`
shares fourteen byte-identical closed passing probe encodings after another
complete canonical decode. It reclaims 132,534,272 allocated bytes. Every
encoded path, original/raw identity, encoding receipt, source/result/log remains
available and unchanged; original/adopted mtimes are retained in the maintenance
receipt. No unresolved failure archive, build input or historical experiment is
changed.

All six portable gates pass in
`factorybox-checks-f143df068fc649cfbffc1545d88fa7d5`, including enabled vet,
all 232 reference files and the existing fixture extension chains. Feedback
index `factorybox-worker-metadata-feedback-e1c3088c4a1a472991c07aaa2a8429dc`
passes its verification of both counterexamples and another complete decode
of both failed stores. Its passing index means the failing native evidence is
consistent; both native verdicts stay false and remain a release blocker.
The minimal fixture parameterization preserves every original fatal assertion
and the ordinary three-pause/200-base scenario. Exact native-test SHA-256 changes
from `160dfcb2e86d914dadd45180e6ba8bca6560f8db0cc63ce70d2efc85fd99283c`
to `20853f06f25f43b9436f240fa2a67d7bd670b7c28161fefa60ed5ea2ebeb631d`;
the index retains the exact reconstructed patch and both sources. Runtime core
bytes remain unchanged, and the first-release goal remains active.

## Aggregate worker metadata admission candidate

The common bounded-worker admission now includes canonical bases, recovered
bases, immutable capture/fork aliases and paused sandbox records in one
4-MiB completion budget. It reserves the full existing 1-MiB record bound for
each active or prepared sandbox and a future pause alias. Committed paused
records count at their actual compact JSON size. Pending fork/recovery inputs,
array keys/brackets/commas, encoded string widths and both maximum completion
timestamps count before new backing or intent. Unknown unfinished inventory
refuses new work. Retired capture aliases stay conservatively charged until
epoch replacement. Complete replay and the original strict exit integrity and
ownership preflight remain unchanged. Admission reads metadata rather than
rehashing all CRIU pages on every new request.

Worker `factorybox-build-worker-4652f4cbbb3d4f529d16be2fc5d76774` and
independent reproduction
`factorybox-build-worker-3ab9e1e702ae4827b16934987cc92b25` have identical
package SHA-256
`6b8fe00f8e10b0a607048ddc1387fb74bd395a14369043134c0377df8d7a0fc6`,
image ID
`sha256:4975cb19fe3063c5915990433f622f39795c5fc3990d874e238c1e45e2769130`
and CLI SHA-256
`c6a41ed879f7ec408336b1a65114c5eec753cf31b82f9fdc6131b84b0e8a9c86`.
Neither package is selected or production qualified.

Four portable tests cover the real 1-MiB publisher boundary, preservation of
encoded tree widths without input mutation, the complete pending recovered
base/alias/record allowance and unknown inventory refusal without mutation.
All six portable gates pass in
`factorybox-checks-6c037423410f45a3a10bdb12d0839d29` before the later
test-profile compatibility and maintenance-tool additions.

The extended native driver now requires normal stop, a fresh worker epoch,
both actual exit ledgers within 4 MiB and unchanged old source records,
configuration, checkpoint payloads and filesystem trees. Its tree digest
includes file contents, mode, owner, nanosecond mtime, inode/link count,
extended attributes, symlinks and special-device identity. It runs every
original `TestNativeBoundedLossRecover` assertion, including 32-MiB private
RAM, private files, exact replay, independent descendants, pause/resume and
useful work after source deletion. Both workers must exit with 0 and PID 0.
The whole native lifecycle retains its 420-second deadline and resource limits.

Initial extended run
`factorybox-checkpoint-retirement-b2ef1270613c4d5197438bfee8ca9838`
passes the new refusal test, clean source stop and preserved old backing/ledgers,
but its later recovered-descendant checkpoint correctly refuses at 170,340,352
available bytes. This is below the unchanged 64-MiB floor plus 128-MiB write
reserve. Its overall result remains failed and the complete store remains
reconstructable. Only this extended fixture receives a declared 2-GiB store
to retain four real old pauses while executing every original descendant
assertion. Compatibility receipt
`factorybox-worker-metadata-fixture-compat-e33ad11fc67242e3b48757a8c570b302`
binds the exact driver change and hashes.

The next run `factorybox-checkpoint-retirement-04f1f26edea84a13926a66ba868d17de`
fails the existing helper's exact 1-GiB profile assertion before preparing any
sandbox. Compatibility receipt
`factorybox-worker-metadata-profile-compat-ab5cc09053e14dbb8a68623f5a040a26`
keeps that exact 1-GiB/direct-I/O check for every original bounded-loss case;
only the separately declared worker-metadata fixture selects 2 GiB. The shared
test SHA-256 changes from
`131fc11423059c1c6709f4ec481f81bae0254d577f517968ca16b6176fe454bf`
to `421fbc3c23a89f8c9f193e645aaf27e01f3147e2ef7b6bd31eb68abcdf964f82`.
Every original recovery test body remains intact and vet stays enabled. This
test-only overlay follows the completed independent reproduction; native core
bytes still exactly match the delivered worker. Both earlier failures stay
failed.

Reduced green
`factorybox-checkpoint-retirement-ee0c36dfaa3a4ef0b261e97c993aa1b7`
passes refusal, clean stop, preserved backing, real recovery and clean
replacement stop. Four committed pauses and one canonical base remain in the
actual 4,192,256-byte completion. Admission and recovery take 14.76 and 14.24
seconds respectively. The original three-pause/200-base regression
`factorybox-checkpoint-retirement-f591932da9604a539bb579460c7ed036`
passes the same complete assertions, taking 44.70 and 15.19 seconds. Its actual
completion is 4,192,255 bytes. Both have zero skips, unchanged inputs/global
settings and confirmed exact cleanup. Injected history remains explicitly
synthetic; the pauses, base imports, RAM/files and recovery are real.

The [operator procedure](worker-contract.md#aggregate-worker-metadata-admission-candidate)
describes the shared refusal and same-store epoch replacement. Manual epoch
retirement, the PID and busy-mount causes, final external-agent/operations
evidence and final package acceptance remain open. The goal remains active
and production qualification remains false.

## Refine the shared reservation without dropping restore assertions

The coarse candidate's first full regression attempt
`factorybox-worker-metadata-regressions-15d77ddb8d28424e9c7328fe96d75c11`
passes retirement and timer/signal groups, then the artifact headroom guard
refuses the third run before it creates an artifact or worker. Its batch
result remains failed. Repeated source-sharing plans themselves occupied
12-18 MiB each. Lossless maintenance
`factorybox-storage-compact-plans-fcf5fc11f01941c68810ecb4ab2f0c62`
fully decodes and verifies four exact completed plans before retaining their
encoded bytes and reconstruction receipts, reclaiming 56,094,720 allocated
bytes. Their original results and plan-content hashes remain unchanged. The
first command-order attempt `factorybox-storage-compact-plans-6460ca16309547f185075bc024f0d2a7`
stays failed; its automatically produced redundant encoding was fully checked
against the still-present original before removal and the fresh corrected run.

The next batch
`factorybox-worker-metadata-regressions-11c8e24d92b44828af253a88c63f181b`
passes its first five groups but fails the unchanged restore-hold suite.
`factorybox-checkpoint-retirement-9a531b7d61a64862856e74c6995940b4`
passes fork, resume and both disconnect cases. The final wrong-ack case is
refused because the common guard charges one full MiB for each small unresolved
record. Existing fault cases deliberately preserve their unresolved records;
the fourth reservation plus the envelope cannot fit 4 MiB. The fatal helper
return also leaves its test-held lock until process exit, explaining the
secondary fixture-delete `resource temporarily unavailable`. Both failures
and the complete failed store remain preserved.

The corrected guard shares the existing local maximum future-pause record
constructor. Known records retain their complete old history, maximum changing
field widths and bounded future errors; their compact future form is capped by
the independent 1-MiB atomic publisher. A pending intent without its record
still reserves the full MiB and its future pause alias. No local admission,
reader, transport, writer or exit bound increases. The original local
constructor's logic is extracted without changing its fields. No restore-hold
assertion or fixture body changes.

New worker `factorybox-build-worker-0b75abed9eb0490db9f722e0354ddfb4`
and independent comparison build
`factorybox-build-worker-926ddebd5e974f48831b7c12af460dc5`
have identical package SHA-256
`c0cdcf9724b7821540e4f587096189ce8d4ef6a02d57a869c4d2222b576a5a30`,
image ID `sha256:2a29112f8af9dc2f1448d74ad00946861f41c00d9bbce8a847bff4c834740a50`
and CLI SHA-256
`d7ad8e410d0d7b6173e57a4b3bd8dea0530118ad413367b1135b8ad1ed7f91b8`.
`factorybox-checkpoint-retirement-89b2ea25cef04b08b8c20cdb10760400`
passes all five original restore-hold cases in 19.40 seconds on these bytes,
with unchanged inputs/global settings and exact cleanup. All six portable
gates pass in `factorybox-checks-d71ebd258e5d43b0a654feb64d26c85b`.
Both aggregate boundaries now pass on the corrected bytes:
`factorybox-checkpoint-retirement-a6ed1eb6195d45bba5d6fc46d366b84b`
for four pauses/one base and
`factorybox-checkpoint-retirement-ec302a47b4384b96812f2a052a196a14`
for three pauses/200 bases. Both actual completion records are 4,192,256
bytes; both old and replacement workers stop with 0 and PID 0. All old
records, checkpoint payloads and filesystem identities remain unchanged.
The original full recovered-descendant tests pass in 14.07 and 14.52 seconds
respectively. Entire runner times are 39.80 and 72.34 seconds. These are
functional regression durations, not latency-distribution observations.
Neither package is selected or production qualified. The remaining regressions
and full release acceptance still need these exact bytes.

The completed-input sharing command can now explicitly include immutable worker
CLI copies. It chooses an existing shared inode before a newly built unique
copy, then only replaces byte/mode/owner-identical single-link duplicates.
Whole frozen worker sources, bundles, packages, image identities and original
results are verified before and after. Receipt
`factorybox-storage-share-inputs-fb3561f9d8c242ecac67f39ba8865ac2`
reclaims 86,716,416 allocated bytes; independently completed package/component/
CLI pair receipt `factorybox-storage-share-inputs-b98d59b243c3421d997ce590a1494946`
reclaims another 180,957,184 bytes. Every path stays directly usable and both
original/shared mtimes are retained.

The two obsolete invalid-CLI fixtures `d04d4ebb` and `ee60ee01` now have
explicit retirement receipts
`factorybox-storage-retirement-ec6c940a8e764b07a50a6bf3cf16b504` and
`factorybox-storage-retirement-f0183425030242a19895066f4314a980`.
Each entire original store stream is freshly decoded and verified before its
46-MB encoding is retired. The public command implementation is identical to
the later worker passing the corrected complete metadata test. Native source,
results/logs and all archive member headers/hashes stay retained; these two
invalid-verb fixture volumes are intentionally no longer reconstructable.
They remain failed. The actual original record-metadata failure, both aggregate
counterexamples, restore-hold red and original PID/signal/busy failure archives
remain complete. Lossless repack
`factorybox-storage-repack-10c7c63f824e4a8d8b66c3843924330b`
also reclaims 19,262,764 bytes from the closed unselected ordinary-native
capture archive after verifying all 790,626,304 original bytes; its full content
remains reconstructable through the original encoding receipt and one pointer.

All thirteen original native core groups pass on the refined package in
`factorybox-worker-metadata-regressions-ab6ad782325b4428bbf8e450c8c5f938`.
This includes every original restore-hold assertion, planned/abrupt bounded
worker loss, actual guest capacity pressure and controller-loss/storage
admission. Every leaf has exit 0, unchanged inputs/global settings and exact
cleanup. The original near-1-MiB sandbox-history refusal/independent-fork case
also passes in `factorybox-checkpoint-retirement-89bb7ca398f34924aa8f464fbf97a81d`.
Its original 1,048,426-byte seeded record and all 32-MiB RAM/file assertions
remain intact.

Full Factory/Theia/OpenCode local-model worker replacement
`factorybox-consumer-worker-loss-a668b6531fe645e9bc6d3d7187826cdf`
passes on these exact CLI/component bytes with its 32-GiB bounded store.
The old worker is removed before recovery; an empty separately owned worker
forces the retained image from loop 0 to loop 1. All original consumer
assertions pass, including exact replay, removal of saved source files,
independent restored inputs, new checkpoint/fork, private mutations, original
Theia connection and useful descendant work after source deletion. Native
prepare/recover tests take 75.35/82.09 seconds. Both workers stop with 0 in
4.232/5.486 seconds; the empty loop holder also stops cleanly. Owned cleanup,
unchanged frozen inputs and unchanged global sysctls pass. Separate Theia
admission `factorybox-theia-admission-729771ddb1624efdb6a9614de341495e`
passes. This is a development regression with a local model, not an external
provider result or part of the final predeclared ten-run acceptance series.

A standalone Python test invocation accidentally used `/tmp`, a tmpfs with
7,969,116,160 free bytes, below the unchanged 12-GiB host floor. Its 212 tests
report seven failures and nine errors, all caused by that precondition rather
than the supported disk-backed runner. Environment comparison
`factorybox-checks-tempfs-comparison-6d91c2768acc4dbdbe07e310d4f9839d`
retains a fresh same-source failing tmpfs run and a passing 212-test disk-backed
control, with complete stdout/stderr and unchanged source hashes. No reserve
or assertion changes. All six supported portable gates pass again in
`factorybox-checks-650e2addfc144042b66484247a1040d4`.

Encoded-probe sharing now recognizes the existing `factorybox-bounded-loss`
run name and its checked executable hash. Receipt
`factorybox-storage-share-test-encodings-f6206870399c41da952f28f6b17b51d5`
shares 37 byte-identical closed executable encodings and reclaims 108,896,256
allocated bytes. Canonical payloads are fully decoded; every encoded path,
native source/result/log and encoding receipt remains available and unchanged.

Verification recipe
`factorybox-worker-metadata-notes-de923b79dbdb4d578b55f65aa5ad5833`
checks both independent packages, complete frozen native source, all current
gates and original failing evidence, fresh full decoding of all five selected
metadata/restore failure stores, fixture compatibility, supported disk control,
intentional invalid-verb retirements, the full Factory loop-remap proof and
exact source patches. Its earlier `59e8ec3e` attempt fails because the verifier
compares a hash with a manifest object instead of its `sha256` member. The
`e1ca7290` attempt fixes that and fails when it expects a raw consumer test
binary that normal closed-run retention has already encoded. The corrected
recipe verifies the encoded helper against its original native hash and
complete decompression receipt. Both earlier index results remain failed;
no native verdict changes. The shared admission change is now functionally
covered. Manual epoch retirement, original PID/busy-mount attribution, final
external-agent/operations and first-release package acceptance remain open.
The goal remains active and production qualification remains false.

## Release exact selections retained by a replaced worker

An ordinary controller operation correctly refuses a sealed worker epoch. That
also prevents its original `release-checkpoint` command from releasing a retained
selection after a clean worker replacement. New `release-recovery-selection`
uses the validated current worker profile and an exact old controller, source,
checkpoint hash/owner and selection ID. It validates the same persistent image,
store, boot and runtime/CRIU identities as bounded device recovery, then locks
current controller followed by the existing old controller. It initializes no
old directory or runtime and publishes only the exact release record. The
existing ordinary exit fence remains enforced.

Worker `factorybox-build-worker-f89547e7acea4c15a32d7f7ead2ef81b` contains this
path. Independent offline comparison build
`factorybox-build-worker-5ce353a6b9964ccb8a3d5cb8549abdef` reproduces its package
SHA-256 `3e8b4ec24efdc79f7bdd21168f8bf8421221f5fd7762af14eb5baac100fdc4fb`
and image `sha256:8c51078021366f00fa3ff14954656c1809046c318789023d54f7238385b5d481`
exactly. Portable checks `factorybox-checks-c045c4ff9c7d4bb9a8fc2232d994e294` pass
all six gates, including the unchanged retained-image identity controls,
212 Python checks, 232 immutable imported files and fixture-extension checks.
No existing native test or setup assertion is changed. A separate setup runs
new wrappers that call both complete original bounded-loss test bodies.

Native `factorybox-bounded-loss-b5a472e21e7c4f768f1e7b34a26ea99f` passes after
clean stop/removal, and `factorybox-bounded-loss-045032eb517e4006b4cfcfdfe3c05dd4`
passes after exact old-worker SIGKILL/removal. Both use the delivered CLI,
an unchanged 1-GiB bounded image and a real committed 32-MiB private RAM pause.
The clean case first verifies the original lifecycle's exit-fence refusal.
Each rejects six wrong source/owner/hash/selection requests without publishing
a release, replays an exact release byte for byte, and leaves a second selection
protected until its separate explicit release. After both releases, the original
retirement dependency check still refuses the unresumed pause. All original recovery, private RAM
and file, independent fork, pause/resume, fork of resumed descendant and useful
work after source deletion assertions then pass. A complete owned-object/content
inventory of the old objects/forks/bases is identical afterward except for the
two specifically requested new release records. Both runs confirm exact cleanup,
unchanged frozen inputs and unchanged global sysctls. Entire runner durations
are 25.50 and 23.33 seconds, not performance-distribution observations.

Only the selection protection is released. A committed pause remains separately
protected; checkpoint payload deletion, manual epoch retirement, the original
PID/busy-mount attribution and final external-agent/package acceptance remain
open. The previous selected baseline is unchanged and the full goal stays active.

The exact source patches, independent package identities, both complete native
proofs and fully decoded retained probe identities are linked by
`factorybox-retained-selection-notes-ea59ee422b19442181f301ce6ba87662`.

## Read complete retained backing without reviving old mount authority

The old immutable-base verifier correctly refuses a base whose worker mount
has disappeared. Copy recovery already validates its original stored inode and
metadata before making and verifying an independent new base. The same original
read-only opening checks are now shared with inspection. Copy recovery retains
every existing assertion, full target verification and rejection of an unknown
covering mount or unauthorized device translation.

New `inspect-recovery-checkpoint` uses the current validated bounded worker and
the exact old controller/source/checkpoint/owner/hash. It reads every serialized
RAM/opaque metadata file and every filesystem tree. An immutable lower is read
at its retained canonical backing after its original empty alias mountpoint
is verified. It does not recreate a mount, copy backing, change the old manifest,
release a selection/pause or grant authority to delete. Source epoch, same-boot
store/image identity and original inode/metadata checks bind a changed loop
device; former mount IDs are not current authority.

Worker `factorybox-build-worker-b5d3f162359b4235a20c213f891ff4cc` and independent
offline comparison `factorybox-build-worker-320529a4a5414eb5b4bd66fdfdf00bd0`
produce identical package SHA-256
`c13c66e2dc7633173f97b072744d67f71ab22488d81b52bbc16be6440aab6ea5`,
image `sha256:deee8c669fb8274bbc4083e8e7f41265f5cc54839325a3e642ddee14e82d5d8a`
and CLI `5af1e19a5c6a83207689f1f6575321fc095265573283eb8b700a5b181a32596e`.
Both original base-recovery tests plus all sixteen new retained-inspection
controls pass in `factorybox-base-recovery-eb2da8ced73d4808af2e6e56592b0017`.
The latter use an owned 512-MiB ext4 store and explicitly synthetic old-device
translation. They cover authorized/unauthorized/denied translation, actual
foreign self-binds over base/alias/upper, changed content at unchanged file
size/mtime, changed xattrs, alias payload/successor/mode, changed inode identity
and cancellation. Exact owned-object/content inventories and saved manifests
stay unchanged by successful or refused inspections. No read-only check grants
the vanished immutable mount's runtime authority.

Real committed 32-MiB RAM pauses survive actual old-worker removal in
`factorybox-bounded-loss-acdf76b56e70467882897df7cb151703` with clean stop and
`factorybox-bounded-loss-e1b5ce17f6e1483f87186940f0ddd977` with SIGKILL.
Both inspect the old generation through the delivered CLI before every previous
selection/pause-protection and complete recovery assertion. All original
RAM/file, replay, independent-descendant, pause/resume and useful-work checks
pass afterward; the complete old objects/forks/bases inventory stays identical
except for the two explicitly requested selection-release records. Inputs,
global settings and exact cleanup remain verified.

Complete Factory/Theia/OpenCode local-model case
`factorybox-consumer-worker-loss-c041b21756db44ef921a83f13300fc76` also passes.
The same retained image moves from loop 0 to loop 1 while an empty separately
owned worker holds the old slot. Before the complete original recovery test,
the new CLI verifies all 351 serialized files, 542,700,130 bytes excluding the
filesystem-tree payloads, and all nine filesystem trees in 28.28 seconds.
Both application workers and the empty holder stop cleanly; source/replacement
stop observations are 4.164/6.503 seconds. This explicit full-content inspection
is an integrity observation, not a start optimization or a latency-distribution
claim. Separate Theia admission
`factorybox-theia-admission-d22197519478429fb2ce985bd1d2845c` passes. No external
provider or final ten-run acceptance series is claimed.

All six supported portable gates pass in
`factorybox-checks-90bda549a85746398782e5b3e40249d9`, including 212 Python checks,
232 immutable imported files, original fixture-extension verification and vet.
Closed encoded-consumer helper sharing is now explicit opt-in. It verifies each
helper against that run's original `inputs_sha256`, every encoding receipt and
complete canonical decompression, chooses an already-shared canonical inode,
then rechecks all result/receipt hashes. Maintenance
`factorybox-storage-share-test-encodings-bf42995fb7994846a8e4f5d53a8e54e7`
shares 56 identical encodings and reclaims 238,780,416 allocated bytes. All paths,
original sources/results/logs/receipts and complete failure stores remain.
Other completed archive re-encodings preserve every original decoded byte and
one-hop reconstruction receipts; selected baseline inputs remain unchanged.

The proposed retirement decision now distinguishes explicit recovery handover
to an independent committed successor pause from releasing an old selection.
It requires durable protection of successor inputs across interrupted deletion.
That handover/epoch-deletion protocol is not yet implemented or qualified.
Manual epoch retirement, original PID/busy-mount attribution, final external
agent and full first-release acceptance remain open; the full goal is active.
Exact source patches and current proof bindings are recorded in
`factorybox-retained-inspection-notes-e5a47382374f49129900acc78667f754`.

## Durable retirement maintenance survives another worker loss

The read-only observer
`factorybox-epoch-layout-23d4ee76f0594e97b42f609736b5eedb` inventories the actual
old controller after exact worker SIGKILL/removal. It leaves payload trees
unread and inventories regular runtime/capture/guard metadata. Every original
retained-selection and complete bounded-recovery assertion then passes in
`factorybox-bounded-loss-f457f4017347417a890c4ec556e57882`. This confirms retained
runtime files exist after abrupt loss; their presence grants no deletion or
old-process authority.

Candidate `factorybox-build-worker-64b08fd2a21c4561adbb418680985034` adds an
internal durable maintenance fence. Its package SHA-256 is
`0cde27f1d718ef53fdec2f1cd34df258f929a7abf646e26ee0f94a195f7f7313`, image
`sha256:e0f683d7a4ece223caa1ac80c9b3bfcd9d34fe9b17326cb7ab49115afc39fc66`
and CLI `ffbdee27d41714d29978d42216a055cb36d07dd1076b938593ff0c88a6470f53`.
It is not selected. Store-wide refusal protects successor inputs across
controller invocations and worker replacement, including explicit CLI options
without a worker profile. Source preparation and acceptance remain permanent
source fences. An accepted retry binds the same preparation, original fence
inode, complete contents and persistent image. Only device/mount IDs may be
translated after independently verified same-image reattachment; original
inventories are not rewritten. Completion cannot clear a remaining payload
or a successor at its path. A same-byte successor fence and an external
hardlink are refused.

The native coordination fixture first runs the complete unchanged retained
selection/recovery scenario and its original cleanup. It retires three exact
obsolete checkpoints through the delivered public API, preserving the original
1-GiB capacity assertion. It independently recovers the old pause, advances
its private RAM/files, and commits a new complete successor pause. Its
internal retirement inventory deliberately covers only the original old RAM
image directory. A second actual worker SIGKILL/removal follows acceptance;
an empty separately owned 512-MiB worker then holds the old loop slot. The next
worker must replay the original acceptance on loop 1, refuse premature
completion, remove the exact authorized original images, clear only the
original fence, retain the source tombstone and recover the useful successor
with its complete private state. Exact finish replay also passes.

Both first-loss variants pass: abrupt in
`factorybox-bounded-loss-c4b88440625542e88e7a9fa7a08acd13`, planned in
`factorybox-bounded-loss-c5a5efff3e034cfa9c5faf4d75ed2861`. Whole driver
observations are 44.52 and 43.20 seconds. All original assertions, input hashes,
global settings and exact cleanup pass; both empty loop holders stop with 0.
These durations are test observations, not runtime latency distributions.

Two failed predecessors remain failed and completely reconstructable.
`factorybox-bounded-loss-9c7be3248bb142449562aed8378d56f3` exposes a false
new-fixture assumption: all three distinct worker epochs legitimately receive
numeric mount ID 786 in separate namespaces. The corrected fixture uses exact
old-worker removal and a forced different loop device.
`factorybox-bounded-loss-19ec1c86f2984442a8eda256cc5fbf9e` passes those fence
checks but correctly refuses new recovery with only 172,318,720 available
bytes against the unchanged 201,326,592-byte floor-plus-reserve. The extended
fixture now explicitly retires its three obsolete checkpoints before adding
more work. No original assertion or capacity bound is removed or raised.
Complete archive re-encoding preserves every decoded byte and reconstruction
receipts for both failures.

Production source bytes match the delivered candidate. Its frozen new native
fixture predates these two corrections; the exact before/after test hashes
and ordered fixture patch are recorded separately. Every pre-existing native
test remains unchanged. An independent build of the full final retirement
implementation remains required, along with its package and complete tests.
There is no public epoch-retirement entry point yet. Complete source inventory,
unknown-entry rejection, cross-epoch dependencies, handover admission and full
epoch payload deletion remain open. The full goal remains active, production
qualification false. Source patches and proofs are bound by
`factorybox-epoch-retirement-fence-notes-5ef7cc49b1f34e8f889116d70cc31a74`.

## Public manual epoch-retirement candidate

The candidate now exposes `begin-epoch-retirement` and `retire-epoch` through
the validated current worker. Admission locks every controller in the store,
requires committed pauses or confirmed deletions, verifies complete source
backing and rejects current mount dependencies. Each old committed pause needs
an independently verified successor checkpoint with exact recovery provenance.
A paused descendant can provide that handover after its recovered parent has
been deleted. Ordinary lifecycle operations remain fenced across worker loss
until the exact accepted deletion completes. Old source metadata remains as a
permanent tombstone. Caller bundles and vendor-manager storage outside the
controller are outside this deletion authority.

Unselected build `factorybox-build-worker-b9e78b21a3d44e42b6ebcf6a5dfed146`
and independent offline rebuild
`factorybox-build-worker-f9618142bae74796ba121ab8a179c7c9` reproduce package
SHA-256 `ba479aaed20e07cf6ea37dce2984a1bbee6990ee95dcd52ecd66e995cb9f0097`
and image
`sha256:c5a2949551adfe9190bbc5654f3d4c47f8139ad742c87f191b27c6f8bb561a6e`.
Both contain CLI SHA-256
`0fd1bb9634bfb9ff0fbcef75d197a5b96a976acebe7dff9c83afa07496e347b2`.
The native source and fixtures match this complete Go source manifest.

The original complete retained-selection and 32-MiB recovery assertions remain
intact. The new wrapper adds useful work in a recovered parent, an independent
fork, parent deletion and a committed descendant pause. It refuses eight
invalid requests before acceptance and two unknown-file additions after
accepted partial deletion. Admission takes a complete metadata witness and a
separate exact payload inventory. Before acceptance, every inventoried object
must exist; afterward only inventoried payload objects may be absent. Root
siblings and coordination metadata are checked as well. Replacement objects,
changed metadata, active selections and foreign covering/overlay mounts refuse
deletion. An unaccepted preparation marker preserves the intact recovery reader.

After public acceptance the fixture removes exactly the original source RAM
image directory using the owned-object remover, then the driver kills and
removes the accepted worker. An independent empty owned holder forces Loop 0
to Loop 1 on replacement. The public CLI completes the original accepted
request, exactly replays its saved report, preserves metadata and observes all
authorized source payload roots absent. Its successor is independently
recovered and performs useful RAM/file work. The CLI then retires the next old
epoch containing completed imports, forks, restore/resume backing and retired
checkpoints. The current successor resumes and works after that deletion.
This is real worker loss after injected partial deletion, not yet an observed
SIGKILL during the public CLI's own unlink loop.

Both native cases pass, including cleanup and unchanged inputs/global settings:
abrupt first loss in
`factorybox-bounded-loss-5e756e4b1e664633b3693397720f9d96`, and planned first
loss in `factorybox-bounded-loss-bc359962709243f69cd84d35734e5436`.
Their complete outer observations are 111.472 and 106.132 seconds; these are
development checks, not a latency distribution or the final acceptance series.
The first retired epoch observes 62,394,368 and 62,373,888 additional available
filesystem bytes respectively. Unlinked names do not generally guarantee
returned physical capacity. The original 1-GiB store, resource limits and
420-second lifecycle ceiling remain unchanged.

Four closed failed attempts remain fully reconstructable: `42065233` rejects
an invalid external-overlay fixture; `b9431756` exposes the new inventory's
incorrect capture-version check; `41730708` identifies the omitted exact fork
bundle configuration; `75cb3c3f` identifies private restore overlay workdirs.
The corrected inventory uses the pinned version-3 capture protocol, verifies
the fork configuration against its original intent, and binds restore/resume
workdir names to completed operations. It also refuses missing resume backing.
Every original test assertion and imported reference remains unchanged.

Portable regression
`factorybox-epoch-completion-bound-3b4800d6e77b45939b8e5c299fc55c1c` first
reproduces acceptance of a report larger than its real completion reserve.
The corrected bound admits at most half of the allocated 1-MiB reserve for
the largest serialized completion, leaving the other half for publication.
The same test bytes fail before the correction and pass afterward. All six
portable gates pass in
`factorybox-checks-1e03602519774b139c26450683a302d0`.

New worker packages can now be losslessly encoded during creation, before
their result is published. Complete decompression verifies their exact raw
tar bytes; loaders verify both encoding and raw identity. Existing completed
builds stay directly usable. Complete pinned component reuse avoids fallback
copies without changing any component bytes. Closed source/input sharing and
lossless dump repacks retain their exact verification receipts. The superseded
minimal paused-source Stop-refusal volume `71d2ec0d` is retired only after its
complete passing replacement `805b260a`; receipt
`factorybox-storage-retirement-f2b7ad3156644837b807d4e29528ac82` verifies all
34,360,786,944 original tar bytes and member hashes before retiring its
572,296,018-byte encoding. Frozen source, failed result and outer logs remain;
the complete old volume is intentionally no longer reconstructable. The
original PID, signal and busy-overlay evidence remain retained.

The source patch, exact fixture hashes, failed stores, passing cases, independent
build and storage receipts are indexed in
`factorybox-epoch-retirement-notes-d2bd74d4ad6349d98eac63296119c864`.
Full Factory/base retirement, actual public unlink interruption, real preparation
ENOSPC controls, original PID/busy-mount attribution, external agent acceptance,
final package gates and the predeclared final series remain open. The complete
goal stays active; production and full epoch-retirement qualification stay false.

## Registered bases and observed public unlink interruption

The additive epoch fixture now prepares an unused registered base containing
5,000 files through the public CLI. After public retirement acceptance it starts
the actual `retire-epoch` child, observes removal of its original last file,
stops and kills only that child through its pidfd, and verifies that another
original file survives and completion is absent. A complete worker replacement
with forced Loop 0 to Loop 1 then finishes that exact request. Every authorized
payload is absent, base provenance and all 5,000 caller-input files remain, and
the independently preserved successor performs useful work. All preceding
32-MiB recovery, second-epoch and negative-admission assertions remain intact.

Both first-loss variants pass with unchanged inputs/global settings and cleanup:
`factorybox-bounded-loss-ad614aaf6a134f658792d7d92778bf31` is planned;
`factorybox-bounded-loss-1325fdc1f53f4e11bc515ad39ccb958e` is abrupt.
Their outer observations are 121.885 and 128.216 seconds. These are development
test durations, not a latency distribution. The exact additive fixture patch
is declared in `factorybox-epoch-base-unlink-plan-b12a4d6cb3ff415da50df29db7cf58fc`.
The planned runner additionally binds the original unlink receipt directly to
the completed acceptance hash and registered base. The earlier abrupt runner
predates that host-side binding; its unchanged native fixture has the same
observations and assertions. Neither case is a full Factory used-base proof.

The full Factory extension patches only a new, ownership-checked test copy.
It replaces the original direct saved-source `RemoveAll` with a committed
independent successor, public admission/deletion, exact replay, verification
that every inventoried payload is absent, and useful resume. Every remaining
original worker-recovery, Theia, agent, private-mutation and descendant assertion
stays byte-identical. Original repository tests and imported overlays are intact;
the bounded-path patch precedes this separately hashed operation patch.

Its first run, `factorybox-consumer-worker-loss-f70e478d496847bf8280f3c67f9f7636`,
fails on the previous B9 package at `criu-retained-2462.log`. The ownership-checked
read-only archive helper exposes the original ARM's 170 threads and sixteen
process leaders: source PID 131, final active PID 2304, and original leader 2462.
The pinned producer opens a separate log for every serially active process.
The inventory incorrectly authorized only the source PID's name. The correction
uses only process leaders in the complete, bounded, ordered original ARM tree,
with matching source/active identities and process-leader closure. Thread IDs,
foreign names and incomplete trees grant no additional name; no old live PID
is consulted. Regression `factorybox-epoch-retained-logs-3fb1aa9f521e487287e45760432f699c`
records identical test bytes failing before the correction and passing after it,
plus nine invalid-tree controls.

New unselected package `factorybox-build-worker-d7241b59ad3a4d39a76e732d0c156e1b`
contains CLI `989c7eec62297bafc2421ca3838392a353c30d5d59d411d80723cb634d86d62b`
and raw package SHA-256
`4681c5b631659e1f38c9c948b9f7d070c26004ceb8cd3bab28bb82a648deecaf`.
All six portable gates pass in
`factorybox-checks-7b441dd097a64aaf921655e1d4eafa75`, including 220 Python tests,
unchanged reference/fixture verification and enabled Go vet. That portable pass
alone does not qualify the package. Independent reproduction and the complete
final-package gate set remain required.

The first corrected full-consumer attempt, `7bf56d0e840d4c8998f6be876fb602aa`,
gets past the log inventory but the new Begin call reaches the original
fixture's 90-second per-command bound at 90.020 seconds. Its complete preparation
and 1-MiB reserve already exist; durable acceptance has not been claimed.
Read-only diagnostic `factorybox-epoch-timeout-diagnostic-399e8112edef4ac4b8a9abe8fb4a87f9`
retains the original ARM, preparation and directory listings. Lossless repack
`factorybox-storage-repack-0a0eadb093a147a5b74c16b7f1b27de5` preserves every raw
byte of that complete failed store. This is a failed overall attempt.

The declared full-consumer fixture now uses the ordinary complete public
`retire-epoch` command, including its internal durable admission, followed by
exact replay. Only these new full-tree calls get a 180-second bound. Every
original lifecycle command/assertion and the complete 420-second driver bound
stay unchanged. The additional standalone pre-recovery inspection is omitted
from this composed case; public recovery and retirement still verify complete
source/successor backing. Separate Begin/kill/replay has its dedicated native
cases. The exact fixture change is frozen in
`factorybox-consumer-epoch-direct-plan-8dc8577d5ad041cea7a4007c0ef79a34`.

`factorybox-consumer-worker-loss-19cdf02881f4448ab096db43659260ce` passes this
complete Factory/Theia/local-OpenCode path on the corrected D724 CLI, with the
forced replacement loop change. All eleven authorized roots, including the
actually used registered base and original RAM images, are absent. Base
provenance and the caller-input marker remain. The independent committed
successor resumes with its complete parent/child/app RAM and files; every
original later checkpoint/fork, independent mutation, Theia edit, real agent
tool completion and source-deletion assertion also passes. Both workers stop
with 0; inputs/global settings and exact cleanup pass. The direct retirement
is 175.778 seconds and exact replay 1.017 seconds. These are single development
observations. The final worker has 18.965 seconds of the original whole-driver
budget remaining before its successful 6.999-second Stop. This is not a final
predetermined-series member, performance distribution or production release.

After that complete replacement passes, receipt
`factorybox-storage-retirement-6d82bf52112b42d8b01a9a55b3322750` fully decodes
and verifies the earlier F70 guard-log failure before retiring its
1,238,439,825-byte encoding. Source, failed result and outer logs remain. That
superseded complete image is now intentionally unreconstructable. The separate
complete prepared 7BF timeout image and original PID/signal/busy evidence stay.

Storage maintenance preserves all decoded bytes of the new failed 34,360,786,944-byte
volume through lossless repack `factorybox-storage-repack-993c487767234c699ccd8e036cd2ae27`.
Two older, specifically superseded diagnostics are retired after complete
decompression and passing replacements: attached-session admission `7922e2c4`
through receipt `factorybox-storage-retirement-f051551d31fc4b1faecd7c87f1eb0b4e`,
and the minimal image-tag assertion fixture `da763fe1` through
`factorybox-storage-retirement-a13e6975e3c94537b38433421b241271`.
Their source, failed results, outer logs, all tar member hashes and selected
diagnostics remain; those two complete old volumes are intentionally no longer
reconstructable. Original PID, signal, busy-mount and current Factory failure
evidence are preserved.

Completed source-sharing output plans can now be losslessly encoded only when
their successful result binds the exact raw plan hash, operation and frozen
recipe. Failed, changed and unrelated receipts refuse. Build/native inputs
remain protected. `factorybox-storage-plan-compaction-810cc38b66224169b78167cf8cc771f1`
preserves both raw plan identities and reconstruction receipts, reclaiming
34,037,760 allocated bytes without changing any original source/result.

`factorybox-storage-share-components-3bbc97e3f01c426488ba39669507c23e` verifies
and shares 22 exact duplicate closed Sysbox outputs, reclaiming 491,999,232
allocated bytes. All paths, bytes, modes, owners and original results remain;
original/shared mtimes are explicit in its plan. Privilege is used only for
protected hardlinks to existing root-owned outputs. Changed bytes/results,
permissions and redirected paths refuse before replacement. No installation
or host configuration changes.

The earlier minimal listener failure D35 is independently decoded before
retirement. Its retained log identifies `Can't restore sid (801), expected 91`,
the same unsupported external-session boundary as the preserved attached-exec
diagnostic. Complete minimal replacement and the exact negative attached-session
admission both pass. Receipt
`factorybox-storage-retirement-463652019f704c1aaf8079d1946a2407` retains every
member hash/header and selected diagnostics; its old complete volume is now
intentionally unavailable. This does not explain the separate rare PID collision.

The two old systemd fixture volumes DE819 and CC1 are likewise superseded by
complete passing suite `factorybox-bats-dd234e09657d459cb5493021e62ccc70`.
Retention requires identical original source-tar bytes and all 243 verified
test files, including the same two declared compatibility changes. Every
formerly failed case must actually pass without a skip; missing assertions,
changed input, lost verification and skipped replacements refuse. Receipts
`factorybox-storage-retirement-12431cccabb84def93c0c9d55a423aea` and
`factorybox-storage-retirement-7b02ff6662084a68b3654453994ab6e7` fully verify
before retiring the superseded volumes. Their original failed outcomes, source,
logs, member hashes and selected diagnostics remain. Both original unrelated
systemd skips stay explicit; no final-package gate is reclassified.

## Actual ENOSPC during epoch preparation

The first extended pressure attempt
`factorybox-bounded-loss-8255be709c264d94bc763d428a008623` fails in its new
fixture's pressure allocator before calling public preparation. It requests
free blocks including root-reserved space in one fallocate. The complete
original recovery assertions pass; the overall case remains failed and its
complete bounded image is retained.

The corrected fixture first binds the open pressure descriptor to the actual
current 1-GiB image device, store root and filesystem capacity. It refuses every
other allocation target. It syncs only that private filesystem and fills in
bounded steps using observed available blocks, leaving at most 512 KiB. Exact
before/after hashes are declared in
`factorybox-epoch-pressure-guard-plan-9ba92ab085b344a2bc2f33fc784f73dc` and
`factorybox-epoch-pressure-allocator-plan-b4b7d0dd7bd4446abb370ba48b446641`.
No production core or original assertion is changed by this fixture correction.

Planned replacement
`factorybox-bounded-loss-aeacd4061d434ca9a94e205f2458bf6a` passes on the D724
CLI. Seventy-eight private allocations reduce available blocks from 79,568 to
128 at 4096 bytes each; 4,224 free blocks remain, including reserved space.
The actual public preparation returns ENOSPC and leaves a real partial stage,
with no preparation completion, acceptance, deletion or global acceptance fence.
The complete original backing is unchanged. Both its recovery lock and full
public retained-checkpoint inspection still work under that pressure.

After releasing pressure, only the test's exact unaccepted stage is removed
using its complete owned-tree inventory. The same case then passes the full
public acceptance, actual unlink pidfd-SIGKILL, complete worker replacement with
forced loop change, exact deletion replay, two retired epochs and useful
independent successor. Cleanup and unchanged inputs/global settings pass; outer
observation is 133.229 seconds. The returned filesystem capacity observation
is 82,919,424 bytes, not a guaranteed physical reclamation amount.

This proves safe original-data preservation and read-only recovery after actual
preparation ENOSPC. It does not qualify a public repair/cancellation or retry
operation for an unaccepted partial preparation. Writes remain fenced and the
fixture-only stage cleanup is not an operator API. That manual operating path,
original PID/busy-mount attribution, external model acceptance, final package
reproduction/gates and the predetermined final series remain open. Production
and full epoch-retirement qualification stay false; the complete goal stays active.

The matching abrupt first-loss case
`factorybox-bounded-loss-28a3ea9d1eeb44d4b2ced39db33e5068` also passes the
complete extended scenario in 133.770 seconds, including actual preparation
ENOSPC, original-data/read-only recovery checks, public unlink interruption,
forced replacement attachment change and useful successor. Cleanup and frozen
inputs/global settings checks pass. Neither native case qualifies the fixture's
private partial-stage cleanup as a public cancellation operation.
All six portable gates pass in
`factorybox-checks-fea709843eab4e63bd6193da1b28abe2`.
Source reconstruction, exact native fixture differences and the full-consumer
proof are indexed in
`factorybox-epoch-used-base-notes-ee369ef7a8124cba88c86996d05f3f69`.

## Public cancellation of unaccepted preparation

The exact extended fixture in
`factorybox-epoch-cancellation-plan-68a14469393b4a7681e5750555dc08b9` replaces
only its private partial-stage cleanup with public inspection and cancellation.
The previous D724 package fails in
`factorybox-bounded-loss-971eee0d40cc438e8e239f97cb7ab3c2` at the missing
`inspect-epoch-preparation` command, after the complete original recovery and
actual preparation ENOSPC assertions pass. Its complete failure store, source,
result and outer logs are preserved; cleanup and input checks pass.

Initial candidate `factorybox-build-worker-7112f109db4448d1a6288825b11c4cfc`
passes exactly that same native fixture in
`factorybox-bounded-loss-62eb81e2c6944b288a00fcfcb80d8563`. The public command
removes the observed preparation metadata, complete original payload inventories
remain unchanged, and the following ordinary retirement admission, real unlink
interruption, worker replacement and useful independent successor all pass.
This first case cancels after releasing the fixture's pressure allocation.

The candidate then adds a strict directory generation check using filesystem
birth/change times. Cancellation requires the complete fresh inventory, exact
current source controller, same-boot verified attachment and every expected
original object. Accepted or uncertain source/store state always refuses;
unknown metadata, altered content, ownership, mounts or external hardlinks refuse.
Only known preparation metadata can be removed. Cancellation neither interprets
partial encoded plans as payload authority nor removes a committed checkpoint.
An interrupted cancellation requires a new complete inspection of its remainder.

The ordered additive fixture extension
`factorybox-epoch-cancellation-controls-plan-dfa8faf2e8d649b18205646709e35918`
preserves the complete first public case and adds public refusals for modified
inspection hashes, current-epoch substitution, wrong directory birth, changed
metadata bytes, unknown input and actual retirement acceptance. It requires
cancellation while at most 512 KiB remains available, before releasing pressure,
then complete original-data equality and the full subsequent retirement path.
All original recovery/retirement assertions and the 420-second limit remain.
The six portable gates pass in
`factorybox-checks-e619553cde0348bfb6d253401fe05dec`.

Both stronger native cases pass on
`factorybox-build-worker-eef7eaea107d4feea182733019cbd4eb`:
`factorybox-bounded-loss-ac7164466cf2426682095725f3d861dd` is planned and
`factorybox-bounded-loss-8ac11380f9834e2b8ec9a00714c2d1ef` is abrupt. They take
131.478 and 131.748 seconds respectively. Both public cancellations execute
with only four available 4096-byte blocks, before pressure is released.
Five altered/foreign-input controls and both actual accepted-state refusals
pass; accepted metadata and all original saved payloads remain intact.
The following ordinary acceptance, actual public unlink interruption, forced
replacement loop change, two retired epochs and useful independent successor
all pass. Cleanup, unchanged frozen inputs and unchanged global settings pass.
These are development cases, not the predetermined final acceptance series.

Independent build
`factorybox-build-worker-85fa866ca223485b9902fdade526e645` reproduces every
package/image byte from EEF. CLI SHA-256 is
`42381f1aaf7696d14ebd62ed296ed57329c718c026054aa27e93f21d05183566`,
raw package SHA-256 is
`6166bfa7d0d2f18e4a7e688ff3fcb77782c8cfa413d5e7f556c7a110e2bedf22`,
and image identity is
`sha256:bb732d6846e33b951942b5c681fbadc72f05c02535793e6ecb0ec45c6f5a9667`.
Source reconstruction, exact fixture layers, the original missing-command
counterexample and both passing pressure cases are indexed in
`factorybox-epoch-cancellation-notes-2e452dda65b449d18837f5ed009c2035`.

The public manual unaccepted-preparation cancellation gap now has concrete
bounded native evidence. Broader final-package/consumer qualification, the
original rare PID and busy-overlay causes, external model acceptance, operating
acceptance and the predetermined series remain open. Production and full
epoch-retirement qualification remain false; the complete goal stays active.

## Historical busy-overlay replay and file-reference observation

The original admission failure remains
`factorybox-runtime-8829166825b0476888776bdfd2c7d5ae`. Its transient foreign-proc
case passes the RAM, descriptor, admission and independent-fork assertions,
then the fixture's immediate ordinary overlay unmount returns EBUSY. The
original test, CLI, component bytes and complete unresolved failure archive
remain retained. No unmount retry, delay before the assertion, lazy unmount,
GC workaround or runtime repair is selected.

`tools/historical_admission_cleanup.py` replays the exact original test binary,
CLI, components, setup and resources. The eleven adapter tests run once and the
six original main admission tests run three times, stopping at the first
failure. The original named inventory requires 29 top-level and 112 subtest
passes. Each run has fresh owned storage and retains the complete source store,
native outputs, cleanup and input checks. The tracer and decoder are frozen
inputs; decoding does not import a subsequently edited parser.

Two earlier diagnostic parents remain failed, despite their native assertions
passing. `factorybox-historical-admission-6a8d8a7465fd4ab0951b16d7d0957ea6`
used an incorrect expected top-level test count. Its corrected outcome
evaluation is `factorybox-historical-admission-evaluation-ca4a7618dbe14c0f9347c4d904d43406`.
`factorybox-historical-admission-3d6c3c8aed8d4d498752bde499e29115` rejected
72 correctly encoded events whose legacy mount ID is zero. Their nonzero
mount pointers and file generations remain observations, not mount-ownership
authority. Evaluation
`factorybox-mount-ref-observation-f3d7801b35ac410aaabdc590dfa75a65` retains the
old/new decoder bytes and confirms every native assertion and loss counter.
Neither evaluation rewrites a failed parent or explains the original EBUSY.

The bounded read-only observer lives in `tools/diagnostics/mountrefs/`, with
the binary event decoder in `tools/mount_ref_events.py`. It observes successful
`path_openat` returns, final file releases and ordinary unmount entry/return
in the exact owned outer PID namespace. A tracked final release may occur on
a kernel worker. File pointer generations distinguish reused kernel addresses.
No file contents or paths are collected. The installed probes have explicit
kernel miss counters and the observer rejects event loss, map failures or
failed kernel reads. Tracking starts only after all paired probes are attached.
It installs no host runtime or kernel, mounts no host filesystem and changes
no global sysctl.

The current kernel's optional BTF layout header is accepted only after every
layout and type-record bound is checked against the pinned reader's supported
encoding. Only a private in-memory header copy is adapted. The original kernel
BTF remains byte-identical and its hash is recorded. This is a diagnostic
compatibility path, not a runtime dependency or additional platform release.

The original LSM-open observer misses O_PATH. Native control
`factorybox-mount-opath-control-834b32b1509940fa83634ae2d37c0f76` demonstrates
that an actual held O_PATH blocks unmount, while the observer reports no held
file interval. Strict coverage evaluation
`factorybox-mount-opath-evaluation-9002c7585a824c48b48153739df941ab` fails.
After observing successful path-open returns, the byte-identical control passes
in `factorybox-mount-opath-control-582619229f6f43189f13f5e78d0928dc`, with
passing coverage evaluation
`factorybox-mount-opath-evaluation-f3232552486046cc8086284c738b7af3`.
Its earlier failed image-inspection attempt, before any container creation,
is separately retained; the corrected invocation explicitly selects the local
Docker context and elevation.

The intermediate normal-file control
`factorybox-mount-ref-control-76c8d54ce59848eca59c2bf2e4548371` remains failed
with three observer errors. Open events could enter before the paired release
return probe was attached. The final observer enables tracking after complete
attachment. The same normal-file control then passes in
`factorybox-mount-ref-control-f3cd53b6f8de45d28a0c091a29b7a225`. Both final
controls record the held-file interval across actual EBUSY, its exact final
release, and successful unmount afterward, with all three error and five miss
counters zero.

Final diagnostic build
`factorybox-mount-ref-tracer-dfa89db897b8405cb842d1d5c0466593` freezes its
sources, pinned helper headers, commands, tool versions and output hashes.
Historical replay
`factorybox-historical-admission-3e67ba7b2e3b416eb3436c2c2f21dece` passes the
complete 29/112 inventory, exit zero, no skips, unchanged inputs/global sysctls
and exact cleanup. All 45 original-test unmounts return zero. Its 421,823
events have zero error/miss counters; the historical EBUSY is not reproduced.
The complete source store is compressed only after full decompression/hash
verification, retaining its original hash
`ec074fe03de2f1ed57c29bdaa58abe21cc2bab015217ba6ce34e4c656cedacb5`.

`factorybox-mount-reference-notes-0773359f1f194faf80060e9ed63ef615` independently
checks the exact named outcomes, frozen decoder, raw trace, both final held-file
controls and 45 selected source-mount observations. Its first analysis attempt
failed because JSON converts numeric dictionary keys to strings; the script
and failure receipt remain, with the corrected comparison serialized before
comparison. Native results are unchanged. All six portable gates pass in
`factorybox-checks-8062c8acf40849a293b97577982f8362`, including 228 Python
tests, reference/fixture verification and enabled Go vet. The diagnostic Go
package also passes explicit vet in the notes artifact.

Storage maintenance
`factorybox-storage-observation-retirement-e32a64ee647e4472a5287f23a603b897`
fully decodes and hashes the superseded LSM observation's complete store before
retiring its 369,085,394 encoded bytes. Every member header and regular-file
hash, plus 4,288,185 bytes of selected diagnostic contents, remain retained.
That superseded complete store is intentionally no longer reconstructable;
its original source, failed result, raw trace, logs and input executables remain.
The stronger complete replacement store stays available. Maintenance also
fully verifies the original unresolved EBUSY archive through its lossless
replacement pointer before deleting only the superseded control payload.
The earlier count-defect store has the equivalent retained receipt in
`factorybox-storage-observation-retirement-beb2f34829f44a81b59c7fb59369ef5f`.

These are observed file intervals only. Cwd, root and namespace references,
opens before coverage and file creation outside the recorded path-open hook
are outside this inventory. An empty observed set cannot exonerate the runtime.
The original busy mount and rare PID conflict remain unresolved. No production
runtime code, upstream assertion, imported overlay or release scope changes;
the full first-release goal remains active and unqualified.

## Focused Proc repetition and observation after failure only

The diagnostic runner now has an explicit `--focus-proc` mode. Both original
`TestProcDirectoryAdmissionNative` cases run twenty times, preserving the held
case immediately before the transient case, every assertion and the original
immediate unmount. All eleven adapter tests still run once. Its exact expected
inventory is 31 top-level and 101 subtest passes. This focused selection is
not the full six-test main admission suite or the final acceptance series.

`factorybox-historical-admission-3efc5930914a47f6b36f4f8a72ab00eb` passes that
complete inventory on the byte-identical original test/CLI/components. The
749,989 kernel events have zero error and miss counters. All 80 original-test
unmounts, comprising source and fork cleanup, return zero. The complete
3,169,845,248-byte store is fully verified before compression; its original
SHA-256 is `607c1aa2a8aaa55627eddf6afc54631c7cfe6e43e54d8697c4a12fccc2bc6039`.
Cleanup, immutable inputs and global sysctls checks pass. The original EBUSY
is not reproduced.

To reduce observation before failure, build
`factorybox-historical-busy-observer-53cb0072f0814550a8cfcd3d82002551` first
rebuilds the untouched historical context. Its binary exactly reproduces
`4e516bc7ccc8f02c4d5c780c2b7d64b55ec335016abebc35e6286291158969db`.
It then applies only the already declared
`tests/compatibility/busy-overlay-diagnostic.patch`, adds its read-only helper
and a separate deliberate fault-control test. The existing production fixture
declaration supplies the engine-test before/after hashes. Every original
production source and assertion remains intact, with explicit vet enabled.
The diagnostic binary SHA-256 is
`9d2ac535a4fae674c5a49025544cb338d02bbc93e75100e5014cf51be3e68c55`.
This is a diagnostic test binary, not a new production worker.

Deliberate control
`factorybox-busy-observer-control-571c9b5b1ed945cfaa679abc8dc96fdf` holds an
owned O_PATH across the original immediate unmount. Its native parent remains
failed with exit 1. A separate evaluation verifies that the post-failure report
finds exactly PID 130, FD 7 and mount 786, with the expected O_PATH flags and
rootfs target. Closing that exact reference and then normally unmounting the
control succeeds. The separate evaluation passes observation coverage only;
it does not reinterpret the original assertion or the historical failure.

`--busy-observer-build` accepts only a completed build that reproduces the
original binary, passes vet and preserves the exact source inventory outside
the three declared test files. It verifies the engine-test bytes differ only
by the post-failed-unmount call. A changed production source, original
assertion, binary, baseline result or missing vet proof is rejected.

Without any kernel tracer,
`factorybox-historical-admission-937991bac29c49618b7dbaac391fc3d2` passes the
same twenty repetitions and complete 31/101 inventory on this declared
diagnostic binary. No busy-overlay report is generated. The complete
3,167,748,096-byte store is fully verified before compression, original SHA-256
`a4ea5630e151632125d8029d7a395b30b4fd3a701f29c3eee5e742ef8f8b85d5`.
Inputs, global sysctls and exact cleanup pass. This does not establish whether
the previous continuous tracing affected reproduction probability. During
the first archive, the normal eight-GiB headroom guard rejects a new build;
that build starts only after verified compression finishes.

All six portable gates pass in
`factorybox-checks-8cbe2269976840e594878846d873a05a`, including 230 Python tests,
the negative observer-build controls, reference/fixture checks and Go vet.
The combined verification is
`factorybox-proc-observer-notes-db4214b02d084947921532c3e44ab31d`. It preserves
two failed analysis attempts: an older run lacks later optional metadata fields,
and forty Proc subcases perform eighty source/fork unmounts. The corrected
analysis verifies the actual frozen binaries and all eighty events; native
results are unchanged. Neither focused run attributes the original busy mount.

## CRIU core gate on the current packaged component

`factorybox-criu-core-1ca92c87ed0a4ee7a902c85a2d790fcf` passes all eighteen
existing selected ZDTM C cases with two actual dump/restore cycles and their
original final assertions. All 1,030 upstream test files remain unchanged.
The native Linux AMD64 kernel/page-size checks, zero hugepages before/after,
owned archive and complete cleanup pass. The tests run in the existing private
native fixture with `/proc/sys/vm` read-only; no host tuning is performed.

The tested CRIU SHA-256 is
`6263dd24728ecb8350486ca829c8776e27f62090bd493f6c733d02d0e7f836d0`, from
`factorybox-build-criu-retained-a3a1bd4425e9455fb58416e2af4d5315`. Combined
verification above reads every archived per-case result, requires both cycles
and final exit zero, and matches these bytes to current worker
`factorybox-build-worker-eef7eaea107d4feea182733019cbd4eb` after verifying its
complete package, bundle and image. This refreshes the selected core gate for
the current CRIU component. It is not the complete ZDTM matrix, final worker
acceptance, a PID-conflict repair or production qualification. The full goal,
both historical causes and remaining release gates stay open.

## Real-provider continuation after cancellation on the current worker

The current worker is still
`factorybox-build-worker-eef7eaea107d4feea182733019cbd4eb`; no production runtime
or packaged component changes in this step. Read-only discovery
`factorybox-free-model-discovery-541110171dc14822aaed958fe2b9de9a` confirms the
explicit free pricing and protocol identity for `glm-5.3-flash-free`.
The matching ordinary native gateway proof
`factorybox-provider-gateway-c418e046b63a4991811df017b52d429a` passes. The separate
pinned OpenCode 2.0.27 protocol proof
`factorybox-external-opencode-95c58a67d29b464e884ec2d3e2d3870c` passes its actual
foreground shell, private RAM/watch and model result/exit ingestion. That small
probe does not claim a checkpoint or restore.

The additive real-provider cancellation path keeps the deterministic client's
original assertions. Its fixture compatibility declaration is
`tests/compatibility/external-cancel-fixture.json`, with exact before/after and
ordered patch hashes. The original inputs are retained in
`factorybox-external-cancel-inputs-599ab279185949f9b1d39c473e9e56d2`.
Only explicit external continuation gets a fresh `continue` gateway lease and
a 90-second operation context. All other cancellation calls keep 30 seconds;
the complete native case keeps its 420-second deadline. The host peer still
checks exact sandbox ownership and actual helper birth, network namespace and
confirmed pidfd exit. The local literal tool-ID and model-log assertions remain
in their original path. The real path instead requires the actual dynamic
call ID, durable structured error, original process exit, independent new
prompts and actual error acknowledgement. Independent host verification checks
that the error reached the provider as the original tool result, rather than
being supplied in the new user prompt.

All six portable gates pass in
`factorybox-checks-fd9831da98c64253a90aad6fa2d7a5c9`, including 233 Python tests,
the negative wire-input cases, gateway continuation lease checks, reference and
fixture verification and enabled Go vet.

The first full attempt,
`factorybox-consumer-worker-loss-61eed6ca5db348adbcafa180cb582167`, uses an
incorrectly chosen 8-GiB test store instead of this full case's earlier 32 GiB.
The actual model/tool interruption and committed pause pass. Recovery fails
while copying private images with ENOSPC. This is a failed overall attempt,
not a passed low-disk safety test. Lossless replacement
`factorybox-storage-repack-730576f905d64c2796461a2731a413f1` retains all
8,590,983,168 original archive bytes after complete decompression verification.
Its encoding is 1,130,514,152 bytes. The original result remains failed.

The complete 32-GiB attempt,
`factorybox-consumer-worker-loss-a53c1101a9424fba8fc173c7e7a34ae7`, uses identical
native test bytes. Both native phases pass, taking 67.14 and 135.73 seconds.
They preserve the actual canceled tool/session through committed pause, a
distinct worker epoch, exact recovery replay and an independent checkpoint/fork.
The recovered parent and the fork each acknowledge their saved tool error in
a new prompt without another tool launch. The fork does so after deletion of
its recovered source. All three optional gateways are released and both
workers are removed.

The complete parent verdict remains **failed**. Actual provider exchanges 2
through 6 return HTTP 429 with `Endpoint is unavailable`. OpenCode retries the
same inference request byte for byte; successful exchanges 7 and 8 then serve
the two independent new prompts. All eight attempts are retained. The default
acceptance guard rejects the provider failures; the same eight records cannot
qualify the required clean three-exchange case. This is evidence of the actual
native continuation through an observed provider outage, not a passed complete
external acceptance or a reason to discard failed attempts. No model/version
switch or mutating prompt retry is introduced. The complete failed store is
preserved. Clean external acceptance, abrupt replacement, uncertain outside
effects and all other first-release gates remain open. The full goal remains
active and production qualification is false.

To preserve the pre-run storage reserve, maintenance
`factorybox-passed-admission-retirement-44b20798dc7c4e289ea7ca7638996645` retires
only eight obsolete passing admission stores. Every former named assertion is
present in the stronger retained complete three-repetition original-binary
proof. Maintenance
`factorybox-passed-worker-loss-retirement-6789c53462dc4601bc50315434484c43`
similarly retires three obsolete passing worker-loss stores and retains the
later complete twelve-test store. Each original stream is fully decompressed
and hashed before retirement; every member header and file hash plus selected
diagnostic contents remain. Original source, component/build inputs, native
results and logs are unchanged. Those eleven obsolete complete stores are
intentionally no longer reconstructable. The original unresolved EBUSY and PID
stores, selected stronger proofs and both new cancellation failures remain.

The full 32-GiB cancellation failure is losslessly retained by
`factorybox-storage-repack-2ea7c3caf1e1493f97b65727d2bf83b8`: all 34,360,786,944
original bytes are verified before the old encoding is removed; the new
encoding is 1,315,496,047 bytes. Both new native failure verdicts stay false.
Evidence verification is
`factorybox-external-cancel-notes-988288fc703a42959a66a5d6da22225a`. Its scope is
verification of actual phase results, exact inference retries, error ingestion,
resource cleanup and preservation of failed provider acceptance. It does not
qualify either failed overall attempt.

Additional maintenance
`factorybox-storage-retirement-fc2245f11e394b7d94365b78ec3a259b` fully verifies
and retires only the old `7bf56d0e` 90-second preparation-fixture timeout store.
The already passing complete `19cdf028` replacement on the same D724 package
uses the declared 180-second full-tree retirement bound, preserves all original
Factory/Theia/agent/successor assertions and records actual retirement at
175.778 seconds followed by exact replay. The old failed result, source, logs,
separate timeout diagnostic and complete member/hash inventory remain; that
superseded complete 32-GiB store is intentionally no longer reconstructable.
This does not retire the original PID, EBUSY or caught-signal failures, either
new cancellation failure, or waive the final EEF consumer/retirement acceptance.

`factorybox-passed-pre-signal-retirement-2a9a9c9601364ffbb33399f806b53f25`
likewise verifies and retires only the old passing `f9fc7488` admission store.
The retained `ef76c4cb` full seventeen-test admission proof uses the same
selected Sysbox binaries, the current signal-capable CRIU and every old named
assertion. Both frozen source/component inputs and native results/logs remain;
the obsolete old complete store is intentionally no longer reconstructable.
After maintenance, allocated generated artifacts are 25,686,261,760 bytes, below
the pre-run threshold with eight GiB headroom. Host reserve checks also pass.

## Clean planned and abrupt real-provider cancellation acceptance

The separately identified clean complete cases both pass on the unchanged EEF
worker and identical native test executable:

- `factorybox-consumer-worker-loss-ca5642913ab742a89b57aa385e3d5cdb`, planned
  replacement, has three actual HTTP-200 provider exchanges. The prepare and
  recover tests take 75.62 and 95.35 seconds.
- `factorybox-consumer-worker-loss-5fbcbf7dc7774800b4242e0cf465a591`, actual
  abrupt loss after its confirmed pause, also has three actual HTTP-200
  exchanges. The recovery test takes 82.64 seconds. The preparation deliberately
  waits after publishing its completed pause so the controller can kill that
  exact worker; it is not reported as an ordinarily returned Go test.

Both complete parent verdicts, cleanup and unchanged global settings pass.
Each requires two distinct worker epochs, exact recovery replay, the original
canceled tool/session/error, an independent checkpoint/fork, separate new
prompts in both descendants, and useful branch work after source deletion.
The actual launch count remains one in each descendant. The wire verifier
checks both original tool-result IDs and error contents independently. All
three optional gateways in each case have confirmed exits; both workers and
the exactly owned volume are absent after cleanup. No failed attempt is
discarded or relabeled.

`factorybox-external-cancel-complete-8277bb2a417a47059a20cc6e1a4e40a2` verifies
both complete frozen sources, native inputs, raw provider responses and their
exact request/error checks. It also revalidates the independent identical EEF
and `85fa866c` packages and image. Both cases use native executable SHA-256
`b4354d84eb4ef0ef9f2cbfcc0e6684f6e9991a49ab9df990fb8c3c36c12d690f`, packaged
CLI `42381f1aaf7696d14ebd62ed296ed57329c718c026054aa27e93f21d05183566` and
CRIU `6263dd24728ecb8350486ca829c8776e27f62090bd493f6c733d02d0e7f836d0`.
The compatibility-input verification additionally proves the original complete
native cancellation test body is byte-identical and the original local
continuation body is byte-identical except for dispatch to the additional
explicit external case.

This closes the actual external cancellation/new-prompt case on the current
candidate for both loss modes. The real-model uncertain-outside-effect case,
remaining ordinary external/final-package acceptance, PID and busy-mount causes,
broader operations/storage requirements and the complete first-release goal
remain open. These development cases are not the predetermined final regular
series, a latency distribution or production qualification. The goal and saved
parallel-chat handoff remain byte-identical.

After the clean replacements pass, maintenance
`factorybox-storage-retirement-605d8ed628704450b398f7cbd0f625b8` fully decodes and
hashes the superseded `a53c1101` provider-outage store before retiring its
1,315,496,047-byte encoding. All eight actual request/response bodies, original
failed result, source, native logs and exact-retry verification remain. The
complete obsolete store is intentionally no longer reconstructable. The
earlier 8-GiB ENOSPC store, original rare-PID/EBUSY/caught-signal stores and clean
replacement proofs remain available. Failed verdicts and the full goal do not
change.

Final portable verification is
`factorybox-checks-c6b49eaf04c64b61b9333fc918956e36`: all six gates, 233 Python
tests, enabled Go vet, unchanged reference/fixture checks and the staged diff
check pass. The existing per-file patch-context whitespace convention now also
covers the two new byte-pinned Go patches; production source whitespace checks
remain enabled.

## Real external model and uncertain outside effects on the current worker

The independent effect-only namespace/stop proof
`factorybox-provider-gateway-683f24afe7e747318fcd2cd62848f436` passes on the EEF
worker. It preserves its endpoint separation, foreign birth/mode refusals,
confirmed process exit, cleanup and unchanged global settings. Its helper bytes
match ordinary inference proof `c418e046`. No production component or worker
package changes in this step.

The combined fixture now keeps an inference peer and a separate outside-effect
peer. Both independently verify the owned sandbox, executable, birth, network
namespace and pidfd exit. A second private IPC directory and explicit outside
Unix socket keep the channels separate. Only arm/finish inference operations in
the combined source copy acquire the additional effect lease. The original
root `opencode_flow_linux_test.go` remains byte-identical to its production
fixture declaration. The ordered copied-source patch and exact hashes are in
`tests/compatibility/external-effect-gateway.json`; the original inputs are in
`factorybox-external-effect-inputs-e1b60916e7fd46ac8fe3563b232b9544`.

The prior cancellation declaration retains its original before/after rows and
patches. An explicit ordered follow-up records only the peer's optional fixed
outside socket. Default inference and cancellation behavior, ownership, birth
and stop checks are preserved. A preflight exercises the existing real-provider
cancellation preparation through this chain. Unknown/redirection sockets are
rejected.

For the real model, the existing uncertain-effect wrapper still submits once,
retains the HTTP-502 unknown outcome and delegates the ordinary client. Only
its explicit remote arm/finish delegation gets 85 seconds inside the original
90-second external context; other calls keep 25 seconds. The complete case
still has its original 420-second deadline. All original RAM, watch, nonce,
counter, exit/result, recovery, replay and independent-consumer assertions stay.
The local deterministic model/client path remains unchanged.

After an actual read-only outside lookup, the held tool appends the exact
reconciliation to its ordinary stdout before the same deliberate exit 37.
The real model must acknowledge the original uncertainty, operation/commit ID
and `DO_NOT_RETRY` alongside its real nonce, RAM hash and exit. Independent
host verification reads the actual provider messages, confirms the receipt is
an original tool result rather than a supplied user-prompt hash, and preserves
the originally model-issued call ID through replacement and fork. The outside
counter deliberately does not deduplicate repeated applications.

An initial portable preflight in
`factorybox-checks-784d0cbf99014742bae0ad07dbad6b6b` rejects inconsistent generated
patch headers before any combined native run. The corrected generator names
the same complete source path as its declaration. Its portable replacement
`factorybox-checks-8addc7a12d1242e2a5d7f5c9cf76ba04` passes; after the additional
peer-chain controls, all six gates pass in
`factorybox-checks-694efd4ef4d942a9baa832dcaad47b4d`, including 241 Python tests,
enabled Go vet and unchanged reference/production-fixture verification.

Both complete real-model native cases pass on the unchanged EEF package:

- `factorybox-consumer-worker-loss-db7d6fd52dce4851a71d7840b6605fd3`, planned
  replacement. Preparation and recovery tests take 58.93 and 107.74 seconds.
- `factorybox-consumer-worker-loss-4b1f18b6eb054a03972647c863907433`, actual
  abrupt loss after the committed pause. Its preparation deliberately waits
  for the controller's exact worker kill; the recovery test takes 119.60 seconds.

Each case has one actual outside application, a lost acknowledgment, the same
outside controller/status through prepare, replacement and completion, and
three delivered HTTP-200 real-model exchanges. Both independent descendants
consume their original unknown result and explicit outside reconciliation;
their launch count remains one. All six optional gateways exit with 0, both
workers are removed and the exact owned volume is absent. The fork remains
useful and completes after deletion of its recovered source. Inputs, global
settings and complete parent cleanup pass.

`factorybox-external-effect-notes-b28824897c2041f685eae16c878b24db` independently
checks both frozen sources and complete inputs, the copied-source delta,
hash-chained ledger and actual status/apply/status/status sequence, original
provider tool IDs, response bytes and all six gateway exits. It revalidates the
independent identical EEF/85fa package and image. The native test executable SHA-256 is
`f64612c00301f7ac0a0aa6590308fe247669e005abc216b919e234ec7a011352`; the packaged
CLI and CRIU remain `42381f1a...` and `6263dd24...`. No new runtime build is
needed for these test-controller additions.

This closes the real-model uncertain-effect reconciliation case on the current
candidate for both loss modes. It does not supply a general exactly-once
guarantee, restore an outside service, qualify the ordinary final external
lifecycle or complete the predetermined final series. The original PID and
busy-mount causes, broader final storage/operations and full package acceptance
remain open. Goal/handoff bytes are unchanged; production qualification is
false and the complete first-release goal stays active.

## External-provider quota and original ENOSPC checkpoint integrity

The complete ordinary external Factory/Theia lifecycle attempt
`factorybox-consumer-worker-loss-455a85b21e124cf4b542d63b0d89978c` is failed.
Its first actual provider request returns HTTP 429 `FreeUsageLimitError`, with
`Retry-After: 8252`. The shipped OpenCode therefore never starts the intended
held tool. The original native test fails after 113.58 seconds; no later
lifecycle, fork or agent-result assertion is claimed as passed. No paid model,
different identity or provider fallback is selected. The complete original
request/response, frozen inputs and failed verdict remain. Owned cleanup and
unchanged global settings pass. This provider limit blocks that external
acceptance case; independent runtime work continues.

Lossless maintenance
`factorybox-storage-repack-db1819cb738f4665adffa9f3379ca58f` verifies all
34,360,786,944 original decoded archive bytes before replacing its encoding.
The new encoding is 698,101,731 bytes and reclaims 707,583,059 bytes. Original
source/results/logs and the exact reconstruction pointer remain unchanged.
The failed native verdict is not relabeled.

The separate original 8-GiB failure `61eed6ca5db348adbcafa180cb582167` remains
failed, with its complete archive still retained in
`factorybox-storage-repack-730576f905d64c2796461a2731a413f1`.
`factorybox-enospc-image-inspection-1084e17061d746189819b46614d73895` verifies
all 8,590,983,168 decoded archive bytes and inventories its exact image.
The image SHA-256 is
`d0df6ea26c51eeabe20486f6704045ec7e1d2225c9d5aa3376b641944d177afd`.

Offline inspection
`factorybox-enospc-checkpoint-verification-276288564dfc4fb1a8f2fadbe1dfc5d1`
then verifies the original checkpoint identity, still-committed paused record
and every one of its 322 published RAM/opaque-metadata files, totaling
501,546,418 bytes. Read-only debugfs leaves the copied image byte-identical.
A preceding per-file compression calibration did not reduce accounted storage;
the calibration is recorded and no compression-based budget reduction is
assumed. The disposable image is removed only after its full hash and complete
original encoded source are reverified; its reconstruction receipt remains.

Native diagnostic `factorybox-enospc-retained-verify-bcedc7fb426d45839d576c062fdacdbd`
first stops before content verification because its UUID comparison expects
the internal unhyphenated spelling. It remains failed with successful cleanup.
The exact superblock bytes independently confirm the canonical UUID spelling.
The declared new-copy correction in
`factorybox-enospc-retained-verify-68b88680fe1948c2afb371ace0b0970b/fixture.json`
changes only that comparison, with exact before/after/patch hashes; all other
diagnostic inputs and the verifier source remain identical.

The corrected complete native diagnostic passes in 66.368 seconds. It verifies
the entire original image before and after a private read-only ext4 mount with
journal replay disabled. Its helper uses the unchanged
`VerifyRetainedFilesystemSnapshot` implementation from the exact EEF packaged
Go source. All nine original filesystem trees, including contents, metadata,
xattrs, inode/base checks and alias checks, match their saved fingerprints;
all 322 published files also match. The original source still has its complete,
unresumed committed pause. The 3-CPU/3-GiB limit, 420-second deadline, exact
read-only/autoclear loop ownership, ordinary unmount, loop release, container/
volume cleanup and unchanged global settings all pass. No diagnostic device
translation was needed in this observation.

Its independent `closure.json` binds the frozen packaged core, identical
diagnostic executable, original failed results, provider error and exact
absence of all owned containers/volumes. Native helper vet, all 232 reference
files, production fixture/ordered-extension verification and the diff check
pass. No implementation changes require another runtime build in this step.

This establishes that the original private-image-copy ENOSPC did not corrupt
its confirmed checkpoint backing. It does not change the original failed
recovery into success, exercise kernel restore or useful work after freeing
space, adopt a copied image as a production store, or qualify general low-disk
recovery. Those broader storage/operations requirements, ordinary external
acceptance after the provider limit, original PID/busy-mount causes and final
package/series acceptance remain open. No production component changes here;
the full first-release goal and saved parallel-chat context remain unchanged.

## Original PID images, namespace controls and independent restore sequence

Inspection `factorybox-failure-inspection-8164cb3403754598b1a716ced1bb09a0`
fully decodes the original PID-failure archive and extracts the root and two
shim namespace images. Together with the previously verified four inner images,
they establish root PID/net/time namespace IDs 10/11/14 and primary inner IDs
24/25/28, with user namespace ID 9 shared throughout. These are saved image
identities, not an observation of the process occupying the conflicting PID.

Small native controls keep the selected EEF Sysbox and CRIU bytes and all
original actor/lifecycle assertions. Baseline
`factorybox-pid-inner-net-control-2df5288a14b3442b89e8111ed9fc4c3b` passes once;
private inner network `factorybox-pid-inner-net-36dbb8c33de64934ba4759bbba6bc7f4`
passes three predetermined cases; private inner time
`factorybox-pid-inner-time-direct-eb732ca25bfe4fd3b67f5fec9d377142` passes three.
The time fixture verifies distinct time namespaces at zero initial offsets and
the same network namespace. Network and time are separate single-variable
controls. Their nonreproduction neither excludes an interaction nor qualifies
the historical or final package lifecycle.

Four earlier time-fixture setup failures remain failed with their complete
results and cleanup. The initial launcher requested an unsupported BusyBox
time option. Subsequent capability checks exposed a wrong-root executable/libc
check and then an unnecessarily strict help requirement. The final fixture
queries BusyBox inside its actual root and creates time through the direct
unshare syscall. Exact before/after/patch hashes retain each correction; no
original actor assertion or production runtime is changed.

Predeclared sequence
`factorybox-pid-image-sequence-607904b9022e445b8ad2346db86f8ccc` then completes
ten independent restorations of the actual original process images, each with
an exclusive worker and volume and stopping at the first failure. Every case
fully verifies the 12,155,092,992-byte original decoded archive and all 330
prepared image files, totaling 515,577,040 bytes, before restoring. It uses the
original C9a worker image and Sysbox components with current read-only CRIU
diagnostics `6263dd24...`. All ten fresh restores succeed, taking 1.827–1.931
seconds; all owned resources close, inputs and global settings remain intact.
These restore-only times are not useful-application latency measurements.

One vetted diagnostic executable from completed helper build
`factorybox-build-pid-image-tests-cead30c634bb42d2a46036e6e3eafa75`, SHA-256
`e704934e412f76e7fae8c66e6ac1183f5d9c9081ca96c80bd1b8d47cd14a7e40`, is shared
immutably across the cases. The added assertion makes a fresh observation of
the exact original collision fatal immediately after saving its evidence.
All prior assertions remain, including rejection by the unchanged filesystem
guard because the historical archive lost nanosecond mtimes/xattrs/ACLs.
No copied historical restore log counts as a new observation.

The sequence's independent `closure.json` checks every native verdict, exact
helper/core/component identities, full-decode receipt, fresh log, resource
absence, original namespace bytes, retained setup failures and unchanged
goal/handoff. The ten successes do not explain or fix the rare collision and
are not members of the final complete-lifecycle acceptance series. The original
failed lifecycle and strict replay remain failed and fully retained.

The next concrete candidate is the old Sysbox pass-through path: some filesystem
operations dispatch an nsenter agent into the requesting PID namespace, and
its nsexec path forks after setns and creates a session. The closure pins the
exact original source files. Actual occupation of the conflicting ID by such
an agent during restore is unproven; it requires process/PID-lifetime evidence
through the full checkpoint/pause/resume/restore chain. No runtime workaround
or repair is selected from this source-level hypothesis. Production
qualification stays false and the complete first-release goal remains active.

The read-only lifetime observer is subsequently checked against an independent
private PID-2 allocation/setsid/reap control. Initial image attempt
`factorybox-pid-image-lifetime-21ca2ca1caf9421d8317a802510a5a62` fails its BTF
preflight before restoring: syscall-wrapper symbols exist but lack BTF function
records. Only prototypes for arguments actually read need those records;
return probes still require actual attachment and the verified return-register
layout. Corrected build `factorybox-build-pid-reference-tracer-b547294e38b142beaa0b0003951d23cf`
loads, but control `factorybox-pid-trace-control-c3d055db54994129981a017aca24d447`
fails because the namespace helper filters out descendants of the outer PID
namespace. Zero loss counters alone did not establish coverage.

Build `factorybox-build-pid-reference-tracer-ba2a3827a42143da8e206137a3560be0`
keeps that loader and changes only membership lookup to the exact ancestor in
the task's typed PID namespace chain. Control
`factorybox-pid-trace-control-3ed1299a56a64a809fef76e8d50de99b` now matches the
independently recorded PID 2, namespace, successful setsid and final PID-object
release. All five probe miss counters and all loss/map/read error counters are
zero. Both failed diagnostics remain failed with their full source/results.

Observed original-image restore
`factorybox-pid-image-lifetime-bb54440c659742be850cd1aff5742d6f` then passes with
849 lifetime/session/group events and the same zero counters. Its original
process-image executable is unchanged. In the primary namespace, a random PID
2 is allocated by a caller named `runc:[1:CHILD]`; `runc:[2:INIT]` successfully
creates session 2. Its final PID release occurs in `sysbox-fs` context, about
11.97 ms before CRIU successfully allocates the saved PID 2. This is actual
transient occupation of the needed ID during the restore, rather than a toy
process that merely could reserve it. Caller names alone do not attest an
executable or identify the occupant of the earlier historical failure.

The independent `analyze.py`/`closure.json` checks the observer control, all
849 records, primary namespace mapping, fresh original-image restore, complete
cleanup and the remaining filesystem limitation. The original Sysbox-fs source
receives the agent response and schedules its wait asynchronously; its general
reaper can also reap completed agents. The next falsifiable experiment is to
defer those actual reaping paths in an explicitly diagnostic component, then
check whether the unchanged restore reproduces the exact historical collision.
No runtime repair or causal closure follows from the successful observed run.

Lossless maintenance retains the initial observer-preflight failure's complete
2,824,863,744-byte dump in
`factorybox-storage-repack-ad9402020de14ce0b2b2b2ffab9a22c3`, reclaiming
392,921,131 encoded bytes. Subsequent closed-executable/stream compactions
initially remain below the required native-start headroom; their failed capacity
verdicts are preserved. Two still-selected gateway inputs were included too
broadly and are reconstructed exactly, with directly usable original hashes,
in `factorybox-storage-restore-selected-gateways-bdaee9184ce7477abf89576b4d4e65c2`.
`factorybox-storage-share-new-encodings-fb5bf31be7c542f89138530b5c902d61` then
shares 42 identical new encodings/inputs, reclaiming 175,243,264 allocated bytes
and restoring the required headroom. The observed replay's closure fully
decodes all 68 executable encodings, 15 streams and the retained failed dump;
selected builds, original failures, source, results and logs remain intact.

## Exact PID collision from delayed real-agent reaping

Predeclared two-case experiment
`factorybox-pid-reap-causality-09d1f6ed305f414c9ed8246345c92f3a`
uses the same diagnostic Sysbox-fs executable in both cases. With the fault
disabled, original-image restore passes in
`factorybox-pid-image-reap-22c39886e65e4f6fafd139bcb04011c2`.
With only the actual asynchronous agent-reaping paths delayed by 500 ms,
`factorybox-pid-image-reap-10b856f2dc50489bac28f2966cd4469c` fails with the
exact original `Can't fork for 4463: File exists`. Its native result remains
false and its complete failed volume remains retained.

The unchanged original process-image test executable is `e704934e...` and
CRIU remains `6263dd24...`. All 330 original image files, totaling 515,577,040
bytes, are verified. The original filesystem metadata rejection remains an
assertion; this diagnostic cannot qualify the original full lifecycle.

The failed restore's fresh proc/pidfd evidence identifies PID 2 as an unreaped
`runc:[2:INIT]` zombie, with process-group and session ID 2. The independent
controlled kernel trace observes the real helper's random PID-2 allocation,
its successful setsid, and the absence of final PID release before CRIU's
requested PID-2 allocation returns EEXIST in that same PID namespace.
All three read/loss counters and all five missed-probe counters are zero.
CRIU's namespace-based reference scan itself reports matched=0/errors=2 for
the zombie; that scan cannot substitute for the direct proc/pidfd and lifetime
observations. `closure.json` checks the complete causal pair independently.

`selected-source-applicability.json` also verifies that the selected Sysbox
source retains exactly the same asynchronous completion/reaping functions.
It checks the actual pristine runc archive that Sysbox-fs reextracts before
compiling, rather than treating a different patched context copy as that
import. The earlier uninstrumented historical occupant was not observed;
the evidence establishes the same exact collision class with the original
images under controlled scheduling.

Lossless repack `factorybox-storage-repack-c11691e412b44d0599682984ed898b2f`
fully verifies all 2,824,863,744 bytes of this new failed volume and reclaims
392,648,941 encoded bytes. Both this current counterexample and the complete
original PID failure remain reconstructable. Separately,
`factorybox-storage-retirement-422693c0c9f74aa4869d59ab7c5cc49c`
retires only the superseded observer-BTF preflight volume after its passing
replacement `bb54440...`, preserving complete decompression verification,
all 59,103 member records/hashes and 2,489,074 unique diagnostic bytes. That
obsolete setup volume is explicitly no longer fully reconstructable; its
failed verdict, sources, logs and retirement pointer remain unchanged.

Ordered candidate `criu-nested-pid-counter-candidate.patch` now addresses a
specific gap: root PID-counter seeding did not seed fresh inner namespaces.
It opens each new inner namespace's own proc mount after the userns mapping
handshake and before credentials/mount preparation can trigger helpers.
It retains the inherited root proc service FD and the original root seeding
path. Build `factorybox-build-criu-retained-5e4fcc79d85b44a59d5fcfd8523fd78d`
produces CRIU `8c9a9b0d...`; all 1,030 upstream test files remain unchanged
and the 31 ownership cases pass. This build result alone is not a runtime
fix proof or selection. The exact original-image fault/control checks are
predeclared in `factorybox-pid-inner-seed-proof-471fa08b08ad4b9b9b1b41a965eb8128`.
Complete native/package acceptance and every other first-release requirement
remain required; production qualification remains false and the goal active.

The predeclared seeded pair subsequently passes both complete native image
checks: delayed-reap case `factorybox-pid-image-seeded-d5264f39b96e416a9f1cdb9ca9082abc`
and ordinary control `factorybox-pid-image-seeded-b97e1c76c2dd4fecb1ea947f30ae75f8`.
The same real helpers now receive random PIDs 5494 and 5499 in the primary
inner namespace. Both PID objects remain unreleased while CRIU successfully
allocates the original saved PID 2. The fault remains enabled in that case;
the control has no fault marker. All observer error/loss/miss counters are zero,
all original image assertions pass, and both owned containers/volumes close.

The first supplementary analyzer fails because it incorrectly expects the
allocation generation in a session-change event. Those session records are
explicitly zero-initialized by the frozen observer and have no generation.
Its exact failed execution and unchanged source remain recorded in
`factorybox-pid-inner-seed-evaluation-78b7c3dfa896436b87f1a12fa5314f98`.
The declared new-copy analyzer requires zero there, and binds the successful
session operation to the same namespace, PID-object pointer, local PID and
expected helper name strictly inside the allocation's unreleased interval.
Allocation/free generation matching remains required. Exact before/after/patch
hashes document this interpretation correction; no native input, assertion,
outcome or frozen pair plan changes. Its independent `closure.json` passes
and also verifies that precisely the three declared CRIU files differ.

The new CRIU then passes all 18 declared unchanged upstream core cases, each
with two ordinary dump/restore cycles, in
`factorybox-criu-core-2785f6847873472b90af3db3d80f7333`.
All six portable check groups pass in
`factorybox-checks-d8335db4608e4b76894123cb1f2e4f09`, including 241 Python tests,
Go tests/vet, all 232 sealed reference files and ordered fixture verification.

Worker candidate `factorybox-build-worker-a66c5db009d5458b9f5d85b7c233516e`
assembles this CRIU with the current SHM-corrected Sysbox components and the
unchanged packaged CLI `42381f1a...`. Its deterministic raw package hash is
`d89ecaa02f23b59bdda4ad40bbe64f7ba134b36bb223d3adf78105166b160e0b`;
the image is `sha256:95752232a5fab113bf9d5d40ac2dc1515738d78e09738478d640364d4fbf68e9`.
The encoded package has complete decompression verification and the original
source/byte reconstruction receipt. This is an unselected candidate: its
complete consumer/regression proofs, independent reproduction and every
remaining first-release requirement still need acceptance.

The first complete bounded Factory/Theia/local-model lifecycle on these bytes
passes every original assertion in
`factorybox-consumer-worker-loss-5451d610d05c4cc68f5b12f25f171c7c`.
The native developer test takes 152.14 seconds, including useful resume,
resumed-source checkpoint/fork, independent descendants and real Connector
edits through the same separate Theia. The delivered 32-GiB store, ordinary
worker exit 0, exact owned cleanup and unchanged global settings all pass.
This uses the deterministic local model fixture, not an external provider.

The existing strict timing extractor records prepared useful work 22.531 s,
cold worker-to-useful work 59.903 s, first useful fork 6.585 s, source capture
plus useful fork 13.819 s, pause 13.238 s, useful resume 11.709 s and resumed
capture plus useful fork 33.732 s. Source anonymous memory falls from
221,696,000 to zero and resident snapshot bytes from 515,006,464 to zero;
4,923,392 source cgroup bytes remain. This is one development observation,
not a matched speed comparison, whole-worker zero-memory claim or member of
the final predetermined series.

Independent component build
`factorybox-build-criu-repro-08e68692c4f64db9b8316187778ee915`
starts from the original pinned tar archive, newly copies and verifies each
declared overlay/patch input, and reapplies every ordered patch without fuzz.
Its entire assembled source tree equals the candidate before compilation.
The same pinned isolated native toolchain, with no copied object/cache/binary,
produces exactly `8c9a9b0d...`; all 1,030 upstream source tests remain unchanged
and its 31 ownership cases pass.

Independent worker build
`factorybox-build-worker-a8be76d32fdf43078109e68af7c73008`
then independently compiles the same Go inputs and reproduces every bundle,
raw-package, image and layer byte from A66. Frozen identical components are
shared read-only, with the CRIU clean-source rebuild proved separately.
The independent `packaged-closure.json` in
`factorybox-pid-inner-seed-evaluation-78b7c3dfa896436b87f1a12fa5314f98`
binds both reproductions, the exact red/green evidence, original assertions,
delivered CLI/component identities, bounded store and complete native outcome.
The remaining native/package regressions, external provider, worker loss on
these new bytes, busy-mount attribution, broader storage/operations and final
series remain open. The full goal and saved parallel-chat context are unchanged;
production qualification remains false.

Post-build maintenance
`factorybox-storage-share-pid-fix-rebuilds-89273b937870487c81ce8d7412b9c626`
fully revalidates both independent worker builds, their complete package
decompression and the separate clean CRIU build before sharing three identical
closed outputs. All paths, bytes, owners, modes, sources and build results remain;
the receipts retain original/shared mtimes. It reclaims 57,790,464 allocated
bytes and restores the required 8-GiB native-start headroom within the 32-GiB
artifact budget. No failure dump is removed by this maintenance.

## Descendant mount-reference coverage and historical Proc replay

The existing mount observer covered successful path opens only in the active
outer PID namespace. Control
`factorybox-mount-descendant-control-5e7f788aa487470cacc9aa196b24402e`
demonstrates the blind spot: a real O_PATH directory held by PID 1 in a child
PID namespace makes ordinary unmount return EBUSY, but the observer misses the
held interval. Releasing that exact helper makes ordinary unmount succeed.
Its kernel-control verdict passes and its observer-coverage verdict remains
false. This is not a reproduction or attribution of the historical failure.

New diagnostic build
`factorybox-mount-ref-tracer-97707c9501da4164a0328a551d71804e`
walks the typed current task's PID namespace ancestry and matches the exact
owned outer namespace, returning the PID visible there. Its loader verifies
the live kernel's upid layout. The five file/unmount probes, event format and
file-generation rules are unchanged; Go vet passes. The byte-identical control
sources pass with this observer in
`factorybox-mount-descendant-control-b8df8af032a442609f75bfebfc50fdb1`.
The observed child's held interval spans the real EBUSY and ends before
successful cleanup. All read/loss/map and missed-probe counters are zero.

The predetermined twenty paired held/transient original Proc cases pass in
`factorybox-historical-admission-f7b2dfe70cd844b3871ffd8b5d0921f5`.
The exact historical production sources, CLI and components remain unchanged.
The existing diagnostic test binary adds observation only after failed ordinary
unmount, preserving every original assertion and the immediate cleanup boundary.
All 31 top-level and 101 subtest outcomes pass without skips; owned cleanup and
unchanged global settings pass. This is not the full six-main-test admission
suite or the final release repetition series. All 870,871 events have zero
observer errors and missed probes. Descendant runc init unmounts, absent from
the old outer-only observation, are now visible.

Independent evaluation
`factorybox-mount-descendant-evaluation-8df3c072b1b74955a3982229b17b55ee`
rechecks both controls, frozen sources/binaries, counters and the full trace
digest. Every one of the 80 original-test unmounts succeeds with no observed
successful path-open interval spanning its entry. The last observed release
caller is original.test in all 80 cases; entry follows that release by 5.727
to 195,732.379 microseconds. These are descriptive event gaps, not a latency
distribution or attribution. Cwd/root, namespace references, other file
acquisition paths and pre-coverage references remain unobserved. Mount pointer
and legacy-ID reuse do not establish persistent mount identity. The original
EBUSY remains unexplained, retained and a release blocker.

The Sysbox-mgr shutdown warning is not evidence of a surviving container:
unregister sets a retained container-table entry to stopped, while shutdown
prints every table key without checking that state. The same warning appears
on these successful runs. No runtime change is justified by it.

The latest complete passing store and all 62,702,712 trace bytes remain
losslessly reconstructable after complete decompression verification. Maintenance
`factorybox-passed-proc-retirement-025f19c3065d45349de5e0e1a199b363`
retires only the two older closed passing twenty-pair stores, 3efc5930 and
937991ba, after verifying the full original unresolved failure and the newer
passing store. Every old member header/hash and selected diagnostics remain;
their large old stores are explicitly no longer fully reconstructable. Native
results, sources, executables, traces and logs remain unchanged. The retained
preflight errors concern Python regex escaping and the older no-tracer result's
exit_code field; declared new-copy continuations preserve those failures and
all retirement checks. Instrumentation can affect timing; retirement does not
explain EBUSY or qualify production. The goal remains active.

## Current-package admission, capture interruption and worker loss

The exact core shipped in independently reproduced worker A66 now passes the
complete admission suite in
`factorybox-runtime-3fc216eb1aca4a5faefa0b7f6a5ee255`: 17 top-level tests and
78 subtests without skips. Its five native admission test source files are
byte-identical to the original historical failure's files. This successful
current-package run neither reproduces nor explains that historical EBUSY.

The first serialized batch,
`factorybox-pilot-native-gates-730fe939759044e691b854e6feb8025e`, remains failed:
admission passed, but the next suite's native-run allocation was refused by
the required 8-GiB headroom check. That second test never started. No failed
assertion is relabeled or removed. After closed-store maintenance, the separate
remaining batch
`factorybox-pilot-native-gates-42af4cbe859a4e2b9bbd4381d2eda01d` passes both:

- Actual CRIU interruption:
  `factorybox-runtime-41cc6d773c884386bec27d83d6bfb2dd`, 12 top-level tests and
  61 subtests, no skips.
- Actual capture-worker loss:
  `factorybox-runtime-1737c5a91bd441d8bf1dd3063fcdb792`, 12 top-level tests and
  64 subtests, no skips.

Every suite preserves source identity/useful work, independently useful prior
backing and incomplete-operation evidence under its exact declared fault.
Owned cleanup, unchanged sources/inputs/components and all original assertions
pass. These are in-process regression binaries from the delivered core with
the shipped Sysbox/CRIU, not complete external-provider consumer proof. The
default native gate list now also includes the existing interruption suite;
its required assertions are unchanged.

Independent verifier
`factorybox-candidate-regression-notes-6a5f099066f942129d3161e21b281c7e`
binds those three results, their full archive decompression and frozen inputs
to A66's exact component/package identities. It also rechecks the existing
successful Sysbox component evidence against the identical shipped binaries
and component source manifest. The raw Go JSONL records confirm 727 passes
and the 18 original skips, including 28 top-level libcontainer unit passes and
47 integration passes. Bats TAP still contains seven lifecycle passes and four
systemd passes with its original two skips. The new CRIU's eighteen core cases
with two cycles remain separately bound. These unchanged component proofs do
not need new runs solely because the Go worker package is assembled again.
They do not qualify skipped paths or any missing whole-worker regression.

To restore start headroom, maintenance
`factorybox-passed-admission-successor-retirement-5750a5e226774e63b29719c4fcc7add0`
retires only the earlier passing 3e67ba7b store after full verification of the
original unresolved failure and current passing store. The original 95 named
assertions and all five test sources are retained unchanged in the current
proof. The old diagnostic had three repetitions; the current package has one.
No repetition equivalence is claimed. Its complete old store is explicitly
unavailable after retirement, while results, test inputs, trace, all member
headers/hashes and 4,293,257 unique diagnostic bytes remain. The actual original
failure and latest selected successful stores remain reconstructable.

Immutable input sharing keeps every file path and byte. New explicit
`--include-closed-runtime-tests` support also shares identical encoded lifecycle
tests only for passing, stopped native runs bound to inputs_sha256 and complete
canonical decompression. A concurrent maintenance run exposed an accounting
defect: the old encoded-test receipt reported the change in all artifact blocks
as if that operation alone reclaimed them. The frozen f1774eb6 result's
223,756,288 bytes are a combined concurrent allocation observation, not isolated
encoded-test reclamation; both successful byte-sharing receipts stay unchanged.
The corrected tool counts only the last-link blocks it replaces and reports
the global snapshot change separately. A real filesystem regression reproduces
the old 2,084,864-versus-4,096-byte misattribution under unrelated cleanup and
passes after the fix. The independent verifier retains the exact old-runner
red and corrected-runner green logs. This changes maintenance reporting only.

The historical EBUSY, remaining current-package native/consumer/storage checks,
manual operating-path acceptance and predetermined final series remain open.
Production qualification remains false and the complete goal stays active.

All six portable check groups pass after these changes in
`factorybox-checks-3c28f8d0df8a45aca70e8f5465398e02`: Go tests with vet enabled,
the worker-probe compile, 242 Python tests, all 232 sealed imported files,
ordered fixture verification and diff validation. The earlier successful
check run before the maintenance-accounting change remains separate.

## Root/cwd diagnostic controls and combined historical replay

The optional diagnostic in `tools/diagnostics/fsrefs` records coherent last
root/cwd snapshots for observed fs_struct generations in the owned PID
namespace tree. It does not change a runtime, delay an unmount, retry cleanup
or provide ownership authority. Eight kprobes cover copy/setter and unmount
boundaries; the kmem_cache_free allocator tracepoint ends tracked generations
without reading freed memory. The loader checks live kernel BTF, register and
namespace layouts, the nested sequence counter and allocator argument layout.
The decoder keeps pointer generations separate and preserves an independent
root reference when cwd changes. Two portable regressions exercise those rules.

The initial build `factorybox-fs-ref-tracer-ea55cbeb919d42a2b952ce4653613419`
fails actual kernel loading because its assumed sequence-counter layout differs.
Its control `factorybox-fs-reference-control-6f05e073fe214786ac4f3003c526ee19`
remains failed. Corrected build
`factorybox-fs-ref-tracer-775b596b5b604afc8bde6cad127c8121` loads and observes
held cwd/root, but free_fs_struct function probes miss real process-exit
release on this kernel. Its control
`factorybox-fs-reference-control-d53f774cc20f411f9db9d710e524018d` passes the
actual kernel operations and then fails host decoding on root-owned output.
A privileged read-only decode establishes the missing release events and stale
snapshots. Neither earlier verdict is rewritten; the exact missed kernel path
is not established by this observation.

Build `factorybox-fs-ref-tracer-44dffef8db3c488691c8e478330ed541` passes vet
and native control
`factorybox-fs-reference-control-d32071f3a3054426a5f8a0352a7dac85` with the
allocator release tracepoint. Its three descendant-process cases hold only cwd,
hold chroot root/cwd, or inherit cwd through fork after the creator changes
away. None holds an open descriptor on the target mount. Ordinary unmount
returns EBUSY while held and succeeds after exact helper exit. Each observed
held generation ends before successful cleanup. Four error counters and eight
kprobe miss counters are zero. The raw tracepoint shares the bounded event ring
but has no kretprobe instance-miss counter. Output ownership is set only after
the observer shuts down, enabling ordinary host decoding.

The independent evaluator initially rejects exact fixture-copy equality.
Its preserved failed source is superseded by an explicitly declared new-copy
evaluator, not an assertion relaxation. The three control.py copies have
SHA-256 5394fa6488a85cfb82825231204976040ec1b37afd7ac5ba4e5c981462befaf4,
f3c77bc0038f4e3a75a65e703a7ff9a23dafc1d892f86e19e9b2584c9825d89e,
and 8101efc56514b305f7ed515f318194b06d6ff8f3ee904d99dae23f6a62f51013.
Each later copy is exactly the preceding bytes plus one EOF newline. The final
shell adds only post-shutdown output ownership and one EOF newline, changing
a82bcf51fd4f8f676268f99193aa8bd3f7ab2e830cb270234a0df5459eb0eedb
to 65309c69f12c7aa71a7662296101ad53f80834988b2b0f44260cc1648396b9b8.
All executable control bodies and native assertions remain intact; the exact
declaration, hashes, patch and initial analyzer failure are retained in
`factorybox-fs-reference-evaluation-576399cf7a9649b88a7b8e60147607c9`.

Combined historical replay
`factorybox-historical-admission-cee2f6b7d93e499bac5451d9ede871d4`
passes the predetermined twenty original held/transient Proc pairs plus the
eleven original adapter tests: 31 top-level tests and 101 subtests, no skips.
The historical production sources, CLI, components and immediate ordinary
unmount assertions remain unchanged. The existing diagnostic test binary adds
observation only after an unmount failure. Both observers are ready before
testing. There are 871,199 file events and 41,494 root/cwd events, zero error
and missed-probe counters, exact owned cleanup and unchanged inputs/global
settings. This is not the full admission suite or final release series.

Independent evaluation in the same 576399cf artifact verifies frozen sources,
builds, controls, trace identity and counters. All 80 original-test unmounts
return zero and have no last-observed context snapshot matching the target at
entry or return. Last setter/copy snapshots can be stale after namespace copy,
pivot_root and other kernel updates; pre-coverage and in-progress references
and namespace-only holds are outside the inventory. Allocator release is an
upper lifetime bound. Empty or overlapping observations cannot independently
attribute or exonerate the historical runtime. The original EBUSY is still
unreproduced, unexplained and a release blocker. No production component changes.

Both raw traces have complete byte/hash-verified compression receipts. Immutable
closed-input sharing in
`factorybox-storage-share-historical-inputs-0b32003f6d5447c8a862adcd44a8cad4`
reclaims 23,818,240 allocated bytes without changing input paths or contents.
The complete new 3,164,602,368-byte store is verified before and after lossless
re-encoding in `factorybox-storage-repack-b6263704fa364c94a9a1cc5097ab446d`,
reclaiming 62,817,909 bytes and retaining exact reconstruction receipts. All
selected unresolved failures and the previous passing store remain retained.
Allocated artifacts are 25,732,059,136 bytes at the next-turn preflight, within
the 32-GiB budget and with the required 8-GiB start headroom. The intervening
user-requested status/performance explanation changed no implementation or
qualification state; this continuation resumes concrete acceptance work.

All six portable check groups pass in
`factorybox-checks-a682653445b143b4afd5075ec6bdf2a7`, including Go tests with
vet, worker-probe compilation, 244 Python tests, all 232 sealed reference files,
ordered fixture verification and diff validation. GOAL.md and the saved
parallel-chat handoff retain their previously recorded SHA-256 identities.

## Current-package basic gates and reproducible restore-controller-loss failure

The current A66 core passes the complete basic lifecycle suite in
`factorybox-runtime-056c8b690897413a868b7a48c6daa565`: six top-level tests and
three subtests. Its source guard passes in
`factorybox-runtime-b826cea65d414aca9c944bc952ce3fcc`: one top-level test and
twelve subtests. Both have zero skips, exact owned cleanup and unchanged
sources, inputs and components. The first batch
`factorybox-pilot-native-gates-e19bde208f86415083309f475deeb3f0` remains failed:
lifecycle passes, then the required artifact headroom refuses guard before it
starts. A separate remaining batch
`factorybox-pilot-native-gates-598b6332589745baa63bbe16ffac5b56` passes guard
and stops on a real capture assertion failure. Neither batch is relabeled.

The complete capture gate fails in
`factorybox-runtime-f4c3aecdb75548d392ea4ffae284695d`. The unchanged
`actual-controller-loss-after-restore` case stops its exact public controller
after observing the matching direct restore backend. That backend does not
exit within the existing twenty-second deadline. Eleven adapter tests and
seventy subtests pass; the required enclosing capture test fails, with no skips
or OOM. Later recapture/delete attempts also cannot acquire the still-held
operation lock. The full selected failure archive, every input and all original
assertions remain. This gate blocks release independently of the unresolved
historical EBUSY. No test timeout, assertion or production runtime is changed.

The first passive observation attempt in
`factorybox-restore-controller-observation-deb276800cf54d17adf1cb55834a219c`
uses generic Docker inspection before its same-named container exists and
mistakes the already-created volume for a container. Its KeyError is retained.
The child runner subsequently disappears without its outer result. Its exact
container is independently confirmed stopped with exit 1 and its stdout repeats
the same original failure after twenty seconds. The separate frozen
`reconcile.py` verifies the complete source and executable/component inputs
against F4, archives the entire stopped owned volume with complete decompression
verification, and removes only that exact owned container/volume. Its outcome
remains false, with no process snapshot or original outer qualification claimed.

Declared new-copy observers inspect containers explicitly and always wait for
the native child even if observation fails. The unchanged full native capture
gate fails again in `factorybox-runtime-c745f204225b4280b0c18dbde56dd68a`,
`factorybox-runtime-3ec1b28098704bd2acbd01107a1f9dec` and
`factorybox-runtime-27569afbca654de5bbcb82dd393abc5e`, each with the same
eleven top-level passes/seventy subtest passes, no skips, exact owned cleanup
and unchanged source/component/executable inputs. These are diagnostic repeats,
not members of the final ten-run acceptance series. Every failed verdict stays.

The immediate snapshot in observation CBE2564B confirms the exact stopped
controller and direct matching Sysbox restore child, but is too early to locate
the later wait. Observation
`factorybox-restore-controller-observation-61e63a1ef27b4158811489594598326c`
waits two seconds after observing the already-stopped controller, then reads
only its descendant process/thread state and exact target cgroup. The target
is populated and frozen; its restored actor is still traced. The prepared
runtime is polling and CRIU is awaiting an RPC response. No held/final restore
receipt exists. The pinned runtime's pre-resume protocol sends the exact held
task identity, waits up to three seconds for the controller's sealed-request
acknowledgement, and only later publishes a held receipt. The Go controller
performs that acknowledgement and subsequent timer activation itself.

Observation
`factorybox-restore-controller-observation-096c6d3f3c4141169ab55a750caff045`
instead reads the same failure window after ten seconds. CRIU is now in wait4
for the exact namespace-init PID found in the same descendant snapshot; that
init is in zap_pid_ns_processes. The prepared runtime and outer backend wait
for their children. The target still requests cgroup.freeze=1, is populated,
and reports frozen=0 during teardown. Neither completion receipt exists.
The snapshots and pinned code support a controller-dependent acknowledgement
timeout followed by incomplete abort teardown. They do not yet identify the
remaining namespace member/reaping dependency. No repair, historical EBUSY
attribution, successful controller-loss recovery or production acceptance is
claimed. The original red gate must be preserved while correcting completion
independence and/or exact owned abort teardown, including the existing
pre-activation identity, freeze and timer protections.

Independent `evaluate.py` in the ten-second observation binds the six complete
native outcomes to A66's exact core/components, verifies every frozen input and
fully decodes all four failed capture archives and the separately reconciled
complete failed volume. It binds both snapshot phases to their actual native
results and the pinned controller/runtime protocol source. Diagnostic success
means the failure was observed and retained; capture qualification remains false.

Storage maintenance keeps the required reserve without deleting failures.
`factorybox-storage-repack-3f59e18998184bfaa22d2b0c9d18ac48` losslessly
re-encodes the current successful admission store after full original/replacement
decompression, reclaiming 16,379,996 bytes. Old successful EF76 admission and
06A1 worker-loss stores are retired only after their current A66 successors
pass the same named native assertions. Their source-compatibility declarations
retain exact prior/current test hashes and explain the unrelated metadata
profile, timer dispatch and added bounded-storage test; every prior assertion
body remains. The original sources, native results/logs, compiled inputs and
all archived member headers/hashes plus bounded diagnostics remain. Only these
two older complete passing stores cease to be reconstructable; current stores
and unresolved failures stay complete. The first retirement batch
`factorybox-passed-native-successor-retirement-2958c727fa7e4bd7a1327739a28719a8`
retires EF76, then its exact import-spacing comparison rejects the remaining
pair before that archive is touched. Its failure is retained. The declared
new copy `factorybox-passed-worker-loss-retirement-43633d5881d84d1db83033763097305d`
restores the one pre-existing separator newline in that comparison and retires
only the remaining 06A1 store after all unchanged checks pass.

Explicit `--include-closed-failed-runtime-inputs` extends immutable compiled
input sharing to failed runtime suites only with confirmed cleanup, stopped
containers, unchanged complete source/component flags, reverified full source
manifests and exact owner/input hashes. Identical input paths/bytes/modes/owners
remain; receipts retain original/shared mtimes. Failed results, logs and dumps
are not rewritten or retired. The first such actual maintenance,
`factorybox-storage-share-inputs-4021d8cf62c64b64bd418e4171f5ed32`, verifies and
shares five inputs, reclaiming 101,572,608 allocated bytes. Subsequent closed
diagnostic runs use the same guards. The separate interrupted native without an
outer result is ineligible and retains its direct inputs unchanged. No new
performance optimization or runtime service is introduced; the full goal stays
active, with both current restore-controller-loss and historical EBUSY open.

All six portable groups pass after the sharing-tool extension in
`factorybox-checks-0bc12b09338e4438b29cc2b0022252dd`: Go tests/vet, worker-probe
compilation, 244 Python tests, all 232 sealed imports, ordered fixture checks
and diff validation. These portable successes do not override the failed
native capture gate. No labeled Factorybox container remains, and allocated
artifacts are 25,761,652,736 bytes, preserving the required 8-GiB start reserve
inside 32 GiB. Further generation must repeat its capacity check; the margin
above that reserve is small. The full goal and parallel-chat handoff are intact.

## Independent restore completion preserves the original controller-loss assertion

The public controller previously performed the pre-resume acknowledgement,
timer activation and final restore-receipt publication itself. An independent
runtime child could therefore no longer finish when that controller stopped.
The runtime's three-second acknowledgement timeout entered the observed abort
path; the original twenty-second controller-loss assertion failed repeatedly.

`internal/lifecycle/restore_worker_linux.go` moves the complete existing
handshake into a private worker in the runtime package. The public controller
passes a sealed bounded envelope, the original sealed runtime request and
descriptors for its exact root and already-held operation lock. The worker
verifies seals, strict JSON, root/boot/lock identities, the persisted target
digest, the original fork or completed-pause resume recipe, epoch fencing and
the empty owned cgroup. It inherits the same exclusive flock and completes
activation plus durable publication independently of the public controller.
Its observed command identifies the real restore/resume backend operation;
the pinned Sysbox child now runs below this coordinator. No test dependency
enters the shipped CLI. Cancellation and the existing sixty-second execution
bound remain; the helper remains inside the same owned outer worker limits.

The original handshake body is byte-identical after renaming its Go function
to `heldRestoreWorker`. Its task/tracer identity, frozen-group checks, exact
acknowledgement, held receipt and timer activation checks are retained. The
public CLI and native TestMain gain only the private dispatcher. Exact TestMain
compatibility is declared in
`factorybox-restore-worker-proof-f234ce0e0670482cabcc66d984637e23/intent.json`:
f9481fb62a134418b1a8585471b42c3bf81d9ab717ad585a9f961797353fc287
becomes d3b5584bd49fd88e0083512df009002f1d8339656d16af630133d3c425a2e911
by adding only the restore-worker dispatch block. All 95 prior test files
otherwise remain byte-identical, including the original failing assertion.
New real-memfd tests reject unsealed, oversized, unknown-field and trailing
requests, accept a valid sealed request, and verify inherited offsets do not
change. Every original assertion and the twenty-second deadline remain intact.

The unchanged complete native capture suite passes in
`factorybox-runtime-51670eb9ae5b424d832d9651c216def7`: 12 top-level tests and
78 subtests, no skips. The exact actual-controller-loss case passes in 1.69 s:
the controller remains stopped while its independent backend finishes, is
killed after durable completion, and a fresh controller recovers the original
PID/owner/RAM from the receipt. Subsequent recapture, independent descendant
and exact deletion assertions also pass. This single fault-case time is not a
performance distribution. The original failure and all diagnostic verdicts stay.

The same core passes the complete pause suite in
`factorybox-runtime-dc2369cfd93341b0895116114579660f`, 12 top-level tests and
67 subtests, and namespace suite in
`factorybox-runtime-0d1542c06ca049d98f57e4b953a11679`, 14 top-level tests and
62 subtests. These retain RAM-releasing repeated pause/resume, independent
resumed descendants, time-namespace admission and nested-zombie semantics.
All original assertions, exact owned cleanup, inputs, sources and components
pass without skips or OOM. Resource limits and the 420-second native deadline
are unchanged. Sysbox and CRIU binaries are identical to the original failure.

Independent `evaluate.py` and `fix-proof.json` in F234 verify all three native
contexts/inputs and full archive decompression, bind the original failed run,
and confirm every old failed/pass case appears among the full new passes.
The only existing-file changes are CLI dispatch, the renamed original function
and TestMain dispatch; the new worker and its tests are separate files. This
proves the original controller-loss counterexample on the new native core.
It does not yet qualify a final worker package, loss of the completion worker
itself, all other fault cases or production. The original historical EBUSY is
separate and still open. Required final-package consumer/timer/hold/storage and
remaining native acceptance stay mandatory.

To retain capacity without changing failure outcomes, explicit
`--include-closed-failed-runtime-tests` allows immutable encoded-test sharing
only after stopped/cleanup/source/component checks and full canonical decode.
The actual operation `factorybox-storage-share-test-encodings-095e6bd5a7ba4dc0b0760aceba1068c6`
shares five identical compiled-test encodings and reclaims 52,080,640 allocated
bytes. Every path, source, result, log, encoding receipt and failure dump stays.
The missing-result interrupted run is ineligible. Subsequent passing current-core
test encodings share their identical bytes under the existing passing-run guard.

Two older successful pause stores are superseded only after the current full
79 named assertions pass. Maintenance
`factorybox-passed-pause-successor-retirement-cd8665a18698480f9c115e7a8d97702b`
declares exact prior/current source compatibility and completely decodes each
old store before retaining every archived member header/hash and bounded
diagnostics. Their results, sources, binary inputs and logs remain; their old
large payloads are explicitly no longer fully reconstructable. The current
passing store and all selected actual failures remain complete.

The equivalent namespace preflight
`factorybox-passed-namespace-successor-retirement-26e43ff855724d0faea2596233da6477`
first rejects a comparison against storage_linux_test.go, which did not yet
exist in the exact old source manifest. No archive is touched. Its failed result
is retained. The declared new copy
`factorybox-passed-namespace-successor-retirement-62234c55d077497db8c4c5a3c0741106`
requires that exact old absence, binds the additional new test source and keeps
every prior-byte/76-named-assertion check before superseding only the one old
successful namespace store. Required headroom also refuses two attempted
portable-check allocations before any check starts; those are unrun allocations,
not failed or passing assertions. Further maintenance/checks follow separately.

Source sharing in
`factorybox-storage-share-sources-191f652ae50e44ed9778a0dab748db97`
verifies 706 closed trees and shares 8,859 byte-, metadata- and mtime-identical
files, reclaiming 83,251,200 allocated bytes. Its 21,341,911-byte plan is
losslessly encoded with complete decompression verification and the original
SHA-256 retained in its encoding receipt. No failure result or dump changes.
The subsequent complete portable check
`factorybox-checks-bca164e13f7e46ec9a3968739ea1b688` passes all six groups,
including Go tests/vet, worker-probe compilation, Python tests, all 232 sealed
reference files, ordered fixture verification and whitespace checks.

The intervening user-requested performance/effort explanation is a no-progress
goal turn: it changes no implementation or acceptance result. Resumption
confirms the check above is terminal and successful and that no build, native
suite or portable check remains live. The next action is to commit the proven
core fix and independently build the delivered package. The complete goal and
parallel-chat handoff remain unchanged; production qualification remains false.

## Independent delivered packages after the restore-worker fix

Commit `d0d2834` preserves the native-proven restore completion change. Fresh
worker build `factorybox-build-worker-2291d87be1f24212ba7af731968ea94b`
and independent comparison
`factorybox-build-worker-68c046296c8e440a8afe8a43114d1f23` both pass.
Each starts a separate empty compiler cache with pinned native Go 1.27.1,
offline module resolution and the unchanged pinned Sysbox/CRIU components.
Both reproduce every source, CLI, bundle, deterministic raw package, image
and layer byte. Their optional compiler scratch is removed only after each
successful build, with its inventory and reconstruction receipt retained.

The delivered CLI SHA-256 is
`5fd51d939f859df01d83fa3b057b083e7978b7165620d9b804fb811978bbd81f`.
The 94,566,400-byte deterministic raw package SHA-256 is
`4205f26771855483aac72741f847b0fab263c66130781b9611684c41d95d5a15`.
The fully decoded verified encoded package is 34,292,884 bytes, SHA-256
`319a117063717dae7351195a788da9482dff8baf313208e3e81e12abfa073707`.
The common image ID is
`sha256:7b521eaa4c3e4d6d787930d1d1d66bc11b0876c034a291e904588fb598093cbc`.
No consumer test dependency enters the CLI.

`package-proof.py` and `packaged-core-closure.json` in
`factorybox-restore-worker-proof-f234ce0e0670482cabcc66d984637e23`
independently verify both complete builds and bind their exact Go source and
component bytes to the three native capture/pause/namespace successes above.
All three full native archives decode and all frozen source/compiled inputs
verify again. The original counterexample, portable checks, unchanged goal
and saved parallel-chat context remain bound. Native in-process regression
proof and actual delivered CLI/image consumer acceptance retain their distinct
scopes; neither this closure nor reproducibility alone qualifies production.

The 8-GiB start reserve refuses a subsequent capacity preflight at 24.02 GiB
before any consumer starts. Offline maintenance
`factorybox-storage-share-restore-rebuilds-f5a248f6582841a6802cab34eb4f6e29`
then revalidates both complete independent builds and shares only their two
identical CLI/encoded-package files, reclaiming 50,544,640 allocated bytes.
Every original path, byte, source, result, mode, owner and reconstruction
receipt remains. Original/shared mtimes and complete package verification
are retained. The required start headroom is restored without retiring a dump.

The actual delivered package then passes the unchanged complete Factory
lifecycle in
`factorybox-consumer-worker-loss-867cf04fd2464dd4af15944728a9fe60`.
The original native test takes 191.40 seconds. The separate native Theia
connection, real Connector responses/edits, local deterministic OpenCode tool,
complete RAM/filesystem capture, independent fork/fork-of-fork, source deletion,
RAM-releasing pause, useful resume and checkpoint/fork of the resumed source
all pass. The actual 32-GiB bounded store, clean worker exit, exact owned
container/volume cleanup and unchanged global settings pass. This is one
ordinary local-model observation, not external-provider or worker-loss proof
and not a member of the final predetermined series.

`packaged-consumer-closure.json` verifies the exact delivered CLI/image,
independent package reproduction, complete current consumer source and every
original compiled/fixture input, while retaining the native red/green binding.
`factory-performance.json` records the unchanged strict timing extractor and
all observations. This run measures prepared useful work 24.492 s, cold
worker-to-useful work 59.436 s, useful fork 16.519 s, capture plus useful fork
35.356 s, pause 10.535 s, useful resume 7.998 s, and resumed capture plus useful
fork 38.776 s. These individual observations differ from the preceding
candidate and are not a matched regression or optimization comparison.
Anonymous source memory falls from 103,477,248 to zero and resident snapshot
bytes from 503,484,416 to zero; the source cgroup still holds 17,489,920 bytes.
No whole-worker zero-memory or latency-distribution claim follows.

The following raw restore-hold suite allocation is refused by the unchanged
8-GiB start reserve at 24.01 GiB, before an artifact or native test is started.
Its preflight log remains `restore-hold-native.log` in the restore-worker proof.
No required assertion is recorded as passed or failed by that refusal.

Maintenance
`factorybox-storage-share-test-encodings-e9f1c5cfde23480e84e639bfb39a5910`
then shares 35 identical closed encoded helpers after full canonical decode,
reclaiming 229,314,560 allocated bytes. Original results, inputs, encoding
receipts and every path remain. The subsequent complete raw restore-hold
suite passes in
`factorybox-checkpoint-retirement-6b0e1e7b70174ae4853b7621904af3c0`.
All five unchanged cases pass: fork, resumed source, fork disconnect, resumed
disconnect and foreign acknowledgement. The original counter stays held
before acknowledgement and after CRIU detach until explicit thaw; the three
negative controls refuse without exposing the application or inventing a
completion receipt. The native test takes 16.79 seconds. Exact owned cleanup,
unchanged inputs and global settings pass. This tests the pinned raw backend
protocol; it does not by itself test loss of the new completion helper.

The complete timer guard-loss boundary passes on the delivered package in
`factorybox-checkpoint-retirement-ff77e3f0b8ac43558d2362f693ce6065`.
Actual guard and capture-worker SIGKILL, controller completion/reaping, exact
unchanged replay refusal, queued signal preservation and the original frozen,
untraced source all pass. This is the existing ungranted completed-process
boundary, not a claim of automatic recovery at every capture phase.

`component-proof.py` and `component-closure.json` reverify and bind the existing
selected upstream component outcomes to the new delivered package's identical
Sysbox/CRIU bytes. Frozen Sysbox source, native SHM correction, all original
Go JSONL outcomes, Bats TAP/inputs and CRIU case/component identities verify.
The reused evidence contains 727 Go passes with 18 original skips, seven
lifecycle Bats passes, four systemd Bats passes with two original skips, and
18 CRIU core cases with two ordinary dump/restore cycles. No old Factorybox
Go-core native gate is reused for the changed core, and no original skip is
converted into a pass. This remains the selected supported component scope,
not the entire upstream umbrella matrix or production qualification.

The predeclared complete additional boundary sequence in
`remaining-boundaries-intent.json` and `remaining-boundaries.json` passes all
eleven selected groups on the same delivered core, without a failure or skip:
timer-state-reader, timer-exit-guard, timer-signal, timer-admission-signal,
timer-remainder, timer-remainder-protection, timer-orphan, record-metadata,
worker-metadata, retirement, and storage-timer-abort. Each child retains its
original runner, full source/input context, native assertions and outcome.
The sequence stops on any failure or unrun allocation; no failed member is
replaced. This sequence is separate from the still-required final ten complete
ordinary lifecycle runs per example.

The worker-metadata member
`factorybox-checkpoint-retirement-ec850ba804f2498c990ca46b1c5f2b85`
uses all 200 public base imports and three actual committed pauses. The
original admission boundary passes in 44.39 s; complete outer execution takes
71.65 s, including clean source-worker drain, unchanged protected inventory,
replacement-worker recovery and clean replacement exit. Its completion ledger
contains all 200 bases and three pauses in 4,192,256 bytes, below the original
4-MiB reader bound. The exact original assertions and full-size fixture remain.

The complete retirement member
`factorybox-checkpoint-retirement-b0f90369ff9b4d168a4aba60ae5b45cf`
passes all six original top-level tests, including actual controller SIGKILL,
observed reclaimed capacity, independent descendants, protected selections,
alias/base preservation and original worker exit/recovery fences. The actual
post-admission ENOSPC member
`factorybox-storage-admission-9bd58c3a1f6949ebb4cf65fc9aeb25f0`
passes confirmed source rollback and the original protected-remainder
EINTR/ABI/deadline assertions. These do not establish useful recovery after
freeing space in the separate historical 8-GiB consumer failure, automatic
retention, general completion-helper loss or the remaining full release gates.

Closed-run helper sharing
`factorybox-storage-share-test-encodings-19864796f2c94acb8de0e29605e9d8c0`
fully verifies and shares twelve identical encoded test inputs, reclaiming
125,779,968 allocated bytes. The storage timer-abort binary has its full
decompression/reconstruction receipt as well. No result, source, assertion,
failure dump or original path changes.

Final milestone verifier `turn-closure.py` and `acceptance-progress.json` bind
the two package builds, actual delivered consumer, three native core suites,
component proof reuse and all thirteen additional boundary suites. It
reverifies complete source/compiled inputs, original success/no-skip logs,
unchanged globals, the 200-base/three-pause recovery and actual ENOSPC proof.
Direct label-filtered Docker queries confirm no container or volume remains
for any of the thirteen boundary owners. The required next-run storage reserve
passes. The full goal and saved parallel-chat handoff remain byte-identical.
Historical EBUSY attribution, remaining current-package native/integration and
external-provider cases, broader storage/operator recovery, complete tested
operating procedures, the final ten-per-example series and final release
profile remain required. The goal stays active and production qualification
remains false.

## Untraced original Proc repetition and complete archive reconciliation

The preceding goal turn makes substantive progress: commits `d0d2834` and
`320ff8f` preserve the restore-worker fix, independently reproduced delivered
package, actual Factory/Theia lifecycle and thirteen additional boundaries.
Resumption verifies the clean worktree and absence of live native/build jobs.
The full production goal and saved parallel-chat context remain unchanged.

The historical admission driver now accepts an explicit bounded
`--proc-repetitions` count, defaulting to the previous twenty and allowing up
to forty without changing either original case, fixture, 360-second Go timeout
or 420-second native deadline. Its original inventory tests retain all earlier
assertions and additionally verify the exact forty-pair frequencies. Portable
check `factorybox-checks-927cf20f4c1d421dab9a27a477bbe872` passes all six groups.

Native diagnostic
`factorybox-historical-admission-ae7dcc7d5da743c9b1c2c91b6f642d90`
uses the same historical CLI/components and reproduced test binary with only
the previously declared post-failed-unmount observation. It has no continuous
file or root/cwd tracer. All forty pairs pass, with 51 top-level and 141
subtest outcomes, no skips, no failures and exit zero. The immediate ordinary
unmount assertions remain unchanged. The historical EBUSY does not reproduce;
this is neither an attribution nor a repair or current-package acceptance.

The larger run exposes a diagnostic retention limit: its selected-path raw
archive reaches 3,562,602,496 bytes before the separate 180-second archive
timeout. The native assertions already completed within their unchanged
deadline. The original result remains failed with cleanup false and its full
owned source volume retained. No assertion failure is inferred from this
archive-only failure, and no partial tar is treated as complete.

Offline reconciliation
`factorybox-historical-archive-reconcile-f7f46835d3e14d3289bba7b72117b388`
then archives the complete stopped volume through the existing bounded,
read-only streaming encoder. All 6,276,775,936 raw bytes, SHA-256
`a371c62c940253f88fa7b90e5b25e7dab70efef1ab1c5fe80cb38be9ea7b02d6`,
are independently decoded and verified in 20.46 seconds. It also preserves
every byte of the original incomplete tar with a full reconstruction receipt.
The source volume is deleted only after both verifications. Its final
next-run capacity query nevertheless fails at 24.05 GiB; argument evaluation
prevents its final status update even though archive preservation and exact
resource removal already succeeded. This failed maintenance journal remains
unchanged as well.

Explicit offline repacks
`factorybox-storage-repack-a3a80d59a3c94f30bd7871c381aea13d` and
`factorybox-storage-repack-d418c4051a1243f2bf2ee33d0f52e884`
first verify exact resource absence, source identities and both unchanged
failed results. They completely decode and preserve the full-volume archive
and incomplete-tar bytes, reclaiming respectively 93,479,340 and 66,561,053
encoded bytes. Standard verified replacement pointers retain reconstruction;
the partial tar remains explicitly incomplete. No source, native result,
assertion or selected actual failure is retired.

Independent `verify-closure.py` and `closure.json` in the reconciliation
artifact reverify the full volume, partial-output reconstruction, all forty
paired original outcomes, preserved failure journals and exact absent owned
resources. The required 8-GiB next-run headroom passes. This is a new audit of
actual completed retention/cleanup, not a rewrite of either failed result.

The diagnostic driver now explicitly supports `--stream-archive`, automatically
selected above twenty paired repetitions. It stops and verifies its exact
owned source, then preserves the complete read-only volume with bounded
streaming compression and full decompression verification. The separate
offline archive bound is 600 seconds; native deadlines and all assertions
remain unchanged. All six portable check groups pass in
`factorybox-checks-8ccaab1c5a5d427fab2debba08734dfa`. A fresh single-pair
integration observation of this new archive branch follows separately.

That complete original single-pair integration passes in
`factorybox-historical-admission-a4832767c9074e788174a3e96512377f`,
including all adapter assertions, complete compressed-volume preservation,
unchanged inputs/globals and exact owned cleanup. Both original Proc cases
pass in 7.84 seconds. The fixed archival path is exercised through the actual
diagnostic CLI rather than inferred from its unit checks. The earlier failed
journals stay false and the historical EBUSY cause remains open.

## Further exact-core gates after the restore-worker correction

The intervening user-requested performance/effort explanation changed no
implementation or qualification state. On continuation, session 89761 is
confirmed terminal. Batch
`factorybox-post-restore-native-gates-d6629c9d7de94fe0b6ccc52289104150`
passed complete interruption and capture-worker-loss suites, respectively
12 top-level/61 subtests and 12/64, without skips. The following lifecycle
allocation was refused at 24.08 GiB before creating a native run. The batch
remains false and its unrun row/log remain unchanged.

The earlier full current-core admission passed 17/78 in
`factorybox-runtime-a0fe7ec731954f368f7fceb4b56adcf3`. Only after complete
replacement-source, assertion, input and archive verification,
`factorybox-passed-admission-successor-retirement-28ca56e68bbd41ec82a4d10a5e50f61c`
retires the older passing 3fc216eb store. The retained original EBUSY archive
and every selected actual failure remain complete. Likewise,
`factorybox-passed-fault-successor-retirement-063e4424acc7442d8dd88bb754690ed3`
verifies current successors 192803354061419f9c472698338cfbaf and
20a4a51a4a83456db0e9e5d8634df81b before retiring the older passing 41cc6d77
and 1737c5a9 stores. These retain every source/result/log/input, original
member headers/hashes and bounded diagnostics. Their old full stores are
intentionally unavailable; the replacement stores remain complete. Exact
source compatibility preserves every original assertion body.

Continuation batch
`factorybox-post-restore-native-gates-d0155e8355e2404fb56f3f7f9893bdc0`
binds the completed admission/interruption/worker-loss prefix and passes
lifecycle 6/3, guard 1/12, base 12/78, base-pause 12/67, delegation 12/61
and auxiliary 12/61. All maintenance steps pass. The following time-suite
allocation is refused at the unchanged artifact reserve before any native
run starts; that batch/result also remains false.

An initial private superseded-pass verifier in
`factorybox-passed-core-successor-retirement-46c0d9113d3b4f5a86c2624d9b758b80`
rejects its over-escaped comparison literal before removing any payload.
Its failed result is preserved. Corrected private verifier
`factorybox-passed-core-successor-retirement-703abf40c4c64cf191ba9366a05bd417`
fully verifies and retires only four declared older passing base, base-pause,
delegation and auxiliary stores. Exact TestMain dispatch/preparation additions
and the single post-failed-unmount diagnostic call are the only changes to
prior test files. Every original assertion body, every original named passing
outcome and all other prior test-file bytes remain. Additional test sources,
outcomes and changed component identities are explicit in compatibility
receipts. The new entire suites on current components are the replacement
evidence. No old-component result is treated as current acceptance.

The next batch
`factorybox-post-restore-native-gates-35b90e8e3b1e428a9a41b40f54fc246d`
preserves all nine completed-prefix groups and continues the five remaining
groups. The original native deadlines, full goal and parallel-chat context
remain unchanged. Allocation refusals do not relax the 8-GiB start reserve.
Production qualification remains false.

## Complete selected current-core gate set and bounded Compose preparation

The remaining time, mount-FD, overlay and storage-admission groups pass in
`factorybox-post-restore-native-gates-35b90e8e3b1e428a9a41b40f54fc246d`.
The final controller-loss allocation is refused before creating a native run;
that batch stays false. Only after the new full time/mount-FD/overlay outcomes
pass, older successful same-family stores are retired in
`factorybox-passed-mount-successor-retirement-548c9653dbd144b4a517ac305ab5d20a`.
All old source/results/logs, member inventories and bounded diagnostics remain,
and every selected failure and new full passing archive remains complete.

The final storage/controller-loss group then passes in
`factorybox-storage-admission-02710a5b716a4d5e8fcb10244cbbcd50`, bound by
`factorybox-pilot-native-gates-7a9386abb5f04a47a0b1a70da0649bdb`.
It preserves both original checkpoint/pause cases, actual capture-worker
SIGKILL, stopped controller, zero available bytes and independent earlier
checkpoint assertions.

Independent closure
`factorybox-current-core-closure-2eb673665ae94f2eafcf9e3fb2da3ae4`
reverifies all seventeen selected exact-core native groups, complete current
sources and compiled inputs, all retained archives, zero native skips and
actual absence of every owned container/volume. A byte-identical copy of the
existing component verifier separately rereads original Go/TAP/CRIU evidence
on the identical pinned components. All eighteen selected gate names are
covered, including CRIU's eighteen cases and two cycles. Existing upstream
skips remain explicit; no full umbrella or final production acceptance is
inferred. The original EBUSY failed result and complete archive are reverified
and remain unexplained. The complete goal and chat handoff remain unchanged.

The first closure attempt,
`factorybox-current-core-closure-5821a3755dc742eeb8ebc9d6956dc3e6`,
also verifies all seventeen groups, then rejects an extra trailing blank line
in the copied component verifier. Its false result is retained. The succeeding
closure copies that verifier byte-for-byte and changes no native input/result.

The operating example now supplies a bounded Compose override, gives normal
Stop 45 seconds, and requires exit 0 without OOM before replacement. It records
its administrator-owned overlay/base mount identities before Start and releases
only those same inactive mounts after the committed pause. Changed, nested or
stacked mounts refuse release. The native driver additionally checks a changed
inode request leaves both mounts intact and requires a clean final Stop of the
replacement worker. The exact original preparation script remains unchanged;
only three input paths in a fresh fixture copy move into the bounded store,
with both hashes declared in
`tests/compatibility/worker-example-bounded-paths.json`. Go tests/vet, tagged
probe compilation, Python tests, all 232 sealed reference files, fixture
verification and diff validation pass in
`factorybox-checks-aee848a6194a4b24a0c54015932112ec`.

First native bounded example
`factorybox-worker-example-64b50d70891c4a7691ed95be060cd36a` verifies the
requested 4-GiB image/capacity, changed-input refusal, start, pause/resume,
committed second pause, exact input-mount release and clean old-worker exit 0.
It then fails while cloning filesystem backing into the replacement epoch:
the actual kernel copy reports ENOSPC. The small actor still uses the full
1.8-GB delivered rootfs input; four GiB does not cover all retained and recovery
copies. This is an actual failed observation, not a passing recovery proof.
The full failed volume is preserved as 6,106,906,624 raw bytes, SHA-256
`cfc6e30ce92774d0cc0d9ee3101c79dec6dfa534945cfd422ba680c8821c748c`.
Lossless repack
`factorybox-storage-repack-0cc40264fe5b49b29c9d9a4afaceb814` verifies all raw
bytes and reclaims 1,137,177,295 encoded bytes. The failed result and complete
state remain intact. The next original example run uses the previously
exercised 32-GiB consumer capacity, changing no runtime or workflow source.

For the required start reserve, only the already resolved first bounded minimal
Stop store is retired in
`factorybox-storage-retirement-ddc93206bffd47edb5e952dd9e1fb98e`.
Its source is d2cccba26a6f41a786881468dfc1bb7c's independent cleanup archive,
whose original 34,360,786,944-byte stream is completely decoded and verified
before removal of the 813,234,221-byte encoded replacement. Every member
header/hash, original source, result, phase observations and compiled input
remains. The full old store is intentionally unavailable afterward. The
original native result remains failed with cleanup=false; its independent
cleanup reconciliation remains passed. Replacement
`factorybox-consumer-worker-loss-3c22440d8547430896c77a9b72e3f01c`
has byte-identical prior consumer test sources, the same eight original useful
phases and 32-GiB size, and actual clean Stop/cleanup. This is historical
retention supersession, not current-package minimal acceptance. The initial
private retirement preflight rejects a missing legacy `inputs_unchanged`
field before any deletion; the succeeding verifier checks the actual frozen
source and every input directly. Both preflight recipe/outcome and the
completed private recipe remain. Current native proofs, original selected
failures and the new complete 4-GiB ENOSPC archive are preserved.

The candidate operating instructions are in `docs/first-release-operations.md`.
They explicitly leave useful recovery after reclaim, final external-agent
cases, update/rollback exercises, the predetermined final consumer series and
full release acceptance open. The revised operating example is not qualified
by syntax checks or the current-core suite alone.

The same revised operating workflow then passes at 32 GiB in
`factorybox-worker-example-3f93920dace74945b3cb7d405211a90d`, including
the exact bounded-store identity/capacity, negative input-identity control,
complete original CLI sequence, both committed pauses, exact mount release,
clean old-worker exit, fresh epoch, recovery/replay, checkpoint, independent
fork after source deletion and clean final worker exit. Native owned cleanup
and global sentinels pass. Both exits are 0 without OOM.
`factorybox-bounded-example-proof-b1eee18e7e604e5e8ef023c583f5a8dc`
independently verifies the actual transcript, precise command sequence,
recovery PID/owner replay, code/fixture hashes, current delivered image,
portable/core closures and absent exact store-labeled containers/volumes.
It also completely decodes the retained 4-GiB failure again and requires the
same frozen workflow/actor sources at both capacities. This proves the
optional operating example, not complete consumer/agent acceptance or useful
recovery after reclaiming space from the failed store. Update/rollback,
external-agent cases, the final series, historical EBUSY attribution and full
first-release qualification remain open.

## Native kernel busy-decision controls

The intervening performance/effort explanation changes no implementation or
qualification state. The full goal and saved parallel-chat context remain in
place. Investigation continues at the kernel's actual unmount decision rather
than treating another successful replay as a repair.

The read-only observer in `tools/diagnostics/mountbusy/` records paired
`path_umount`, `propagate_mount_busy` and nested `mnt_get_count` calls. Only an
unmount started in the exact owned outer PID namespace or a descendant can
create an observation. Candidate mount identity, namespace, flags, child-list
state and the actual returned count are recorded without collecting paths or
contents or modifying kernel state. A count does not identify a holder or
authorize cleanup. Missing pairs, map errors, read failures, ring loss,
unavailable/nonzero kernel miss counters and unfinished maps reject coverage.

Build `factorybox-mount-busy-tracer-50cb8f6dcfd14ea3b4fe082833d076a0` passes
the pinned CO-RE compilation, unchanged-module Go build and explicit vet. It
preserves the earlier mount observer's checked BTF-header compatibility path,
checks all three kernel prototypes and records the kernel BTF digest. The
preceding completed build `4dc273a063d54f6196edbf4611fbf1e5` samples metadata
at unmount entry; the selected object additionally resamples metadata at the
busy-decision entry. Frozen inputs remain unchanged. Its independently
recompiled, byte-identical Go loader shares the earlier immutable binary with
recorded identities and hashes.

`tools/mount_busy_control.py` then passes all five actual native controls in
`factorybox-mount-busy-control-9fff67191c2548ce865144ef85e0b9d2`:

- A descendant-PID-namespace helper's O_PATH file reference gives count 3,
  actual busy result 1 and ordinary unmount errno EBUSY. After exact release
  and helper exit, count 2, busy result 0 and ordinary unmount success follow.
- Cwd and root controls have the same real failure/release/success sequence,
  with held counts 3 and 4 respectively, both returning to 2.
- A real child mount makes the parent busy before a count query is needed.
  Releasing the helper and ordinarily unmounting that child permits the parent
  unmount; the observed child-list state changes accordingly.
- A propagated peer has count 2 while the syscall target also has count 2.
  The distinct held peer is the blocking candidate. Releasing its reference
  leaves peer count 1, then ordinary unmount of the target succeeds and removes
  the propagated peer mount as well.

All 66 events decode completely, all three error counters and six probe miss
counters are zero, and no active observation remains. Exact ownership cleanup,
frozen input verification and unchanged global sysctls pass. These controls
qualify observation of each deliberate blocker; they do not attribute the
uncaptured historical failure or qualify a runtime fix.

The historical runner accepts the new observer only with this complete native
proof on the same kernel. It rechecks original control inputs, actual raw
events, five-case coverage and the exact current decoder/verifier before use.
The observer runs separately from the high-volume file/cwd observers. The
original test invocation, 360-second test deadline, 420-second native deadline
and immediate ordinary unmount assertions remain. A shell regression executes
both native-success and native-failure controls and verifies that observation
preserves every native argument and exit code.

Historical replay
`factorybox-historical-admission-5627fe8078874c2e930714524e6cd4d0`
then passes all forty predetermined paired Proc repetitions, 51 top-level and
141 subtest outcomes, with no skips. The original test/CLI/component bindings
and post-failure-only observer declaration remain intact. The actual native
test exits zero. Complete kernel coverage has 10,574 events and 4,567 unmount
calls, all error/miss counters zero and no unfinished maps. All 160 ordinary
unmounts called by the original test executable succeed. Seven separate
`fusermount3` calls with flag 8 return EINVAL and remain in the raw trace and
result's failure list; they are not EBUSY or failed test assertions. No observed
call returns EBUSY. This is another non-reproduction with newly qualified
coverage, not historical attribution or a runtime repair.

The complete stopped volume is preserved as 6,376,390,656 decoded bytes,
SHA-256 `4b4e243ec644ac07e35bfb8b8673a3b02fd15d7ea7b4586280fd416bb68bdb03`.
Native assertions retain their original deadlines; the complete outer native
and retention observation takes 347.97 seconds. Owned cleanup, frozen inputs
and global sysctl checks pass. Lossless repack
`factorybox-storage-repack-06ad32265b844365bc456ba35cb509bc` verifies every
decoded byte before reclaiming 77,947,960 encoded bytes. The full source store
remains reconstructable. Closed-input sharing
`factorybox-closed-busy-input-sharing-f528b1f99adf4fa7a6fd79a83fbb452e`
then verifies five passed historical replays and shares two identical input
executables plus one identical BTF input. It releases 41,234,432 allocated
bytes, retaining every byte, path, mode, owner and result; both original and
shared mtimes are recorded. No archive or selected failure is retired.

All six portable groups pass in
`factorybox-checks-f688b3bc66c84e1e993787955d8b727d`, including 252 Python
tests, Go tests with vet enabled, worker-probe compilation, all 232 reference
files and ordered fixture verification. Independent raw/control/outcome,
complete-archive and exact resource checks are bound in
`factorybox-mount-busy-notes-05bc672f69c142f7baa81e3b30718a5e`.
The delivered runtime core is unchanged. The historical EBUSY, remaining
external/storage/operating acceptance and final predetermined consumer series
remain open. Production qualification remains false and the full goal stays
active.

## Current delivered package: external provider still refuses inference

Synthetic gateway proof
`factorybox-provider-gateway-fdbc8d48062d4d67bf0c669f6b4fda61` passes on the
current worker `2291d87be1f24212ba7af731968ea94b`, including two actual namespace
HTTP exchanges, foreign identity/mode refusal, exact pidfd stop, clean worker
exit, unchanged inputs/globals and owned cleanup. This qualifies its optional
consumer helper on the current image; external inference is a separate test.

The first consumer invocation uses an incorrect Theia image repository name
and stops during image inspection before creating a native artifact or test
resources. Its failed preflight is recorded in the busy-notes artifact's
`external-preflight.json`. The corrected invocation uses the exact image from
the earlier complete Factory proof. Fresh Theia setup passes in
`factorybox-theia-admission-4163a5ec86c14f8287a33750b4dc3a04`.

Actual complete Factory/Theia/OpenCode attempt
`factorybox-consumer-worker-loss-c8b44679edb740469501252e92b24217` then fails
on the current delivered package with the same 32-GiB bounded store, original
assertions and separate IDE ceiling. Current catalog and pricing requests
return 200 and still explicitly identify `glm-5.3-flash-free` as free. The
shipped OpenCode 2.0.27 makes one actual model request, which returns HTTP 429
`FreeUsageLimitError` and `Retry-After: 67029`. No inference retry occurs.
The original developer test fails after 117.21 seconds because the model never
starts its required 16-MiB held tool. Later capture/fork/pause/resume and agent
result assertions remain unrun. The source result stays failed and external
acceptance stays false.

The request starts at 2026-10-24 05:22:50.953 UTC. Its reported wait sets the
earliest next attempt at approximately 2026-10-25 00:00 UTC; this does not
promise availability then. Keep that backoff while progressing independent
storage/operating work. The earlier EEF-package provider failure remains intact.

The complete failed store is preserved as 34,360,786,944 decoded bytes,
SHA-256 `746ccb98fada1fffe679745b0a6cc1f9e3957ba834df8e458f254945e0c10f74`.
Exact owned cleanup and unchanged globals pass. Repack
`factorybox-storage-repack-c76f8823f78548a293b968af9c4726a6` verifies every
decoded byte before reclaiming 799,443,366 encoded bytes. The full failed store
remains reconstructable in 663,511,406 encoded bytes. All original source,
request/response, native result, logs and exact helper reconstruction remain.

Independent binding
`factorybox-current-provider-observation-5d6d33ac92ca4850be51901432a7a11b`
rechecks the one real 429 exchange, free-price discovery, original failed
assertion, exact delivered sources/CLI/components, gateway proof, all frozen
inputs, complete failed archive and actual absent owned resources. Its passed
evidence-verification result explicitly leaves native external acceptance
false. The retained archive raises allocation to 24.66 GiB after compression;
the unchanged 8-GiB start reserve therefore refuses another native allocation.
Further verified offline compaction is required before new builds/native runs.
The complete goal and original chat handoff are unchanged; production
qualification remains false.

## Restore the native start reserve without changing evidence verdicts

The intervening user-requested performance and effort explanation changes no
implementation or acceptance state. The complete goal, original handoff and
deferred work remain unchanged.

Closed encoded-input sharing
`factorybox-storage-share-test-encodings-615cca26738f4cfb8eed92bab91ae551`
shares four byte-identical closed consumer helpers and reclaims 28,377,088
allocated bytes. The sharing tool now also admits exact closed PID namespace
and startup-manager component inputs with an explicit flag. It requires
confirmed cleanup, stopped state, unchanged inputs, the exact diagnostic owner,
and PID diagnostic source/component manifests. Original native failures and
encoding receipts remain unchanged. Every canonical compressed input is fully
decoded before its byte-identical encoding is shared. Three focused tests pass,
including preservation of failed native results and rejection of changed source
or a foreign owner. Actual maintenance
`factorybox-storage-share-test-encodings-2244250ed51446b79bf756c3cd45fba5`
shares 22 component encodings and reclaims 224,817,152 allocated bytes.

Private startup preflight
`factorybox-startup-retention-preflight-07b8d9c2932f46469e7562fd1b83471e`
then verifies the original early startup failure `f7c02247159d4e13bf768da0ad1d39c5`
against complete successor `b6ddf5164eef4068bbc6ee90e3419a01`. Original test
executable, source, components, example, host, rootfs, project images and
normalized invocation agree. The independently packaged successor changes
only CLI and runner inputs and passes the original full test. The current
SHM red/green controls and current full Factory pass are also verified.
This supports historical retention supersession; the missing historical child
heap/stack remains unobserved and no retrospective GC-state attribution follows.

Retirement `factorybox-storage-retirement-1ec654dcb2684b05bd279780c06e5e49`
completely decodes and verifies the original 1,914,699,776-byte archive before
retiring its 432,389,364-byte encoding. All 47,733 member headers/hashes,
bounded startup diagnostics, original source, inputs and failed result remain.
The complete old filesystem is intentionally unavailable afterward. The
selected unresolved failures and all current proofs remain intact.

Two private closed-input preflights `83130f65e3dc4a51a6226076b2d95b97` and
`8e62d1ad8caa466791cc96821ae17980` refuse before mutation because the consumer
gateway mode differs from the native gateway output. The corrected scope in
`factorybox-closed-input-sharing-27c0a3150cd249069e7a2ec6d04f59d8` shares
only three identical completed CRIU outputs and one identical native gateway
output, with exact original modes and owners. Frozen source and native/build
results verify, exact original owner resources are absent, all paths and bytes
remain usable, and original/shared mtimes are recorded. Actual allocation falls
by 32,665,600 bytes to 25,764,986,880 bytes. The unchanged 8-GiB start reserve
passes. No frozen package, archive, active input, global setting or historical
experiment is modified by this sharing.

This maintenance restores the ability to run the remaining acceptance. It does
not close useful ENOSPC recovery, manual retirement, operating acceptance,
historical EBUSY attribution, external-agent acceptance or the final series.

All six portable groups pass in
`factorybox-checks-09a91f2626684f44b1d3a90ad89ac986`, including 254 Python
tests, Go tests with vet, tagged consumer-probe compilation, all 232 sealed
reference files, ordered fixture verification and diff validation. Runtime
core and the delivered package are unchanged.

## Useful work after actual bounded-store ENOSPC

The existing 512-MiB native capacity test now retains every original line and
adds actual useful recovery after its exact pressure inode is released. The
delivered public CLI restores the original pre-full checkpoint and advances
its private RAM without changing the source. It then creates a fresh checkpoint,
requires exact byte-for-byte replay, restores an independent descendant,
advances that descendant, pauses with measured RAM release, resumes its new
state and verifies useful state after deleting the original source. At most
two sandboxes are live. Runtime code and the selected worker bytes are unchanged.
The runner requires every new outcome in addition to its existing actual
ENOSPC, bounded capacity, source preservation and ownership assertions.

`factorybox-bounded-store-301ead43c5d64a5585c904620e8d9cc5` passes the complete
test in 15.36 seconds, 21.50 seconds including the outer driver. Actual available
store space reaches zero, new capture is refused, and the containing host keeps
more than the separate 12-GiB floor. The preserved unlinked guest alias has
383,557,632 bytes and 749,144 allocated 512-byte blocks before its exact owned
truncate. The fresh checkpoint has ID `44444444444444444444444444444444` and
an actual release receipt. The later pause has zero anonymous workload memory
and zero resident snapshot bytes. Native cleanup, unchanged frozen inputs and
unchanged global sentinels pass. The completed test executable is fully verified
and losslessly encoded; no historical executable is reconstructed.

Independent binding
`factorybox-post-enospc-proof-e1d5c39df1fb41609276e3c831f61fd6` verifies the
exact delivered image and all 89 non-test files of this native core context,
every original test line, the full encoded native executable, raw native PASS
without skips, strict proof fields and absent exact owned resources. This
closes useful post-reclaim work for this bounded runtime fixture. It does not
claim recovery of the earlier complete 8-GiB Factory failure, recovery under
worker replacement pressure, full consumer/operating acceptance or production
qualification.

For subsequent acceptance, lossless repack
`factorybox-storage-repack-1b5f2bc4d8e042319596593437eff3af` verifies all
1,074,790,400 original bytes of closed failure `971eee0d40cc438e8e239f97cb7ab3c2`
and its replacement before retiring only the old encoding. Raw SHA-256 remains
`d38ed95db0466856a7af899758a0cf9116830433374becfd701e615a4b296c81`.
The complete failure remains reconstructable in 46,349,226 encoded bytes,
reclaiming 63,832,801 encoded bytes. Original results and encoding receipts
remain unchanged. The original selected failures and complete goal remain.

## Current public retirement after pressure and interrupted deletion

The complete existing public epoch-retirement suite passes on the current
delivered image in
`factorybox-bounded-loss-29c1db8cc9db4ae089f9667c5122e266`, with abrupt initial
worker loss. All original Prepare, full-original-recovery, Recover and Finish
assertions pass without skips in 129.20 outer seconds. This is the existing
1-GiB runtime fixture with 32 MiB of private anonymous actor memory; it does
not substitute for either complete consumer.

The suite reaches real ENOSPC during retirement preparation, preserves the
original recovery backing and verifies the retained checkpoint. Inspection
and cancellation use the public CLI while only four 4096-byte blocks remain
available. All five cancellation refusal controls pass; the test does not
delete partial metadata itself. Ten retirement admission refusals preserve
active selections and reject changed ownership, unknown entries and foreign
covering mounts. After acceptance, an actual partial public unlink is
interrupted, followed by complete worker loss. A separate empty owned worker
holds the former loop slot, so maintenance resumes on a different actual loop
device with the same exact persistent image. All authorized payloads are
removed, original metadata/provenance and caller input remain, exact replay
returns the completed result, the maintenance fence clears and the independent
successor recovers usefully. Actual available capacity rises by 83,058,688 bytes.

`verify-retirement.py` and `retirement.json` in
`factorybox-post-enospc-proof-e1d5c39df1fb41609276e3c831f61fd6` independently
bind the native raw outcomes, strict pressure/cancellation/interruption reports,
current delivered CLI/components, full encoded test, unchanged native core and
absent exact resources for both owners. Its first verification uses a nonexistent
top-level CLI manifest field and stops with KeyError; the corrected verifier
reads the exact existing `bundle_sha256.factorybox`. That correction is recorded
without changing a native verdict. Original global sentinels and source inputs
remain unchanged. The suite explicitly retains `full_epoch_retirement_qualified=false`
because complete consumer and remaining release acceptance are separate.

The closed test is losslessly encoded and its byte-identical encoding is shared
with the preceding bounded-store test in
`factorybox-storage-share-test-encodings-48be97ea78ed40358d68992e4a7b7841`.
Full decompression verifies before sharing; every path/result/receipt remains,
and 10,485,760 allocated bytes are reclaimed.

The first current minimal-consumer invocation supplies the Factory-specific
project-image archive and correctly refuses before new_artifact or resources.
The rejected preflight is recorded in `consumer-preflight.json` in the same
proof directory. The next invocation selects the exact minimal-specific pinned
archive. No required native assertion is treated as passed by either preflight.

Current minimal consumer
`factorybox-consumer-worker-loss-e29c1e60b15a415181cbfab4e4b50436` then passes
the complete existing 32-GiB bounded worker-loss fixture, public epoch
retirement, retained-checkpoint inspection and forced loop-slot change. Its
original preparation commits a complete pause, deliberately parks and is
terminated with its exact worker by SIGKILL; both actual exits are 137 without
OOM. The replacement independently verifies all 175 retained files containing
95,517,600 bytes and all nine filesystem trees before the complete original
recovery test. Public retirement transfers the old pause to a new committed
independent pause, removes every authorized old base/RAM/filesystem payload,
preserves provenance and caller input, replays exactly and resumes useful RAM
and files. Observed store capacity increases by 2,048,659,456 bytes. Retirement
observation including replay/inventory checks takes 88.10 seconds. The complete
Recover test takes 125.85 seconds, and the replacement stops cleanly with exit 0.
Owned cleanup and unchanged global settings pass.

`consumer-minimal.json` in the post-ENOSPC proof independently verifies the
actual native result, all 33 frozen raw/fully decoded inputs, current core/CLI,
example, declared exact before/after fixture hashes and the sole ordered
RemoveAll-to-public-retirement substitution. All other original assertions
remain byte-identical. The first verifier expects a Go PASS from the deliberately
SIGKILLed preparation and refuses; its correction verifies the committed-pause
marker plus actual probe/worker exit 137, retaining PASS requirements for
completed phases. Both verification corrections are recorded and no native
result is rewritten. This is not an external-provider observation or a final
predetermined series member. The complete Factory counterpart remains a
separate current-package acceptance.

Further verified offline repack
`factorybox-storage-repack-3ed79d6ebc904b1ab3cc43a364cd1c40` preserves all
1,074,790,400 decoded bytes of the other closed failure `8255be709c264d94bc763d428a008623`,
SHA-256 `a338554674b8c730ecf816a4eadec7e74aea2ead4a60f9a619df6e0f896b4643`,
in 46,372,474 encoded bytes. It reclaims 54,555,899 encoded bytes after complete
original and replacement verification. The complete failure remains available.
Closed consumer helper sharing
`factorybox-storage-share-test-encodings-293eceb057c548d48f475431491cf200`
then reclaims 17,485,824 allocated bytes from three identical helper encodings,
retaining every native source, result, original receipt and path.

All six portable groups pass with the new post-ENOSPC assertions in
`factorybox-checks-56488c9b3ad34956a9d197f6d18d3c08`, including 254 Python
tests, Go tests with vet, tagged consumer compilation, all 232 reference files,
ordered fixture verification and diff validation. No runtime source or selected
package byte changes. These results leave the full goal active.

The complete current Factory counterpart passes in
`factorybox-consumer-worker-loss-296fe89873b049b7910acf8cd6c442e3`, with the
same 32-GiB bounded-store profile, planned worker loss, changed loop attachment,
separate native Theia and actual shipped OpenCode using its deterministic local
model fixture. The original Prepare test passes in 73.33 seconds and Recover
in 251.56 seconds. Both actual workers stop with exit 0 without OOM; native
ownership cleanup and unchanged global sentinels pass. Full retained inspection
verifies all 346 files containing 523,463,500 bytes and nine filesystem trees
before recovery. Public retirement transfers the complete old pause to a new
committed independent successor, retires its authorized base/RAM/filesystem
payloads, preserves caller input/provenance and replays exactly. Actual available
store capacity rises by 2,889,646,080 bytes. Retirement including replay and
inventory checks takes 133.06 seconds. The original subsequent checkpoint/fork,
independent mutations, IDE edits and useful descendant after source deletion
all pass. This is one observation, with no matched performance comparison.

`consumer-factory.json` in the same post-ENOSPC proof verifies all 35 exact
native inputs, full source/example/agent manifests, current CLI/components,
declared unchanged assertions, actual phase exits, separate Theia admission,
complete retained inspection, public retirement report and absent exact owner
resources. `factorybox-theia-admission-21412dd9bfea4c8b923349ebaf093df3`
passes before the consumer starts. Neither current consumer observation is
a final predetermined series member or an external-provider acceptance.

The current bounded runtime post-reclaim case, interrupted public retirement,
full minimal abrupt replacement/retirement and full Factory planned
replacement/retirement are now independently bound. The original 8-GiB Factory
ENOSPC failure remains preserved and not claimed usefully recovered. Capacity
pressure during recovery import, the other current consumer loss combinations,
external-agent cases, actual update/rollback, historical EBUSY attribution,
remaining required suite coverage, the final series and final release profile
remain open. The full goal and saved parallel-chat context stay unchanged;
production qualification remains false.

## Recovery import ENOSPC counterexample and unqualified candidate correction

The complete consumer pressure observation
`factorybox-consumer-worker-loss-500c7acc634d430c8056ee7b2c644492` fails on
the previously selected delivered package. Its additive ordered fixture first
passes the actual write admission, then reaches real ENOSPC while importing
the retained lower filesystem. Inspection still fully verifies the original
checkpoint's 172 files, 100,997,614 bytes and nine filesystem trees. Original
pause/checkpoint metadata remains byte-identical. Only the exact owned pressure
inode is released; capacity returns and the incomplete target correctly refuses
retry without changing its complete partial-copy inventory.

The original consumer then attempts an independent recovery target and fails
with `new worker metadata refused: no such file or directory`. The native
Recover body fails after 4.72 seconds; its subsequent assertions are unrun.
The incomplete import has no published fork intent, but metadata admission and
worker-exit discovery previously treated every fork directory as a completed
or published fork. Native cleanup and unchanged global settings pass; the
overall native result remains false. The initial pressure subreport's success
and retained-at-cleanup fields describe only that subprobe, not the failed
complete consumer. The current helper and strict verifier now require the
complete original consumer to pass before publishing an overall success.

The full failed store remains reconstructable through verified lossless repack
`factorybox-storage-repack-589f5cfad4df4453ad2ad5eb1d562689`: all
34,360,786,944 raw bytes have SHA-256
`8eb2ea7a667ee98486c0b9d7b3cd7f9ae563fcec9b619cb7649c8fe0059a4c20`.
Its 815,360,121-byte replacement has SHA-256
`64d22255606270f2ebc75ae008381eb3afce6e80509131200c4d0a7b585f4cde`.
The original native verdict, source and selected inputs remain unchanged.

A candidate runtime correction now persists a separate `import.json` before
payload copying. It binds the controller root, target/owner, same boot, pinned
recipe and exact original committed pause/checkpoint. It reserves the eventual
fork's complete exit-metadata allowance without publishing restore authority.
An identified interrupted import therefore remains charged while an independent
request can be admitted. The original unknown-directory refusal stays intact.
Normal fork publication still follows full copied-input verification and sync;
the published fork reuses its import owner and verifies its reservation.
Completed import metadata is explicitly retained in epoch retirement inventories.

Worker exit can inspect and retain an identified pure partial copy which has no
kernel objects to release. This is a read-only complete owned-tree check, with
no removal or unmount. Foreign/descendant mounts, external hardlinks, changed
proofs, unknown control entries or an unexpected target object refuse. Later
interruption after a base/alias mount has been created still requires a proven
reconciliation path; this change does not claim that broader case solved.
Failure while the initial reservation itself is being published also remains
fail-closed and needs its own interruption/cancellation acceptance.

Two minimized tests run against the real metadata-admission and worker-exit
seams fail before this correction with the corresponding missing-intent errors.
Afterward, all four selected root-owned tests pass, including thirteen changed
proof/object/inventory variants and the unchanged unknown-inventory control.
The original full native ENOSPC scenario has not yet been rerun on a newly
built package. These unit results cannot close that native release gate.

Evidence is in
`factorybox-recovery-pressure-diagnosis-8308c8da87e149d5964bebdc4734060b`.
Its `candidate-portable/result.json` records all six portable groups passing:
Go tests with vet, tagged consumer compilation, 258 Python tests, all 232
reference files, ordered fixture verification and diff validation. Root-owned
regression command/output and its red-before-fix transcript are retained
separately. Native and production qualification remain false.

The closed-helper compression experiment in the same private diagnosis is
withdrawn: its small savings did not justify another encoding format. Both
original encodings are restored byte-for-byte, original native results and
encoding receipts verify, experimental payloads have exact retirement receipts,
and the public encoding/repack tools are back to their prior committed bytes.
No unresolved failure archive is retired. The 8-GiB build/native start reserve
currently refuses admission at 26,492,346,368 allocated artifact bytes against
the 25,769,803,776-byte start ceiling. No new packaged build or native acceptance
is started through that refusal. Source checks above use the existing active
diagnosis; they do not create a worker workload or claim native qualification.

The complete goal and parallel-chat handoff still have their original hashes.
The new runtime candidate needs native red/green acceptance, remaining import
interruption/manual recovery work, independently identical builds and renewed
required regression coverage. Historical EBUSY attribution, external-agent
acceptance, operating update/rollback, final series and final release profile
remain open. Performance observations of the preceding delivered package do
not qualify this newly changed runtime source.

## Interrupted import identified; explicit cancellation candidate

Worker build `factorybox-build-worker-f2c46c7ecf074db19b6c1390d96b1586`
successfully packages commit `b2fda3b`, with package SHA-256
`0e0fbad8739fefe1cb6c571d7fc0018ec526b9245478d5f71ff21c6e9b8df60f`
and image `sha256:a0e7d3f726b1bed80e8c04dc638f11474c12c15a9175800e6672805cbb7c8c42`.
Its original full recovery-pressure consumer run,
`factorybox-consumer-worker-loss-a66d0c25e0b74b4eae10c29b5667265c`, gets
past the previous missing-intent admission failure. Actual admitted copy ENOSPC,
unchanged complete original checkpoint, exact pressure release, unchanged
partial import on retry and independent useful recovery/replay all pass.
The original complete minimal RAM/files comparison passes. The run then fails
at public old-epoch retirement with `incomplete descendant fork/import blocks
epoch retirement`. Later original assertions remain unrun. Overall result is
false, cleanup and unchanged global settings true.

The guard is correct: the failed copy is still an unresolved dependency.
The candidate now adds read-only `inspect-recovery-import` and explicit
`cancel-recovery-import`, under the current/retained controller locks and
verified bounded-store lease. A fresh exact inventory, directory birth/change
identity, reservation owner, same boot and pinned original pause/recipe must
match before any removal. Only the unpublished `recovery` subtree and at most
one exact root-owned private interrupted publication file can be removed.
Original `import.json`, target identity and old backup remain. The cancellation
receipt is published only after payload removal and sync; exact replay returns
that original completion. Interrupted deletion requires a fresh inspection.
Foreign mounts/hardlinks, unknown entries, changed contents, a published object
or accepted retirement refuse. Only verified terminal cancellations stop
reserving a future restore and cease blocking ancestor retirement.

The declared ordered consumer patch now leaves the failed copy intact through
the original full useful-recovery assertions, then explicitly inspects/cancels
it through the delivered CLI before original epoch retirement. A second exact
owned pressure inode fills the store to zero available blocks before inspection
and cancellation. Complete original consumer success and an unchanged terminal
reservation at cleanup remain mandatory. No original assertion, vet setting,
resource ceiling or deadline is removed.

Root-owned focused regression evidence
`factorybox-recovery-import-cancel-checks-ece11b9a923343e9bef25839a4e25d40`
passes nine top-level tests, including changed identity/content/inventory,
partial deletion, resurrection, interrupted publication and original unknown
inventory controls. An earlier check runner failed before starting a build or
test because its source manifest incorrectly traversed archived artifacts;
that failure is recorded in `factorybox-recovery-import-cancel-checks-84968b2cac2048d89f60dc1a42bf99db`.
All six portable groups pass in
`factorybox-checks-7fefdaffbc7f4087848d16487f1c67fc`, including enabled vet,
tagged consumer compilation and unchanged reference/ordered fixture verification.
Native full-store cancellation is still pending; these results do not qualify
the candidate or close broader import interruption cases. Failure before the
initial journal and mounted later-stage imports still need explicit acceptance.

The complete A66 failed store remains reconstructable through lossless repack
`factorybox-storage-repack-0dc5a343b461446ba09ac94836c4ce2e`: all
34,360,786,944 original bytes, SHA-256
`278a2604a8a92da93c19f49785b1552b77a8bc3f596e1892b055c745a6eae725`,
verify in both encodings; 954,530,319 encoded bytes are reclaimed.

Storage maintenance first retired 24 superseded closed PASS generated
filesystems in `factorybox-runtime-archive-retention-4919376533d44554a8fc9cd10ad8154c`,
after complete raw/member verification and complete matching replacement
observations. It preserves 15,467,578 diagnostic bytes and returns
1,000,622,370 encoded bytes. Current core archives and selected failures remain.

The next maintenance plan,
`factorybox-resolved-archive-retention-77aa2afc7d8d4a01af5b88e952c7276e`,
verifies 18 explicitly selected closed failed generated filesystems against
complete passing replacements. Selected original runtime test files must be
exact or strictly additive, and every originally observed case must pass.
The operating example separately keeps its byte-identical workflow/inputs and
same worker bytes: its original 4-GiB outcome stays failed, while its complete
32-GiB replacement passes. This grants no 4-GiB success claim. The maintenance
stops on a bookkeeping KeyError after nine independently completed retirements;
the remaining nine finish in
`factorybox-resolved-archive-continuation-91b268542a1d465fbf8777640889207f`.
Both the partial maintenance failure and every original native failure remain
recorded. Post-verification checks all 18 exact receipts, original sources,
results and logs, and unchanged goal/handoff. Complete raw decompression and
member hashes precede every removal. Former generated filesystems are
intentionally unavailable after retirement; frozen inputs, saved logs/results,
member indexes and retained diagnostics remain. Total retired encodings are
1,236,410,020 bytes. This restores required start headroom, without changing
the 32-GiB limit, 8-GiB start reserve or host free-space floor. Original EBUSY,
500/A66 import failures, full Factory failures and current provider rejection
retain their selected complete archives.

The full first-release goal remains active and production qualification false.
