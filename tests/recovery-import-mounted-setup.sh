#!/bin/bash
set -euo pipefail
case "$1" in
  Prepare|Recover) test="TestNativeBoundedLoss${1}" ;;
  Interrupt) test="TestNativeRecoveryImportMountedInterrupt" ;;
  *) exit 2 ;;
esac
mkdir /busybox
tar -xzf /busybox-rootfs.tar.gz -C /busybox
exec /probe.test -test.v -test.run="^${test}$" -test.timeout=400s
