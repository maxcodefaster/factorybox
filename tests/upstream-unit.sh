#!/usr/bin/env bash
# Executed only inside the owned test container. Never run on the host.
set -euo pipefail
test "${FACTORYBOX_UPSTREAM_CONTAINER:-}" = 1
test -d /input/sysbox-runc
test -d /output
mkdir -p /run/factorybox-tests /mnt/scratch /busybox
tar -xzf /busybox-rootfs.tar.gz -C /busybox

# The fixture has its own Docker daemon, storage and socket. Upstream manager
# tests must never reach the worker's daemon.
dockerd --host=unix:///run/factorybox-tests/docker.sock \
  --data-root=/tmp/factorybox-tests-docker --exec-root=/run/factorybox-tests/exec \
  --pidfile=/run/factorybox-tests/docker.pid --storage-driver=vfs \
  --iptables=false --ip6tables=false --ip-forward=false --ip-masq=false \
  --bridge=none > /output/docker.log 2>&1 &
docker_pid=$!
trap 'kill "$docker_pid" 2>/dev/null || true; wait "$docker_pid" 2>/dev/null || true' EXIT
export DOCKER_HOST=unix:///run/factorybox-tests/docker.sock
for attempt in $(seq 1 80); do
  if docker info > /dev/null 2>&1; then break; fi
  if ! kill -0 "$docker_pid" 2>/dev/null; then exit 1; fi
  sleep 0.1
done
docker info > /output/docker-info.txt

rm -rf /src/sysbox-runc
cp -a /input/sysbox-runc /src/sysbox-runc
failed=0
run_suite() {
  local name=$1 directory=$2 tags=$3 status
  set +e
  (cd "$directory" && go test -mod=readonly -buildvcs=false \
    -timeout=3m -p=2 -count=1 -json -tags "$tags" ./...) \
      > "/output/$name.jsonl" 2> "/output/$name.stderr"
  status=$?
  set -e
  printf '%s\n' "$status" > "/output/$name.exit-code"
  if test "$status" -ne 0; then failed=1; fi
}
run_suite runc /src/sysbox-runc 'seccomp apparmor idmapped_mnt netgo osusergo'

# The fs/mgr dependencies use the original nestybox module identity. Keep the
# same source assembly as the component build, including the pinned IPC change.
rm -rf /src/sysbox-runc
mkdir /src/sysbox-runc
tar -xzf /tmp/runc.tar.gz --strip-components=1 -C /src/sysbox-runc
run_suite fs /src/sysbox-fs ''
run_suite mgr /src/sysbox-mgr idmapped_mnt
run_suite ipc /src/sysbox-ipc ''
for module in /src/sysbox-libs/*/go.mod; do
  directory=$(dirname "$module")
  run_suite "libs-$(basename "$directory")" "$directory" ''
done
exit "$failed"
