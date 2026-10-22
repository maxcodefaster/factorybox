#!/bin/bash
# Consumer fixture inputs only. The delivered image's Go init owns services.
set -euo pipefail
started=$(date +%s%N)
python3 /observe-sysctls.py before
finish() {
  status=$?
  python3 /observe-sysctls.py after || status=1
  find /output -maxdepth 1 -type f -exec chown --reference=/output -- {} +
  exit "$status"
}
trap finish EXIT
mkdir -m 0700 /out
for component in sysbox-runc sysbox-fs sysbox-mgr criu; do
  cp -- "/component-input/$component" "/out/$component"
  chmod 0755 "/out/$component"
done
if test "${FACTORYBOX_DEVX_BASELINE_RUNTIME:-}" = 1; then
  cp /baseline-criu /out/criu
  chmod 0755 /out/criu
fi
if test "$FACTORYBOX_CONSUMER_WORKER_PHASE" = prepare; then
  mkdir -p /var/lib/sysbox/devx/bundle/lower
  tar --blocking-factor=2048 --numeric-owner --same-owner --xattrs --acls -xpf /runtime-rootfs.tar \
    -C /var/lib/sysbox/devx/bundle/lower
  root=/var/lib/sysbox/devx/bundle/lower/opt/factorybox
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
  if test "${FACTORYBOX_DEVX_OPENCODE_FLOW:-}" = 1; then
    cp /opencode-probe.cjs "$root/opencode-probe.cjs"
    cp -a /opencode-flow "$root/opencode-flow"
  fi
  chmod 0755 "$root/devx-actor" "$root/devx-registry" "$root/bin/devcontainer"
  python3 - "$started" "$(date +%s%N)" <<'PY'
import json,pathlib,sys
pathlib.Path('/output/preparation.json').write_text(json.dumps({
    'seconds':(int(sys.argv[2])-int(sys.argv[1]))/1e9,
    'scope':'Unpack frozen rootfs and inject the unchanged consumer inputs; worker/service startup is recorded separately.'
},indent=2)+'\n')
PY
fi
test_name=TestNativeConsumerWorkerLoss
if test "${FACTORYBOX_DEVX_EXEC_SESSION:-}" = 1; then test_name=TestNativeAttachedExecBoundary; fi
if test "${FACTORYBOX_DEVX_SESSION_TOPOLOGY:-}" = 1; then test_name=TestNativeSessionTopology; fi
if test "${FACTORYBOX_DEVX_WORKER_LIFECYCLE:-}" = 1; then
  test_name=TestNativeDeveloperLifecycle
  if test "${FACTORYBOX_DEVX_MINIMAL:-}" = 1; then test_name=TestNativeMinimalLifecycle; fi
fi
if test "${FACTORYBOX_DEVX_WARM_PREPARATION:-}" = 1; then test_name=TestNativeWarmPreparation; fi
/consumer-worker.test -test.v -test.run="^${test_name}$" -test.timeout=390s
