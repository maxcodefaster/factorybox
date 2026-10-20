"""Run the unmodified required failure assertions with an unselected CRIU candidate."""
import argparse
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
import uuid

sys.path.insert(0, str(Path(__file__).resolve().parents[2] / "tools"))
import lifecycle_native
import native_base
import reference
import reference_lifecycle
import sources

root = reference.ROOT
parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument("--candidate", required=True)
parser.add_argument("--native-base-build", required=True)
parser.add_argument("--sysbox-build", required=True)
parser.add_argument("--corruption-control", action="store_true")
options = parser.parse_args()
candidate_name = options.candidate
corruption_control = options.corruption_control
assert re.fullmatch(r"factorybox-build-criu-retained-[0-9a-f]{32}", candidate_name)
candidate = root / "artifacts" / candidate_name
criu = json.loads((candidate / "result.json").read_text())
assert criu["passed"] and criu["cleanup"] and criu["diagnostic_only"]
assert reference.sha(candidate / "criu") == criu["binary_sha256"]
assert reference.sha(candidate / "source-lock.json") == criu["source_lock_sha256"]
assert reference.sha(candidate / "retained.patch") == criu["patch_sha256"]
assert sources.tree_sha(candidate / "context") == criu["context_sha256"]
candidate_lock = json.loads((candidate / "source-lock.json").read_text())
component = candidate_lock["components"]["criu"]
sources.validate_inputs(component)
sources.check_source(candidate / "context", root / ".cache/downloads" / component["archive"], component)
base_name = options.native_base_build
image, base = native_base.load(base_name)
sysbox_name = options.sysbox_build
sysbox_path, sysbox = reference_lifecycle.component_build(sysbox_name, "factorybox-build-sysbox-")
owner = uuid.uuid4().hex
name = "factorybox-runtime-retained-" + owner
archive = reference.new_artifact(name)
output = archive / "output"
output.mkdir(parents=True)
context = archive / "context"
for directory in ["internal", "cmd/factorybox"]:
    shutil.copytree(root / directory, context / directory)
for file in ["go.mod", "go.sum"]:
    shutil.copyfile(root / file, context / file)
source_sha = sources.tree_sha(context)
env = dict(os.environ, GOOS="linux", GOARCH="arm64", CGO_ENABLED="0")
for file, package, test in [
    ("factorybox", "./cmd/factorybox", False),
    ("lifecycle.test", "./internal/lifecycle", True),
    ("sysboxcapture.test", "./internal/sysboxcapture", True),
]:
    command = ["go", "test", "-c"] if test else ["go", "build"]
    subprocess.run([*command, "-trimpath", "-buildvcs=false", "-o", str(archive / file), package],
                   cwd=context, env=env, check=True, timeout=120)
for script in ["lifecycle-native.sh", "private-cgroup-delegation.sh", "capture-archive.sh"]:
    shutil.copyfile(root / "tests" / script, archive / script)
components = archive / "components"
components.mkdir()
for file in ["sysbox-runc", "sysbox-fs", "sysbox-mgr"]:
    reference.artifact_storage.share_frozen_input(
        sysbox_path / "output" / file, components / file, root, sysbox["binary_sha256"][file])
reference.artifact_storage.share_frozen_input(
    candidate / "criu", components / "criu", root, criu["binary_sha256"])
busybox = json.loads((root / "upstream/test-inputs.json").read_text())["busybox-arm64"]
rootfs = root / ".cache/downloads" / busybox["archive"]
assert reference.sha(rootfs) == busybox["sha256"]
inputs = {p.name: reference.sha(p) for p in archive.iterdir() if p.is_file()}
component_hashes = {p.name: reference.sha(p) for p in components.iterdir()}
command = [
    *reference.DOCKER, "run", "--pull=never", "--name", name,
    "--label", "com.factorybox.run=" + owner, "--privileged", "--cgroupns=private",
    "--cpus=3", "--memory=3g", "--pids-limit=512", "--network=none",
    "--env", "FACTORYBOX_NATIVE_LIFECYCLE=1", "--env", "FACTORYBOX_UPSTREAM_CONTAINER=1",
    "--env", "FACTORYBOX_NATIVE_RETAINED_CAPTURE=1", "--env", "FACTORYBOX_COMPONENT_INPUTS=1",
    "--env", "FACTORYBOX_NATIVE_INTERRUPTION=1", "--env", "FACTORYBOX_CRIU_RETAINED_DIAGNOSTIC=1",
    "--env", "FACTORYBOX_RUNTIME_SHA256=" + sysbox["binary_sha256"]["sysbox-runc"],
    "--env", "FACTORYBOX_CRIU_SHA256=" + criu["binary_sha256"],
    "--env", "GOMAXPROCS=2", "--env", "GOMEMLIMIT=256MiB",
    "--mount", f"type=volume,source={name},target=/var/lib/sysbox",
    "--mount", f"type=bind,source={output},target=/output",
    "--mount", f"type=bind,source={components},target=/component-input,readonly",
    "--mount", f"type=bind,source={rootfs},target=/busybox-rootfs.tar.gz,readonly",
]
for file, target in [
    ("lifecycle.test", "/lifecycle.test"), ("factorybox", "/factorybox"),
    ("sysboxcapture.test", "/sysboxcapture.test"), ("lifecycle-native.sh", "/native.sh"),
    ("private-cgroup-delegation.sh", "/delegate.sh"),
]:
    command += ["--mount", f"type=bind,source={archive / file},target={target},readonly"]
