#!/bin/bash
# Small owned fixture for the shared-source contract. No product startup policy.
set -euo pipefail
bundle=/var/lib/sysbox/fixed-store/data/store/input/bundle
mkdir -p "$bundle/lower/opt" "$bundle/lower/workspace" "$bundle/lower/home/coder/project" "$bundle/lower/etc" "$bundle/lower/tmp" "$bundle/lower/run" "$bundle/lower/var/log"
cp /var/lib/sysbox/native-minimal-actor "$bundle/lower/opt/factorybox-example-actor"
chmod 0755 "$bundle/lower/opt/factorybox-example-actor"
if [[ ${3:-} == 1 ]]; then
  mkdir -p "$bundle/lower/workspace/project"
  cc -std=c11 -Wall -Wextra -Werror -static -O2 /var/lib/sysbox/native-alias.c -o "$bundle/lower/opt/factorybox-alias-init"
  cp "$bundle/lower/opt/factorybox-alias-init" /var/lib/sysbox/native-alias-built
fi
printf 'ID=alpine\nVERSION_ID=3.23\n' > "$bundle/lower/etc/os-release"
cli=/opt/factorybox/factorybox
"$cli" --worker-run-root /run/factorybox prepare-base --source "$bundle/lower" example-base > /var/lib/sysbox/fixed-store/data/store/input/base.json
mv "$bundle/lower" "$bundle/lower-input"
mkdir "$bundle/lower" "$bundle/upper" "$bundle/work" "$bundle/rootfs"
base=$(python3 -c 'import json; print(json.load(open("/var/lib/sysbox/fixed-store/data/store/input/base.json"))["tree"]["saved"])')
mount --bind "$base" "$bundle/lower"
mount -o remount,bind,ro "$bundle/lower"
mount -t overlay overlay -o "index=off,lowerdir=$bundle/lower,upperdir=$bundle/upper,workdir=$bundle/work" "$bundle/rootfs"
/opt/factorybox/components/sysbox-runc spec --bundle "$bundle"
