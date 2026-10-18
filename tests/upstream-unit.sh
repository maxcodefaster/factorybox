#!/usr/bin/env bash
# Executed only inside the owned test container. Never run on the host.
set -euo pipefail
test "${FACTORYBOX_UPSTREAM_CONTAINER:-}" = 1
test -d /input/sysbox-runc
test -d /output
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
if test "${FACTORYBOX_UPSTREAM_SUITE:-all}" = libcontainer; then
  set +e
  go tool test2json -t -p github.com/nestybox/sysbox-runc/libcontainer \
    /out/libcontainer.test -test.v -test.timeout=30s \
    > /output/libcontainer.jsonl 2> /output/libcontainer.stderr
  status=$?
  set -e
  printf '%s\n' "$status" > /output/libcontainer.exit-code
  exit "$status"
fi
case "${FACTORYBOX_UPSTREAM_SUITE:-all}" in
  fs) run_suite fs /src/sysbox-fs ''; exit "$failed" ;;
  ipc) run_suite ipc /src/sysbox-ipc ''; exit "$failed" ;;
  idmap) run_suite libs-idMap /src/sysbox-libs/idMap idmapped_mnt; exit "$failed" ;;
esac
mkdir -p /run/factorybox-tests /mnt/scratch /busybox
tar -xzf /busybox-rootfs.tar.gz -C /busybox
. /delegate.sh > /output/cgroup-delegation.txt

if test "${FACTORYBOX_UPSTREAM_SUITE:-all}" = runc; then
  rm -rf /src/sysbox-runc
  cp -a /input/sysbox-runc /src/sysbox-runc
  run_suite runc /src/sysbox-runc 'seccomp apparmor idmapped_mnt netgo osusergo'
  exit "$failed"
fi

if test "${FACTORYBOX_UPSTREAM_SUITE:-all}" = runc-integration; then
  pattern='.'
  test_timeout=3m
  if test -n "${FACTORYBOX_UPSTREAM_TEST:-}"; then
    [[ "$FACTORYBOX_UPSTREAM_TEST" =~ ^Test[A-Za-z0-9_]+$ ]]
    pattern="^${FACTORYBOX_UPSTREAM_TEST}$"
    test_timeout=30s
  fi
  set +e
  go tool test2json -t -p github.com/nestybox/sysbox-runc/libcontainer/integration \
    /out/integration.test -test.v "-test.timeout=$test_timeout" -test.run "$pattern" \
      > /output/runc-integration.jsonl 2> /output/runc-integration.stderr
  status=$?
  set -e
  printf '%s\n' "$status" > /output/runc-integration.exit-code
  exit "$status"
fi

# The fixture has its own Docker daemon, storage and socket. Upstream manager
# tests must never reach the worker's daemon.
dockerd --host=unix:///run/factorybox-tests/docker.sock \
  --data-root=/var/lib/docker --exec-root=/run/factorybox-tests/exec \
  --pidfile=/run/factorybox-tests/docker.pid --storage-driver=overlay2 \
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
if test "${FACTORYBOX_UPSTREAM_SUITE:-all}" = all; then
  test -n "${FACTORYBOX_TEST_ALPINE_IMAGE:-}"
  docker pull "$FACTORYBOX_TEST_ALPINE_IMAGE" > /output/alpine-pull.txt 2>&1
  docker tag "$FACTORYBOX_TEST_ALPINE_IMAGE" alpine:latest
  docker image inspect alpine:latest > /output/alpine-inspect.json
fi
if test "${FACTORYBOX_UPSTREAM_SUITE:-all}" = mgr; then
  run_suite mgr /src/sysbox-mgr idmapped_mnt
  exit "$failed"
fi

rm -rf /src/sysbox-runc
cp -a /input/sysbox-runc /src/sysbox-runc
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
  tags=''
  if test "$(basename "$directory")" = idMap; then tags=idmapped_mnt; fi
  run_suite "libs-$(basename "$directory")" "$directory" "$tags"
done
exit "$failed"
