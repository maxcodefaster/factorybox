#!/usr/bin/env python3
"""Exercise extracted platform admission and logical page readers on Linux."""
import datetime
import json
import os
import pathlib
import subprocess
import uuid

import reference

ROOT = reference.ROOT
EXPECTED_NATIVE_TESTS = 12


def main():
    owner = uuid.uuid4().hex
    name = "factorybox-core-" + owner
    evidence = reference.new_artifact(name)
    evidence.mkdir(parents=True)
    result = {
        "schema": 1, "owner": owner, "container_name": name, "phase": "build",
        "started_at": datetime.datetime.now(datetime.timezone.utc).isoformat(),
        "passed": False, "cleanup": False, "production_qualified": False,
    }
    failure = None
    container_attempted = False
    try:
        execute(name, owner, evidence, result)
        container_attempted = result.get("container_attempted", False)
    except Exception as error:
        failure = error
        result["error"] = str(error)
        container_attempted = result.get("container_attempted", False)
    finally:
        try:
            if container_attempted:
                reference.owned_delete("container", name, owner)
            result["cleanup"] = True
        except Exception as error:
            result["cleanup_error"] = str(error)
            failure = failure or error
        (evidence / "result.json").write_text(json.dumps(result, indent=2) + "\n")
        print(json.dumps({"evidence": str(evidence), **result}), flush=True)
    if failure:
        raise RuntimeError("native core gate failed; preserved evidence: " + str(evidence)) from failure


def execute(container_name, owner, evidence, result):
    output = ROOT / "dist/core/linux-arm64"
    output.mkdir(parents=True, exist_ok=True)
    env = dict(os.environ, GOOS="linux", GOARCH="arm64", CGO_ENABLED="0")
    for package, binary_name in [("./internal/platform", "platform.test"),
                                 ("./internal/pageview", "pageview.test"),
                                 ("./internal/snapshot", "snapshot.test")]:
        subprocess.run(["go", "test", "-c", "-o", str(output / binary_name), package],
                       cwd=ROOT, env=env, check=True, timeout=120)
    subprocess.run(["go", "build", "-trimpath", "-buildvcs=false", "-o",
                    str(output / "factorybox"), "./cmd/factorybox"],
                   cwd=ROOT, env=env, check=True, timeout=120)
    result["phase"] = "preflight"
    result["binaries_sha256"] = {p.name: reference.sha(p) for p in output.iterdir() if p.is_file()}
    # Run the immutable configuration identity directly. An extra tag inspection
    # adds a daemon round trip and cannot strengthen this exact --pull=never input.
    result["base_image_config"] = reference.BASE_ID
    command = [
        *reference.DOCKER, "run", "--pull=never", "--name", container_name,
        "--label", "com.factorybox.run=" + owner, "--cgroupns=private",
        "--cpus=1", "--memory=128m", "--pids-limit=64", "--network=none",
        "--read-only", "--cap-drop=ALL", "--cap-add=SYS_RESOURCE",
        "--security-opt=no-new-privileges",
        "--tmpfs", "/tmp:rw,nosuid,nodev,size=16m",
        "--tmpfs", "/experiment:rw,nosuid,nodev,size=16m",
        "--env", "FACTORYBOX_NATIVE=1", "--env", "DESCENDANT_READER_NATIVE=1",
        "--env", "GOMAXPROCS=1", "--env", "GOMEMLIMIT=96MiB",
    ]
    for file in ["platform.test", "pageview.test", "snapshot.test", "factorybox"]:
        command += ["--mount", f"type=bind,source={output / file},target=/{file},readonly"]
    command += ["--entrypoint", "/bin/sh", reference.BASE_ID, "-ec",
                "/factorybox doctor\n/platform.test -test.v -test.timeout=15s\n"
                "/snapshot.test -test.v -test.timeout=15s\n"
                "/pageview.test -test.v -test.timeout=15s"]
    result["command"] = command
    result["phase"] = "native"
    result["container_attempted"] = True
    try:
        completed = subprocess.run(command, capture_output=True, text=True, timeout=65)
        (evidence / "stdout.txt").write_text(completed.stdout)
        (evidence / "stderr.txt").write_text(completed.stderr)
        result["exit_code"] = completed.returncode
        result["tests_passed"] = sum(line.startswith("--- PASS: ")
                                     for line in completed.stdout.splitlines())
        result["tests_skipped"] = sum(line.startswith("--- SKIP: ")
                                      for line in completed.stdout.splitlines())
        state = json.loads(reference.docker(["container", "inspect", container_name]))[0]["State"]
        result["container_state"] = state
        result["passed"] = (completed.returncode == 0 and
                            result["tests_passed"] == EXPECTED_NATIVE_TESTS
                            and result["tests_skipped"] == 0 and not state["OOMKilled"])
        if not result["passed"]:
            raise ValueError("extracted native core regression failed")
        result["phase"] = "observed"
    except subprocess.TimeoutExpired as error:
        for field, value in [("stdout", error.stdout), ("stderr", error.stderr)]:
            if value:
                (evidence / (field + ".txt")).write_text(
                    value.decode(errors="replace") if isinstance(value, bytes) else value)
        raise


if __name__ == "__main__":
    main()
