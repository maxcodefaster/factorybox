#!/usr/bin/env python3
"""Optional file-syscall observation after the full consumer assertions finish."""
import hashlib
import io
import json
from pathlib import Path
import re
import shutil
import subprocess
import tarfile

import reference


DESTINATION = "/opt/factorybox-stop-trace"
OUTPUT = "/tmp/factorybox-worker-stop.strace"

# This runs only in the exact owned worker, after useful work has completed.
# The diagnostic executable and its output stay outside the bounded store.
ATTACH = r'''
import hashlib, json, os, subprocess, sys, time
root = sys.argv[1]
expected = json.loads(sys.argv[2])
for relative, digest in expected.items():
    with open(root + '/' + relative, 'rb') as stream:
        actual = hashlib.sha256()
        for block in iter(lambda: stream.read(1 << 20), b''):
            actual.update(block)
        if actual.hexdigest() != digest:
            raise ValueError('copied diagnostic file differs: ' + relative)
environment = dict(os.environ, LD_LIBRARY_PATH=root + '/usr/lib/x86_64-linux-gnu')
with open('/tmp/factorybox-worker-stop-tracer.stderr', 'xb') as error:
    tracer = subprocess.Popen([root + '/usr/bin/strace', '-f', '-qq', '-yy',
        '-e', 'trace=%file', '-s', '512', '-o', '/tmp/factorybox-worker-stop.strace',
        '-p', '1'], stdin=subprocess.DEVNULL, stdout=error, stderr=error,
        env=environment, start_new_session=True)
deadline = time.monotonic() + 5
while time.monotonic() < deadline:
    if tracer.poll() is not None:
        raise ValueError('stop tracer exited before attachment: ' + str(tracer.returncode))
    threads = sorted(os.listdir('/proc/1/task'))
    statuses = {}
    try:
        for thread in threads:
            with open('/proc/1/task/' + thread + '/status') as stream:
                statuses[thread] = next(int(line.split()[1]) for line in stream if line.startswith('TracerPid:'))
    except FileNotFoundError:
        continue
    if statuses and all(pid == tracer.pid for pid in statuses.values()):
        with open('/proc/' + str(tracer.pid) + '/stat') as stream:
            birth = stream.read().rsplit(')', 1)[1].split()[19]
        print(json.dumps({'pid': tracer.pid, 'start_time': birth,
            'threads': statuses, 'executable': os.readlink('/proc/' + str(tracer.pid) + '/exe'),
            'output': '/tmp/factorybox-worker-stop.strace',
            'attached_after_consumer_assertions': True}))
        break
    time.sleep(.02)
else:
    tracer.terminate()
    tracer.wait(timeout=5)
    raise TimeoutError('stop tracer did not attach to every worker thread')
'''


def file_sha(path):
    with path.open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def validate_root(root, files):
    actual = {str(path.relative_to(root)): path for path in root.rglob("*")
              if not path.is_dir() or path.is_symlink()}
    if set(actual) != set(files) or "usr/bin/strace" not in files:
        raise ValueError("diagnostic rootfs inventory differs")
    for name, row in files.items():
        path = actual[name]
        if "symlink" in row:
            if (not path.is_symlink() or path.readlink().as_posix() != row["symlink"] or
                    not path.resolve(strict=True).is_relative_to(root.resolve())):
                raise ValueError("diagnostic symlink identity differs")
        elif (path.is_symlink() or not path.is_file() or file_sha(path) != row["sha256"] or
              path.stat().st_mode & 0o777 != row["mode"]):
            raise ValueError("diagnostic file identity differs: " + name)


