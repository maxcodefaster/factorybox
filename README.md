# Factorybox

Factorybox is an experimental Linux system-container runtime based on Sysbox and CRIU.
The intended lifecycle is start, checkpoint, independent RAM/filesystem fork, pause with
snapshot-RAM release, resume and deletion. It requires no KVM or custom kernel.

This repository is being extracted from the existing runtime experiment. The retained reference
contains the working checkpoint/fork/resume implementation and its regressions. The production
lifecycle API has not yet been extracted. The current `factorybox` command provides version and
platform inspection only.

Sysbox containers share the Linux worker kernel. The initial isolation scope is separate workers
per customer or trust domain. IDEs and agent harnesses are optional applications. Dev Containers
describe development workloads; they do not define the snapshot engine.

## Layout

| Path | Purpose |
| --- | --- |
| `cmd/factorybox` | Small diagnostic entry point during extraction |
| `internal/platform` | Explicit reference-platform admission |
| `internal/pageview` | Extracted logical snapshot reader and its native regressions |
| `internal/snapshot` | Extracted CRIU pagemap codec and complete logical memory comparison |
| `upstream/lock.json` | Exact Sysbox/CRIU source commits, archive hashes and patch order |
| `upstream/patches`, `upstream/overlays` | Source inputs applied to the pinned upstream trees |
| `upstream/ports.json` | Provenance of extracted runtime code |
| `tests/reference` | Unchanged source reference, tests and import provenance |
| `tests/reference/fixture` | Sealed full developer lifecycle fixture and required assertion counts |
| `examples/minimal/.devcontainer` | Ordinary minimal Dev Container consumer |
| `tools` | Source preparation and bounded verification commands |
| `artifacts` | Ignored test evidence, including failed runs |
| `dist` | Ignored local build outputs |

The old experiment remains the historical evidence archive. This repository contains source and
provenance rather than copies of its images, binaries, failed runs or large native archives.

## Develop on the Mac

Install mise, then use the pinned Go and Python tools:

```sh
mise install
mise exec -- go test ./...
mise exec -- python3 -m unittest discover -s tools -p 'test_*.py'
mise exec -- go build -trimpath -buildvcs=false -o dist/factorybox ./cmd/factorybox
./dist/factorybox version
./dist/factorybox doctor
```

`doctor` prints JSON and returns nonzero on the Mac. The runtime executes on Linux. Matching the
reference platform alone does not qualify a node: filesystem, userfaultfd, ownership, leases and
runtime installation need their own checks.

## Pinned sources

```sh
mise exec -- python3 tools/sources.py prepare
mise exec -- python3 tools/sources.py verify
```

Preparation downloads immutable source archives, checks SHA-256, applies patches with zero fuzz
and verifies the compiled CRIU sources against the experiment's recorded build. Upstream assertions
are preserved. One error-construction fixture has an explicit format correction for Go vet; the
original and prepared file hashes are both pinned in the lock. Every other original upstream test
file must remain byte-for-byte unchanged. A fresh checkout can fetch these sources without the old
experiment directory. `--offline` uses previously verified cached archives.

Source preparation does not install a runtime, change the worker kernel or run the upstream suites.
The selected CRIU source starts from the recorded CAST AI nested-namespace commit. It is not the
unmodified upstream CRIU release. Sysbox components also carry explicit experimental changes.
The unmodified Sysbox umbrella integration suite is pinned separately at v0.7.1; individual runtime
component commits remain explicit in the lock.

Cached trees also record the complete source tree and its preparation recipe. A changed
implementation file, patch or recipe is rejected even when its tests are untouched. When a reviewed
recipe changes, prepare one exact component again; the previous tree is retained under `.cache/retired`:

```sh
mise exec -- python3 tools/sources.py prepare --component sysbox-runc --refresh
```

The existing worker's retained Linux build image can compile the prepared CRIU source:

```sh
mise exec -- python3 tools/build_criu.py
```

