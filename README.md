# Factorybox

Factorybox is an experimental Linux system-container runtime built around pinned Sysbox and CRIU.
Its Go CLI starts workspaces, captures complete admitted process/filesystem state, creates
independent forks, pauses with snapshot RAM release, resumes and deletes owned runtimes.
It requires no KVM or custom kernel.

There is **no production-qualified release**. Sysbox containers share their worker's Linux kernel.
The initial trust boundary is a separate worker per customer or trust domain. Factorybox does not
provide VM isolation, a Kubernetes control plane or an installer for arbitrary cluster nodes.
IDEs, Dev Containers and agent harnesses are optional consumers.

Development and native tests currently use the existing Mac and OrbStack Linux ARM64 worker.
The runtime executes on Linux. Reference admission requires ARM64, 4096-byte pages and kernel
`7.0.14-orbstack-00380-ga7e0a2dc9535`. AMD64, other kernels and cloud workers remain unqualified.
Matching this tuple alone does not establish runtime installation, filesystem compatibility
or recovery.

## Current qualification

| Selected input | Exact local artifact |
| --- | --- |
| Sysbox | `factorybox-build-sysbox-a62d052944c84818b85a67c5f0a53e30` |
| CRIU | `factorybox-build-criu-182f84bb8fca4746b606fb83fc56f380` |
| Native fixture base | `factorybox-build-native-base-aade0777712c460cbd8bf22156888b6f` |
| Frozen developer rootfs | `factorybox-devx-inputs-03da7afcc972412591e68df3222c40c0` |
| Offline project images | `factorybox-devx-project-images-80f6bd67353a4641adb50b71530d2b1b` |

These names select ignored local evidence, not downloadable releases. A fresh checkout builds
and prepares its own inputs using the commands below. Runners verify supplied identities before use.

| Gate | Evidence and limits |
| --- | --- |
| Portable Go packages and Python tools | Mac Go checks pass; 82 Python tool tests pass |
| Reference integrity | 232 sealed reference files verified; imported overlays remain unchanged |
| Current Sysbox clean rebuild | `factorybox-build-sysbox-526aba43b72d44bc8f0a1769e6a1fe50`: fresh caches, identical runc/fs/mgr binaries and package manifest |
| Current runc module | `factorybox-upstream-e3c049a6660a47a5ad44b00f51236a0d`: 279 main tests, 227 subtests, 14 original upstream skips |
| Current runc integration | `factorybox-upstream-9e3324f1d97e4130a6d6ea6040d62b5a`: 47 passes, twelve original upstream skips |
| Unchanged fs/mgr/IPC/library modules | Retain the combined pass in `factorybox-upstream-eafc65ac3e6f46fcb89b97480cf224d8`; the manager's four shiftfs cases skip on this worker |
| Current CRIU core | `factorybox-criu-core-ea025fce7b7f4939b70aeec382d5bdfe`: 18 unchanged C tests, two dump/restore cycles each, zero skips |
| Prepared-base pause/resume | `factorybox-runtime-ddb555c6666c407a87ab91555edbe0af`: nine tests, 46 subtests, zero skips, owned cleanup |
| Filesystem preflight rejection | `factorybox-runtime-f8ec18cb222e4feb9e91acccce6abb24`: checkpoint/pause and immutable replay preserve the original running PID, RAM and useful work; forged rejection kind refused |
| Image ownership and durability | `factorybox-runtime-f8ec18cb222e4feb9e91acccce6abb24`: nine tests and 40 subtests, including one complete-directory barrier, failed-barrier refusal and symlink/hardlink/foreign-mount rejection |
| Earlier separate-Theia baseline | `factorybox-devx-lifecycle-e3c84fef88f548a68ed7ecbd49da5a3b`: eight app phases and nine IDE checks, unchanged inputs/global sentinels, full cleanup |
| Current developer lifecycle with Theia and OpenCode | `factorybox-devx-lifecycle-147a342d25c245909ffc9e308334fcbf`: eight app, nine IDE and eight authenticated OpenCode API checks; unchanged inputs/global sentinels, full cleanup |
| Interrupted CRIU process recovery | **Failing**: the current source remains held after the verified CRIU child is killed |
| Upstream Systemd `/proc/kcore` assertion | **Failing on the reference worker**; the original assertion is preserved |

