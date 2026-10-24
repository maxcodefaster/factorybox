# Local worker integration example

This optional administrative client uses the delivered Go worker and its
schema-1 CLI. The existing [minimal Dev Container](../minimal) and
[Factory composition](../factory) remain separate consumers. Read the
[worker contract](../../docs/worker-contract.md),
[current release status](../../docs/first-release-status.md) and
[operating procedure](../../docs/first-release-operations.md) before integration.
The commands below use the bounded-store first-release candidate, whose
production qualification remains false. The older
[pilot profile](../../upstream/amd64-pilot-profile.json) and
[pilot evidence](../../docs/pilot-validation.md) remain historical comparisons.

Set an exact locally built image digest, a fresh store identity and a dedicated
volume name. Preserve the same values across worker replacement. Provision the
volume with its exact `io.factorybox.store-id` label; `workflow.py` rejects
foreign or unlabeled volumes. The Compose file requires an existing external
volume and never pulls or publishes an image.

```sh
export FACTORYBOX_WORKER_IMAGE='YOUR_LOCAL_IMAGE@sha256:EXACT_IMAGE_DIGEST'
export FACTORYBOX_STORE_ID="$(python3 -c 'import uuid; print(uuid.uuid4().hex)')"
export FACTORYBOX_STORE_VOLUME="factorybox-$FACTORYBOX_STORE_ID"
export FACTORYBOX_STORE_BYTES=34359738368
(
  umask 077
  printf 'FACTORYBOX_WORKER_IMAGE=%s\nFACTORYBOX_STORE_ID=%s\nFACTORYBOX_STORE_VOLUME=%s\nFACTORYBOX_STORE_BYTES=%s\n' \
    "$FACTORYBOX_WORKER_IMAGE" "$FACTORYBOX_STORE_ID" "$FACTORYBOX_STORE_VOLUME" \
    "$FACTORYBOX_STORE_BYTES" > factorybox-worker.env
)
docker volume create --label "io.factorybox.store-id=$FACTORYBOX_STORE_ID" "$FACTORYBOX_STORE_VOLUME"
docker compose --env-file factorybox-worker.env --project-name factorybox-demo \
  --file examples/worker/compose.yaml --file examples/worker/compose.bounded.yaml \
  up --detach --pull never worker
```

Use the Docker context and administrative access appropriate to the qualified
host. The client accepts `--context default` and `--sudo`; the example does
not install or register a host runtime.
The explicit environment file also works when administrative access discards
the calling shell's exported variables. The workflow writes its own validated
private copy inside the response directory before issuing Compose commands.

Before the workflow, an administrator places a complete application OCI bundle
under `/var/lib/sysbox/fixed-store/data/store/input/bundle` in the running worker.
All bundle backing must be on that bounded filesystem. The current capture
path requires these private sibling directories: `lower`, `upper`, `work` and
`rootfs`. `rootfs` must be the admitted overlay mounted with exactly those
lower/upper/work paths (the tested path uses `index=off`); `config.json` refers
to that rootfs. Keep the process noninteractive and launch its long-lived
service from the container's init. Supply the required private namespaces,
ID mappings, cgroups and supported mounts. Application dependency installation,
images and readiness belong to the consumer. No arbitrary OCI/mount topology
support is implied. The native acceptance fixtures show the fully exercised
minimal and Factory consumer preparations.

The workflow then performs start, pause/resume, a second committed pause,
complete Compose worker removal/recreation, verified recovery with exact
replay, a new checkpoint, independent fork and exact deletion of both live
objects. It keeps the persistent volume, committed old checkpoint and durable
operation records for inspection. It enforces a 420-second whole-workflow
deadline and uses at most two live sandboxes. Its command responses are an
integration transcript; the acceptance tests separately prove RAM/filesystem
independence and first useful application work.

The client records the two administrator-owned input mounts before Start.
After the complete pause, it requires the same mount identities and releases
only those mounts with ordinary unmount. Their backing and the complete
checkpoint remain. Changed, nested or stacked input mounts stop the workflow.
The worker receives 45 seconds for its own drain; a nonzero exit or OOM stops
replacement and retains the old container/store for inspection.

```sh
mise exec -- python3 examples/worker/workflow.py \
  --project factorybox-demo \
  --bundle /var/lib/sysbox/fixed-store/data/store/input/bundle \
  --bounded-bytes 34359738368 \
  --context default --sudo --output /absolute/new/private-response-directory
```

A failed or uncertain request stops the workflow and retains its exact
responses and resources for diagnosis. Inspect those records before retrying;
the client does not select another recovery ID to evade a partial-operation
refusal. The output contains the fenced source epoch, owner, checkpoint hash,
old/new worker identities and all command results. Restrict access to it.

The new worker remains available after a successful run. Stop this dedicated
Compose service when finished. Remove the exclusively owned volume only when
its retained snapshots are intentionally no longer needed.

## Bounded-store candidate

The commands above combine `compose.yaml` with `compose.bounded.yaml`. Keep the
same complete external volume, store ID and exact fixed image size across
worker replacement. `FACTORYBOX_STORE_BYTES=34359738368` selects 32 GiB. This is the tested Factory
capacity, not a promise that every workload or recovery generation fits.
The worker reports capacity and refuses writes that cannot be admitted.

```sh
docker compose --env-file factorybox-worker.env --project-name factorybox-demo \
  --file examples/worker/compose.yaml --file examples/worker/compose.bounded.yaml \
  up --detach --pull never worker
```

Prepare the private bundle inside
`/var/lib/sysbox/fixed-store/data/store/input/bundle`, on the bounded filesystem.
Call the workflow with that bundle and `--bounded-bytes 34359738368`. It uses
both Compose files for every stop/replacement and verifies the actual bounded
store identity on both workers. The fixed image and its ownership metadata,
controller epochs and all backing remain one persistent storage unit.
The [first-release operating procedure](../../docs/first-release-operations.md)
tracks the required acceptance separately from the historical pilot.

The bounded example passes with the current independently reproduced
`factorybox-build-worker-6016296965364344a8ef9eede420dbc8` package and a 32-GiB store in
`factorybox-worker-example-83994d5e4825404fb3eb121c0afda237`.
It checks unchanged mounts after a refused identity request, both clean
worker stops, exact recovery replay and an independent descendant. The
32-MiB actor uses the full delivered rootfs input. The earlier 4-GiB attempt
fills the store during recovery cloning and remains failed with its complete
archive retained. Current complete Minimal/Factory consumers, all twenty
ordinary trials and all five operating cases are independently closed as
indexed in the operating procedure. Actual external-agent acceptance,
historical cleanup attribution and the final profile remain open.

The older unbounded pilot uses only `compose.yaml`, omits `--bounded-bytes` and
places its input under `/var/lib/sysbox/input`. It does not provide this fixed
store limit and is not the selected first-release deployment.

Historical validation: the previous Compose/workflow client passes with the C9
delivered worker in
`factorybox-worker-example-ef2308c7d09b430595d5581a214e9c06`, including full
worker removal/recreation, recovery/replay, subsequent checkpoint/fork and
deletion. The native driver checks real resource ceilings, exact image,
project/store labels, volume ownership, cleanup and global sentinels.
Its administrator-prepared workload is a separate 32-MiB Go test actor;
the minimal Dev Container and complete Factory/IDE/agent proofs remain distinct.
The first driver invocation lost its Compose values through elevated access;
it started no worker, and its empty owned volume was archived/removed with a
separate reconciliation receipt. Explicit private environment files now avoid
that failure. The installed Compose version is 5.6.0.
