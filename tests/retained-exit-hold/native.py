"""Minimize timed-source loss after tracer death on the real private cgroup."""
import argparse
import json
from pathlib import Path
import shutil
import subprocess
import sys
import uuid

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / "tools"))
import native_base
import reference

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument("--native-base-build", required=True)
parser.add_argument("--candidate-platform", choices=["linux/amd64"])
parser.add_argument("--mode", choices=["pidfd", "exit-stop", "child-exit-stop", "child-cancel",
                                      "child-job-stop"], required=True)
parser.add_argument("--observer-socket", action="store_true",
                    help="Apply the ordered socket EOF fix to the same observer used by CRIU")
options = parser.parse_args()
target = options.candidate_platform or "linux/arm64"
host = json.loads(reference.docker(["info", "--format", "{{json .}}" ]))
native_base.require_native_host(target, host)
image, _ = native_base.load(options.native_base_build, candidate_platform=options.candidate_platform)
owner = uuid.uuid4().hex
name = "factorybox-exit-hold-" + owner
archive = reference.new_artifact(name)
archive.mkdir()
for source, destination in [
    (Path(__file__), archive / "runner.py"),
    (Path(__file__).with_name("probe.c"), archive / "probe.c"),
    (ROOT / "tests/private-cgroup-delegation.sh", archive / "delegate.sh"),
    (ROOT / "upstream/candidates/criu-exit-observer.h", archive / "criu-exit-observer.h"),
]:
    shutil.copyfile(source, destination)
if options.observer_socket:
    patch = ROOT / "upstream/patches/criu-retained-exit-socket-candidate.patch"
    shutil.copyfile(patch, archive / "observer-socket.patch")
    subprocess.run(["patch", "--batch", "--fuzz=0", str(archive / "criu-exit-observer.h"),
                    "-i", str(archive / "observer-socket.patch")],
                   check=True, capture_output=True, timeout=15)
inputs = {p.name: reference.sha(p) for p in archive.iterdir() if p.is_file()}
result = {
    "owner": owner, "container_name": name, "mode": options.mode,
    "native_base_build": options.native_base_build, "inputs_sha256": inputs,
    "platform": target, "native_lifecycle_qualified": False,
    "observer_socket": options.observer_socket,
    "passed": False, "cleanup": False, "production_qualified": False,
    "scope": "One actual timed source in its own private cgroup, held by an actual "
             "ptrace tracer. After its timer expires, kill only the original tracer. "
             "Compare asynchronous freeze after death with PTRACE_EVENT_EXIT-before-detach "
             "freeze under a real 100ms observer delay. No CRIU, Docker or runtime qualification.",
}
command = [
    *reference.DOCKER, "run", "--pull=never", "--platform=" + target, "--name", name,
    "--label", "com.factorybox.run=" + owner, "--privileged", "--cgroupns=private",
    "--cpus=3", "--memory=3g", "--pids-limit=64", "--network=none", "--read-only",
    "--env", "FACTORYBOX_UPSTREAM_CONTAINER=1", "--tmpfs", "/tmp:rw,exec,size=32m",
    "--mount", f"type=bind,source={archive},target=/input,readonly",
    "--entrypoint", "/bin/bash", image, "-ec",
    ". /input/delegate.sh\nmount --make-rprivate /\n"
    "cc -O2 -Wall -Wextra -Werror /input/probe.c -o /tmp/probe\n"
    "exec setpriv --bounding-set=-sys_module,-sys_rawio,-sys_boot,-sys_time,-syslog "
    "/tmp/probe " + options.mode,
]
result["command"] = command
(archive / "intent.json").write_text(json.dumps(result, indent=2) + "\n")
print(json.dumps({"running": str(archive), "mode": options.mode}), flush=True)
attempted = False
failure = None
try:
    attempted = True
    with (archive / "stdout.txt").open("x") as stdout, (archive / "stderr.txt").open("x") as stderr:
        done = subprocess.run(command, stdout=stdout, stderr=stderr, timeout=90)
    result["exit_code"] = done.returncode
    result["container_state"] = json.loads(reference.docker(["inspect", name]))[0]["State"]
    result["inputs_unchanged"] = all(reference.sha(archive / p) == sha for p, sha in inputs.items())
    lines = (archive / "stdout.txt").read_text().splitlines()
    result["observation"] = json.loads(lines[-1]) if lines and lines[-1].startswith("{") else {}
    observation = result["observation"]
    preserved = observation.get("source_preserved") is True and observation.get("source_ran_after_detach") is False
    if options.mode == "child-cancel":
        preserved = (observation.get("ordinary_cancel") is True and
                     observation.get("source_normal_exit") is True and
                     observation.get("source_ran_after_detach") is True)
    result["passed"] = (
        done.returncode == 0 and result["inputs_unchanged"] and
        preserved and
        not result["container_state"]["OOMKilled"])
except Exception as error:
    failure = error
    result["error"] = str(error)
finally:
    try:
        if attempted:
            reference.owned_delete("container", name, owner)
        result["cleanup"] = True
    except Exception as error:
        failure = failure or error
        result["cleanup_error"] = str(error)
    result["passed"] = result["passed"] and result["cleanup"] and failure is None
    (archive / "result.json").write_text(json.dumps(result, indent=2) + "\n")
    print(json.dumps({"evidence": str(archive), "passed": result["passed"],
                      "cleanup": result["cleanup"], "observation": result.get("observation")}), flush=True)
if not result["passed"]:
    raise RuntimeError("native exit hold did not preserve source: " + str(archive)) from failure