Skipped cases are not executed passes. The CRIU selection is not the full ZDTM matrix.
Required failing or unrun gates block a production release.
An owned diagnostic, `factorybox-proc-kcore-ac45c0f06a5e46f5ac4bcc02140c6756`, reproduced the
missing entry in a freshly mounted private Proc filesystem without Sysbox. `/proc/kmsg` and
`/proc/kallsyms` existed. The original Bats case expects `Permission denied` for `cat /proc/kcore`;
this worker returns `No such file or directory`. No dummy Proc entry or assertion replacement
is used to hide that platform difference.

## Runtime and consumer boundaries

| Path | Responsibility |
| --- | --- |
| `cmd/factorybox` | Local administrative CLI and platform diagnostics |
| `internal/lifecycle` | Persisted lifecycle intent, immutable identities, original kernel handles and reconciliation |
| `internal/sysboxcapture` | Pinned capture/restore adapter, complete filesystem verification and private Sysbox state |
| `internal/pageview`, `internal/snapshot` | Extracted logical memory readers and snapshot validation |
| `upstream/lock.json` | Exact source commits, archives, hashes and ordered patches |
| `upstream/ports.json` | Provenance of extracted runtime behavior |
| `upstream/licenses` | Retained component license notices |
| `tests/reference` | Sealed implementation, fixture and assertion reference |
| `examples/minimal/.devcontainer` | Ordinary digest-pinned workspace without IDE or agent requirements |
| `examples/factory` | Compose Dev Container, existing Factory Connector assignment and optional OpenCode launcher |
| `tools` | Bounded build, native qualification and evidence-retention commands |
| `artifacts`, `dist`, `.cache` | Ignored evidence, binaries and prepared source inputs |

The chosen path uses an ordinary complete retained-source CRIU writer and complete writable
filesystem backing. An explicitly imported immutable lower may be shared read-only. Each fork
gets independent writable layers, private manager volumes and runtime identity.

Factorybox freezes the admitted source while capture runs. It does not need task-completion hooks
from an agent. Capture consistency with arbitrary external writers is unqualified. Dev Containers
describe applications; they do not define the snapshot engine. Factory remains the product
control plane in the composition example.

## Develop on the Mac

```sh
mise install
mise exec -- go test ./cmd/... ./internal/... ./tests/...
mise exec -- python3 -m unittest discover -s tools -p 'test_*.py'
mise exec -- python3 tools/reference.py verify
mise exec -- go build -trimpath -buildvcs=false -o dist/factorybox ./cmd/factorybox
./dist/factorybox version
./dist/factorybox doctor
```

`doctor` returns nonzero on macOS. Host checks do not execute Linux-only fixtures.
Cross-compilation does not qualify another architecture. Native runners currently target the
existing `orbstack` Docker context and own separate containers and volumes. They do not install
a node-wide runtime or change global kernel settings.

## Prepare and build pinned components

```sh
mise exec -- python3 tools/sources.py prepare
mise exec -- python3 tools/sources.py verify --offline
mise exec -- python3 tools/build_native_base.py
mise exec -- python3 tools/build_criu.py \
  --native-base-build factorybox-build-native-base-EXACT_RUN_ID
mise exec -- python3 tools/build_sysbox.py
```

Preparation checks source archive hashes, local overlays and zero-fuzz ordered patches. Declared
test-fixture compatibility edits carry exact before/after hashes; original assertions are
preserved. A recipe or cache mismatch fails instead of silently reusing changed sources.

To replace one prepared source after a deliberate recipe change:

```sh
mise exec -- python3 tools/sources.py prepare --component sysbox-runc --refresh
```

The preceding cache is retained under `.cache/retired`. Do not edit prepared or archived sources
in place. Change maintained patches and lock metadata, then prepare a new source tree.

The native builder uses an exact public Envbox input and dated, signature-checked Ubuntu APT
inputs. CRIU has a pinned source epoch and runs its owned-scope C regressions before compilation.
Artifacts record source contexts, toolchains, package manifests and executable hashes.

```sh
mise exec -- python3 tools/build_sysbox.py \
  --clean --compare factorybox-build-sysbox-EXACT_RUN_ID
mise exec -- python3 tools/build_criu.py \
  --native-base-build factorybox-build-native-base-EXACT_RUN_ID \
  --compare factorybox-build-criu-EXACT_RUN_ID
```

