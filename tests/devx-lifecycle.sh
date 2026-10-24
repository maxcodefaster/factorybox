#!/bin/bash
# The Python runner owns this container and every mounted volume.
set -euo pipefail
test "${FACTORYBOX_DEVX_LIFECYCLE:-}" = 1
. /delegate.sh > /output/cgroup-delegation.txt
mount --make-rprivate /
exec setpriv --bounding-set=-sys_module,-sys_rawio,-sys_boot,-sys_time \
  /bin/bash /devx-lifecycle-body.sh
