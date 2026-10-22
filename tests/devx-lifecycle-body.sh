#!/bin/bash
set -euo pipefail
test "${FACTORYBOX_DEVX_LIFECYCLE:-}" = 1
preparation_start=$(date +%s%N)
python3 /observe-sysctls.py before
if test "${FACTORYBOX_DEVX_PROJECTION:-}" = 1; then
  status=0
  /devx-lifecycle.test -test.v -test.timeout=30s -test.run '^TestNativeIDEProjectionNamespace$' || status=$?
  python3 /observe-sysctls.py after || status=1
  exit "$status"
fi
mkdir -p /out /var/lib/sysbox/devx/bundle/lower
for component in sysbox-runc sysbox-fs sysbox-mgr criu; do
  cp -- "/component-input/$component" "/out/$component"
  chmod 0755 "/out/$component"
done
tar --blocking-factor=2048 --numeric-owner --same-owner --xattrs --acls -xpf /runtime-rootfs.tar \
  -C /var/lib/sysbox/devx/bundle/lower
root=/var/lib/sysbox/devx/bundle/lower/opt/factorybox
mkdir -p "$root"
mkdir -p "$root/bin"
cp -a /example "$root/example"
cp /devx-sandbox.sh "$root/sandbox.sh"
if test "${FACTORYBOX_DEVX_MINIMAL:-}" = 1; then
  cp /minimal-sandbox.sh "$root/sandbox.sh"
  cp /minimal-actor "$root/minimal-actor"
  chmod 0755 "$root/minimal-actor"
fi
cp /devx-devcontainer.sh "$root/bin/devcontainer"
cp /devx-actor /devx-registry /project-images.tar /project-images.json "$root/"
chmod 0755 "$root/devx-actor" "$root/devx-registry"
chmod 0755 "$root/bin/devcontainer"
if test "${FACTORYBOX_DEVX_OPENCODE:-}" = 1; then
  cp /opencode-probe.cjs "$root/opencode-probe.cjs"
fi
if test "${FACTORYBOX_DEVX_OPENCODE_FLOW:-}" = 1; then
  cp -a /opencode-flow "$root/opencode-flow"
fi
# Factorybox captures complete manager volumes; no outer-rootfs export is needed.
/out/sysbox-mgr --disable-shiftfs --disable-inner-image-preload --disable-binfmt-misc --data-root /var/lib/sysbox/manager \
  --log /output/sysbox-mgr.log > /output/sysbox-mgr-stdio.log 2>&1 &
manager=$!
/out/sysbox-fs --mountpoint /var/lib/sysbox/fs --log /output/sysbox-fs.log \
  > /output/sysbox-fs-stdio.log 2>&1 &
filesystem=$!
cleanup() {
  status=$?
  kill "$manager" "$filesystem" 2>/dev/null || true
  wait "$manager" "$filesystem" 2>/dev/null || true
  python3 /observe-sysctls.py after || status=1
  exit "$status"
}
trap cleanup EXIT
/bin/bash /wait-sysbox.sh "$manager" "$filesystem" /output
python3 - "$preparation_start" "$(date +%s%N)" <<'PY'
import json, pathlib, sys
pathlib.Path("/output/preparation.json").write_text(json.dumps({
    "seconds": (int(sys.argv[2]) - int(sys.argv[1])) / 1e9,
    "scope": "Unpack the frozen runtime rootfs, inject test inputs and start Sysbox daemons. "
             "This precedes the prepared-start first-use observation."
}, indent=2) + "\n")
PY
status=0
test_pattern='^TestNativeDeveloperLifecycle$'
if test "${FACTORYBOX_DEVX_MINIMAL:-}" = 1; then
  test_pattern='^TestNativeMinimalLifecycle$'
fi
case "${FACTORYBOX_DEVX_CRIU_FAULT:-}" in
  root|dockerd) test_pattern='^TestNativeDockerCaptureInterruption$' ;;
esac
/devx-lifecycle.test -test.v -test.timeout=390s -test.run "$test_pattern" || status=$?
find /output -maxdepth 1 -type f -exec chown --reference=/output -- {} +
if test "${FACTORYBOX_DEVX_THEIA:-}" = 1; then
  printf '%s\n' "$status" > /output/theia-lifecycle-complete.partial
  mv /output/theia-lifecycle-complete.partial /output/theia-lifecycle-complete
  for attempt in $(seq 1 300); do
    if test -f /output/theia-host-finished; then break; fi
    sleep 0.1
  done
  test -f /output/theia-host-finished || status=1
fi
exit "$status"