The current Sysbox comparison passed from empty Go caches. The current CRIU binary also matched
comparison build `factorybox-build-criu-3781b0dbb40f4928a0b52b046ec6f4bd`. These observations cover
the selected native builder, not another platform. The current Factorybox CLI also matches
between the native image/admission gate and the passing frozen developer context.
Factorybox's original-code license must be
chosen before public distribution. No public release or Factory installation is part of the
current qualification.

## Local lifecycle API

The CLI accepts a trusted, administrator-prepared OCI bundle. It runs as worker root with
matching Sysbox manager/filesystem services, a private root-owned state directory and a finite
private cgroup2 limit. It is not an untrusted OCI-spec service.

Inside an appropriately prepared worker:

```sh
fb() {
  factorybox --root /var/lib/factorybox \
    --runtime /opt/factorybox/bin/sysbox-runc --runtime-sha256 EXACT_SYSBOX_SHA256 \
    --criu /opt/factorybox/bin/criu --criu-sha256 EXACT_CRIU_SHA256 \
    --manager-root /var/lib/sysbox/manager "$@"
}

fb start --bundle /work/bundle source
fb state source
fb checkpoint --snapshot-id aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa source
fb fork --source source --snapshot-id aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa branch
fb pause --snapshot-id bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb branch
fb resume --snapshot-id bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb branch
fb delete branch
```

Snapshot IDs are caller-chosen immutable 32-character hexadecimal identities. Forking a fork or
a resumed sandbox uses the same commands. Forks do not depend on a live source registration
after complete backing has been captured.

The state root must be mode 0700 and root-owned. Operations persist intent before external
creation, retain exclusive locks and bind requests to original executable, PID, cgroup, mount and
directory identities. Successful retries reconcile the original operation. An uncertain create,
capture or restore is not silently repeated. Deleted IDs cannot be reused.

The caller owns the original bundle/rootfs. Factorybox does not remove it on deletion, and
rejects its concurrent use by another sandbox. Generation cleanup verifies ownership and
retains source data, history and ambiguous outcomes.

### Immutable prepared base

```sh
fb prepare-base --source /work/base-input developer-base
mount --bind /var/lib/factorybox/bases/developer-base/rootfs /work/bundle/lower
mount -o remount,bind,ro /work/bundle/lower
```

The OCI bundle uses that empty `lower` mountpoint and annotation
`"io.factorybox.base": "developer-base"`. Import completely compares source and copy, records
intent, and protects the retained base with a read-only self-bind. Checkpoints and forks use
checked read-only aliases. Writable trees retain complete content/metadata digests and
independent inodes.

The adapter admits the saved ID-mapped, single-lower Sysbox overlay and recognized private
manager volumes. It verifies contents, UID/GID, mode, mtime, hardlinks, symlinks, device identities
and xattrs, and rejects unowned binds or foreign mounts. Inode identities and ctime need not
remain equal after copying. Executable hooks and arbitrary mount layouts are not admitted.
Base garbage collection and reconstruction after losing the worker mount namespace are not
implemented.
Filesystem configuration admission runs while the original source is safely frozen, before the
guard enters CRIU capture. The worker repeats admission. Known preflight rejection therefore
thaws the original process and retains a failed immutable operation rather than leaving it held.
Namespace and filesystem rejection records remain distinct; replay refuses a changed kind.
This does not authorize thaw after an uncertain CRIU failure.

### Pause, recovery and failure containment

Pause publishes complete backing before stopping the original source. It records original-cgroup
anonymous memory, syncs file-backed payloads and verifies zero snapshot-payload residency.
RAM-backed pause storage is rejected. Resume retains the original runtime identity and checks
unchanged exclusive working backing before restoring.

Recovery requires the same live worker and retained mount namespace. Worker-restart RAM recovery
and cross-node migration are unsupported. Partial preparation, restore and deletion have
explicit ambiguity checks; complete recovery across every failure boundary remains required
for release.

The interruption gate kills the verified CRIU child through its pidfd after at least 8 MiB of
image data while it traces the source. No complete checkpoint is published. The preceding
committed checkpoint remains independently usable. Verified network rollback restores only
the admitted private firewall/socket delta, including original TCP cookies, marks and repair modes.

