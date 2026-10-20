#!/bin/sh
# Optional consumer for a workspace whose image supplies the pinned OpenCode 2 bundle.
set -eu
if test "$#" -ne 1; then
  printf '%s\n' 'usage: sh opencode.sh /absolute/workspace' >&2
  exit 64
fi
workspace=$(cd "$1" && pwd -P)
test -d "$workspace/.git"
: "${OPENCODE_PASSWORD:?supply the OpenCode server password}"
umask 077
export OPENCODE_DB="${OPENCODE_DB:-$workspace/.git/factorybox-opencode.db}"
export FACTORY_OPENCODE_HOST=127.0.0.1
if test -z "${FACTORY_OPENCODE_CONFIG:-}"; then
  FACTORY_OPENCODE_CONFIG='{}'
fi
export FACTORY_OPENCODE_CONFIG
export OPENCODE_DISABLE_MODELS_FETCH=1
cd "$workspace"
exec /opt/factory/opencode/musl/bun /opt/factory/opencode/server/server.mjs
