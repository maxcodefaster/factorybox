# Native AMD64 validation

The local v0.1 acceptance receipt is
`factorybox-v01-qualification-b354267fdf4945f18cf776ae24b682ba`.
It pins 31 candidate native gates, three ordinary-build gates, the final
portable batch, exact packaged/tested CLI bytes and the independent-component
package comparison. All selected owned containers/volumes are absent after
cleanup. The receipt qualifies the declared local integration scope and
explicitly keeps `production_qualified` false.

The v0.1 target is this Linux AMD64 worker, kernel `7.2.8-9-omarchy`, with
4096-byte pages and cgroup v2. Production qualification remains open. The
ordinary build now admits this tuple and rejects other execution tuples.

The current composition uses Sysbox build
`factorybox-build-sysbox-b86e971efe9b46989e6809996b358f0a`, CRIU recovery build
`factorybox-build-criu-retained-7b00b2618b8c4e1980422be354d2b9c1`, and native base
`factorybox-build-native-base-b8dc7327368e4b9bab47bca533070f4a`.
These are local evidence identities. Rebuild them from the repository recipes
on a fresh worker; they are not downloadable releases.
`upstream/amd64-profile.json` selects the exact base lock, five additional
ordered CRIU recovery patches, observer header and Go source hash. Nested
Overlay2 and SHM optimization candidates are excluded. Historical builder
filenames retain their candidate names; selection does not rewrite their
original build results or qualify other compositions.

## Complete Factory consumer

`factorybox-devx-lifecycle-ffa5070e8ab7437eb8365f018d171d32` passes the complete
original lifecycle with real private Docker VFS, Compose, Dev Containers,
Factory Connector and a separate live Theia process. Source/fork RAM and files
diverge independently. Fork of fork, deletion of the original source, pause,
resume and fork of the resumed source all pass. Theia keeps its original
process/client identity through the operations and reads/writes the selected
live workspace. Inputs, global sentinels and exact owned cleanup pass.
This uses the ordinary CLI, without a candidate build tag, and exactly the
CLI bytes delivered in the runtime bundle. The earlier candidate pass remains
in `factorybox-devx-lifecycle-7bfa7d8d77f44ce487270f33916ea73d`.

The closed authority is a test fixture. This does not qualify external Factory
authority, browser behavior or optional agent inference.

| Observation | Seconds |
| --- | ---: |
| Complete outer execution | 244.964 |
| Preparation, included in outer execution | 26.384 |
| Prepared start to useful application response | 31.985 |
| Fork to useful application response | 18.015 |
| Fork of fork to useful application response | 19.413 |
| Pause CLI execution | 24.602 |
| Resume to useful application response | 11.399 |
| Fork after resume to useful application response | 24.584 |

These are one run on a shared worker. They establish neither a latency
distribution nor a stable sub-three-second p95. Images and dependencies were
already acquired. The preparation charge includes unpacking, immutable-base
verification, OCI setup and Sysbox daemon readiness.

The paused sandbox's anonymous memory falls from 24,301,568 bytes to zero.
Its 239,165,440 bytes of file-backed snapshot payload have zero resident bytes
after release. Its cgroup still charges 12,648,448 bytes in total, including
9,052,160 file bytes. The bounded worker, Sysbox services and separate Theia
remain alive; pause does not promise that the whole worker uses zero RAM.

## Controller and socket fixes

The full run first failed at resume because this worker delegates `hugetlb`,
`rdma`, `misc` and `dmem` in addition to the original five controllers.
`criu-cgroup-v2-auxiliary-limits.patch` preserves their available limit files.
`factorybox-runtime-23b62d70fa3f41feb6b7d9dcb263a7f1` passes nine tests and 40
subtests. It carries finite 2-MiB/1-GiB hugepage limits through fork, pause/resume
and a resumed-source fork, then proves independent limit mutation. No hugepages
are allocated; GPU/RDMA workloads and other hugepage sizes are unqualified.

The consumer explicitly uses ordinary TCP with `GODEBUG=multipathtcp=0`.
Admission rejects real MPTCP descriptors and an unaccepted TCP connection
before entering capture. A transient accept queue may settle within the
existing bounded thaw/refreeze step. The regression preserves queued data and
the original source, and checks a useful independent fork afterward. No
socket-dropping or in-flight-packet omission is enabled.

## Minimal Dev Container

`factorybox-devx-lifecycle-03c0cf395f3b40bd999273444f50ace8` passes the complete
minimal example with the ordinary packaged CLI in 55.699 seconds including
preparation and exact cleanup.
It uses the real Dev Containers CLI and exact Alpine digest with a test-only
32-MiB private RAM actor. No Connector, authority, Compose, IDE or agent
process is started. The actor hashes all 32 MiB after each lifecycle operation
and mutates RAM and workspace files independently in source and fork.
Fork of fork, original-source deletion, actual pause/snapshot-RAM release,
resume and fork after resume all pass.

