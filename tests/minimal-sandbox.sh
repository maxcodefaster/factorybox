#!/bin/sh
set -eu
test "${FACTORYBOX_DEVX_SANDBOX:-}" = 1
mkdir -p /workspace/project /run/private-docker
cp -a /opt/factorybox/example/. /workspace/project/
cp /opt/factorybox/minimal-actor /workspace/project/.minimal-actor
/opt/factorybox/devx-registry --archive=/opt/factorybox/project-images.tar \
  --pins=/opt/factorybox/project-images.json > /var/log/factorybox-registry.log 2>&1 &
registry=$!
for attempt in $(seq 1 100); do
  if wget -q -O /dev/null http://127.0.0.1:5000/v2/ 2>/dev/null; then break; fi
  kill -0 "$registry"
  sleep 0.1
done
printf '{}\n' > /run/daemon.json
dockerd --config-file=/run/daemon.json --host=unix:///var/run/docker.sock \
  --data-root=/var/lib/docker --exec-root=/run/private-docker --storage-driver=vfs \
  --pidfile=/run/private-docker.pid --iptables=false --ip6tables=false \
  --ip-forward=false --ip-masq=false --bridge=none --registry-mirror=http://127.0.0.1:5000 \
  > /var/log/factorybox-dockerd.log 2>&1 &
daemon=$!
for attempt in $(seq 1 200); do
  if docker info >/dev/null 2>&1; then break; fi
  kill -0 "$daemon"
  sleep 0.1
done
# Execute the real CLI with the exact example, without a Connector or IDE.
devcontainer up --workspace-folder /workspace/project \
  --config /workspace/project/.devcontainer/devcontainer.json \
  --log-format json > /var/log/minimal-up.json
container=$(node -e 'const r=JSON.parse(require("node:fs").readFileSync("/var/log/minimal-up.json"));if(r.outcome!=="success"||!r.containerId)throw new Error("missing Dev Container identity");process.stdout.write(r.containerId)')
image=$(node -e 'process.stdout.write(JSON.parse(require("node:fs").readFileSync("/workspace/project/.devcontainer/devcontainer.json")).image)')
docker image inspect "$image" > /workspace/project/.minimal-image.json
devcontainer exec --workspace-folder /workspace/project --container-id "$container" \
  /bin/sh -ec 'setsid /workspace/.minimal-actor </dev/null >/workspace/.minimal-actor.log 2>&1 &'
# Capture a running service after the setup client has completed. An attached
# cross-PID-namespace exec session is outside this reference consumer profile.
printf 'ready\n' > /workspace/project/.minimal-exec-complete
wait "$daemon"
