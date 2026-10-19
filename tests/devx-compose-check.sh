#!/bin/sh
# Component conformance before the full Sysbox/CRIU composition.
set -eu
test "${FACTORYBOX_DEVX_WORKER:-}" = 1
test "$(cat /proc/1/cgroup)" = '0::/factorybox-devx-controller'
mkdir -p /run/private-docker
node /devx-observation.cjs before
cleanup() {
  status=$?
  if test -n "${daemon:-}" && docker info >/dev/null 2>&1; then
    docker ps -a --no-trunc > /output/containers.txt 2>&1 || true
    for id in $(docker ps -aq); do
      docker logs --tail=100 "$id" > "/output/container-$id.log" 2>&1 || true
    done
  fi
  if test -n "${daemon:-}"; then kill "$daemon" 2>/dev/null || true; wait "$daemon" 2>/dev/null || true; fi
  if test -n "${registry:-}"; then kill "$registry" 2>/dev/null || true; wait "$registry" 2>/dev/null || true; fi
  node /devx-observation.cjs after || status=1
  exit "$status"
}
trap cleanup EXIT
/devx-registry --archive=/project-images.tar --pins=/project-images.json > /output/registry.log 2>&1 &
registry=$!
for attempt in $(seq 1 100); do
  if wget -q -O /dev/null http://127.0.0.1:5000/v2/ 2>/dev/null; then break; fi
  kill -0 "$registry"
  sleep 0.1
done
node - <<'JS' > /output/capabilities.json
const fs = require('node:fs');
const status = fs.readFileSync('/proc/self/status', 'utf8');
for (const name of ['CapEff', 'CapPrm', 'CapBnd']) {
  const value = BigInt('0x' + status.match(new RegExp('^' + name + ':\\s+(\\w+)', 'm'))[1]);
  if ([16n,17n,22n,25n,34n].some(bit => value & (1n << bit))) throw new Error('global mutation capability retained');
}
console.log(JSON.stringify({ checked: ['CapEff', 'CapPrm', 'CapBnd'], globalMutationBitsAbsent: [16,17,22,25,34] }));
JS
printf '{}\n' > /tmp/daemon.json
dockerd --config-file=/tmp/daemon.json --host=unix:///run/private-docker.sock \
  --data-root=/var/lib/docker --exec-root=/run/private-docker \
  --pidfile=/run/private-docker.pid --iptables=false --ip6tables=false \
  --ip-forward=false --ip-masq=false --bridge=none \
  --registry-mirror=http://127.0.0.1:5000 > /output/dockerd.log 2>&1 &
daemon=$!
export DOCKER_HOST=unix:///run/private-docker.sock
for attempt in $(seq 1 150); do
  if docker info >/dev/null 2>&1; then break; fi
  kill -0 "$daemon"
  sleep 0.1
done
docker info > /output/docker-info.txt
node -e 'for(const pin of JSON.parse(require("node:fs").readFileSync("/project-images.json"))) require("node:child_process").execFileSync("docker",["pull","--platform=linux/arm64",pin.image],{stdio:"inherit"})' > /output/image-pull.txt 2>&1
node - <<'JS'
const fs = require('node:fs');
const cp = require('node:child_process');
const pins = JSON.parse(fs.readFileSync('/project-images.json', 'utf8'));
const observed = pins.map(pin => {
  const data = JSON.parse(cp.execFileSync('docker', ['image', 'inspect', pin.image]))[0];
  if (!data.RepoDigests.includes(pin.canonical_image) || data.Architecture !== 'arm64' || data.Os !== 'linux') {
    throw new Error('offline project image lost its exact digest or platform');
  }
  return { image: pin.image, id: data.Id, repoDigests: data.RepoDigests };
});
fs.writeFileSync('/output/offline-images.json', JSON.stringify(observed, null, 2) + '\n');
JS
mkdir -p /work/project
cp -a /example/. /work/project/
chown -R 1000:1000 /work/project
devcontainer read-configuration --workspace-folder /work/project \
  --config /work/project/.devcontainer/devcontainer.json --include-merged-configuration \
  --log-format json > /output/configuration.json 2> /output/configuration.log
devcontainer up --workspace-folder /work/project \
  --config /work/project/.devcontainer/devcontainer.json \
  --id-label factory.dev/runtime-attempt=factorybox-example \
  --id-label factory.dev/runtime-generation=1 \
  --log-format json > /output/up.json 2> /output/up.log
container=$(node -e 'const r=JSON.parse(require("node:fs").readFileSync("/output/up.json")); if(r.outcome!=="success"||!r.containerId)throw new Error("missing Dev Container identity");process.stdout.write(r.containerId)')
docker inspect "$container" > /output/primary-container.json
docker exec -d --user node "$container" node /workspace/server.mjs
devcontainer exec --workspace-folder /work/project --container-id "$container" \
  node --input-type=module -e '
    let result;
    for (let i=0; i<100; i++) {
      try { const response=await fetch("http://127.0.0.1:3000/state"); if(response.ok){result=await response.json();break;} } catch {}
      await new Promise(done=>setTimeout(done,100));
    }
    if(!result || result.counter!==0 || result.marker!=="source" || result.hook!=="ready" || result.dependency!=="ready") throw new Error("real Dev Container app/Compose/hook proof failed");
    console.log(JSON.stringify(result));' \
  > /output/app-state.json 2> /output/exec.log
