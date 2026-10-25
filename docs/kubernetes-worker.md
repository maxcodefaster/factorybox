# Kubernetes consumer admission

The runtime now supplies an optional HTTP readiness probe and exact store/epoch
guards for remote CLI calls. It retains its standalone Go lifecycle, pinned
components, private bounded store and initial per-customer/trust-domain worker
boundary. There is no Kubernetes client or IDE dependency in the runtime.

The first consumer is the private
[Open Software Factory repository](https://github.com/maxcodefaster/open-software-factory).
Its `factorybox:worker` operator command renders a dedicated Namespace, complete
RWO store PVC and root PID 1 Pod with a 3 CPU / 3 GiB reservation and limit.
It verifies node profile, resource ownership, UIDs and live runtime identity.
HTTP probes avoid creating processes during the worker's initial cgroup
delegation. A lifecycle exec pins the expected store and epoch inside Factorybox,
so an earlier Kubernetes observation cannot authorize a replacement worker.

The full store is persistent. Its bounded filesystem, outer image/lease,
Sysbox manager state, epochs and operation records remain together. The consumer
does not place `/var/lib/sysbox` on `emptyDir`, expose a Docker host socket or
give the worker a ServiceAccount token. Its Pod uses `restartPolicy: Never`.
Worker replacement and retained recovery require their separate exact protocol.

Cloud providers use the same manifest and configurable StorageClass. This does
not broaden execution admission: the current AMD64 build still accepts only
its linked kernel, 4096-byte pages and the tested native facilities. No new
kernel, VM, host runtime installation or global sysctl change is included.

## Verified scope

The unselected worker build is
`factorybox-build-worker-2afce8f6f3df48848b61e4d02ffb37b1`.
Its package SHA-256 is
`e46f97ac4963622366a88a5ab6a6374c39926b50ce22b17adc18bfbc6263eaf6`.
It reuses the selected release's exact Sysbox and CRIU binaries. It is not a
replacement qualification for the selected 6016/829f release package.

The owned native proof is
`artifacts/factorybox-kubernetes-worker-cae1f73b156e485cba9bc664906d495b/result.json`.
It executes the actual Factory TypeScript adapter against the delivered worker
CLI on the current native Linux host. Kubernetes observations are fixtures;
there was no connected API server, kubelet or CSI acceptance run.

The complete proof confirms Start and useful work, mutation of private RAM,
Pause with zero source anonymous memory and zero resident snapshot pages,
Resume retaining the same RAM/file state, exact completed Resume replay without
a second process and Delete. The HTTP probe refuses missing live readiness;
changed store/epoch commands fail before their operation. The worker drains
cleanly, exact owned resources are removed and global sysctls are unchanged.
This is one contract run, not a latency distribution or production qualification.

An earlier complete adapter workflow in
`artifacts/factorybox-kubernetes-worker-b173800f212e4a17b43b1b6dcbbea9e9/result.json`
remains failed because live consumer source changed during its evidence window.
The runner now executes an immutable consumer source copy. The passing result
does not overwrite that failed run. The narrower HTTP/identity proof is retained
in `artifacts/factorybox-kubernetes-worker-3d777424a36a442f88a8abda81de84af/`.

## Product Workspace boundary

Factory's normal Workspace Start, Source service, Connector and reserve
controller still use their existing Envbox path. The new operator command
admits administrator-prepared private OCI bundles. It does not prepare Project
images, switch the product or add a second pool controller.

The product Source service and compute currently share canonical source inodes.
Editor saves must remain available while execution is paused. The unselected
shared-source candidate below admits one explicit project bind and keeps it
outside private snapshots. Its root directory and mount retain their admitted
identity; the editor may change file contents and replace files atomically.

The candidate retains the original open regular files, mappings and notification
descriptions in worker PID 1. The ordered CRIU patch restores those objects and
preserves queued source inotify events. A running sandbox retains one descriptor
generation until its next committed Pause, so repeated captures can identify
files whose original mount namespace has exited. Independent Fork, import and
recovery of a shared-source snapshot are refused. The live descriptions cannot
survive worker exit.

The native contract proofs cover accepted editor changes, open and mapped file
replacement, unchanged private RAM, project-root cwd and directory access,
queued and future inotify events, and three consecutive Pause/Resume cycles.
The alias fixture repeats the contract in a nested mount namespace at
`/workspace/project`; it is not a Docker or Factory application test.

The root-directory alias proof uses the exact delivered worker
`factorybox-build-worker-e2a0ea05e41a4f0b9e756aa26870cf98`, package SHA-256
`5ce4ee5821580a6e3d1450672db4a47ab07ca94239f7ce4533201dcc4a646453`:
`artifacts/factorybox-shared-source-40b35e2acb65487ca6a78fffd7cee25b/result.json`.
It does not qualify later source changes, a full application or a latency
distribution.

The subsequent directory contract also retains cwd, root and open directory
identities. Worker PID 1 resolves each current directory beneath the pinned
canonical project root without following symlinks, then the CRIU restorer opens
that exact inode in the fresh mount namespace. This preserves a usable cwd
after an editor renames its directory. Deleted directories, moves outside the
admitted source and replacement inodes are refused before the Resume transition.
The restorer repeats path confinement and inode checks at the actual open.

`artifacts/factorybox-shared-source-7d95d08617ec47b7a50643d490fd62df/result.json`
demonstrates three cycles with cwd and an open directory FD in
`/workspace/project/folder`, renamed to `/workspace/project/renamed` during the
second pause while the held file is replaced atomically. Queued events, RAM,
old open file/mapping and new canonical source survive. This diagnostic proof
uses frozen current Go sources and the independently built CRIU candidate
`factorybox-build-criu-retained-617ed4b2eba14dc180d5b4d4b4d78537`.
It is not a qualification of a newly delivered worker image.

The shared-source annotation is a JSON object with only `source` and
`destination`, for example:

```json
{"io.factorybox.shared-source":"{\"source\":\"/factory-source/project\",\"destination\":\"/home/coder/project\"}"}
```

The matching writable bind must be present in the OCI spec. No other external
writable bind is admitted. The Factory Source service uses UID/GID 101000;
the demonstrated guest mapping is root 100000 with a 65536-ID range and
application UID/GID 1000. This permits both services to use the same inodes.

The complete Factory environment, network access, Runtime Attempt/Connector
and PostgreSQL reserve integration also remain to be implemented and proved.
Target-cluster admission and additional Linux profiles require their own native
evidence. The existing
[first-release gates](first-release-status.md) remain open and unchanged.

Reproduce the native transport proof from this repository with its pinned tools:

```sh
mise exec -- env FACTORYBOX_DOCKER_CONTEXT=default FACTORYBOX_DOCKER_ELEVATION=sudo python3 tools/kubernetes_worker_probe.py --worker-build factorybox-build-worker-2afce8f6f3df48848b61e4d02ffb37b1 --factory-repo /path/to/open-software-factory --consumer-inputs factorybox-devx-inputs-e67b951171c5481982908b2603d42f82
```

The runner uses `reference.new_artifact`, freezes inputs, enforces the 420-second
lifecycle deadline, owns its container/volume and runs one sandbox at a time.
Its consumer source and results remain in the artifact directory; the runnable
consumer copy is under the Factory repository's `.factory/test-results/`.
