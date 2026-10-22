#!/bin/bash
# Test workload inputs only. The image's actual Go init already owns services.
set -euo pipefail
mkdir -m 0700 /out
for component in sysbox-runc sysbox-mgr sysbox-fs criu; do
  cp -- "/component-input/$component" "/out/$component"
  chmod 0755 "/out/$component"
done
mkdir /busybox
tar -xzf /busybox-rootfs.tar.gz -C /busybox
exec /probe.test -test.v -test.run='^TestWorkerRestartProbeNative$' -test.timeout=360s
