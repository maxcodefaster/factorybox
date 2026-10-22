#!/bin/bash
# Deliberate native fault fixture. The delivered component bundle stays intact.
set -euo pipefail
mkdir -m 0700 /fault-components
cp /opt/factorybox/components/* /fault-components/
if [ "$FACTORYBOX_STARTUP_FAULT" = blocked-child ]; then
  printf '#!/usr/bin/python3\nimport os, time\npid = os.fork()\nif pid == 0:\n    time.sleep(300)\n    os._exit(0)\nos.waitpid(pid, 0)\n' > /fault-components/sysbox-fs
elif [ "$FACTORYBOX_STARTUP_FAULT" = service-exit ]; then
  printf '#!/bin/sh\nsleep 2\nexit 23\n' > /fault-components/sysbox-fs
else
  exit 2
fi
chmod 0755 /fault-components/*
python3 - <<'PY'
import hashlib
import json
import os
from pathlib import Path

root = Path('/fault-components')
hashes = {}
for file in root.iterdir():
    digest = hashlib.sha256()
    with file.open('rb') as stream:
        for chunk in iter(lambda: stream.read(1 << 20), b''):
            digest.update(chunk)
    hashes[file.name] = digest.hexdigest()
fd = os.open(root / 'components.json', os.O_CREAT | os.O_EXCL | os.O_WRONLY, 0o600)
with os.fdopen(fd, 'w') as stream:
    json.dump({'schema': 1, 'binary_sha256': hashes}, stream)
PY
exec /opt/factorybox/factorybox worker --store /var/lib/sysbox/store \
  --store-id "$FACTORYBOX_STARTUP_OWNER" --components /fault-components \
  --manifest /fault-components/components.json
