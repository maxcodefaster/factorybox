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
| `upstream/lock.json` | Exact Sysbox/CRIU source commits, archive hashes and patch order |
| `upstream/patches`, `upstream/overlays` | Source inputs applied to the pinned upstream trees |
| `upstream/ports.json` | Provenance of extracted runtime code |
| `tests/reference` | Unchanged source reference, tests and import provenance |
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
and verifies the compiled CRIU sources against the experiment's recorded build. Original upstream
test files must remain unchanged. A fresh checkout can fetch these sources without the old
experiment directory. `--offline` uses previously verified cached archives.

Source preparation does not install a runtime, change the worker kernel or run the upstream suites.
The selected CRIU source starts from the recorded CAST AI nested-namespace commit. It is not the
unmodified upstream CRIU release. Sysbox components also carry explicit experimental changes.
The unmodified Sysbox umbrella integration suite is pinned separately at v0.7.1; individual runtime
component commits remain explicit in the lock.

The existing worker's retained Linux build image can compile the prepared CRIU source:

```sh
mise exec -- python3 tools/build_criu.py
```

This command uses the exact existing builder image, 2 CPUs, 1 GiB, no network and an owned
temporary container. It records whether the resulting binary matches the historical build.
Reconstructing that builder from public inputs is still a release requirement; the local builder
image is not a published Factorybox build dependency.

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
```

The selected reference ownership/graph regression uses one uniquely owned 1 CPU/256 MiB container
and a uniquely owned volume. This selected suite is not the complete upstream Sysbox/CRIU gate.
The extracted core tests use 1 CPU/128 MiB without a privileged container. Each stores stdout,
stderr, input identities, outcome and cleanup observations under `artifacts/`. Removal requires
the exact resource name and owner label. A Docker failure is not proof that a resource is absent.
The larger developer lifecycle retains its original 3 CPU/3 GiB probe, separate 1 CPU/768 MiB
Theia, 420-second deadline and maximum two live sandboxes; its independent runner is still being
extracted.

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
