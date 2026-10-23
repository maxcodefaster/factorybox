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
the captured environment. No cancellation/error qualification is claimed.

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
