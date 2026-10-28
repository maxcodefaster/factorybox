# Build and release

Detailed source, native build and release automation instructions for Factorybox.
Start with the [README](../README.md) and the
[current release scope](releases/0.1.0-rc.1.md) for the supported boundary.
Historical pilot packages and observations remain identified separately.

## What changed in Sysbox and CRIU

The [source lock](../upstream/lock.json) records archive hashes, ordered patches,
imported overlays, prepared-file hashes and fixture compatibility declarations.
The [pilot profile](../upstream/amd64-pilot-profile.json) adds the selected CRIU
patches, actual binary identities and evidence hashes. No upstream version
floats at build time.

| Component | Pinned source commit |
| --- | --- |
| [Sysbox runc](https://github.com/nestybox/sysbox-runc) | `081856cc5d17e7095f066b08d0eca6bb0b515c47` |
| [Sysbox fs](https://github.com/nestybox/sysbox-fs) | `c3d2ebc65102e32e74e383675f03b45556326888` |
| [Sysbox mgr](https://github.com/nestybox/sysbox-mgr) | `2d45af01bcfd8641e6641f76104c6003bf501cd7` |
| [CRIU source fork](https://github.com/castai/criu) | `086df34a2da27125fa05e66ba4d6b63cbb220b2e` |

The lock also pins Sysbox ipc, libs, fuse and upstream tests. These are the
actual changes, rather than a claim that the components run unmodified:

- Sysbox patches and retained overlays capture/restore virtual filesystem
  state, preserve registration and namespace lifetime, reconstruct rootfs,
  seccomp listeners and cgroup delegation, and issue restore receipts.
  The manager can disable unqualified binfmt_misc handling. Restored overlays
  use a mount-local `index=off` setting.
- The ordered Sysbox-fs seccomp poll patch backports upstream
  `1ac3fd588495674eb8604f3c79b1573348eb3487` and all six decision regressions.
  A lone transient `POLLERR` retries the listener; hangup, invalid descriptors
  and other terminal events still close it. Original tests remain intact and
  vet stays enabled. This requires new exact component/image qualification.
- The base CRIU patch stack handles the admitted nested namespaces, detached
  proc roots/descriptors, time namespaces, cgroup-v2 limits and zombie PIDs.
  Five additional retained-context/exit-observer patches protect original
  source state during the evidenced capture failures. Three nested-overlay
  patches preserve layer mappings, stable paths and shared upper/work mounts.
- The Go runtime owns intent/completion records, component and object identity,
  filesystem admission/copy verification, memory release, recovery and deletion.
  Bounded inotify fallback roots are an explicit OCI annotation.

Each selected change has regression evidence. The immutable imported reference
and overlays retain their original bytes; later changes are ordered patches.
Candidate filenames are historical provenance, not automatic selection.
SHM, lazy/delta snapshots and read-hint experiments are excluded from the
selected path. [Investigation notes](pilot-progress.md) retain the limits
and failed approaches.

## Build and integrate

Ordinary execution admits two explicit local Linux tuples: AMD64 with kernel
`7.2.8-9-omarchy`, and ARM64 with kernel `7.0.14-orbstack-00380-ga7e0a2dc9535`.
Both require 4096-byte pages, cgroup v2 and the tested Docker/local-volume path.
`factorybox doctor` checks the tuple; it does not replace native tests or qualify
a new image. The historical AMD64 pilot remains unchanged. Cross-compilation
does not qualify another machine, kernel, page size or storage backend.

The Systemd proc-security fixture observes `/proc/kcore` before starting Sysbox.
If the kernel exposes it, all original permission-denied assertions still run.
If the kernel lacks it, the guest must also lack the node and any symlink, and
reading it must fail with `No such file or directory`. Kmsg must still deny reads
and kallsyms must still return zeroed addresses. The ordered test-only patch and
exact before/after hashes are declared in the source lock. No test is skipped,
host node synthesized or runtime permission changed. Historical failures retain
their original results; see the [current status](first-release-status.md).

The launcher can run on macOS or Linux, but native execution requires a matching
Linux Docker host. It cannot run natively on arbitrary Unix kernels. On macOS,
the default Docker context is `orbstack`; on Linux it is `default`.
`FACTORYBOX_DOCKER_CONTEXT` overrides `DOCKER_CONTEXT` and both override that
default. Select the context explicitly when more than one daemon is available.
Inspect the selected host without creating any resources:

```sh
mise exec -- python3 tools/release_native.py --check-host
```

The runner detects the Docker server's architecture, not the client machine's.
An explicit incompatible `--platform` is refused even when Docker can emulate
it. This check grants no kernel admission or lifecycle qualification.
Local images and `artifacts/` are not transferred by Git. Rebuild exact native
inputs on the release machine. An AMD64 pass never qualifies ARM64.

The repository pins Go and Python through mise. Run portable checks first:

Portable checks use Go's host build constraints. Linux-only descriptor-store,
source-actor and consumer checks run on Linux and are reported as unavailable
on other hosts. Their native assertions remain unchanged; a passing macOS
check does not qualify a Linux worker or enable ARM64 runtime execution.

```sh
mise exec -- python3 tools/check.py
mise exec -- python3 tools/sources.py prepare
mise exec -- python3 tools/sources.py verify
```

After preparing CRIU, run the bounded
[patched-function regressions](../tests/criu-defensive-functions/README.md):

```sh
mise exec -- python3 tests/criu-defensive-functions/run.py
```

The source lock includes ordered upstream defensive backports for reused
thread arrays, controller strings, mount/lock scanner widths and loginuid
write lengths. Every controller-string caller propagates rejection, including
the retained fork's delegation caller. Existing OOM-score and pid-max writes
already use actual string lengths and retain their existing checks. Original
CRIU test files remain unchanged. The function checks run on both CI targets;
they do not replace exact-image native qualification.

The 512-MiB capacity fixture compiles its guest test actor without unused DWARF
debug data. ELF symbols, executable code and every native assertion remain intact;
the store size and admission reserves stay unchanged. Regression checks compare
the actual ELF code and preserve candidate kernel pinning. Exact recipe and
assertion hashes are declared in
[`bounded-capacity-debug-info.json`](../tests/compatibility/bounded-capacity-debug-info.json).

Build the native base and Sysbox, then the selected CRIU composition and worker:

```sh
mise exec -- python3 tools/build_native_base.py --platform linux/amd64
mise exec -- python3 tools/build_sysbox.py --platform linux/amd64 --network host \
  --builder "$builder" --bounded-build
mise exec -- python3 tests/criu-retained-candidate/build.py \
  --native-base-build "$base_build" --exit-hold --exit-hold-socket \
  --nested-overlay --nested-overlay-stable-paths --nested-overlay-shared-root
mise exec -- python3 tools/build_criu_recovery.py --base-candidate "$retained_build"
mise exec -- python3 tools/build_worker.py \
  --platform linux/amd64 \
  --native-base-build "$base_build" --sysbox-build "$sysbox_build" \
  --retained-candidate "$criu_build"
```

Set the variables to the exact successful artifact names printed by the
preceding builds. On the tested host the Docker tools use
`FACTORYBOX_DOCKER_CONTEXT=default FACTORYBOX_DOCKER_ELEVATION=sudo`.
These commands build local containers and install no host runtime.
Independent offline rebuilds reproduced all three Sysbox executables, CRIU,
the complete worker package and image. They reuse verified toolchains and
dependency inputs; future network/package availability is a separate concern.

For a bounded Sysbox build, `$builder` must name a running, single-node Buildx
`docker-container` builder. The build checks its actual container's memory
and CPU quotas before compilation. Buildx does not support the previously
used `build --resource` flags. Create a uniquely named builder without
changing your default:

```sh
builder="factorybox-build-$(uuidgen | tr '[:upper:]' '[:lower:]')"
docker --context orbstack buildx create --name "$builder" \
  --driver docker-container \
  --driver-opt 'image=moby/buildkit@sha256:cec9f139f45e93c5c69c60f8b07cfad9f43f4ef6b6a6cd917527fea5ff2e3dea,memory=3g,cpu-period=100000,cpu-quota=300000,network=host' \
  --buildkitd-flags '--allow-insecure-entitlement network.host' orbstack
docker --context orbstack buildx inspect --bootstrap "$builder"
```

The local worker pipeline accepts either `linux/arm64` or `linux/amd64`.
Supply an exact successful native-base build for that same target. It builds
Sysbox, the retained-context/nested-overlay CRIU composition, the ordered
shared-description patch required by Factory's canonical Source, then the
worker image and a losslessly compressed package:

```sh
mise exec -- python3 tools/build_worker_pipeline.py \
  --platform linux/arm64 --native-base-build "$base_build" \
  --builder "$builder" --bounded-build --network host
```

Use `--sysbox-build` and `--retained-candidate` to reuse exact verified completed
inputs, never an inferred latest build. A retained CRIU input without the
required process-tree failure correction is rejected. Before shared descriptions,
the pipeline applies the exact owned PID-counter and cross-parent zombie recovery
patches through `tools/build_criu_recovery.py`. A supplied complete composition
must match all four patch hashes. Ordinary direct worker builds enforce the same
requirement. The original build fixture and seven process-tree assertions stay
unchanged; CI also runs that process-tree regression on the prepared base source.
A retained CRIU input without the
shared-description patch receives that ordered delta in a new build. An input
that already has the exact patch and both required feature receipts is reused.
The worker checks the ELF architecture
of every executable, component platform agreement and the final image platform.
Its receipt records the image and Factorybox executable digest. There is no
push, installation or automatic runtime admission.

The [Linux CI workflow](../.github/workflows/ci.yaml) runs portable checks on pull
requests and pushes to `main`, avoiding duplicate runs on feature-branch pushes.
It uses GitHub's `ubuntu-24.04` AMD64 and
`ubuntu-24.04-arm` ARM64 runners. Each job runs the Linux Go and Python suites,
verifies the immutable reference, then compiles and executes the ordinary CLI
on that architecture. Downloadable artifacts contain the executable, SHA-256,
frozen Go inputs and test logs. On disposable GitHub-hosted X64 VMs, the job
removes the unused preinstalled Android SDK to satisfy the unchanged disk
reserve. Self-hosted runners never run that step. These jobs do not test Sysbox/CRIU lifecycle
operations or admit GitHub's runner kernel as a supported runtime profile.

Use Actions → **Build worker images on demand** for complete component and
worker builds. Select AMD64, ARM64 or both. This separate manual workflow uses
the native pipeline and a verified 3-CPU/3-GiB BuildKit container. It exports the
exact image through a fully verified Zstandard stream and retains the compressed
worker package, reconstruction receipts and build logs for seven days. Download
`worker-image.tar.zst` from its artifact and load it with:

```sh
zstd -dc worker-image.tar.zst | docker load
```

The image is saved by its immutable image ID, recorded in the export's
`result.json`. The workflow publishes no registry image and makes no lifecycle
qualification claim. It needs native Linux Docker/Buildx, Zstandard and enough
disk for the component build. The unchanged storage guard requires 8 GiB of
artifact headroom and reserves 12 GiB of host free space. Standard GitHub runner
disk capacity is not guaranteed to fit the full pipeline. Set repository Actions
variables `FACTORYBOX_BUILD_RUNNER_AMD64` and `FACTORYBOX_BUILD_RUNNER_ARM64`
to larger runner labels when needed. A self-hosted override must name a unique
label for a trusted, matching Linux architecture. Each attempt uses its own
checkout and refuses cleanup if builder ownership cannot be verified. There is
no Docker pruning or host runtime installation. GitHub-hosted X64 builds use
the same Android SDK space preparation as the portable CI.

The [release publication workflow](../.github/workflows/images.yaml) runs only when a
GitHub release is published. It does not run on branch pushes, pull requests or
manual dispatch. The canonical tag, initially `v0.1.0-rc.1`, must name a commit contained
in `main` and match `internal/version/VERSION`, the delivered CLI and image label.
Each native job publishes its qualified
`ghcr.io/<owner>/<repository>/worker:<version>-amd64` or `<version>-arm64` image.
Only after both native jobs pass does the workflow create the combined
`<version>` manifest. An unavailable runner or failing gate blocks that manifest.
An existing version is refused rather than overwritten; an uncertain registry
lookup also refuses publication. There is no moving `latest` tag.

The candidate needs matching dedicated self-hosted Linux runners labelled
`factorybox-native-amd64` and `factorybox-native-arm64`, with mise,
Docker/Buildx, Zstandard, GitHub CLI
and the exact admitted native Docker kernel, cgroups and privileges. The
workflow installs no host Sysbox runtime, VM, kernel or emulator. It builds a uniquely
named 3-CPU/3-GiB BuildKit container without changing the operator's selected
builder. Cleanup requires its recorded immutable identity. Checkout directories
and Docker client credentials are separate for each run and attempt.

The jobs build the exact component pipeline, the pinned Factory consumer and
its offline project-image archive. `tools/release_native.py` refuses Candidate
workers, foreign architectures and a mismatched CLI version. It runs the original
CRIU core and Sysbox unit/integration/lifecycle/systemd gates, all fourteen delivered-worker
suites, planned and abrupt worker replacement, and both actual Factory consumer
lanes. The original upstream skip sets stay explicit. The host-proc fixture is
the same on both architectures. Qualification failure
prevents publication. Uploaded workflow artifacts contain gate receipts and
build/gate logs, not RAM images or private test stores. Source inventories,
checksums and receipts have architecture-specific asset names so the two jobs
cannot collide when attaching release assets. These jobs do not complete the
remaining [controlled-release checklist](first-release-status.md), qualify
deferred endurance work or activate Factory.

Cold isolated builders use in-file stages instead of Docker-daemon-only parent
tags and explicitly load their outputs. The Bats-only fixture reconstructs the
unchanged pinned native-base recipe and checks its full original package
inventory before adding test utilities. Its declaration distinguishes that
recipe rebuild from a byte-identical base-image rebuild. Runtime worker images
still use their exact supplied native-base image digest. Upstream assertions
and the bounded builder ceiling are unchanged.

Worker image transports include the `/opt` destination parent with a fixed
timestamp and its original permissions. Normalizing bundle files alone leaves
Docker to timestamp that missing parent at build time, producing different
image layers from identical packages. Independent no-cache builds must match
the package, image and every layer; a matching package alone is insufficient.

Original Factorybox code uses Apache-2.0. The worker package includes the exact
root `LICENSE`, `NOTICE`, `LICENSING.md` and pinned upstream notices, with their
hashes. CRIU keeps its GPL-2.0-only terms and its library keeps its original
terms. Before pushing the image, the workflow attaches the matching component
source archive, CLI, worker package, checksums and native gate receipt to the
release. `tools/release_sources.py` binds the exact prepared sources, patches,
original upstream archives and build recipes to the tested worker result.
See [LICENSING.md](../LICENSING.md) for the separate operating-system package obligations.
The workflow pins the Factory
checkout to the exact reviewed consumer commit. If that repository is private,
configure `FACTORY_CONSUMER_READ_TOKEN` with read access to it; public checkouts
can use the normal workflow token.

ARM64 component and ordinary worker builds now use the admitted local ARM64
tuple above. Other ARM64 kernels and page sizes remain rejected. The owned
timer reader runs natively on ARM64, using
the iterator's referenced BTF layouts and an inline PID filter without kernel
maps. The ARM64 timer CPU backend also passes native staged/native-wait
register, stack and signal-mask handoff checks. Complete guarded lifecycle,
recovery and consumer checks remain required for each delivered release image.
A successful build alone grants no additional platform or production qualification.

Run the timer-reader prerequisite against an exact native base for the target:

```sh
mise exec -- python3 tools/timerstate_native.py \
  --platform linux/arm64 --native-base-build "$base_build"
```

It verifies the real relative deadline, PID/birth ownership, refusal of a wrong
birth and removal of its temporary BPF program. It does not admit the platform,
test RAM checkpoint/restore, or replace the required lifecycle/consumer suites.

The separate CPU prerequisite executes the real ARM64 private code and confirms
X1-X30, SP, NZCV, unchanged request/stack bytes, native deadline activation and
original context on recapture:

```sh
mise exec -- python3 tools/timer_arm64_native.py --native-base-build "$base_build"
```

Explicit lifecycle candidates link only their selected architecture and the
actual native Docker kernel. `--candidate-platform linux/arm64` is available to
the native lifecycle runner; it does not change ordinary worker admission.

To test a delivered worker image before platform admission, pass
`--native-candidate` to `tools/build_worker.py` with the exact component inputs
above. It requires a matching native Linux Docker host and links only that host's
kernel. The bundle and build receipt retain this unqualified candidate identity.
It is not a release image and does not change ordinary builds or select a worker
for Factory.

The bounded-store proof can execute that exact delivered candidate, retaining
the original ENOSPC, independent RAM, checkpoint/fork and pause/resume assertions:

```sh
mise exec -- python3 tools/bounded_store_native.py \
  --worker-build "$worker_build" --native-candidate --suite capacity
```

The runner requires the delivered candidate's exact native kernel and observes
the fixed global sysctl set on the Docker host, not on macOS. An absent kernel
interface remains explicitly absent in both observations, as in the original
global observer. This isolated proof does not qualify the remaining timer,
worker-recovery or consumer suites.

The same runner has explicit `restore-hold`, `timer-state-reader`,
`timer-exit-guard` and `timer-detach` suites for either native architecture.
They retain the original five sealed restore/resume acknowledgement cases,
bulk and non-leader timer reads with an independent Python comparison, actual
normal/SIGKILL tracer exit and 256 real detach completions under owned CPU
pressure. Every original assertion and refusal control remains in those
fixtures. Their ARM64 passes do not replace the remaining interrupted-timer,
recovery or Factory consumer qualification.

Build Factory's actual runtime consumer and the separate native Linux Bun proof
image from one exact Factory checkout:

```sh
mise exec -- python3 tools/build_factory_consumer.py \
  --platform linux/arm64 --factory-repo /path/to/open-software-factory \
  --builder "$builder" --bounded-build --network host
```

The build freezes the Dockerfile, Connector sources and tool pins. It checks the
delivered ELF architecture, image digests and Bun version. It does not install
Factory or publish an image. On macOS the proof bundles JavaScript locally but
executes it with the native Linux Bun image, never a mounted macOS executable.

Run `tools/factory_consumer_native.py` with the exact `--worker-build`,
`--consumer-build`, `--project-images` and `--factory-repo`, then separately with
`--reserve`. An unqualified candidate also requires `--native-candidate` and its
exact native kernel. Start, real Parent/Child Connector readiness, preserved app
RAM, unchanged Docker child and canonical Source edits during Pause must all
pass. A successful Start alone is not Pause/Resume qualification.

`tools/bounded_store_loss.py --worker-build "$worker_build" --loss planned`
and the separate `--loss abrupt` lane exercise whole-worker replacement with
the original 32-MiB RAM, hash-refusal, recovery-replay and independent-descendant
assertions on either native architecture. Candidate workers require the same
explicit `--native-candidate` guard. This proof uses private test source, not
Factory's mutable shared Source, and does not establish recovery of shared
descriptors after their worker exits.

The `timer-signal`, `timer-admission-signal`, `timer-remainder` and
`timer-remainder-protection` suites use native ABI fixtures for either
architecture. They retain the original raw EINTR, single-handler,
interruption-time remainder and source/fork/resume assertions. Compatibility
declarations pin the original assertion bodies and unchanged AMD64 assembly.
On ARM64, `timer-admission-reader` also distinguishes a frozen live wait from
computation or a completed restart, and exercises bounded settlement of an
expired, unmodified source before capture.

`timer-natural` isolates the original twelve-second source/fork completion
case without removing it from the complete retirement suite. Its shared fixture
first freezes and thaws the source for a read-only probe. The independent
`timer-freezer-abi` ARM64 control confirms that Linux changes X8 from
`clock_nanosleep` to `restart_syscall` at that native boundary, without Factorybox
timer preparation or capture. The frozen natural fixture checks exactly that
restart register; uncaptured and signal fixtures still check the original
syscall register. The raw return, other registers, SP, NZCV and original deadline
checks are unchanged.

`stopped-memory` checks a real committed pause and rejects live or foreign
cgroups. If a stopped child's anonymous-memory statistic is still batched, the
runtime reads only its own identity-checked, bounded worker's statistics before
rereading the child. Completion still requires the child's actual zero anonymous
memory, an empty cgroup and the unchanged three-second observation deadline.
There is no global flush or fabricated zero.

`timer-orphan` and `timer-guard-loss` exercise actual controller/guard SIGKILL
after completed process capture. They require the staged source and queued
signal to remain held and unresolved requests to refuse replay or deletion.
These completed-process boundary tests do not qualify automatic repair or
recovery while a retained-cure grant is in flight.

Use the [worker integration example](../examples/worker/README.md) for the exact
Compose deployment, store ownership, readiness and administrative workflow.
The worker needs privileged container access and owns one persistent store.
It exposes no network API. The tested worker ceiling is 3 CPU/3 GiB with at
most two live sandboxes; separate Theia has 1 CPU/768 MiB. Complete native
lifecycle runs retain the 420-second deadline.

The [minimal](../examples/minimal/.devcontainer/devcontainer.json) and
[Factory](../examples/factory/.devcontainer/devcontainer.json) examples remain
acceptance consumers. Application installation, private Docker startup, image
imports and application readiness belong to those consumers.
