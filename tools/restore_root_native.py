#!/usr/bin/env python3
"""Verify pinned restore/resume mount geometry in separately owned worker storage."""
import argparse
import json
import re
import shutil
import subprocess
import time
import uuid

import artifact_storage
import build_config
import build_worker
import consumer_worker_loss
import native_base
import reference
import reference_lifecycle


CASES = [
    "flat-restore", "flat-resume", "native-restore", "native-resume",
    "native-root-alias", "native-upper-alias", "native-work-alias",
    "native-covered-root", "native-split-upper", "native-existing-work",
]
TEST_BINARY = "factorybox-restore-root.test"


def require_coverage(output):
    if "--- SKIP:" in output or any(
            "--- PASS: TestFactoryboxNativeRootMapping/" + name + " (" not in output
            for name in CASES):
        raise ValueError("native root mapping omitted an original-layout or required refusal case")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--sysbox-build", required=True, help="Exact completed component build with its compiled native test")
    parser.add_argument("--worker-build", required=True, help="Exact delivered worker owning only the bounded fixture")
    args = parser.parse_args()
    _, worker = build_worker.load(args.worker_build)
    component_path, component = reference_lifecycle.component_build(args.sysbox_build, "factorybox-build-sysbox-")
    host = json.loads(reference.docker(["info", "--format", "{{json .}}"]))
    native_base.require_native_host(worker["platform"], host)
    build_config.require_platform(worker["platform"], component)
    image, _ = build_config.local_pinned_image(worker["image"], worker["image_id"], reference.docker)
    expected = component.get("native_test_sha256", {}).get(TEST_BINARY)
    source = component_path / "output" / TEST_BINARY
    if not isinstance(expected, str) or not re.fullmatch(r"[0-9a-f]{64}", expected) or source.is_symlink():
        raise ValueError("Sysbox build has no pinned native root test")
    if reference.sha(source) != expected:
        raise ValueError("native root test changed after component compilation")
    build_config.require_elf(source, worker["platform"])
    owner = uuid.uuid4().hex
    name = "factorybox-restore-root-" + owner
    artifact = reference.new_artifact(name)
    artifact.mkdir()
    binary = artifact / TEST_BINARY
    artifact_storage.share_frozen_input(source, binary, reference.ROOT, expected)
    shutil.copyfile(__file__, artifact / "runner.py")
    result = {
        "schema": 1, "owner": owner, "platform": worker["platform"],
        "container_name": name, "volume_name": name,
        "worker_build": args.worker_build, "component_build": args.sysbox_build,
        "worker_image": image, "test_sha256": expected,
        "component_result_sha256": reference.sha(component_path / "result.json"),
        "scope": "Actual pinned public restore/resume root mapper, ID-mapped lower/private-volume mounts and native upper/work copy-up. Not complete Sandbox lifecycle or consumer qualification.",
        "passed": False, "cleanup": False, "native_lifecycle_qualified": False, "production_qualified": False,
    }
    container_created = volume_created = False
    try:
        command = [
            "run", "--detach", "--pull=never", "--name", name, "--label", "com.factorybox.run=" + owner,
            "--privileged", "--cgroupns=private", "--cpus=3", "--memory=3g", "--pids-limit=512", "--network=none",
            "--mount", "type=volume,source=" + name + ",target=/var/lib/sysbox",
            "--mount", "type=bind,source=" + str(binary) + ",target=/probe.test,readonly",
            image, "--store-id", owner,
            "--bounded-root", "/var/lib/sysbox/fixed-store", "--bounded-bytes", str(512 << 20),
        ]
        result["command"] = command
        # Keep the exact resource intent even if create succeeds but its reply
        # is lost. Cleanup inspects the original names and owner labels.
        with (artifact / "intent.json").open("x") as intent:
            json.dump(result, intent, indent=2)
            intent.write("\n")
        volume_created = True
        reference.docker(["volume", "create", "--label", "com.factorybox.run=" + owner, name])
        container_created = True
        identity = reference.docker(command)
        deadline = time.monotonic() + 420
        result["startup"] = consumer_worker_loss.ready(name, owner, identity, deadline)
        remaining = max(.001, deadline - time.monotonic())
        probe = [
            *reference.DOCKER, "exec", "--env",
            "FACTORYBOX_NATIVE_RESTORE_ROOT=/var/lib/sysbox/fixed-store/data/store",
            identity, "/probe.test", "-test.v", "-test.run=^TestFactoryboxNativeRootMapping$",
            "-test.timeout=" + str(remaining) + "s",
        ]
        result["probe_command"] = probe
        with (artifact / "stdout.txt").open("x") as out, (artifact / "stderr.txt").open("x") as err:
            done = subprocess.run(probe, stdout=out, stderr=err, timeout=max(.001, deadline - time.monotonic()))
        result["exit_code"] = done.returncode
        if done.returncode:
            raise ValueError("native root mapping failed")
        require_coverage((artifact / "stdout.txt").read_text())
        consumer_worker_loss.exact_worker(name, owner, identity)
        reference.docker(["container", "stop", "--time=30", identity], timeout=min(40, deadline - time.monotonic()))
        state = consumer_worker_loss.exact_worker(name, owner, identity)["State"]
        if state["Running"] or state["Pid"] or state["OOMKilled"] or state["ExitCode"]:
            raise ValueError("native root fixture did not close its original store cleanly")
        result["input_unchanged"] = reference.sha(binary) == expected
        result["passed"] = result["input_unchanged"]
    finally:
        if container_created:
            logs = subprocess.run([*reference.DOCKER, "logs", name], capture_output=True, text=True, timeout=5)
            (artifact / "worker.log").write_text(logs.stdout + logs.stderr)
            reference.owned_delete("container", name, owner)
        if volume_created:
            reference.owned_delete("volume", name, owner)
        result["cleanup"] = True
        (artifact / "result.json").write_text(json.dumps(result, indent=2) + "\n")
        print(json.dumps({"evidence": str(artifact), "passed": result["passed"], "cleanup": result["cleanup"]}), flush=True)
    if not result["passed"]:
        raise SystemExit(1)


if __name__ == "__main__":
    main()
