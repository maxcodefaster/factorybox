#!/usr/bin/env bash
# Only the private PID/cgroup/network fixture created by upstream_bats.py.
set -euo pipefail
test "${FACTORYBOX_UPSTREAM_BATS:-}" = 1
test "$(cat /proc/1/cgroup)" = '0::/factorybox-unit-controller'
bats --version > /output/bats-version.txt
cp /opt/factorybox-bats-packages.tsv /output/packages.tsv
uname -a > /output/kernel.txt
python3 - <<'PY'
import json, pathlib, platform
pathlib.Path("/output/platform.json").write_text(json.dumps({
    "kernel": platform.release(), "machine": platform.machine(),
    "kernel_nodes": {name: pathlib.Path(name).exists() for name in ["/proc/kcore", "/proc/kmsg", "/proc/config.gz"]},
}, indent=2) + "\n")
PY
python3 /observe-sysctls.py before
python3 - <<'PY'
import json, pathlib
fields = dict(line.split(":", 1) for line in pathlib.Path("/proc/self/status").read_text().splitlines()
              if ":" in line)
for name in ["CapEff", "CapPrm", "CapBnd"]:
    # SYS_MODULE, SYS_RAWIO, SYS_BOOT, SYS_TIME, SYSLOG.
    assert int(fields[name].strip(), 16) & sum(1 << n for n in [16, 17, 22, 25, 34]) == 0, name
print(json.dumps({name: fields[name].strip() for name in ["CapEff", "CapPrm", "CapBnd"]}))
PY
test -r /source.tar
printf '%s  /source.tar\n' "$FACTORYBOX_BATS_SOURCE_SHA256" | sha256sum --check --status
mkdir /tests
tar -xf /source.tar -C /tests
for component in sysbox-runc sysbox-fs sysbox-mgr; do
  cp -- "/components/$component" "/usr/local/bin/$component"
  chmod 0755 "/usr/local/bin/$component"
done
mkdir -p /var/lib/sysbox /mnt/scratch/work
cleanup() {
  # These process names belong only to this fixture's private PID namespace.
  pkill -TERM -x dockerd || true
  pkill -TERM -x sysbox-mgr || true
  pkill -TERM -x sysbox-fs || true
  wait || true
  for name in sysbox-mgr sysbox-fs dockerd; do
    if test -f "/var/log/$name.log"; then cp -- "/var/log/$name.log" "/output/$name.log"; fi
  done
  python3 /observe-sysctls.py after
}
trap cleanup EXIT
sysbox-mgr --log /var/log/sysbox-mgr.log > /output/manager-stdio.log 2>&1 &
sysbox-fs --ignore-handler-errors --log /var/log/sysbox-fs.log > /output/fs-stdio.log 2>&1 &
for attempt in $(seq 1 100); do
  if test -S /run/sysbox/sysmgr.sock && test -S /run/sysbox/sysfs-seccomp.sock; then break; fi
  sleep 0.1
done
test -S /run/sysbox/sysmgr.sock
test -S /run/sysbox/sysfs-seccomp.sock
export TARGET_ARCH=arm64
suite="${FACTORYBOX_BATS_SUITE:?}"
case "$suite" in
  lifecycle) selected=/tests/tests/lifecycle/basic.bats ;;
  systemd)
    dockerd --host=unix:///run/private-docker.sock \
      --data-root=/var/lib/docker --exec-root=/run/private-docker \
      --pidfile=/run/private-docker.pid --storage-driver=overlay2 \
      --add-runtime=sysbox-runc=/usr/local/bin/sysbox-runc \
      --iptables=false --ip6tables=false --ip-forward=false --ip-masq=false \
      --bridge=none > /var/log/dockerd.log 2>&1 &
    export DOCKER_HOST=unix:///run/private-docker.sock
    for attempt in $(seq 1 100); do
      if docker info >/dev/null 2>&1; then break; fi
      sleep 0.1
    done
    docker info > /output/private-docker-info.txt
    # The host runner provides exact archived upstream images. Tests use their
    # original names only within this new daemon, with no worker Docker socket.
    docker load --input /images.tar > /output/image-load.txt
    docker image inspect ghcr.io/nestybox/ubuntu-focal-systemd:latest \
      ghcr.io/nestybox/ubuntu-focal-systemd-docker:latest > /output/test-images.json
    python3 - <<'PY'
import json, pathlib
expected = json.loads(pathlib.Path("/expected-images.json").read_text())
loaded = json.loads(pathlib.Path("/output/test-images.json").read_text())
assert len(expected) == len(loaded) == 2
for wanted, actual in zip(expected, loaded):
    assert wanted["id"] == actual["Id"], "loaded image configuration changed"
    assert wanted["alias"] in actual["RepoTags"], "upstream alias changed"
    assert actual["Architecture"] == "arm64" and actual["Os"] == "linux"
PY
    selected=/tests/tests/apps/l1/systemd.bats ;;
  *) exit 1 ;;
esac
arguments=(--tap)
if test -n "${FACTORYBOX_BATS_TEST:-}"; then
  arguments+=(--filter "$FACTORYBOX_BATS_TEST")
fi
set +e
timeout --signal=TERM --kill-after=5s 330s bats "${arguments[@]}" "$selected" \
  > /output/tests.tap 2>&1
status=$?
set -e
printf '%s\n' "$status" > /output/tests.exit-code
exit "$status"
