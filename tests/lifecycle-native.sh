#!/usr/bin/env bash
# Only the bounded, uniquely owned container created by lifecycle_native.py.
set -euo pipefail
test "${FACTORYBOX_NATIVE_LIFECYCLE:-}" = 1
test -d /output
test -d /var/lib/sysbox
test -n "${FACTORYBOX_RUNTIME_SHA256:-}"
if test "${FACTORYBOX_NATIVE_RETAINED_CAPTURE:-}" = 1; then
  mkdir -p /out
  for component in sysbox-runc sysbox-fs sysbox-mgr criu; do
    cp -- "/component-input/$component" "/out/$component"
    chmod 0755 "/out/$component"
  done
fi
test_pattern='^TestNative'
if test "${FACTORYBOX_NATIVE_CAPTURE_GUARD:-}" = 1; then
  test_pattern='^TestCaptureGuardNative$'
else
mkdir /busybox
tar -xzf /busybox-rootfs.tar.gz -C /busybox
. /delegate.sh > /output/cgroup-delegation.txt
/out/sysbox-mgr --disable-shiftfs --data-root /var/lib/sysbox/manager \
  --log /output/sysbox-mgr.log > /output/sysbox-mgr-stdio.log 2>&1 &
manager=$!
/out/sysbox-fs --mountpoint /var/lib/sysbox/fs --log /output/sysbox-fs.log \
  > /output/sysbox-fs-stdio.log 2>&1 &
filesystem=$!
trap 'kill "$manager" "$filesystem" 2>/dev/null || true; wait "$manager" "$filesystem" 2>/dev/null || true' EXIT
for attempt in $(seq 1 150); do
  if test -S /run/sysbox/sysfs-seccomp.sock && test -S /run/sysbox/sysmgr.sock; then break; fi
  kill -0 "$manager" "$filesystem"
  sleep 0.1
done
test -S /run/sysbox/sysfs-seccomp.sock
test -S /run/sysbox/sysmgr.sock
fi
if test -n "${FACTORYBOX_NATIVE_TEST:-}"; then
  [[ "$FACTORYBOX_NATIVE_TEST" =~ ^TestNative[A-Za-z0-9_]+$ ]]
  test_pattern="^${FACTORYBOX_NATIVE_TEST}$"
fi
if test "${FACTORYBOX_NATIVE_RETAINED_CAPTURE:-}" = 1; then
  /sysboxcapture.test -test.v -test.timeout=10s
  test_pattern='^TestRetainedCaptureNative$'
fi
set +e
/lifecycle.test -test.v -test.timeout=90s -test.run "$test_pattern"
status=$?
set -e
# Retain only controller metadata. Never traverse guest roots, manager mounts
# or FUSE files while collecting evidence.
mkdir /output/controller-metadata
index=0
for file in /var/lib/sysbox/factorybox-lifecycle-*/controller*/objects/*/record.json \
            /var/lib/sysbox/factorybox-lifecycle-*/controller*/objects/*/config.json \
            /var/lib/sysbox/factorybox-lifecycle-*/controller*/runtime/*/state.json \
            /var/lib/sysbox/factorybox-lifecycle-*/controller*/objects/*/captures/*/guard/*.json; do
  if test -f "$file"; then
    index=$((index + 1))
    cp -- "$file" "/output/controller-metadata/$index.json"
    printf '%s %s\n' "$index.json" "$file" >> /output/controller-metadata/index.txt
  fi
done
exit "$status"
