#!/bin/bash
# Native fixture setup only. Service lifetime and cgroup delegation belong to Go.
set -euo pipefail
mkdir -m 0700 /out
for component in sysbox-runc sysbox-mgr sysbox-fs criu; do
  cp -- "/component-input/$component" "/out/$component"
  chmod 0755 "/out/$component"
done
cp /component-manifest.json /out/components.json
chmod 0600 /out/components.json
mkdir /busybox
tar -xzf /busybox-rootfs.tar.gz -C /busybox
exec /factorybox worker --store /var/lib/sysbox/store \
  --store-id "${FACTORYBOX_WORKER_EXPECTED_STORE_ID:-$FACTORYBOX_BASE_WORKER_OWNER}" --components /out --manifest /out/components.json
