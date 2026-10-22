#!/bin/bash
set -euo pipefail
case "$1" in Prepare|Recover) ;; *) exit 2 ;; esac
mkdir /busybox
tar -xzf /busybox-rootfs.tar.gz -C /busybox
exec /probe.test -test.v -test.run="^TestNativeBoundedLoss${1}$" -test.timeout=400s