This does **not** prove complete process rollback. The source remains held. A read-only native
diagnostic found a residual 5.023-MiB executable `CRIUMFD` mapping. It did not prove every register
or memory mutation safe, and its instrumentation has been removed. There is no manual thaw or
weakened assertion in the required gate. Automatic recovery of that current source is still
failing and blocks production qualification.

## Execute native gates

Commands take exact successful local input artifact names. The reference remains immutable:

```sh
mise exec -- python3 tools/reference.py check
mise exec -- python3 tools/reference.py build
mise exec -- python3 tools/reference.py native
mise exec -- python3 tools/native_core.py
mise exec -- python3 tools/native_failure.py

mise exec -- python3 tools/lifecycle_native.py \
  --sysbox-build factorybox-build-sysbox-EXACT_RUN_ID \
  --criu-build factorybox-build-criu-EXACT_RUN_ID \
  --native-base-build factorybox-build-native-base-EXACT_RUN_ID \
  --suite base-pause
```

Lifecycle profiles include `lifecycle`, `guard`, `capture`, `pause`, `base`, `base-pause`,
`delegation`, `namespaces`, `time`, `mountfd`, `admission` and the required failing `interruption` gate.
Each runner checks declared assertions, inputs, process outcome and exact owned cleanup.
Small native fixtures do not replace full developer-workload qualification.

```sh
mise exec -- python3 tools/upstream_unit.py \
  --sysbox-build factorybox-build-sysbox-EXACT_RUN_ID --suite all
mise exec -- python3 tools/upstream_unit.py \
  --sysbox-build factorybox-build-sysbox-EXACT_RUN_ID --suite runc-integration
mise exec -- python3 tools/upstream_bats.py \
  --native-base-build factorybox-build-native-base-EXACT_RUN_ID \
  --sysbox-build factorybox-build-sysbox-EXACT_RUN_ID --suite lifecycle
mise exec -- python3 tools/criu_core.py \
  --criu-build factorybox-build-criu-EXACT_RUN_ID \
  --native-base-build factorybox-build-native-base-EXACT_RUN_ID
```

The outer Systemd Bats suite has separate pinned image acquisition through `tools/bats_images.py`
and options `--suite systemd --images factorybox-bats-images-EXACT_RUN_ID`. It retains the failing
`/proc/kcore` assertion on this worker. Skips and failures are never converted into executed passes.

CRIU core selection lives in `tests/criu-core-cases.json`. It preserves C assertions and
`-Werror`, verifies credentials and original process retirement, and checks every restored test's
own `PASS`. It does not invoke global hugepage configuration.

## Developer composition

`examples/factory` uses ordinary Compose/Dev Container files and the existing Factory Connector
assignment format. Its app has a RAM-only instance nonce/counter, workspace marker, post-create
hook and Compose dependency. There is no Factorybox-specific template format. Installation
authority URLs, identities and credentials require explicit wiring.
The native proof uses a closed test authority inside the captured network. It does not qualify
reconnecting forked Connectors to an external Factory authority or assigning fresh production
credentials to cloned applications.

```sh
mise exec -- python3 tools/devx_inputs.py \
  --factory-artifacts /absolute/path/to/runtime-artifacts.json --pull-missing
mise exec -- python3 tools/devx_project_images.py
mise exec -- python3 tools/devx_compose.py \
  --consumer-inputs factorybox-devx-inputs-EXACT_RUN_ID \
  --project-images factorybox-devx-project-images-EXACT_RUN_ID \
  --native-base-build factorybox-build-native-base-EXACT_RUN_ID
```

Input JSON supplies exact `build-runtime-host` and `developer-ide` repository digests.
Preparation verifies their platforms/identities and exports a never-started runtime. The full
lifecycle verifies that frozen rootfs archive by size and hash, allowing use after the unused
original image is evicted. Consumers executing images still require their exact cached identity.

The current frozen consumer contains Docker 29.9.0, Compose 5.6.0, Dev Containers CLI 0.89.0,
Node 24.21.0, the real Factory Runtime Connector, OpenCode server 2.0.27 and Bun 1.4.3.
The offline registry serves checked original manifest/blob bytes, preserving digest verification
inside the private Docker daemon.