The test captures a running service after the setup client has completed.
An attached interactive exec session crossing PID namespaces is unqualified.
Its first fork fails with `Can't restore sid (801), expected 91` in
`factorybox-devx-lifecycle-d35df737f28b41d7825dac49bcda184b`. The pinned nested
CRIU implementation documents reparenting exec tasks and the loss of an
in-flight exec session. The selected service fixture starts its actor in its
own session and waits for the real setup client to finish. This is an explicit
consumer profile, not a fix for general attached exec/session restoration.
No original lifecycle or RAM assertion is removed. Preparation takes 30.035
seconds, prepared start to useful work 8.259 seconds, first useful fork 2.063,
fork of fork 2.015, resume 1.018 and fork after resume 2.014 in this individual
run. These are observations for this small service, not Factory-composition
timings or a latency percentile.

The earlier new-fixture failure
`factorybox-devx-lifecycle-da763fe10dca4bafb31f69e14e00da7c` compared a tagged
image spelling to Docker's tagless RepoDigest. The corrected assertion still
requires the exact digest and native architecture.

## Builds

The three Sysbox binaries rebuild byte for byte in
`factorybox-build-sysbox-repro-eb4e293349ee4d7f98306544d99e5b25`. The rebuild
starts with an empty Go compiler cache, no network, no BuildKit cache mounts
and fresh source/output directories. Its exact frozen dependency/toolchain
image supplies the existing protobuf generator, compiler and installed
packages. The package manifest is unchanged. This proves offline source
recompilation; repeating Ubuntu snapshot acquisition remains open after the
recorded HTTP 503, connection timeouts and DNS failure. A further clean attempt
is retained in `factorybox-build-sysbox-154494b3e7a44162a1c5378f16808879`.

CRIU base build `factorybox-build-criu-6bdd4b4e765e4c07bc5a2e87c888ef2f`
rebuilds identically in `factorybox-build-criu-a7fde3e6602c4325861beff70566b62d`.
The recovery composition rebuilds identically in
`factorybox-build-criu-retained-5add1516df884d5daa0c14d8c33302a0`, including the
same prepared-source hash. Its binary SHA-256 is
`75e41cf4c525d7a735eb98e333b9dcd466d85e2b55b4019f67bd148da572b31f`.
Both CRIU compositions pass all 18 selected unchanged upstream C cases with
two ordinary dump/restore cycles each. All 1,030 upstream test files remain
unchanged. Evidence is `factorybox-criu-core-2d2e5c5048a542c58f669949e579aee9`
and `factorybox-criu-core-d489ed56523044abbeb1a9596658e760`.

The complete ordinary runtime bundle, including upstream license notices,
builds byte for byte in `factorybox-build-runtime-f3450055b50645c48c43bfeb9638446b`
and `factorybox-build-runtime-1a47196ec6034d478e7660f891d88aba`. The second uses
independently rebuilt Sysbox and CRIU inputs. Run-specific artifact identities
stay outside the deterministic tar. Both full ordinary consumer tests and the
ordinary native core use exactly the packaged CLI bytes.

- Tar bytes: 92,989,440.
- Tar SHA-256: `f9231951d4b0e332327737ffab86e318bab1af49d4abff8efd094627bb95bf2b`.
- CLI SHA-256: `e77547733b3c16652d26a1bf0f11b51a52861f71120792d5b3033a1c123a91b1`.

The dependency list contains no proof fixture, IDE or Factory consumer. The
bundle includes the profile, source hashes and component hashes; its build
refuses Go source that differs from the reviewed profile.

## Recovery

The selected composition passes the real two-process, two-32-MiB-RAM source,
TCP, prior-checkpoint and twelve physical invalid-grant controls. Original
register/mask restoration and zero injected mappings are required. Evidence:

| Gate | Artifact |
| --- | --- |
| Child page dump / failed pause | `factorybox-runtime-retained-9a903592b2e74a5fbf54ab14a94c2d53` |
| Original controller loss after rollback | `factorybox-runtime-retained-f78a9cae0bc84c1b96a0c8224139958d` |
| Guard loss after accepted grant | `factorybox-runtime-retained-d93a3af0cdac48dbbae32028e94da8f3` |
| Actual thread retirement/churn | `factorybox-runtime-retained-1ea22492855a47adad1b02e7c8be8555` |

The real private Docker/Compose/Dev Container tree also passes these dedicated
gates under the original 420-second deadline. Each preserves useful original
state, a new Docker exec, source mutation, fresh capture and independent
current-state/prior-checkpoint forks. Helper loss additionally requires the
healthy tracer to publish a verified checkpoint.

