#!/bin/bash
# Diagnostic fixture only. The production worker supervisor is separate work.
set -euo pipefail
test "${FACTORYBOX_NATIVE_LIFECYCLE:-}" = 1
mkdir -p /out /busybox
for component in sysbox-runc sysbox-mgr sysbox-fs criu; do
  cp -- "/component-input/$component" "/out/$component"
  chmod 0755 "/out/$component"
done
tar -xzf /busybox-rootfs.tar.gz -C /busybox
. /delegate.sh > /output/cgroup-delegation.txt
/out/sysbox-mgr --disable-shiftfs --disable-inner-image-preload --disable-binfmt-misc \
  --data-root /var/lib/sysbox/manager --log /output/sysbox-mgr.log \
  > /output/sysbox-mgr-stdio.log 2>&1 &
manager=$!
/out/sysbox-fs --mountpoint /var/lib/sysbox/fs --log /output/sysbox-fs.log \
  > /output/sysbox-fs-stdio.log 2>&1 &
filesystem=$!
trap 'kill "$manager" "$filesystem" 2>/dev/null || true; wait "$manager" "$filesystem" 2>/dev/null || true' EXIT
/bin/bash /wait-sysbox.sh "$manager" "$filesystem" /output
/probe.test -test.v -test.run='^TestWorkerRestartProbeNative$' -test.timeout=360s
