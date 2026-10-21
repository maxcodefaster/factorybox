"""Run the real descriptor/executable admission scanner in an owned Linux cgroup."""
import argparse
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import uuid

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / "tools"))
import native_base
import reference
import upstream_unit

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument("--native-base-build", required=True)
options = parser.parse_args()
image, _ = native_base.load(options.native_base_build)
owner = uuid.uuid4().hex
name = "factorybox-file-admission-" + owner
archive = reference.new_artifact(name)
archive.mkdir()
context = archive / "context"
shutil.copytree(ROOT / "internal", context / "internal")
for filename in ["go.mod", "go.sum"]:
    shutil.copyfile(ROOT / filename, context / filename)
shutil.copyfile(__file__, archive / "runner.py")
shutil.copyfile(ROOT / "tests/private-cgroup-delegation.sh", archive / "delegate.sh")
before = upstream_unit.context_manifest(context)
subprocess.run(["go", "test", "-c", "-trimpath", "-buildvcs=false",
                "-o", str(archive / "lifecycle.test"), "./internal/lifecycle"], cwd=context,
               env=dict(os.environ, GOOS="linux", GOARCH="arm64", CGO_ENABLED="0"),
               check=True, timeout=120)
inputs = {p.name: reference.sha(p) for p in archive.iterdir() if p.is_file()}
command = [
    *reference.DOCKER, "run", "--pull=never", "--name", name,
    "--label", "com.factorybox.run=" + owner, "--privileged", "--cgroupns=private",
    "--cpus=3", "--memory=3g", "--pids-limit=64", "--network=none",
    "--env", "FACTORYBOX_NATIVE_FILE_ADMISSION=1", "--env", "FACTORYBOX_UPSTREAM_CONTAINER=1",
    "--env", "GOMAXPROCS=2", "--env", "GOMEMLIMIT=256MiB",
    "--mount", f"type=bind,source={archive / 'lifecycle.test'},target=/lifecycle.test,readonly",
    "--mount", f"type=bind,source={archive / 'delegate.sh'},target=/delegate.sh,readonly",
    "--entrypoint", "/bin/bash", image, "-ec",
    ". /delegate.sh\nmount --make-rprivate /\n"
    "exec setpriv --bounding-set=-sys_module,-sys_rawio,-sys_boot,-sys_time,-syslog "
    "/lifecycle.test -test.v -test.timeout=20s -test.run '^TestDetachedFileAdmissionNative$'",
]
result = {
    "owner": owner, "container_name": name, "native_base_build": options.native_base_build,
    "command": command, "source_context": before, "inputs_sha256": inputs,
    "scope": "Actual admission scanner on frozen, owned processes. Visible/detached O_PATH "
             "regular files and memfd references, plus actual visible/detached/memfd executable "
             "mappings after the original descriptor closes on exec. No CRIU or full lifecycle claim.",
    "passed": False, "cleanup": False, "production_qualified": False,
}
(archive / "intent.json").write_text(json.dumps(result, indent=2) + "\n")
print(json.dumps({"running": str(archive)}), flush=True)
attempted = False
try:
    attempted = True
    with (archive / "stdout.txt").open("x") as stdout, (archive / "stderr.txt").open("x") as stderr:
        done = subprocess.run(command, stdout=stdout, stderr=stderr, timeout=90)
    result["exit_code"] = done.returncode
    result["container_state"] = json.loads(reference.docker(["inspect", name]))[0]["State"]
    text = (archive / "stdout.txt").read_text()
    result["inputs_unchanged"] = (
        upstream_unit.context_manifest(context) == before and
        all(reference.sha(archive / filename) == digest for filename, digest in inputs.items()))
    result["passed"] = (
        done.returncode == 0 and result["inputs_unchanged"] and
        not result["container_state"]["OOMKilled"] and "--- SKIP:" not in text and "--- FAIL:" not in text and
        all("--- PASS: TestDetachedFileAdmissionNative/" + case + " " in text for case in
            ["visible", "detached", "memfd", "exec-visible", "exec-detached", "exec-memfd"]))
except Exception as error:
    result["error"] = str(error)
finally:
    if attempted:
        reference.owned_delete("container", name, owner)
    result["cleanup"] = True
    (archive / "result.json").write_text(json.dumps(result, indent=2) + "\n")
    print(json.dumps({"evidence": str(archive), **{key: result.get(key) for key in
                      ["passed", "cleanup", "inputs_unchanged"]}}), flush=True)
    reference.artifact_storage.compact_closed_run(archive, ROOT)
if not result["passed"]:
    raise RuntimeError("native descriptor/executable admission regression failed: " + str(archive))
