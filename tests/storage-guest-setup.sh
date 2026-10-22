#!/bin/bash
set -euo pipefail
mkdir -m 0700 /out
cp /opt/factorybox/components/* /out/
ln -s /opt/factorybox/factorybox /factorybox
mkdir /busybox
tar -xzf /busybox-rootfs.tar.gz -C /busybox
exec /probe.test -test.v -test.run='^TestNativeStorageGuestFilesystemBoundary$' -test.timeout=410s
