# Factory consumer acceptance

Factory's private integration branch replaces Envbox execution with an exact
Factorybox worker, while retaining the existing Runtime Attempt, authenticated
Parent/Child Connector and PostgreSQL reserve controller. Canonical Source is
shared with the independent editor. Native Delete releases the caller's private
OCI mounts before the worker stops. The worker remains a standalone runtime.

The consumer starts `dockerd` directly because Sysbox already prepares its system
mounts. The generic DinD entrypoint makes those mounts shared and creates proc
mount propagation that CRIU cannot restore. Where available, the consumer OCI
recipe selects ordinary TCP by disabling MPTCP only in its new guest network
namespace. Neither change modifies a host sysctl or weakens CRIU's mount checks.

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

No API server, kubelet, CNI or CSI was exercised here. Installation still requires
the exact private worker image and executable hash, an admitted node/kernel and
the actual storage/network acceptance. Cross-node RAM migration, recovery of
shared-source state after worker exit, additional Linux profiles and the deferred
24-hour endurance run are not established by these results. Existing release
gates remain described in [first-release status](first-release-status.md).
