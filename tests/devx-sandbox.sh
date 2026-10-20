#!/bin/sh
# Runs wholly inside the owned Sysbox user/network/PID namespaces.
set -eu
test "${FACTORYBOX_DEVX_SANDBOX:-}" = 1
mkdir -p /workspace/project /run/private-docker
cp -a /opt/factorybox/example/. /workspace/project/
git -C /workspace/project init -q
git -C /workspace/project config factory.repositoryIdentity factorybox-example
git -C /workspace/project remote add origin file:///opt/factorybox/example.git
git -C /workspace/project add .
git -C /workspace/project -c user.name=Factorybox -c user.email=fixture@example.invalid commit -qm 'Native fixture'
chown -R 1000:1000 /workspace/project
touch /run/factorybox-workspace-ready
if test "${FACTORYBOX_DEVX_OPENCODE:-}" = 1; then
  previous_umask=$(umask)
  umask 077
  OPENCODE_PASSWORD=$(od -An -N32 -tx1 /dev/urandom | tr -d ' \n')
  export OPENCODE_PASSWORD
  printf '%s' "$OPENCODE_PASSWORD" > /run/fixture-opencode-password
  /bin/sh /opt/factorybox/example/opencode.sh /workspace/project \
    > /var/log/factorybox-opencode.log 2>&1 &
  printf '%s\n' "$!" > /run/fixture-opencode.pid
  umask "$previous_umask"
fi
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
  --data-root=/var/lib/docker --exec-root=/run/private-docker \
  --storage-driver=vfs --pidfile=/run/private-docker.pid \
  --iptables=false --ip6tables=false --ip-forward=false --ip-masq=false \
  --registry-mirror=http://127.0.0.1:5000 > /var/log/factorybox-dockerd.log 2>&1 &
daemon=$!
for attempt in $(seq 1 200); do
  if docker info >/dev/null 2>&1; then break; fi
  kill -0 "$daemon"
  sleep 0.1
done
docker info > /var/log/factorybox-docker-info.txt
# The real Connector prepares and validates its images through this mirror.
exec /opt/factorybox/devx-actor
