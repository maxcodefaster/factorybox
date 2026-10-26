#!/usr/bin/env python3
"""Run native release-image gates against exact ordinary builds; never publish."""
import argparse
import json
from pathlib import Path
import re
import shutil
import subprocess
import sys
import time
import uuid

import build_config
import build_worker
import native_base
import reference
import reference_lifecycle


RUNTIME_SUITES = [
    "timer-detach", "timer-admission-reader", "capacity", "retirement",
    "restore-hold", "stopped-memory", "timer-state-reader", "timer-exit-guard",
    "timer-signal", "timer-admission-signal", "timer-remainder",
    "timer-remainder-protection", "timer-orphan", "timer-guard-loss",
]


def native_commands(options, worker, bats_images):
    python = sys.executable
    upstream_target = ["--candidate-platform", "linux/amd64"] if options.platform == "linux/amd64" else []
    bounded = ["--builder", options.builder, "--bounded-build", "--network", "host"]
    base = ["--native-base-build", worker["native_base_build"]]
    sysbox = ["--sysbox-build", worker["sysbox_build"]]
    commands = [
        ("criu-core", [python, "tools/criu_core.py", *base,
                       "--retained-candidate", worker["criu_build"], *upstream_target]),
        ("sysbox-unit", [python, "tools/upstream_unit.py", *sysbox, "--suite", "all",
                         *upstream_target, *bounded]),
        ("sysbox-integration", [python, "tools/upstream_unit.py", *sysbox,
                               "--suite", "runc-integration", *upstream_target, *bounded]),
        ("bats-lifecycle", [python, "tools/upstream_bats.py", *base, *sysbox,
                           "--suite", "lifecycle", *upstream_target, *bounded]),
        ("bats-systemd", [python, "tools/upstream_bats.py", *base, *sysbox,
                         "--suite", "systemd", "--systemd-network-profile",
                         "--images", bats_images, *upstream_target, *bounded]),
    ]
    for suite in RUNTIME_SUITES:
        commands.append(("runtime-" + suite, [python, "tools/bounded_store_native.py",
            "--worker-build", options.worker_build, "--suite", suite]))
    for loss in ["planned", "abrupt"]:
        commands.append(("recovery-" + loss, [python, "tools/bounded_store_loss.py",
            "--worker-build", options.worker_build, "--loss", loss]))
    for role, reserve in [("factory-source", []), ("factory-reserve", ["--reserve"])]:
        commands.append((role, [python, "tools/factory_consumer_native.py",
            "--worker-build", options.worker_build, "--consumer-build", options.consumer_build,
            "--project-images", options.project_images, "--factory-repo", str(options.factory_repo),
            *reserve]))
    return commands


def completed_receipt(log):
    for line in reversed(Path(log).read_text().splitlines()):
        try:
            emitted = json.loads(line)
        except json.JSONDecodeError:
            continue
        if not isinstance(emitted, dict) or "evidence" not in emitted:
            continue
        if emitted.get("passed") is not True or emitted.get("cleanup") is not True:
            raise ValueError("native release gate did not pass with confirmed cleanup")
        artifact = Path(emitted["evidence"])
        path = artifact / "result.json"
        if (artifact.parent != reference.ROOT / "artifacts" or artifact.resolve() != artifact or
                artifact.is_symlink() or path.is_symlink()):
            raise ValueError("native release receipt left its exact artifact directory")
        result = json.loads(path.read_text())
        if result.get("passed") is not True or result.get("cleanup") is not True:
            raise ValueError("native release result differs from its successful receipt")
        return artifact, result
    raise ValueError("native release gate emitted no exact completed receipt")


def validate_doctor(worker, version, output):
    lines = output.splitlines()
    if (worker.get("native_candidate") is not None or
            not re.fullmatch(r"[0-9]+\.[0-9]+\.[0-9]+(?:-[a-z0-9.-]+)?", version) or len(lines) != 3):
        raise ValueError("release requires an ordinary worker and one exact CLI version")
    doctor = json.loads(lines[0])
    platform = doctor.get("platform", {})
    if (doctor.get("matches_reference") is not True or platform.get("os") != "linux" or
            platform.get("architecture") != build_config.architecture(worker["platform"]) or
            platform.get("page_size") != 4096 or not platform.get("kernel") or
            lines[1] != "factorybox v" + version or
            lines[2].split() != [worker.get("original_license_sha256"),
                                 "/opt/factorybox/licenses/factorybox/LICENSE"]):
        raise ValueError("release image does not admit this native tuple or match its release version")


