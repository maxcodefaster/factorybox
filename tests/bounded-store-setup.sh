#!/bin/bash
set -euo pipefail
mkdir /busybox
tar -xzf /busybox-rootfs.tar.gz -C /busybox
exec /probe.test -test.v -test.run='^TestNativeBoundedWorkerStore$' -test.timeout=400s
