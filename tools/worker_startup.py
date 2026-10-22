#!/usr/bin/env python3
"""Bounded owned Sysbox service-start diagnostic, without a workload."""
import argparse
import json
import pathlib
import shutil
import subprocess
import time
import uuid

import native_base
import reference
import reference_lifecycle


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--native-base-build", required=True)
    parser.add_argument("--sysbox-build", required=True)
    options = parser.parse_args()
    owner = uuid.uuid4().hex
    name = "factorybox-worker-startup-" + owner
    artifact = reference.new_artifact(name)
    output = artifact / "output"
    output.mkdir(parents=True)
    shutil.copyfile(__file__, artifact / "runner.py")
    result = {"schema": 1, "owner": owner, "passed": False, "cleanup": False,
              "production_qualified": False, "scope": __doc__}
    attempted = volume_created = False
    try:
        host = json.loads(reference.docker(["info", "--format", "{{json .}}" ]))
        native_base.require_native_host("linux/amd64", host)
        image, _ = native_base.load(options.native_base_build, candidate_platform="linux/amd64")
        source, build = reference_lifecycle.component_build(options.sysbox_build, "factorybox-build-sysbox-")
        executable = source / "output/sysbox-fs"
        if reference.sha(executable) != build["binary_sha256"]["sysbox-fs"]:
            raise ValueError("exact Sysbox filesystem executable changed")
        result["binary_sha256"] = reference.sha(executable)
        result["host"] = {key: host[key] for key in ["OSType", "Architecture", "KernelVersion"]}
        script = r'''
set -euo pipefail
/sysbox-fs --mountpoint /var/lib/sysbox/fs --log /output/sysbox-fs.log &
filesystem=$!
trap 'kill "$filesystem" 2>/dev/null || true; wait "$filesystem" 2>/dev/null || true' EXIT
ready=false
for attempt in $(seq 1 100); do
  if test -S /run/sysbox/sysfs-seccomp.sock; then ready=true; break; fi
  kill -0 "$filesystem" 2>/dev/null || break
  sleep 0.1
done
ps -e -o pid,ppid,stat,wchan:32,args
python3 - "$filesystem" <<'DIAGNOSTIC'
import pathlib,sys
parent=int(sys.argv[1])
pending=[parent]
seen=set()
while pending:
    pid=pending.pop()
    if pid in seen: continue
    seen.add(pid)
    root=pathlib.Path('/proc')/str(pid)
    try:
        print('PROCESS',pid,(root/'cmdline').read_bytes().replace(b'\0',b' '))
        for thread in (root/'task').iterdir():
            for name in ['wchan','stack','children']:
                try: print(str(thread/name),(thread/name).read_text())
                except OSError as error: print(type(error).__name__)
            pending.extend(int(value) for value in (thread/'children').read_text().split())
    except OSError as error: print(type(error).__name__)
DIAGNOSTIC
printf 'FACTORYBOX_SERVICE_READY=%s\n' "$ready"
test "$ready" = true
'''
        command = [*reference.DOCKER, "run", "--pull=never", "--platform=linux/amd64",
                   "--name", name, "--label", "com.factorybox.run=" + owner,
                   "--privileged", "--cgroupns=private", "--cpus=3", "--memory=3g",
                   "--pids-limit=128", "--network=none", "--read-only",
                   "--env", "GOMAXPROCS=2", "--tmpfs", "/run:rw,nosuid,nodev,size=16m",
                   "--tmpfs", "/tmp:rw,nosuid,nodev,size=16m",
                   "--mount", f"type=volume,source={name},target=/var/lib/sysbox",
                   "--mount", f"type=bind,source={executable},target=/sysbox-fs,readonly",
                   "--mount", f"type=bind,source={output},target=/output",
                   "--entrypoint", "/bin/bash", image, "-c", script]
        result["command"] = command
        reference.docker(["volume", "create", "--label", "com.factorybox.run=" + owner, name])
        volume_created = True
        attempted = True
        started = time.monotonic()
        with (artifact / "stdout.txt").open("wb") as out, (artifact / "stderr.txt").open("wb") as err:
            completed = subprocess.run(command, stdout=out, stderr=err, timeout=30)
        result["seconds"] = time.monotonic() - started
        result["exit_code"] = completed.returncode
        result["passed"] = completed.returncode == 0
    finally:
        try:
            if attempted:
                reference.owned_delete("container", name, owner)
            if volume_created:
                reference.owned_delete("volume", name, owner)
            result["cleanup"] = True
        finally:
            result["passed"] = result["passed"] and result["cleanup"]
            (artifact / "result.json").write_text(json.dumps(result, indent=2) + "\n")
            print(json.dumps({"evidence": str(artifact), "passed": result["passed"]}))
    if not result["passed"]:
        raise SystemExit(1)


if __name__ == "__main__":
    main()
