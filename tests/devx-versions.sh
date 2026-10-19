#!/bin/sh
# No daemon or model request is started by this consumer-version check.
set -eu
docker --version
docker compose version
devcontainer --version
node --version
test -x /usr/local/bin/factory-runtime-connector
set +e
usage=$(/usr/local/bin/factory-runtime-connector 2>&1)
status=$?
set -e
test "$status" = 1
printf '%s\n' "$usage" | grep -F 'usage: factory-runtime-connector <parent|child>'
if test -f /opt/factory/opencode/server/package.json; then
  node -e 'const fs=require("node:fs"); const p=JSON.parse(fs.readFileSync("/opt/factory/opencode/server/package.json","utf8")); console.log(JSON.stringify({openCodeServer:p.name,dependencies:p.dependencies}));'
  /opt/factory/opencode/musl/bun --version
fi
