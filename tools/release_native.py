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
import factory_consumer_native
import native_base
import reference
import reference_lifecycle
import upstream_bats


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
    criu_base = (["--criu-base-lock", str(reference.ROOT / "artifacts" / options.worker_build / "criu-base-source-lock.json")]
                 if "criu_base_source_lock_sha256" in worker else [])
    commands = [
        ("criu-core", [python, "tools/criu_core.py", *base,
                       "--retained-candidate", worker["criu_build"], *criu_base, *upstream_target]),
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


def retain_input_image(artifact, result, owner, role, image):
    inspected = json.loads(reference.docker(["image", "inspect", image["image"]]))[0]
    native_base.verify_image(image, inspected)
    volumes = inspected["Config"].get("Volumes") or {}
    if any(not re.fullmatch(r"/[A-Za-z0-9_./-]+", path) or path == "/" for path in volumes):
        raise ValueError("release input image declares an invalid volume")
    name = "factorybox-release-input-" + owner + "-" + role
    command = ["create", "--pull=never", "--name", name,
               "--label", "com.factorybox.run=" + owner,
               "--network=none", "--read-only", "--cpus=.1", "--memory=16m", "--pids-limit=8",
               "--cap-drop=ALL", "--security-opt=no-new-privileges",
               *[argument for path in volumes for argument in ["--tmpfs", path + ":rw,size=16m"]],
               "--entrypoint", "/bin/true", image["image"]]
    # An inspected local image is not a lease. Node image GC can remove an
    # unreferenced input between gates. This owned container is never started.
    hold = {"name": name, "image": image["image"], "image_id": inspected["Id"], "command": command}
    result.setdefault("image_holds", []).append(hold)
    reference_lifecycle.write_json(artifact / "intent.json", result)
    hold["container_id"] = reference.docker(command)
    reference_lifecycle.write_json(artifact / "intent.json", result)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--platform", choices=build_config.PLATFORMS,
                        help="Defaults to the selected Linux Docker host, never the client CPU")
    parser.add_argument("--check-host", action="store_true",
                        help="Report native Docker platform without building, running tests or granting admission")
    parser.add_argument("--version")
    parser.add_argument("--worker-build")
    parser.add_argument("--consumer-build")
    parser.add_argument("--project-images")
    parser.add_argument("--factory-repo", type=Path)
    parser.add_argument("--builder")
    parser.add_argument("--bats-images", help="Reuse an exact verified pinned image archive; all native tests still run")
    options = parser.parse_args()
    if not options.check_host:
        for argument in ["version", "worker_build", "consumer_build", "project_images", "factory_repo", "builder"]:
            if not getattr(options, argument):
                parser.error("--" + argument.replace("_", "-") + " is required unless --check-host is used")
    host = json.loads(reference.docker(["info", "--format", "{{json .}}"]))
    options.platform = options.platform or native_base.native_platform(host)
    native_base.require_native_host(options.platform, host)
    if options.check_host:
        print(json.dumps({"platform": options.platform, "docker_command": reference.DOCKER,
                          "kernel": host.get("KernelVersion"), "cgroup_version": host.get("CgroupVersion"),
                          "native_lifecycle_qualified": False, "production_qualified": False}))
        return
    _, worker = build_worker.load(options.worker_build)
    if worker["platform"] != options.platform or worker.get("native_candidate") is not None:
        raise ValueError("release qualification refuses cross-target or unqualified candidate workers")
    require_original_license(worker)
    _, base = native_base.load(worker["native_base_build"], candidate_platform=options.platform)
    _, consumer = factory_consumer_native.load_consumer(options.consumer_build, options.platform)
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
        # Keep the exited doctor container's exact image reference until every
        # gate finishes. The other selected images need the same lifetime.
        for role, image in [("base", base), *consumer["images"].items()]:
            retain_input_image(artifact, result, owner, role, image)
        target = ["--candidate-platform", "linux/amd64"] if options.platform == "linux/amd64" else []
        if options.bats_images:
            upstream_bats.image_archive(options.bats_images, worker["native_base_build"], platform=options.platform)
            images = options.bats_images
            result["reused_bats_images"] = {"build": images,
                "result_sha256": reference.sha(reference.ROOT / "artifacts" / images / "result.json")}
        else:
            images = stage("bats-images", [sys.executable, "tools/bats_images.py",
                "--native-base-build", worker["native_base_build"], *target,
                "--builder", options.builder, "--bounded-build", "--network", "host",
                "--acquisition-network", "host"])
        for role, command in native_commands(options, worker, images):
            stage(role, command)
        if (build_worker.load(options.worker_build)[1] != worker or
                native_base.load(worker["native_base_build"], candidate_platform=options.platform)[1] != base or
                factory_consumer_native.load_consumer(options.consumer_build, options.platform)[1] != consumer or
                build_config.verify_builder(options, reference.DOCKER) != builder or
                reference.sha(Path(__file__)) != result["runner_sha256"]):
            raise ValueError("native release image, builder or gate runner changed during qualification")
        result["passed"] = True
    except Exception as error:
        result["error"] = str(error)
        raise
    finally:
        cleanup_errors = []
        holds = ([helper] if attempted else []) + [
            hold["name"] for hold in result.get("image_holds", [])
        ]
        for name in holds:
            try:
                reference.owned_delete("container", name, owner)
            except Exception as error:
                cleanup_errors.append(str(error))
        result["cleanup"] = not cleanup_errors
        if cleanup_errors:
            result["cleanup_error"] = "; ".join(cleanup_errors)
            result["passed"] = False
        reference_lifecycle.write_json(artifact / "result.json", result)
        print(json.dumps({"evidence": str(artifact), "passed": result["passed"],
                          "cleanup": result["cleanup"], "image": result["image"]}), flush=True)
        if not result["cleanup"]:
            raise RuntimeError("release inspection cleanup is unconfirmed: " + str(artifact))


if __name__ == "__main__":
    main()
