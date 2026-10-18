#!/usr/bin/env python3
"""Prove the extracted local lifecycle against real Sysbox in an owned fixture."""
import argparse
import datetime
import json
import os
import re
import shutil
import subprocess
import uuid

import reference
import reference_lifecycle
import upstream_unit

ROOT = reference.ROOT
EXPECTED_TESTS = 5


def archive_capture(archive, name, owner, result):
    fixture = json.loads(reference.docker(["container", "inspect", name]))[0]
    if fixture["Config"].get("Labels", {}).get("com.factorybox.run") != owner:
        raise ValueError("capture fixture ownership changed before archive")
    if fixture["State"]["Running"]:
        reference.docker(["container", "stop", "--time=2", name])
        fixture = json.loads(reference.docker(["container", "inspect", name]))[0]
    if fixture["State"]["Running"] or fixture["State"]["Pid"] != 0:
        raise ValueError("capture fixture writers have not stopped")
    volume = json.loads(reference.docker(["volume", "inspect", name]))[0]
    if volume.get("Labels", {}).get("com.factorybox.run") != owner:
        raise ValueError("capture source volume ownership changed")
    helper = name + "-archive"
    command = [
        *reference.DOCKER, "run", "--pull=never", "--name", helper,
        "--label", "com.factorybox.run=" + owner,
        "--cpus=1", "--memory=256m", "--pids-limit=32", "--network=none", "--read-only",
        "--cap-drop=ALL", "--cap-add=DAC_READ_SEARCH", "--security-opt=no-new-privileges",
        "--env", "FACTORYBOX_CAPTURE_ARCHIVE=1",
        "--mount", f"type=volume,source={name},target=/input,readonly",
        "--mount", f"type=bind,source={archive / 'output'},target=/output",
        "--mount", f"type=bind,source={archive / 'capture-archive.sh'},target=/archive.sh,readonly",
        "--entrypoint", "/bin/bash", reference.BASE_ID, "/archive.sh",
    ]
    result["archive_command"] = command
    try:
        with (archive / "archive.log").open("x") as log:
            done = subprocess.run(command, stdout=log, stderr=subprocess.STDOUT, timeout=60)
        state = json.loads(reference.docker(["container", "inspect", helper]))[0]["State"]
        if done.returncode or state["OOMKilled"]:
            raise ValueError("capture source archive failed")
        result["capture_archive_sha256"] = reference.sha(archive / "output/capture-source.tar")
    finally:
        reference.owned_delete("container", helper, owner)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--sysbox-build", required=True)
    parser.add_argument("--suite", choices=["lifecycle", "guard", "capture"], default="lifecycle")
    parser.add_argument("--criu-build", help="Required rebuilt CRIU artifact for --suite capture")
    parser.add_argument("--test", help="One exact native diagnostic test")
    options = parser.parse_args()
    if options.test and not re.fullmatch(r"TestNative[A-Za-z0-9_]+", options.test):
        parser.error("--test must be one exact native test name")
    if options.test and options.suite == "guard":
        parser.error("the guard suite always runs all failure/disarm cases")
    if options.suite == "capture" and (not options.criu_build or options.test):
        parser.error("--suite capture requires --criu-build and runs its complete selected suite")
    if options.criu_build and options.suite != "capture":
        parser.error("--criu-build is only used by --suite capture")
    build, components = reference_lifecycle.component_build(
        options.sysbox_build, "factorybox-build-sysbox-")
    owner = uuid.uuid4().hex
    name = "factorybox-runtime-" + owner
    archive = ROOT / "artifacts" / name
    output = archive / "output"
    output.mkdir(parents=True)
    result = {
        "schema": 1, "owner": owner, "container_name": name, "volume_name": name,
        "started_at": datetime.datetime.now(datetime.timezone.utc).isoformat(),
        "component_build": options.sysbox_build, "test": options.test, "suite": options.suite,
        "scope": ("Extracted source guard's eleven retained native failure/disarm cases and "
                  "foreign-mount rejection. "
                  "Kernel RAM actors only; not complete Sysbox capture qualification."
                  if options.suite == "guard" else
                  "Actual retained-source CRIU capture with a private state view and an 8192-byte "
                  "RAM payload. No filesystem snapshot, fork or restore qualification."
                  if options.suite == "capture" else
                  "Application-independent start/state/delete, saved-outcome recovery, rejection "
                  "before destructive actions and the actual CLI source guard on Sysbox. "
                  "No CRIU capture; not checkpoint/fork/pause/resume qualification."),
        "passed": False, "cleanup": False, "production_qualified": False,
    }
    attempted = False
    volume_attempted = False
    failure = None
    try:
        env = dict(os.environ, GOOS="linux", GOARCH="arm64", CGO_ENABLED="0")
        binary = archive / "lifecycle.test"
        subprocess.run(["go", "test", "-c", "-o", str(binary), "./internal/lifecycle"],
                       cwd=ROOT, env=env, check=True, timeout=120)
        cli = archive / "factorybox"
        subprocess.run(["go", "build", "-trimpath", "-buildvcs=false", "-o", str(cli),
                        "./cmd/factorybox"], cwd=ROOT, env=env, check=True, timeout=120)
        if options.suite == "capture":
            subprocess.run(["go", "test", "-c", "-o", str(archive / "sysboxcapture.test"),
                            "./internal/sysboxcapture"], cwd=ROOT, env=env, check=True, timeout=120)
        for script in ["lifecycle-native.sh", "private-cgroup-delegation.sh"]:
            shutil.copyfile(ROOT / "tests" / script, archive / script)
        if options.suite == "capture":
            shutil.copyfile(ROOT / "tests/capture-archive.sh", archive / "capture-archive.sh")
        input_files = [binary, cli, archive / "lifecycle-native.sh",
                       archive / "private-cgroup-delegation.sh"]
        if options.suite == "capture":
            input_files.append(archive / "sysboxcapture.test")
            input_files.append(archive / "capture-archive.sh")
        result["inputs_sha256"] = {p.name: reference.sha(p) for p in input_files}
        result["source_sha256"] = {str(p.relative_to(ROOT)): reference.sha(p)
                                   for directory in ["internal/lifecycle", "internal/sysboxcapture",
                                                     "cmd/factorybox"]
                                   for p in (ROOT / directory).glob("*.go")}
        for module_file in ["go.mod", "go.sum"]:
            result["source_sha256"][module_file] = reference.sha(ROOT / module_file)
        dockerfile = ROOT / "build/sysbox.Dockerfile"
        if reference.sha(dockerfile) != components["dockerfile_sha256"]:
            raise ValueError("Sysbox builder recipe changed")
        expected = json.loads((build / "context-manifest.json").read_text())
        if upstream_unit.context_manifest(build / "context") != expected:
            raise ValueError("Sysbox context changed")
        command = [*reference.DOCKER, "build", "--provenance=false", "--progress=plain",
                   "--platform=linux/arm64", "--target", "build", "--output",
                   "type=image,compression=uncompressed", "--tag", name,
                   "--metadata-file", str(archive / "builder-metadata.json"),
                   "-f", str(dockerfile), str(build / "context")]
        if options.suite == "capture":
            criu_build, criu = reference_lifecycle.component_build(
                options.criu_build, "factorybox-build-criu-")
            inputs = archive / "components"
            inputs.mkdir()
            for component in ["sysbox-runc", "sysbox-fs", "sysbox-mgr"]:
                source = build / "output" / component
                if reference.sha(source) != components["binary_sha256"][component]:
                    raise ValueError("rebuilt Sysbox executable changed: " + component)
                shutil.copyfile(source, inputs / component)
            if reference.sha(criu_build / "criu") != criu["binary_sha256"]:
                raise ValueError("rebuilt CRIU executable changed")
            shutil.copyfile(criu_build / "criu", inputs / "criu")
            result["component_sha256"] = {p.name: reference.sha(p) for p in inputs.iterdir()}
            result["criu_build"] = options.criu_build
            image = reference.BASE_ID
        else:
            with (archive / "builder.log").open("x") as log:
                subprocess.run(command, stdout=log, stderr=subprocess.STDOUT, check=True, timeout=120)
            image = name + "@" + json.loads((archive / "builder-metadata.json").read_text())[
                "containerimage.digest"]
        result["builder_image_manifest"] = image
        busybox = json.loads((ROOT / "upstream/test-inputs.json").read_text())["busybox-arm64"]
        rootfs = ROOT / ".cache/downloads" / busybox["archive"]
        if reference.sha(rootfs) != busybox["sha256"]:
            raise ValueError("pinned native rootfs changed")
        result["busybox_rootfs_sha256"] = busybox["sha256"]
        command = [
            *reference.DOCKER, "run", "--pull=never", "--name", name,
            "--label", "com.factorybox.run=" + owner, "--privileged", "--cgroupns=private",
            "--cpus=" + ("1" if options.suite == "guard" else "3"),
            "--memory=" + ("256m" if options.suite == "guard" else "3g"),
            "--pids-limit=512", "--network=none",
            "--env", "FACTORYBOX_NATIVE_LIFECYCLE=1",
            "--env", "FACTORYBOX_UPSTREAM_CONTAINER=1",
            "--env", "FACTORYBOX_NATIVE_CAPTURE_GUARD=" + ("1" if options.suite == "guard" else "0"),
            "--env", "FACTORYBOX_NATIVE_RETAINED_CAPTURE=" + ("1" if options.suite == "capture" else "0"),
            "--env", "FACTORYBOX_NATIVE_TEST=" + (options.test or ""),
            "--env", "FACTORYBOX_RUNTIME_SHA256=" + components["binary_sha256"]["sysbox-runc"],
            "--env", "GOMAXPROCS=2", "--env", "GOMEMLIMIT=256MiB",
            "--mount", f"type=volume,source={name},target=/var/lib/sysbox",
            "--mount", f"type=bind,source={output},target=/output",
            "--mount", f"type=bind,source={binary},target=/lifecycle.test,readonly",
            "--mount", f"type=bind,source={cli},target=/factorybox,readonly",
            "--mount", f"type=bind,source={rootfs},target=/busybox-rootfs.tar.gz,readonly",
            "--mount", f"type=bind,source={archive / 'lifecycle-native.sh'},target=/native.sh,readonly",
            "--mount", f"type=bind,source={archive / 'private-cgroup-delegation.sh'},target=/delegate.sh,readonly",
            "--entrypoint", "/bin/bash", image, "/native.sh",
        ]
        if options.suite == "capture":
            extra = [
                "--env", "FACTORYBOX_CRIU_SHA256=" + criu["binary_sha256"],
                "--mount", f"type=bind,source={inputs},target=/component-input,readonly",
                "--mount", f"type=bind,source={archive / 'sysboxcapture.test'},target=/sysboxcapture.test,readonly",
            ]
            command[command.index("--entrypoint"):command.index("--entrypoint")] = extra
        result["command"] = command
        reference_lifecycle.write_json(archive / "intent.json", result)
        print(json.dumps({"running": str(archive)}), flush=True)
        volume_attempted = True
        reference.docker(["volume", "create", "--label", "com.factorybox.run=" + owner, name])
        attempted = True
        with (archive / "stdout.txt").open("x") as log, (archive / "stderr.txt").open("x") as errors:
            done = subprocess.run(command, stdout=log, stderr=errors, timeout=150)
        result["exit_code"] = done.returncode
        text = (archive / "stdout.txt").read_text()
        result["passed_tests"] = sum(line.startswith("--- PASS: ") for line in text.splitlines())
        result["passed_subtests"] = sum(line.startswith(" ") and line.lstrip().startswith("--- PASS: ")
                                         for line in text.splitlines())
        result["skipped_tests"] = sum(line.startswith("--- SKIP: ") for line in text.splitlines())
        state = json.loads(reference.docker(["container", "inspect", name]))[0]["State"]
        result["container_state"] = state
        result["inputs_unchanged"] = all(reference.sha(archive / name) == expected
                                         for name, expected in result["inputs_sha256"].items())
        if options.suite == "capture":
            result["components_unchanged"] = all(
                reference.sha(inputs / component) == expected
                for component, expected in result["component_sha256"].items())
        result["passed"] = (
            done.returncode == 0 and not state["OOMKilled"] and result["inputs_unchanged"] and
            result["passed_tests"] == (2 if options.suite == "capture" else
                                      1 if options.test or options.suite == "guard" else EXPECTED_TESTS) and
            (options.suite != "guard" or result["passed_subtests"] == 12) and
            result.get("components_unchanged", True) and
            result["skipped_tests"] == 0)
        if not result["passed"]:
            raise ValueError("native lifecycle qualification failed")
    except Exception as error:
        failure = error
        result["error"] = str(error)
    finally:
        preserve_volume = False
        try:
            if attempted:
                if options.suite == "capture":
                    try:
                        archive_capture(archive, name, owner, result)
                    except Exception as error:
                        preserve_volume = True
                        result["native_passed"] = result["passed"]
                        result["passed"] = False
                        result["archive_error"] = str(error)
                        result["retained_source_volume"] = name
                        failure = failure or error
                reference.owned_delete("container", name, owner)
            if volume_attempted and not preserve_volume:
                reference.owned_delete("volume", name, owner)
            result["cleanup"] = not preserve_volume
        except Exception as error:
            result["cleanup_error"] = str(error)
            failure = failure or error
        reference_lifecycle.write_json(archive / "result.json", result)
        print(json.dumps({"evidence": str(archive), **result}), flush=True)
    if failure:
        raise RuntimeError("native lifecycle gate failed: " + str(archive)) from failure


if __name__ == "__main__":
    main()
