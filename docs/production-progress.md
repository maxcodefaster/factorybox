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

Still open: pause/resume and independent descendants with this external
provider, provider/client disconnection, complete worker replacement after a
committed pause, and refusal of unintended tool repetition after uncertain
results. The previous deterministic-model lifecycle proofs remain separate.

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

## Release work still required

External lifecycle/reconnection and uncertainty handling; a predetermined
repeated performance comparison; verified base reuse in the supplied
operational path; store quotas/admission and low-disk cases; dependency-checked
checkpoint/epoch retirement; explanation of the preserved Sysbox-fs timeout;
repeated lifecycle/pressure/fault cases and bounded 24-hour soak; a reduced,
independently reproducible worker package and operations instructions; required
native and upstream suites on the selected delivered bytes. Missing or failed
required assertions continue to block production qualification.