| Interrupted component and boundary | Artifact | Outer seconds |
| --- | --- | ---: |
| Tracer during root checkpoint pages | `factorybox-devx-lifecycle-0dc881833dc74f579ec5ccd12124d9c8` | 166.606 |
| Tracer during dockerd checkpoint pages | `factorybox-devx-lifecycle-f7ccdcc8e6844ddaa85b4601052c0d0e` | 164.968 |
| Tracer during dockerd pause pages | `factorybox-devx-lifecycle-0cf6af79e64149c992619ac96a8631cd` | 160.880 |
| Retained helper during root checkpoint pages | `factorybox-devx-lifecycle-51d567a7f8a14a0f875274fb5a57c937` | 213.810 |
| Original controller after rollback, dockerd pause | `factorybox-devx-lifecycle-ccdcf3ff7c0d4888b57c5dddf52ffc93` | 161.976 |
| Original guard after accepted grant, dockerd pause | `factorybox-devx-lifecycle-7aaa3c97c86f4d65b9686acda3faf452` | 156.819 |

All input/global checks and exact owned cleanup pass. These are individual
observations. Before-grant guard death and earlier controller-loss windows
remain unqualified. No worker-restart RAM recovery or migration is inferred.

## Upstream omissions

The independent Linux platform/snapshot/page-reader core passes all 13 tests
with zero skips in `factorybox-core-e2c1e4ddabaa401789f8158859600562`, using the
ordinary packaged CLI. The earlier candidate core also passes. The actual
AMD64 execution tuple replaces only the historical ARM64 host check; all static
historical tuple assertions still execute. The original RAM graph, complete
byte comparison, malformed-input and real scratch-allocation failure assertions
remain. The first adapted run omitted the generic native-fixture flag and
correctly failed with five skips; that result is retained separately.

`factorybox-upstream-716f9a8228c541ec85bc217921669a6f` runs the complete selected
Sysbox Go modules with vet enabled. It passes 399 main tests and 323 subtests.
The original 18 skipped tests remain unrun, with their original assertions:

| Original skip | Reason reported by upstream |
| --- | --- |
| Four `TestShiftfsMgr*` cases | Shiftfs unavailable |
| `TestWriteCgroupFileHandlesInterrupt` | cgroup-v1 memory hierarchy absent |
| `TestInvalidCgroupPath` | cgroup v2 unsupported by this test |
| `TestUsernsCheckpoint`, `TestCheckpoint` | Unconditional `UNSUPPORTED` |
| `TestSystemdFreeze`, `TestCpuSharesSystemd`, `TestPidsSystemd`, `TestCgroupResourcesUnifiedErrorOnV1Systemd`, `TestCgroupResourcesUnifiedSystemd` | Systemd unavailable in the Go integration fixture |
| `TestCpuShares` | cgroup v2 has no CpuShares |
| `TestCgroupResourcesUnifiedErrorOnV1` | Requires cgroup v1 |
| `TestRootfsPropagationSharedMount`, `TestPIDHost` | Upstream marks not applicable |
| `TestExecInTTY` | Upstream marks racy, runc issue 2425 |

The distinct libcontainer core fixture passes 28 tests and 30 subtests without
skips. The integration suite passes 47 cases with its 12 original skips.
The native Factorybox/CRIU tests provide additional lifecycle evidence; they
do not turn these upstream skips into executed tests. Required unrun upstream
assertions continue to block a complete production qualification.

The original systemd mount-overrides test passes with the pinned image and
unchanged body/assertions in `factorybox-bats-e35903e1695544f4b0c4960d64692829`.
A separate immutable daemon config disables Docker bridge/NAT only for this
systemd fixture. The unmodified image otherwise tries to load unavailable
legacy iptables modules. No host module or global sysctl is changed, and no
legacy NAT qualification is inferred. The original `/proc` exposure case also
passes independently. The complete systemd file passes in
`factorybox-bats-61c38bd02f914609b5c31e40c7b750fe`: four original cases execute
successfully, bionic retains its cgroup-v1 skip and Arch retains its original
systemd-failure skip. These skips remain explicit production limitations.

`tests/amd64-fixture-compatibility.json` records exact before/after hashes and
the reason for every necessary existing-fixture compatibility change. Frozen
reference files and imported overlays remain immutable.

Final portable checks pass in
`factorybox-v01-checks-de15e5aea2404436bf483366621c2507`: Go packages and vet,
all 127 Python tool tests, 232 sealed reference files, all prepared upstream
source/test checks, 18 exact fixture before/after hashes and whitespace checks.
The final helper-API regression was fixed without changing its original
ownership assertion. Artifact allocation is 18,281,254,912 bytes, within the
32-GiB budget with more than 8 GiB of headroom at this check.
