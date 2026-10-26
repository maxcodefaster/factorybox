# Factory consumer acceptance

Factory's consolidated `main` replaces Envbox execution with an exact
Factorybox worker, while retaining the existing Runtime Attempt, authenticated
Parent/Child Connector and PostgreSQL reserve controller. Canonical Source is
shared with the independent editor. Native Delete releases the caller's private
OCI mounts before the worker stops. The worker remains a standalone runtime.

The consumer starts `dockerd` directly because Sysbox already prepares its system
mounts. The generic DinD entrypoint makes those mounts shared and creates proc
mount propagation that CRIU cannot restore. Where available, the consumer OCI
recipe selects ordinary TCP by disabling MPTCP only in its new guest network
namespace. Neither change modifies a host sysctl or weakens CRIU's mount checks.

## Native ARM64 local execution

The ordinary ARM64 worker build is
`factorybox-build-worker-28a65e26aff44e7484f55bd7c6dbf0c7`, image
`factorybox-worker-28a65e26aff44e7484f55bd7c6dbf0c7@sha256:a3627f7ffdea8c4ce188c8a3b75977f2912c1fc538e4c24a28f6b9f254afce73`.
Its delivered Factorybox executable SHA-256 is
`41f615efcbdb36f6519ec835afedea183081d2629340b6d6c02a7c3f6add3c9b`.
It was compiled without Candidate tags or a linked test kernel. Ordinary
execution admits only the exact local Linux ARM64 kernel
`7.0.14-orbstack-00380-ga7e0a2dc9535`, 4096-byte pages and the checked worker
resources. Other kernels/page sizes remain rejected.

The real Factory runtime and separate Linux Bun proof image come from
`factorybox-build-consumer-9303760253644018aaecbf4285ff5c5f`, frozen from Factory
commit `c8ae26464a7705e59317e47e9f5f0cdf37128963`. Both complete consumer lanes
passed on that ordinary worker, with confirmed cleanup, unchanged inputs and
unchanged observed host sysctls:

- `factorybox-factory-consumer-836a3b412c0b43d8baf3dc9b5c2da128` performs real
  Start and authenticated Parent/Child connection, pauses, atomically edits the
  canonical Source, and resumes the same app nonce, PID and advancing memory
  counter. The source edit is visible and the same Docker child has zero restarts.
- `factorybox-factory-consumer-9f5033f398034a69a6dfcc4d7c6e5f09` prewarms and
  activates the actual owned reserve fixture with one native Start, then
  preserves its app RAM and Docker child through Pause/Resume.

Both pauses confirm actual Sandbox anonymous RAM and resident snapshot pages at
zero. Worker services and filesystem cache remain charged to the 3-GiB ceiling;
this is not zero retained worker memory. These are single functional observations,
not p95, a latency guarantee or production qualification.

The separate full 32-MiB private-actor recovery cases also pass on this exact
ordinary image: planned replacement
`factorybox-bounded-loss-d680a20ec9614e93ab41d3c814d58b67` and abrupt replacement
`factorybox-bounded-loss-0824cda4320e4ab9ae514ec097708142`. They preserve private
RAM/files, refuse a wrong checkpoint hash, replay recovery without replacement,
and retain independent descendants after original-source deletion. They do not
establish worker-loss recovery of Factory's independently mutable shared Source
or its retained file descriptions.

The local tests neither activate a retained Factory installation nor publish an
image. The release-only workflow must run its native gates again on each actual
release image; the broader production-release programme remains separate.

The separate upstream Systemd suite still has one required failure on this
kernel: `systemd /proc exposure` expects `/proc/kcore` to deny a read, but the host
has no such kernel node. The other three executable cases pass and the original
two skips remain explicit. The exact failure is retained as
`factorybox-bats-5498a7fb7436489b831822a8a1e39777`. No assertion or skip condition
was changed. This blocks full release qualification even though the actual
Factory lanes above pass.

## Earlier AMD64 acceptance

The exact delivered worker is `factorybox-build-worker-c53c97dcc88e4555a2dd5c2426076b18`.
The actual Factory consumer image was built from its frozen Connector/image
sources at commit `205e985e5fc8753bef7ffbccc74920bd5e1e7832`, build
`factorybox-build-consumer-2fcf987deb7042edb22e3532f50cbc67`. Tests use a bounded,
offline image mirror from verified OCI archive bytes, not a public-registry
availability claim. The worker has a 3 CPU / 3 GiB ceiling and a 420-second
lifecycle deadline. The separate endpoint fixture owns its private namespace.

Two complete native runs passed, including exact resource cleanup and unchanged
host sysctls:

- `factorybox-factory-consumer-c7f4282059ce4e55ad57f8195e3016e7` starts the actual
  Factory environment and Dev Container, pauses it, atomically edits canonical
  Source, and resumes the same app nonce, PID, memory counter and Docker child
  without a restart. The new editor content is visible after Resume.
- `factorybox-factory-consumer-d35a03d03b354458b36382f281ae27bd` runs the actual
  reserve Connector, prewarms the pinned image, observes readiness and activates
  the slot. It uses exactly one native Start, then preserves the app and Docker
  child through Pause/Resume. A used slot is never returned to the reserve.

These single observations are not a latency distribution. The shared-source run
spent 25.07 seconds exporting the consumer image, 46.64 seconds preparing its
private filesystem, and 14.14 seconds from native Start to the first app response.
The reserve run spent 29.45, 18.75 and 24.59 seconds respectively. Its last number
includes prewarming and activation, not activation latency alone. The fixture
also gates initial Connector connection while Docker becomes ready. These are
not browser-click timings or a claim of a faster pool allocation.

Native Resume took 3.13 and 4.06 seconds respectively. Both pauses reported zero
Sandbox anonymous RAM and zero resident snapshot pages after completion. Total
worker cgroup memory still included about 1.50 and 2.28 GiB of file cache plus
about 27 and 25 MiB of anonymous memory immediately after Pause. That cache is
charged to the 3 GiB worker ceiling; it is not zero retained worker memory.

`factorybox-factory-postgres-84598b9f7c5044ac9298944185179cb6` passes 20 checks
with 193 assertions against real PostgreSQL, using controlled Kubernetes replies.
These cover immutable profiles, lost Start/Delete replies without a second
mutation, pause/resume credentials, foreign worker refusal, reserve ownership,
and Source placement without starting execution.

Reproduce the consumer runs with `tools/factory_consumer_native.py`, once normally
and once with `--reserve`. Supply the exact worker build, consumer build, retained
project-image archive and private Factory checkout. The runner freezes consumer
programs and the real Runtime Hub, records preparation, first useful work, memory
and failures, and preserves full failed stores after a Pause attempt. Run
`tools/factory_postgres_probe.py --factory-repo /path/to/open-software-factory`
for its owned database fixture and versioned controller checks.

No API server, kubelet, CNI or CSI was exercised by these consumer proofs. Installation still requires
the exact private worker image and executable hash, an admitted node/kernel and
the actual storage/network acceptance. Cross-node RAM migration, recovery of
shared-source state after worker exit, other Linux profiles and the deferred
24-hour endurance run are not established by these results. Existing release
gates remain described in [first-release status](first-release-status.md).