```sh
mise exec -- python3 tools/devx_lifecycle.py \
  --consumer-inputs factorybox-devx-inputs-EXACT_RUN_ID \
  --project-images factorybox-devx-project-images-EXACT_RUN_ID \
  --native-base-build factorybox-build-native-base-EXACT_RUN_ID \
  --sysbox-build factorybox-build-sysbox-EXACT_RUN_ID \
  --criu-build factorybox-build-criu-EXACT_RUN_ID \
  --theia-image EXACT_CACHED_IDE_REPOSITORY_DIGEST
```

Omit `--theia-image` for a lifecycle without an IDE. The fixture retains its 3-CPU/3-GiB worker,
separate 1-CPU/768-MiB Theia, maximum two live Sysbox sandboxes, 390-second Go test and 420-second
outer execution deadline. It has no external network or published service ports.

Theia starts once live checkout ownership is prepared, alongside private Docker initialization.
Short-lived helpers clone actual live project mounts in their original namespaces, transfer
detached mount descriptors over a private socket, and bind them into the IDE's pinned mount/root
namespace. They do not edit raw overlay upper directories. The IDE uses the mapped project UID,
a read-only root and no capabilities. Views/handles are released and the IDE is stopped before
final sandbox teardown.

The passing full proof retained one native Theia connection through checkpoint, forks, a fork of
a fork, pause, resume and a fork of the resumed source. Independent final IDE edits were read
back through both real Connectors and Dev Containers. This is a test composition, not a public
workspace projection API or browser-editing qualification.

`examples/factory/opencode.sh` optionally launches the supplied OpenCode 2 bundle. It accepts one
workspace, requires `OPENCODE_PASSWORD`, binds to loopback and keeps its database in that
workspace's `.git` directory unless `OPENCODE_DB` is supplied. `FACTORY_OPENCODE_CONFIG` remains
optional. Model fetching is disabled. The `--opencode` fixture option checks the supplied server's
authenticated native API across lifecycle phases. Add it to the same command to include the
optional harness in the Sysbox workload. The current combined gate passed in 352.08 seconds with full
cleanup. Every phase verified the supplied OpenCode 2.0.27 process's namespace PID/version through
`/api/info`, authenticated session reads and rejection of unauthenticated reads. Probes execute
inside the sandbox's user, PID, mount and network namespaces. Their own HTTP connections consume
complete responses and confirm socket closure before the next checkpoint.

This checks the restored server's liveness, identity and authentication. It does not qualify model
inference, resumed tool execution or agent-turn semantics. External network peers are not forked.
In-flight TCP connections can cause CRIU to reject capture; the fixture does not enable
`--skip-in-flight` or discard connection state to pass.

Additional IDE, desktop or harness applications belong in consumer images and ordinary app
configuration. Their checkpoint compatibility requires native proof. No application is required
by the core lifecycle.

## Performance observations

| Phase | Separate Theia | Separate Theia and in-sandbox OpenCode |
| --- | ---: | ---: |
| Frozen rootfs, immutable base and OCI preparation | 42.707 s | 31.885 s |
| Prepared start through first useful Connector response | 43.300 s | 57.278 s |
| Source checkpoint preceding the first fork | 25.948 s | 17.374 s |
| Fork of that checkpoint through first useful app response | 13.439 s | 29.156 s |
| Branch checkpoint preceding the descendant fork | 10.735 s | 25.995 s |
| Fork of fork through first useful app response | 14.380 s | 24.037 s |
| Pause with stopped source and snapshot RAM release | 14.980 s | 15.666 s |
| Resume through first useful app response | 4.122 s | 8.966 s |
| Resumed-source checkpoint preceding its fork | 34.678 s | 31.192 s |
| Fork of resumed source through first useful app response | 28.514 s | 14.070 s |

Both columns are individual shared-worker observations with the selected components. Evidence:
`factorybox-devx-lifecycle-e3c84fef88f548a68ed7ecbd49da5a3b` and
`factorybox-devx-lifecycle-147a342d25c245909ffc9e308334fcbf`. Outer execution took 318.03 and
352.08 seconds respectively; Go lifecycle including teardown took 291.53 and 337.22 seconds.
The first column precedes the filesystem-admission and image-durability changes; it is a
historical baseline, not a controlled before/after performance comparison.

