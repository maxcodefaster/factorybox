"""Execute the actual public SHM regression with exact native ownership."""
import argparse
import json
from pathlib import Path
import re
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
parser.add_argument("--build", required=True)
parser.add_argument("--native-base-build", required=True)
args = parser.parse_args()
if not re.fullmatch(r"factorybox-build-shm-[0-9a-f]{32}", args.build):
    raise ValueError("requires one exact frozen native SHM build")
build = ROOT / "artifacts" / args.build
saved = json.loads((build / "result.json").read_text())
if (not saved["passed"] or not saved["cleanup"] or not saved["candidate_only"] or
        upstream_unit.context_manifest(build / "inputs") != saved["source_inputs"] or
        reference.sha(build / "output/shm.test") != saved["binary_sha256"]["shm.test"]):
    raise ValueError("native SHM build inputs changed")
owner = uuid.uuid4().hex
name = "factorybox-shm-probe-" + owner
archive = reference.new_artifact(name)
output = archive / "output"
output.mkdir(parents=True)
shutil.copyfile(__file__, archive / "runner.py")
shutil.copyfile(ROOT / "tests/global-sysctl-observation.py", archive / "observe.py")
shutil.copyfile(Path(__file__).with_name("observe_shm.py"), archive / "observe-shm.py")
image, _ = native_base.load(args.native_base_build)
command = [
    *reference.DOCKER, "run", "--pull=never", "--name", name,
    "--label", "com.factorybox.run=" + owner, "--privileged", "--cgroupns=private",
    "--cpus=3", "--memory=3g", "--pids-limit=512", "--network=none",
    "--env", "GOMAXPROCS=2", "--env", "GOMEMLIMIT=256MiB",
    "--env", "FACTORYBOX_NATIVE_SHM_PROBE=1",
    "--mount", f"type=bind,source={build / 'output/shm.test'},target=/shm.test,readonly",
    "--mount", f"type=bind,source={output},target=/output",
    "--mount", f"type=bind,source={archive / 'observe.py'},target=/observe.py,readonly",
    "--mount", f"type=bind,source={archive / 'observe-shm.py'},target=/observe-shm.py,readonly",
    "--entrypoint", "/bin/bash", image, "-ec",
    "mount --make-rprivate /\n"
    "python3 /observe.py before\n"
    "python3 /observe-shm.py before\n"
    "status=0\n"
    "setpriv --bounding-set=-sys_module,-sys_rawio,-sys_boot,-sys_time,-syslog "
    "/shm.test -test.v -test.timeout=20s "
    "'-test.run=.' || status=$?\n"
    "python3 /observe.py after || status=1\n"
    "python3 /observe-shm.py after || status=1\n"
    "exit \"$status\"\n",
]
result = {"owner": owner, "build": args.build, "variant": saved["variant"],
          "native_base_build": args.native_base_build,
          "passed": False, "cleanup": False, "command": command,
          "scope": "Direct public SHM probe, 32 repetitions, native 3 CPU/3 GiB, "
                   "20-second Go test limit and 90-second outer limit; all unchanged linuxUtils "
                   "unit tests also execute. No application lifecycle claim."}
(archive / "intent.json").write_text(json.dumps(result, indent=2) + "\n")
print(json.dumps({"running": str(archive)}), flush=True)
try:
    with (archive / "stdout.txt").open("x") as stdout, (archive / "stderr.txt").open("x") as stderr:
        done = subprocess.run(command, stdout=stdout, stderr=stderr, timeout=90)
    result["exit_code"] = done.returncode
    result["container_state"] = json.loads(reference.docker(["inspect", name]))[0]["State"]
    text = (archive / "stdout.txt").read_text()
    result["completed_probes"] = len(re.findall(r"probe [0-9]+ completed, supported=", text))
    result["supported_probes"] = len(re.findall(r"probe [0-9]+ completed, supported=true", text))
    result["inputs_unchanged"] = (
        reference.sha(build / "output/shm.test") == saved["binary_sha256"]["shm.test"] and
        upstream_unit.context_manifest(build / "inputs") == saved["source_inputs"])
    result["global_sysctls_unchanged"] = (
        json.loads((output / "sysctls-before.json").read_text()) ==
        json.loads((output / "sysctls-after.json").read_text()))
    result["parent_shm_unchanged"] = (
        json.loads((output / "shm-before.json").read_text()) ==
        json.loads((output / "shm-after.json").read_text()))
    result["passed"] = (
        done.returncode == 0 and result["completed_probes"] == result["supported_probes"] == 32 and
        not result["container_state"]["OOMKilled"] and result["inputs_unchanged"] and
        result["global_sysctls_unchanged"] and result["parent_shm_unchanged"] and
        "--- SKIP:" not in text and "--- FAIL:" not in text and
        "--- PASS: TestFactoryboxShmProbeNative " in text and
        "--- PASS: TestGetDistroPath " in text)
    if saved["variant"] == "candidate":
        result["passed"] = result["passed"] and all(
            "--- PASS: " + name + " " in text for name in [
                "TestShmProbeRefusesUnmappedProcess",
                "TestShmProbeRefusesSharedNamespaces",
                "TestShmProbeRefusesSharedNamespaces/ipc",
                "TestShmProbeRefusesSharedNamespaces/mount"])
except Exception as error:
    result["error"] = str(error)
finally:
    reference.owned_delete("container", name, owner)
    result["cleanup"] = True
    (archive / "result.json").write_text(json.dumps(result, indent=2) + "\n")
    print(json.dumps({"evidence": str(archive), **{key: result.get(key) for key in [
        "passed", "cleanup", "completed_probes", "global_sysctls_unchanged", "error"]}}), flush=True)
if not result["passed"]:
    raise ValueError("native SHM regression did not pass")
