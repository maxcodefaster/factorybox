#!/bin/bash
set -euo pipefail
mkdir /busybox
tar -xzf /busybox-rootfs.tar.gz -C /busybox
if [[ "${FACTORYBOX_NATIVE_TIMER_DETACH:-0}" == 1 ]]; then
  # Use the delivered image's root-owned runtime, not a host bind whose UID
  # depends on Docker's file-sharing implementation. Keep the original test's
  # /out path and its root-ownership and exact-hash checks.
  install -D -m 0755 /opt/factorybox/components/sysbox-runc /out/sysbox-runc
  printf '%s  /out/sysbox-runc\n' "$FACTORYBOX_RUNTIME_SHA256" | sha256sum -c -
fi
exec /probe.test -test.v -test.run="${FACTORYBOX_RETIREMENT_TEST:-^TestNativeCheckpointRetirement}" -test.timeout=400s