Preparation precedes prepared start. Fork timings use an already published checkpoint. A fresh
current-state fork must also pay its preceding checkpoint: the first combined observation's
17.374-second checkpoint plus 29.156-second fork totals about 46.53 seconds. These are component
sums, not a separately instrumented aggregate request. App-response timings precede the
subsequent IDE/OpenCode assertions; those checks have their own observations. Artifacts retain
CPU, I/O and memory samples. External IDE writes are serialized outside capture.

These observations do not establish p95 or isolate improvements from background load.
The sub-three-second p95 target remains unachieved. Selected optimizations are the verified
immutable lower, complete target verification with fresh source-digest reuse, one durability
barrier per filesystem/image generation and a reused digest buffer. Rejected lazy/delta/read-hint experiments
remain historical evidence, not runtime defaults.

The preceding full regression, `factorybox-devx-lifecycle-16442a4d4a5c46c9895d407b6a04487a`, hit
the unchanged 60-second capture deadline after its resumed-source dump. Its retained metadata
shows CRIU finished in 1.06 seconds, filesystem copying/verification took 41.99 seconds and
filesystem durability took 16.53 seconds. Only six of 268 images had their ownership adjusted
before timeout. Image finalization previously forced a separate file barrier for each image.
The current path verifies independent regular files through the pinned directory, rejects
foreign mounts, and uses one complete filesystem barrier plus directory sync. The failed
run remains failed; a later complete pass does not establish a latency distribution.

## Storage and exact cleanup

At admission, new builds/runs require 8 GiB of headroom inside a 64-GiB artifact budget and
12 GiB of additional host free space. Developer runs refuse to start while a preceding volume is retained.
Fixtures share verified immutable component outputs through hardlinks and mount them read-only.

```sh
mise exec -- python3 tools/artifact_storage.py status
mise exec -- python3 tools/reconcile_devx.py factorybox-devx-lifecycle-EXACT_RUN_ID
mise exec -- python3 tools/artifact_storage.py compact \
  /absolute/path/to/artifacts/closed-run/output/capture-source.tar
```

Failed developer volumes stream directly into bounded single-thread Zstandard archives.
Stored bytes are decoded and hashed during capture; the final encoded file must match the
verified stream. Source removal requires complete verification and exact ownership checks.
Timeouts, corruption and capacity failures retain the source and remove incomplete copies.
Other closed-run dumps are automatically compacted after successful cleanup. Frozen inputs
and active runs remain directly usable.

Reconciliation `b6c1452e4c3f46c498a1b3a8365276f1` verified 21,307,064,320 original bytes against a
7,145,530,085-byte archive in 441 seconds, then removed the original owned developer volume.
Encoding receipts retain hashes and reconstruction commands.

At the user's cleanup request, 18 superseded Factorybox failed-state payloads were retired after
a later successful lifecycle. Source contexts, results, logs and checksum receipts remain.
The initial journal is in `factorybox-storage-cleanup-d18a0d90d81041618cafa478cd84abec/summary.json`.
The later passing Theia lifecycle superseded three additional payloads, recorded in
`factorybox-storage-retirement-e36113b34bf140589aff22417eb44e44`. Not every older encoding receipt
still has a payload. The historical experiment's source and evidence contents remain unchanged.
There is no global Docker pruning or deletion of unrelated installation data.
The obsolete OpenCode API-route probe payload was separately retired after the combined passing
gate, in `factorybox-storage-retirement-495dcaaaad464d51b5a04053858ff3a6`. Its logs remain. The
in-flight TCP capture failure is retained for failure-recovery hardening.

## Remaining release requirements

Production qualification requires complete bounded failure recovery, including the failing
actual-CRIU interruption case; resolution of required upstream failures/skips on supported
workers; and native qualification of every claimed deployment target. Browser behavior,
concurrent external writers and further workload compatibility need their own evidence.
A deployment/installation contract, worker storage quotas and license review are required
before distribution. The optional OpenCode API composition has native proof; model inference,
agent-turn semantics and resumed tool execution remain outside that proof.

Kubernetes scheduling and worker lifecycle management remain separate integration work.
Factory production integration and public publishing have not been performed.
