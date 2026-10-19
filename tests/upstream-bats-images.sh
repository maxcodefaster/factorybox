#!/usr/bin/env bash
# Acquire public pinned test images only in this owned daemon and data volume.
set -euo pipefail
test "${FACTORYBOX_BATS_IMAGES:-}" = 1
test "$(cat /proc/1/cgroup)" = '0::/'
cleanup() {
  pkill -TERM -x dockerd || true
  wait || true
}
trap cleanup EXIT
dockerd --host=unix:///run/acquisition-docker.sock \
  --data-root=/var/lib/docker --exec-root=/run/acquisition-docker \
  --pidfile=/run/acquisition-docker.pid --storage-driver=overlay2 \
  --iptables=false --ip6tables=false --ip-forward=false --ip-masq=false \
  --bridge=none > /output/dockerd.log 2>&1 &
export DOCKER_HOST=unix:///run/acquisition-docker.sock
for attempt in $(seq 1 100); do
  if docker info >/dev/null 2>&1; then break; fi
  sleep 0.1
done
docker info > /output/docker-info.txt
python3 - <<'PY'
import json, os, pathlib, re, subprocess
fields = dict(line.split(":", 1) for line in pathlib.Path("/proc/self/status").read_text().splitlines()
              if ":" in line)
assert int(fields["CapEff"].strip(), 16) & sum(1 << n for n in [16, 17, 22, 25, 34]) == 0
pins = json.loads(pathlib.Path("/pins.json").read_text())
assert pins["schema"] == 1 and pins["platform"] == "linux/arm64" and len(pins["images"]) == 2
observed = []
for number, row in enumerate(pins["images"]):
    alias, image = row["alias"], row["image"]
    assert re.fullmatch(r"ghcr\.io/nestybox/ubuntu-focal-systemd(?:-docker)?@sha256:[0-9a-f]{64}", image)
    assert alias == image.split("@")[0] + ":latest"
    print(json.dumps({"pulling": image}), flush=True)
    with pathlib.Path("/output/pull-%d.log" % number).open("xb") as log:
        subprocess.run(["docker", "pull", "--platform=linux/arm64", image],
                       stdout=log, stderr=subprocess.STDOUT, check=True, timeout=120)
    inspected = json.loads(subprocess.check_output(["docker", "image", "inspect", image], timeout=15))[0]
    assert inspected["Architecture"] == "arm64" and inspected["Os"] == "linux"
    assert image in inspected["RepoDigests"]
    subprocess.run(["docker", "tag", image, alias], check=True, timeout=15)
    observed.append({"alias": alias, "image": image, "id": inspected["Id"]})
subprocess.run(["docker", "image", "save", "--output", "/output/images.tar",
                *[row["alias"] for row in observed]], check=True, timeout=120)
pathlib.Path("/output/images.json").write_text(json.dumps(observed, indent=2) + "\n")
PY
