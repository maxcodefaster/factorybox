#!/usr/bin/env bash
# Only the bounded, uniquely owned container created by lifecycle_native.py.
set -euo pipefail
test "${FACTORYBOX_NATIVE_LIFECYCLE:-}" = 1
test -d /output
test -d /var/lib/sysbox
test -n "${FACTORYBOX_RUNTIME_SHA256:-}"
if test "${FACTORYBOX_NATIVE_RETAINED_CAPTURE:-}" = 1 || test "${FACTORYBOX_COMPONENT_INPUTS:-}" = 1; then
  mkdir -p /out
  for component in sysbox-runc sysbox-fs sysbox-mgr; do
    cp -- "/component-input/$component" "/out/$component"
    chmod 0755 "/out/$component"
  done
  if test "${FACTORYBOX_NATIVE_RETAINED_CAPTURE:-}" = 1; then
    cp -- /component-input/criu /out/criu
    chmod 0755 /out/criu
  fi
fi
test_pattern='^TestNative'
if test "${FACTORYBOX_NATIVE_CAPTURE_GUARD:-}" = 1; then
  test_pattern='^TestCaptureGuardNative$'
else
mkdir /busybox
tar -xzf /busybox-rootfs.tar.gz -C /busybox
. /delegate.sh > /output/cgroup-delegation.txt
if test "${FACTORYBOX_NATIVE_TEST:-}" != TestNativeEmptyCgroupRetirement; then
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
fi
if test -n "${FACTORYBOX_NATIVE_TEST:-}"; then
  [[ "$FACTORYBOX_NATIVE_TEST" =~ ^TestNative[A-Za-z0-9_]+$ ]]
  test_pattern="^${FACTORYBOX_NATIVE_TEST}$"
fi
if test "${FACTORYBOX_NATIVE_RETAINED_CAPTURE:-}" = 1; then
  /sysboxcapture.test -test.v -test.timeout=30s
  test_pattern='^TestRetainedCaptureNative$'
  if test "${FACTORYBOX_NATIVE_PAUSE:-}" = 1; then
    test_pattern='^TestPauseNative$'
  fi
  if test "${FACTORYBOX_NATIVE_NAMESPACES:-}" = 1; then
    test_pattern='^TestCapture(Namespaces|ChildrenNamespace)Native$'
  fi
  if test "${FACTORYBOX_NATIVE_TIME:-}" = 1; then
    test_pattern='^TestTimeNamespaceForkNative$'
  fi
  if test "${FACTORYBOX_NATIVE_DELEGATION:-}" = 1; then
    test_pattern='^TestDelegatedCgroupForkNative$'
  fi
  if test "${FACTORYBOX_NATIVE_MOUNT_FD:-}" = 1; then
    test_pattern='^TestDetachedMountForkNative$'
  fi
  if test "${FACTORYBOX_NATIVE_INTERRUPTION:-}" = 1; then
    test_pattern='^TestInterruptedCaptureNative$'
  fi
  if test "${FACTORYBOX_NATIVE_FILESYSTEM_ADMISSION:-}" = 1; then
    test_pattern='^TestFilesystemAdmissionNative$'
  fi
fi
set +e
test_timeout=90s
if test "${FACTORYBOX_NATIVE_RETAINED_CAPTURE:-}" = 1; then test_timeout=360s; fi
/lifecycle.test -test.v "-test.timeout=$test_timeout" -test.run "$test_pattern"
status=$?
set -e
# Retain only controller metadata. Never traverse guest roots, manager mounts
# or FUSE files while collecting evidence.
mkdir /output/controller-metadata
index=0
for file in /var/lib/sysbox/factorybox-lifecycle-*/controller*/objects/*/record.json \
            /var/lib/sysbox/factorybox-lifecycle-*/controller*/objects/*/config.json \
            /var/lib/sysbox/factorybox-lifecycle-*/controller*/objects/*/restore-result.json \
            /var/lib/sysbox/factorybox-lifecycle-*/controller*/forks/*/intent.json \
            /var/lib/sysbox/factorybox-lifecycle-*/controller*/forks/*/preparation.json \
            /var/lib/sysbox/factorybox-lifecycle-*/controller*/forks/*/overlay-origin.json \
            /var/lib/sysbox/factorybox-lifecycle-*/controller*/runtime/*/state.json \
            /var/lib/sysbox/factorybox-lifecycle-*/controller*/objects/*/captures/*/admission-rejected.json \
            /var/lib/sysbox/factorybox-lifecycle-*/controller*/objects/*/captures/*/guard/*.json; do
  if test -f "$file"; then
    index=$((index + 1))
    cp -- "$file" "/output/controller-metadata/$index.json"
    printf '%s %s\n' "$index.json" "$file" >> /output/controller-metadata/index.txt
  fi
done
exit "$status"
