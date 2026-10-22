#!/bin/bash
# Administrator preparation used only by the native Compose-client proof.
set -euo pipefail
test "$FACTORYBOX_WORKER_EXAMPLE_NATIVE" = 1
printf '%s  %s\n' "$1" /var/lib/sysbox/native-rootfs.tar | sha256sum -c -
printf '%s  %s\n' "$2" /var/lib/sysbox/native-minimal-actor | sha256sum -c -
bundle=/var/lib/sysbox/input/bundle
mkdir -p "$bundle/lower"
tar --blocking-factor=2048 --numeric-owner --same-owner --xattrs --acls -xpf /var/lib/sysbox/native-rootfs.tar -C "$bundle/lower"
cp /var/lib/sysbox/native-minimal-actor "$bundle/lower/opt/factorybox-example-actor"
chmod 0755 "$bundle/lower/opt/factorybox-example-actor"
mkdir -p "$bundle/lower/workspace"
cli=/opt/factorybox/factorybox
"$cli" --worker-run-root /run/factorybox prepare-base --source "$bundle/lower" example-base > /var/lib/sysbox/input/base.json
mv "$bundle/lower" "$bundle/lower-input"
mkdir "$bundle/lower" "$bundle/upper" "$bundle/work" "$bundle/rootfs"
base=$(python3 -c 'import json; print(json.load(open("/var/lib/sysbox/input/base.json"))["tree"]["saved"])')
mount --bind "$base" "$bundle/lower"
mount -o remount,bind,ro "$bundle/lower"
mount -t overlay overlay -o "index=off,lowerdir=$bundle/lower,upperdir=$bundle/upper,workdir=$bundle/work" "$bundle/rootfs"
/opt/factorybox/components/sysbox-runc spec --bundle "$bundle"
python3 - "$bundle" <<'PY'
import json,pathlib,sys
path=pathlib.Path(sys.argv[1])/'config.json'
spec=json.loads(path.read_text())
spec['process']['terminal']=False
spec['process']['cwd']='/'
spec['process']['args']=['/bin/sh','-c','exec /opt/factorybox-example-actor </dev/null >/var/log/factorybox-example.log 2>&1']
spec['process']['env']=['HOME=/root','PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin']
for key,values in spec['process']['capabilities'].items():
    spec['process']['capabilities'][key]=[value for value in values if value not in ['CAP_SYS_MODULE','CAP_SYS_RAWIO','CAP_SYS_BOOT','CAP_SYS_TIME','CAP_SYSLOG']]
spec['root']['readonly']=False
spec.setdefault('annotations',{})['io.factorybox.base']='example-base'
spec['linux']['resources']={'pids':{'limit':128},'devices':[{'allow':True,'access':'rwm'}]}
for mount in spec['mounts']:
    if mount['destination']=='/sys/fs/cgroup':mount['options']=['nosuid','noexec','nodev','rw']
path.write_text(json.dumps(spec,indent=2)+'\n')
PY