def freeze(name, worker, artifact):
    if not re.fullmatch(r"factorybox-stop-trace-inputs-[0-9a-f]{32}", name):
        raise ValueError("expected exact stop diagnostic input artifact")
    source = reference.ROOT / "artifacts" / name
    saved = json.loads((source / "result.json").read_text())
    if (saved.get("passed") is not True or saved.get("cleanup") is not True or
            saved.get("platform") != "linux/amd64" or
            saved.get("native_base_build") != worker["native_base_build"] or
            saved.get("image") != worker["native_base_image"] or
            saved.get("base_packages_sha256") != worker["base_packages_sha256"] or
            set(saved.get("packages", {})) != {"strace", "libunwind8"}):
        raise ValueError("diagnostic input acquisition or pinned native base differs")
    validate_root(source / "rootfs", saved["files"])
    for row in saved["packages"].values():
        package = source / "packages" / row["file"]
        if Path(row["file"]).name != row["file"] or file_sha(package) != row["sha256"] or package.stat().st_size != row["bytes"]:
            raise ValueError("diagnostic package differs from authenticated acquisition")
    target = artifact / "stop-trace-rootfs"
    shutil.copytree(source / "rootfs", target, symlinks=True)
    validate_root(target, saved["files"])
    shutil.copyfile(__file__, artifact / "consumer_stop_trace.py")
    return {"artifact": name, "result_sha256": file_sha(source / "result.json"),
            "files": saved["files"], "packages": saved["packages"],
            "scope": "File syscall tracing of the original worker only after all consumer assertions pass. Diagnostic instrumentation; no timing or production qualification."}


def attach(identity, artifact, phase_artifact, receipt):
    validate_root(artifact / "stop-trace-rootfs", receipt["files"])
    reference.docker(["exec", identity, "mkdir", "-m", "0700", DESTINATION])
    subprocess.run([*reference.DOCKER, "cp", str(artifact / "stop-trace-rootfs") + "/.",
                    identity + ":" + DESTINATION], check=True, timeout=10,
                   capture_output=True)
    expected = {name: row["sha256"] for name, row in receipt["files"].items() if "sha256" in row}
    attached = subprocess.run([*reference.DOCKER, "exec", identity, "python3", "-c", ATTACH,
                               DESTINATION, json.dumps(expected)], timeout=12, capture_output=True, text=True)
    (phase_artifact / "stop-tracer-attach.stdout").write_text(attached.stdout)
    (phase_artifact / "stop-tracer-attach.stderr").write_text(attached.stderr)
    if attached.returncode:
        try:
            collect(identity, phase_artifact)
        except Exception as error:
            (phase_artifact / "stop-tracer-collect-error.txt").write_text(repr(error) + "\n")
        raise ValueError("stop tracer attachment failed: " + attached.stderr[-2000:])
    observed = json.loads(attached.stdout)
    if observed["executable"] != DESTINATION + "/usr/bin/strace" or not observed["attached_after_consumer_assertions"]:
        raise ValueError("diagnostic attachment identity differs")
    (phase_artifact / "stop-tracer.json").write_text(json.dumps(observed, indent=2) + "\n")
    return observed


def collect(identity, phase_artifact):
    files = {}
    for remote, local in [(OUTPUT, "worker-stop.strace"),
                          ("/tmp/factorybox-worker-stop-tracer.stderr", "worker-stop-tracer.stderr")]:
        destination = phase_artifact / local
        copied = subprocess.run([*reference.DOCKER, "cp", identity + ":" + remote, "-"],
                                check=True, timeout=15, capture_output=True)
        if len(copied.stdout) > (32 << 20) + (1 << 20):
            raise ValueError("stop trace archive exceeds diagnostic limit")
        with tarfile.open(fileobj=io.BytesIO(copied.stdout)) as archive:
            members = archive.getmembers()
            if (len(members) != 1 or not members[0].isfile() or
                    members[0].name != Path(remote).name or members[0].size > 32 << 20):
                raise ValueError("stop trace archive has another identity or size")
            with destination.open("xb") as output, archive.extractfile(members[0]) as source:
                shutil.copyfileobj(source, output, length=1 << 20)
        files[local] = {"sha256": file_sha(destination), "bytes": destination.stat().st_size}
    return files
