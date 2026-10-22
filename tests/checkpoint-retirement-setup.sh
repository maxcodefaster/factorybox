#!/bin/bash
set -euo pipefail
mkdir /busybox
tar -xzf /busybox-rootfs.tar.gz -C /busybox
exec /probe.test -test.v -test.run="${FACTORYBOX_RETIREMENT_TEST:-^TestNativeCheckpointRetirement}" -test.timeout=400s
