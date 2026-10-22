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
Planned/abrupt worker replacement, all full consumers, broader exhaustion/fault
cases, final repetitions and upstream qualification on this new profile remain
unrun and block its selection and production qualification.

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

## Release work still required

Broader cancellation and uncertain external side-effect handling; resolution
of the PID collision and new admission-fixture busy-mount cleanup; a predetermined
repeated performance comparison; verified base reuse in the supplied
operational path; full-consumer bounded-store recovery and broader low-disk cases; dependency-checked
checkpoint/epoch retirement; explanation of the preserved Sysbox-fs timeout;
repeated lifecycle/pressure/fault cases and bounded 24-hour soak; a reduced,
independently reproducible worker package and operations instructions; required
native and upstream suites on the selected delivered bytes. Missing or failed
required assertions continue to block production qualification.
