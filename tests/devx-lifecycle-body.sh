#!/bin/bash
set -euo pipefail
test "${FACTORYBOX_DEVX_LIFECYCLE:-}" = 1
preparation_start=$(date +%s%N)
python3 /observe-sysctls.py before
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
cp /devx-devcontainer.sh "$root/bin/devcontainer"
cp /devx-actor /devx-registry /project-images.tar /project-images.json "$root/"
chmod 0755 "$root/devx-actor" "$root/devx-registry"
chmod 0755 "$root/bin/devcontainer"
/out/sysbox-mgr --disable-shiftfs --data-root /var/lib/sysbox/manager \
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
/devx-lifecycle.test -test.v -test.timeout=390s -test.run '^TestNativeDeveloperLifecycle$'