command += ["--entrypoint", "/bin/bash", image, "/native.sh"]
if corruption_control:
    location = command.index("--entrypoint")
    command[location:location] = ["--env", "FACTORYBOX_CRIU_RETAINED_CORRUPTION_CONTROL=1"]
result = {
    "owner": owner, "container_name": name, "volume_name": name, "command": command,
    "candidate": candidate_name, "candidate_sha256": criu["binary_sha256"],
    "corruption_control": corruption_control,
    "inputs_sha256": inputs, "component_sha256": component_hashes, "source_sha256": source_sha,
    "scope": "Actual CRIU kill during pages dump; retained source must remain held. "
             "Diagnostic cure/raw-RAM/TCP checks do not replace the still-required running-source assertion.",
    "passed": False, "mechanism_passed": False, "cleanup": False, "production_qualified": False,
}
(archive / "intent.json").write_text(json.dumps(result, indent=2) + "\n")
print(json.dumps({"running": str(archive)}), flush=True)
attempted = False
try:
    reference.docker(["volume", "create", "--label", "com.factorybox.run=" + owner, name])
    attempted = True
    with (archive / "stdout.txt").open("x") as stdout, (archive / "stderr.txt").open("x") as stderr:
        done = subprocess.run(command, stdout=stdout, stderr=stderr, timeout=420)
    result["exit_code"] = done.returncode
    result["container_state"] = json.loads(reference.docker(["container", "inspect", name]))[0]["State"]
    text = (archive / "stdout.txt").read_text()
    result["passed_tests"] = sum(line.startswith("--- PASS: ") for line in text.splitlines())
    result["passed_subtests"] = sum(line.lstrip().startswith("--- PASS: ") and not line.startswith("--- PASS:")
                                   for line in text.splitlines())
    result["inputs_unchanged"] = (
        all(reference.sha(archive / p) == digest for p, digest in inputs.items()) and
        all(reference.sha(components / p) == digest for p, digest in component_hashes.items()) and
        sources.tree_sha(context) == source_sha)
    result["passed"] = done.returncode == 0 and result["inputs_unchanged"]
    proof = output / "interruption-retained-context.json"
    if proof.exists():
        result["diagnostic"] = json.loads(proof.read_text())
        sockets = json.loads((output / "interruption-sockets.json").read_text())
        result["network_unchanged"] = (
            (output / "interruption-network-before.txt").read_bytes() ==
            (output / "interruption-network-after.txt").read_bytes() and
            sockets["before"] == sockets["after"] and
            "rolled-back original TCP sockets are not bidirectionally usable" not in text)
        result["mechanism_passed"] = (
            result["diagnostic"]["passed"] and result["network_unchanged"] and result["inputs_unchanged"] and
            result["passed_tests"] == 8 and result["passed_subtests"] == 40 and
            "previous committed checkpoint remained independently usable after actual CRIU interruption" in text and
            not result["container_state"]["OOMKilled"])
except Exception as error:
    result["error"] = str(error)
finally:
    preserve = False
    if attempted:
        try:
            lifecycle_native.archive_capture(archive, name, owner, result, image)
            result["capture_archive_sha256"] = reference.sha(output / "capture-source.tar")
        except Exception as error:
            preserve = True
            result["archive_error"] = str(error)
            result["retained_volumes"] = [name]
        try:
            reference.owned_delete("container", name, owner)
            if not preserve:
                reference.owned_delete("volume", name, owner)
            result["cleanup"] = not preserve
        except Exception as error:
            result["cleanup_error"] = str(error)
    result["mechanism_passed"] = bool(result["mechanism_passed"] and result["cleanup"])
    (archive / "result.json").write_text(json.dumps(result, indent=2) + "\n")
    print(json.dumps({"evidence": str(archive), "passed": result["passed"],
                      "mechanism_passed": result["mechanism_passed"], "cleanup": result["cleanup"]}), flush=True)
    reference.artifact_storage.compact_closed_run(archive, root)
if not result["mechanism_passed"]:
    raise RuntimeError("real CRIU retained-context diagnostic failed: " + str(archive))
if not result["passed"]:
    raise RuntimeError("retained-context mechanism passed, but required running-source lifecycle assertion still failed: " + str(archive))