This command uses the exact existing builder image, 2 CPUs, 1 GiB, no network and an owned
temporary container. It records whether the resulting binary matches the historical build.
Reconstructing that builder from public inputs is still a release requirement; the local builder
image is not a published Factorybox build dependency.

Build the selected Sysbox runc, fs and mgr components from prepared sources:

```sh
mise exec -- python3 tools/build_sysbox.py
```

The recipe pins the Go and Envbox image digests, disables automatic Go toolchain replacement and
records native compiler/package versions and every context input. The manager includes upstream's
`idmapped_mnt` build tag. A successful build includes the selected inherited fork, registration and
seccomp regressions. It does not establish complete upstream compatibility. APT dependencies are
recorded but still need a pinned clean-machine reconstruction.
The restore helper is in a separate package so the core library can compile under its natural
Sysbox module name. The component build compiles that test package as a guard; execution is a
separate native gate, and the full upstream Go gate keeps vet enabled.

## Retained reference

```sh
mise exec -- python3 tools/reference.py verify
mise exec -- python3 tools/reference.py check
mise exec -- python3 tools/reference.py build
```

`verify` checks all 232 imported reference and Sysbox input files. `check` executes the original
host-compatible driver tests. `build` cross-compiles the Linux ARM64 reference and test programs.
Cross-compilation is not native qualification.

The one-time import tool is retained for provenance. Ordinary development must not overwrite the
reference. Port behavior with its tests into `internal`; compare against the unchanged reference.
Fixed `/experiment` paths and optional application-specific assertions remain inside that reference
until their production counterparts have independent proof.

## Native regression on the existing worker

These commands require the existing `orbstack` Docker context and the exact locally retained base
image configuration `sha256:041140e968e8efdbc6b2f33651f84bc19c13593c6d3f527e90af648f967dd52f`.
They never pull a replacement implicitly.

```sh
mise exec -- python3 tools/reference.py native
mise exec -- python3 tools/native_core.py
mise exec -- python3 tools/native_failure.py
```

The selected reference ownership/graph regression uses one uniquely owned 1 CPU/256 MiB container
and a uniquely owned volume. This selected suite is not the complete upstream Sysbox/CRIU gate.
The extracted core tests use 1 CPU/128 MiB without a privileged container. Each stores stdout,
stderr, input identities, outcome and cleanup observations under `artifacts/`. Removal requires
the exact resource name and owner label. A Docker failure is not proof that a resource is absent.
The extracted native core suite has passed all 12 platform, pagemap, inherited-memory and page-reader
tests with zero skips and successful owned cleanup. The reference ownership/graph suite has passed
50 top-level tests. Neither is a full lifecycle or upstream qualification.
The separate source-preservation suite has passed all three native tests and 16 subtests with zero
skips and owned cleanup. It covers controller/helper death, live-tracer rejection, preservation of
foreign PIDs, and rejection of replaced or foreign-mounted cgroups before deletion.

The developer lifecycle runner retains the original 3 CPU/3 GiB probe, separate 1 CPU/768 MiB
Theia, 420-second deadline and maximum two live sandboxes. Supply successful named build artifacts:

```sh
mise exec -- python3 tools/reference_lifecycle.py \
  --criu-build factorybox-build-criu-EXACT_RUN_ID \
  --sysbox-build factorybox-build-sysbox-EXACT_RUN_ID
```

The runner builds an owned image with the new binaries and uses a unique Kubernetes namespace.
It reserves 2.5 probe CPUs on the shared worker while retaining the original 3-CPU limit. This
allows admission beside the retained Factory workloads; observations from this shared scheduling
profile are qualification runs, not a clean performance baseline.
It retains all 461 assertion records from the sealed successful reference, including complete
independent RAM/files, forks from forked/resumed sources, pause RAM release, private Docker,
Compose, Dev Containers, browser/OpenCode responses and native Theia read/edit. A zero process exit
without those assertions fails. Namespace deletion includes the exact saved UID as an API
precondition. Failed runs retain streamed logs and available native metadata.

