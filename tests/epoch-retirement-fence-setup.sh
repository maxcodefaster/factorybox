#!/bin/bash
set -euo pipefail
case "$1" in Prepare|Recover|Finish) ;; *) exit 2 ;; esac
mkdir /busybox
tar -xzf /busybox-rootfs.tar.gz -C /busybox
exec /probe.test -test.v -test.run="^TestNativeEpochRetirementFence${1}$" -test.timeout=400s
