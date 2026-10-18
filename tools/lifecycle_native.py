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
EXPECTED_TESTS = 4


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--sysbox-build", required=True)
    parser.add_argument("--test", help="One exact native diagnostic test")
    options = parser.parse_args()
    if options.test and not re.fullmatch(r"TestNative[A-Za-z0-9_]+", options.test):
        parser.error("--test must be one exact native test name")
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
        "component_build": options.sysbox_build, "test": options.test,
        "scope": "Application-independent start/state/delete only, including saved-outcome recovery "
                 "and rejection before destructive actions. Not checkpoint/fork/pause/resume qualification.",
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
        for script in ["lifecycle-native.sh", "private-cgroup-delegation.sh"]:
            shutil.copyfile(ROOT / "tests" / script, archive / script)
        result["inputs_sha256"] = {p.name: reference.sha(p) for p in
                                   [binary, cli, archive / "lifecycle-native.sh",
                                    archive / "private-cgroup-delegation.sh"]}
        result["source_sha256"] = {str(p.relative_to(ROOT)): reference.sha(p)
                                   for directory in ["internal/lifecycle", "cmd/factorybox"]
                                   for p in (ROOT / directory).glob("*.go")}
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
            "--cpus=3", "--memory=3g", "--pids-limit=512", "--network=none",
            "--env", "FACTORYBOX_NATIVE_LIFECYCLE=1",
            "--env", "FACTORYBOX_UPSTREAM_CONTAINER=1",
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
        result["skipped_tests"] = sum(line.startswith("--- SKIP: ") for line in text.splitlines())
        state = json.loads(reference.docker(["container", "inspect", name]))[0]["State"]
        result["container_state"] = state
        result["inputs_unchanged"] = all(reference.sha(archive / name) == expected
                                         for name, expected in result["inputs_sha256"].items())
        result["passed"] = (
            done.returncode == 0 and not state["OOMKilled"] and result["inputs_unchanged"] and
            result["passed_tests"] == (1 if options.test else EXPECTED_TESTS) and
            result["skipped_tests"] == 0)
        if not result["passed"]:
            raise ValueError("native lifecycle qualification failed")
    except Exception as error:
        failure = error
        result["error"] = str(error)
    finally:
        try:
            if attempted:
                reference.owned_delete("container", name, owner)
            if volume_attempted:
                reference.owned_delete("volume", name, owner)
            result["cleanup"] = True
        except Exception as error:
            result["cleanup_error"] = str(error)
            failure = failure or error
        reference_lifecycle.write_json(archive / "result.json", result)
        print(json.dumps({"evidence": str(archive), **result}), flush=True)
    if failure:
        raise RuntimeError("native lifecycle gate failed: " + str(archive)) from failure


if __name__ == "__main__":
    main()