def require_original_license(worker):
    original = reference.ROOT / "LICENSE"
    if (original.is_symlink() or not original.is_file() or original.stat().st_size == 0 or
            reference.sha(original) != worker.get("original_license_sha256") or
            worker.get("bundle_sha256", {}).get("licenses/factorybox/LICENSE") != worker["original_license_sha256"]):
        raise ValueError("release requires the exact approved original-code LICENSE inside the delivered image")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--platform", choices=build_config.PLATFORMS, required=True)
    parser.add_argument("--version", required=True)
    parser.add_argument("--worker-build", required=True)
    parser.add_argument("--consumer-build", required=True)
    parser.add_argument("--project-images", required=True)
    parser.add_argument("--factory-repo", type=Path, required=True)
    parser.add_argument("--builder", required=True)
    options = parser.parse_args()
    _, worker = build_worker.load(options.worker_build)
    if worker["platform"] != options.platform or worker.get("native_candidate") is not None:
        raise ValueError("release qualification refuses cross-target or unqualified candidate workers")
    require_original_license(worker)
    host = json.loads(reference.docker(["info", "--format", "{{json .}}"]))
    native_base.require_native_host(options.platform, host)
    options.bounded_build = True
    builder = build_config.verify_builder(options, reference.DOCKER)
    owner = uuid.uuid4().hex
    artifact = reference.new_artifact("factorybox-release-native-" + owner)
    artifact.mkdir()
    shutil.copyfile(__file__, artifact / "runner.py")
    result = {"schema": 1, "owner": owner, "passed": False, "cleanup": False,
              "version": options.version, "platform": options.platform,
              "worker_build": options.worker_build, "image": worker["image"],
              "image_id": worker["image_id"], "executable_sha256": worker["bundle_sha256"]["factorybox"],
              "original_license_sha256": worker["original_license_sha256"],
              "bounded_builder": builder, "runner_sha256": reference.sha(artifact / "runner.py"),
              "production_qualified": False, "stages": [],
              "scope": "Native ordinary release-image functional and upstream gates on the exact delivered "
                       "image. No runtime installation, automatic admission, publication, cross-node recovery "
                       "or completion of the separate endurance/security/production release programme."}
    helper = "factorybox-release-inspect-" + owner
    attempted = False

    def stage(role, command):
        row = {"role": role, "command": command, "passed": False}
        result["stages"].append(row)
        reference_lifecycle.write_json(artifact / "intent.json", result)
        started = time.monotonic()
        with (artifact / (role + ".stdout")).open("x") as stdout, (artifact / (role + ".stderr")).open("x") as stderr:
            done = subprocess.run(command, cwd=reference.ROOT, stdout=stdout, stderr=stderr)
        row.update(exit_code=done.returncode, seconds=time.monotonic() - started)
        done.check_returncode()
        evidence, _ = completed_receipt(artifact / (role + ".stdout"))
        row.update(passed=True, evidence=evidence.name, result_sha256=reference.sha(evidence / "result.json"))
        print(json.dumps({"native_gate": role, "evidence": str(evidence), "passed": True}), flush=True)
        return evidence.name

    print(json.dumps({"qualifying": str(artifact)}), flush=True)
    try:
        inspected = json.loads(reference.docker(["image", "inspect", worker["image"]]))[0]
        volumes = inspected["Config"].get("Volumes") or {}
        if any(not re.fullmatch(r"/[A-Za-z0-9_./-]+", path) or path == "/" for path in volumes):
            raise ValueError("worker image declares an invalid inspection volume")
        command = ["run", "--pull=never", "--name", helper, "--label", "com.factorybox.run=" + owner,
                   "--network=none", "--read-only", "--cpus=1", "--memory=128m", "--pids-limit=32",
                   "--cap-drop=ALL", "--security-opt=no-new-privileges",
                   *[argument for path in volumes for argument in ["--tmpfs", path + ":rw,size=16m"]],
                   "--entrypoint", "/bin/sh", worker["image"], "-ec",
                   "/opt/factorybox/factorybox doctor; /opt/factorybox/factorybox version; "
                   "sha256sum /opt/factorybox/licenses/factorybox/LICENSE"]
        result["inspection_command"] = command
        reference_lifecycle.write_json(artifact / "intent.json", result)
        attempted = True
        doctor = reference.docker(command, timeout=65)
        (artifact / "doctor.stdout").write_text(doctor + "\n")
        validate_doctor(worker, options.version, doctor)
        reference.owned_delete("container", helper, owner)
        attempted = False
        target = ["--candidate-platform", "linux/amd64"] if options.platform == "linux/amd64" else []
        images = stage("bats-images", [sys.executable, "tools/bats_images.py",
            "--native-base-build", worker["native_base_build"], *target,
            "--builder", options.builder, "--bounded-build", "--network", "host",
            "--acquisition-network", "host"])
        for role, command in native_commands(options, worker, images):
            stage(role, command)
        if (build_worker.load(options.worker_build)[1] != worker or
                build_config.verify_builder(options, reference.DOCKER) != builder or
                reference.sha(Path(__file__)) != result["runner_sha256"]):
            raise ValueError("native release image, builder or gate runner changed during qualification")
        result["passed"] = True
    except Exception as error:
        result["error"] = str(error)
        raise
    finally:
        try:
            if attempted:
                reference.owned_delete("container", helper, owner)
            result["cleanup"] = True
        except Exception as error:
            result["cleanup_error"] = str(error)
            result["passed"] = False
        reference_lifecycle.write_json(artifact / "result.json", result)
        print(json.dumps({"evidence": str(artifact), "passed": result["passed"],
                          "cleanup": result["cleanup"], "image": result["image"]}), flush=True)
        if not result["cleanup"]:
            raise RuntimeError("release inspection cleanup is unconfirmed: " + str(artifact))


if __name__ == "__main__":
    main()
