#!/bin/bash
# Test-only profile for the original systemd tmpfs-override assertion.
set -euo pipefail
test "${FACTORYBOX_UPSTREAM_BATS:-}" = 1
test "${FACTORYBOX_SYSTEMD_NETWORK_PROFILE:-}" = 1
test "${DOCKER_HOST:-}" = unix:///run/private-docker.sock
case "${FACTORYBOX_BATS_DOCKER_REAL:-}" in
  /usr/bin/docker|/usr/local/bin/docker) ;;
  *) exit 1 ;;
esac
if test "${1:-}" = run; then
  for argument in "$@"; do
    case "$argument" in
      ghcr.io/nestybox/ubuntu-focal-systemd-docker|ghcr.io/nestybox/ubuntu-focal-systemd-docker:latest)
        shift
        exec "$FACTORYBOX_BATS_DOCKER_REAL" run \
          --mount type=bind,source=/systemd-docker-profile.json,target=/etc/docker/daemon.json,readonly "$@"
        ;;
    esac
  done
fi
exec "$FACTORYBOX_BATS_DOCKER_REAL" "$@"
