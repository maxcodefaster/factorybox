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

Next, retain bounded owned-child diagnostics from the actual Go worker before
startup-failure cleanup. The old record lacks those child details. Further
successful starts alone cannot replace the missing causal evidence.

## Release work still required

Broader cancellation and uncertain external side-effect handling; resolution
of the new PID collision; a predetermined
repeated performance comparison; verified base reuse in the supplied
operational path; store quotas/admission and low-disk cases; dependency-checked
checkpoint/epoch retirement; explanation of the preserved Sysbox-fs timeout;
repeated lifecycle/pressure/fault cases and bounded 24-hour soak; a reduced,
independently reproducible worker package and operations instructions; required
native and upstream suites on the selected delivered bytes. Missing or failed
required assertions continue to block production qualification.
