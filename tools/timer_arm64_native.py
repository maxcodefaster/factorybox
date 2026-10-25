#!/usr/bin/env python3
"""Native owned ARM64 timer CPU regression; never runtime admission."""
import argparse
import json
import os
from pathlib import Path
import shutil
import subprocess
import uuid

import build_config
import native_base
import reference
import reference_lifecycle
import upstream_unit


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--native-base-build", required=True)
    options = parser.parse_args()
    platform = "linux/arm64"
    host = json.loads(reference.docker(["info", "--format", "{{json .}}"]))
    native_base.require_native_host(platform, host)
    image, base = native_base.load(options.native_base_build, for_build=True,
                                  candidate_platform=platform)
    build_config.require_platform(platform, base)
    owner = uuid.uuid4().hex
    name = "factorybox-timer-arm64-native-" + owner
    archive = reference.new_artifact(name)
    archive.mkdir()
    (archive / "runner.py").write_bytes(Path(__file__).read_bytes())
    source = archive / "source"
    shutil.copytree(reference.ROOT / "internal", source / "internal")
    for filename in ["go.mod", "go.sum"]:
        shutil.copyfile(reference.ROOT / filename, source / filename)
    binary = archive / "lifecycle.test"
    result = {"schema": 1, "owner": owner, "platform": platform, "passed": False,
              "cleanup": False, "native_lifecycle_qualified": False, "production_qualified": False,
              "source_context": upstream_unit.context_manifest(source),
              "scope": "Actual owned ARM64 ptrace timer CPU backend only; no admission or installed runtime"}
    attempted = False
    try:
        compile = ["go", "test", "-c", "-trimpath", "-buildvcs=false", "-ldflags=-s -w",
                   "-o", str(binary), "./internal/lifecycle"]
        result["compile_command"] = compile
        with (archive / "compile.stdout").open("x") as out, (archive / "compile.stderr").open("x") as err:
            subprocess.run(compile, cwd=source, check=True, stdout=out, stderr=err,
                           env=dict(os.environ, GOOS="linux", GOARCH="arm64", CGO_ENABLED="0"), timeout=120)
        build_config.require_elf(binary, platform)
        result["binary_sha256"] = reference.sha(binary)
        command = [*reference.DOCKER, "run", "--pull=never", "--platform=" + platform,
                   "--name", name, "--label", "com.factorybox.run=" + owner,
                   "--privileged", "--network=none", "--read-only", "--cpus=1", "--memory=256m",
                   "--pids-limit=64", "--env", "FACTORYBOX_TIMER_ARM64_NATIVE=1",
                   "--mount", f"type=bind,source={binary},target=/lifecycle.test,readonly",
                   "--entrypoint", "/lifecycle.test", image, "-test.v", "-test.timeout=30s",
                   "-test.run=^TestNativeArm64Timer(Remote|CodeABI)$"]
        result["command"] = command
        reference_lifecycle.write_json(archive / "intent.json", result)
        attempted = True
        done = subprocess.run(command, capture_output=True, text=True, timeout=120)
        (archive / "stdout.txt").write_text(done.stdout)
        (archive / "stderr.txt").write_text(done.stderr)
        result["exit_code"] = done.returncode
        result["inputs_unchanged"] = upstream_unit.context_manifest(source) == result["source_context"]
        result["passed"] = (done.returncode == 0 and "--- PASS: TestNativeArm64TimerRemote" in done.stdout
                            and "--- PASS: TestNativeArm64TimerCodeABI" in done.stdout
                            and "--- SKIP:" not in done.stdout and result["inputs_unchanged"])
        if not result["passed"]:
            raise ValueError("native ARM64 timer CPU prerequisite failed")
    except Exception as error:
        if isinstance(error, subprocess.TimeoutExpired):
            (archive / "stdout.txt").write_bytes(error.stdout or b"")
            (archive / "stderr.txt").write_bytes(error.stderr or b"")
        result["error"] = str(error)
        raise
    finally:
        if attempted:
            reference.owned_delete("container", name, owner)
        result["cleanup"] = True
        reference_lifecycle.write_json(archive / "result.json", result)
        print(json.dumps({"evidence": str(archive), "passed": result["passed"], "cleanup": result["cleanup"],
                          "error": result.get("error")}), flush=True)


if __name__ == "__main__":
    main()
