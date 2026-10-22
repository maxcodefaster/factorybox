# Local production work

Status on 2026-10-22: active, **production qualification false**. The accepted
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
cleanup and unchanged global sysctls pass; the complete failure dump remains
available through its encoding and lossless repack
`factorybox-storage-repack-cd42f368c0074adba8482dd250845de6`.
This attempt does not test abrupt worker recovery. The next independent
attempt clarifies the actual direct-shell versus code-mode distinction in
the synthetic prompt. It does not hide failed requests or relax the one-shell,
RAM, watch, result, deadline or gateway-exit assertions.

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

## Release work still required

Broader cancellation and uncertain external side-effect handling; resolution
of the PID collision and new admission-fixture busy-mount cleanup; a predetermined
repeated performance comparison; verified base reuse in the supplied
operational path; store quotas/admission and low-disk cases; dependency-checked
checkpoint/epoch retirement; explanation of the preserved Sysbox-fs timeout;
repeated lifecycle/pressure/fault cases and bounded 24-hour soak; a reduced,
independently reproducible worker package and operations instructions; required
native and upstream suites on the selected delivered bytes. Missing or failed
required assertions continue to block production qualification.