The full lifecycle is not yet independently qualified in this repository. The first attempts found
runner transport/admission issues. A later attempt reached private Docker, Compose and Dev Container
assertions, then OrbStack's internal Docker agent stopped unexpectedly on October 18, 2026 at
14:56:50 UTC. Both test containers subsequently terminated with exit code 255. The worker kernel did
not reboot. The cause of the agent termination is unresolved, and this run provides no complete
fork/resume latency result. Its exact namespace cleanup was later confirmed separately.

If owned namespace deletion outlasts the runner, preserve its artifact and reconcile only that run:

```sh
mise exec -- python3 tools/reconcile_lifecycle.py factorybox-lifecycle-EXACT_RUN_ID
```

This command verifies saved UIDs and ownership, can remove a confirmed terminal Pod with its UID
precondition, and records a new observation without rewriting the original result. It leaves
namespace finalizers and unrelated cluster services intact. Exit 75 means cleanup remains pending.

This is a reference fixture. It still depends on the existing local Theia image and a read-only
Btrfs rootfs cache at its recorded worker path. It does not provide the public lifecycle API or
a portable installation. Theia browser editing and active model inference are not asserted.

Run the full upstream Go unit suites in an owned container with its own Docker daemon:

```sh
mise exec -- python3 tools/upstream_unit.py \
  --sysbox-build factorybox-build-sysbox-EXACT_RUN_ID
```

The complete native `libcontainer` unit package has passed 27 tests and 30 subtests with zero
skips after correcting the restore helper's package boundary. Run that package from its independently
compiled test binary with:

```sh
mise exec -- python3 tools/upstream_unit.py \
  --sysbox-build factorybox-build-sysbox-EXACT_RUN_ID --suite libcontainer
```

The runner checks runc, fs, mgr, ipc and each nested libs module, retaining failures and skips per
test. It also rejects failed test events hidden by an upstream `TestMain` that exits zero.
The test rootfs follows upstream's immutable Busybox ARM64 commit and has a recorded SHA-256.
Do not run the Sysbox umbrella Makefile on the Mac or retained worker. Its integration scripts need
a separately prepared, isolated test environment and include host-oriented cleanup commands.

The first complete Go-suite run remains a failed release gate. Other failures include outdated
mock signatures, Go vet format diagnostics, IPC module bookkeeping, real namespace-file requirements
in an older synthetic fixture, and cgroup integration assumptions that fail on this worker.
The successful core package run does not waive these failures. The umbrella/Bats/systemd and CRIU
ordinary checkpoint/restore suites remain unqualified.

## Release requirements

Factorybox has no production-qualified release yet. Required gates include:

- Pinned upstream Sysbox unit/core/integration suites and CRIU core checkpoint/restore suites.
  Failed or unrun required suites block qualification. Preserve upstream assertions.
- Independent files and RAM across repeated forks, forks of resumed/forked sources and actual
  pause RAM release followed by correct resume.
- Bounded controller/helper failure recovery, source preservation and exact cleanup.
- A supported-platform matrix backed by native execution. Initially only the exact tested ARM64
  worker is a candidate. AMD64 and cloud workers remain unqualified.
- Reproducible native builds, component license notices and a chosen license for Factorybox's
  original code before public distribution.
- Honest request, preparation, first-use, CPU/I/O and retained-memory observations. The sub-3-second
  p95 goal remains unachieved. Historical component improvements and tiny samples do not establish it.

Recovery is currently scoped to the same running worker. Cross-node migration and RAM recovery after
worker restart are outside the first release. Kubernetes deployment needs appropriate worker access
and runtime installation; a container image alone does not make arbitrary managed clusters supported.

`examples/minimal/.devcontainer/devcontainer.json` is an ordinary digest-pinned Linux workspace.
It has no IDE or agent dependency and requires no access to a node Docker socket. A Factory
composition with separate Theia, the Factory Runtime Connector and optional OpenCode is still
being extracted. Compositions use ordinary Dev Container, Compose and Kubernetes files.
Production Factory integration and public publication are separate steps after qualification.
