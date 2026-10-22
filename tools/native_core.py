#!/usr/bin/env python3
"""Exercise extracted platform admission and logical page readers on Linux."""
import argparse
import datetime
import json
import os
import pathlib
import re
import shutil
import subprocess
import uuid

import reference
import native_base
import upstream_unit

ROOT = reference.ROOT
EXPECTED_NATIVE_TESTS = 13


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--native-base-build")
    parser.add_argument("--candidate-platform", choices=["linux/amd64"])
    parser.add_argument("--ordinary-build", action="store_true")
    options = parser.parse_args()
    if bool(options.native_base_build) != bool(options.candidate_platform):
        parser.error("AMD64 candidate requires its exact public native base")
    if options.ordinary_build and not options.candidate_platform:
        parser.error("--ordinary-build requires the explicit AMD64 target")
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
        execute(name, owner, evidence, result, options)
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
        result["passed"] = bool(result["passed"] and result["cleanup"] and failure is None)
        (evidence / "result.json").write_text(json.dumps(result, indent=2) + "\n")
        print(json.dumps({"evidence": str(evidence), **result}), flush=True)
    if failure:
        raise RuntimeError("native core gate failed; preserved evidence: " + str(evidence)) from failure


def execute(container_name, owner, evidence, result, options=None):
    if options is None:
        options = argparse.Namespace(candidate_platform=None, native_base_build=None, ordinary_build=False)
    target = options.candidate_platform or "linux/arm64"
    image = reference.BASE_ID
    compile_options = []
    context = ROOT
    if options.candidate_platform:
        host = json.loads(reference.docker(["info", "--format", "{{json .}}" ]))
        native_base.require_native_host(target, host)
        image, _ = native_base.load(options.native_base_build, candidate_platform=target)
        if not re.fullmatch(r"[A-Za-z0-9_.+-]+", host["KernelVersion"]):
            raise ValueError("unsupported candidate kernel release encoding")
        if not options.ordinary_build:
            compile_options = ["-tags=factorybox_candidate_amd64", "-ldflags=-X=" +
                               "factorybox/internal/platform.candidateKernel=" + host["KernelVersion"]]
        context = evidence / "context"
        for directory in ["internal", "cmd/factorybox"]:
            shutil.copytree(ROOT / directory, context / directory)
        for name in ["go.mod", "go.sum"]:
            shutil.copyfile(ROOT / name, context / name)
        result["source_context"] = upstream_unit.context_manifest(context)
        result["platform_selection"] = "Actual execution tuple check replaces only the historical ARM64 host check. All static historical tuple assertions remain executed."
    output = evidence / "binaries"
    output.mkdir(parents=True, exist_ok=True)
    env = dict(os.environ, GOOS="linux", GOARCH=target.split("/")[1], CGO_ENABLED="0")
    for package, binary_name in [("./internal/platform", "platform.test"),
                                 ("./internal/pageview", "pageview.test"),
                                 ("./internal/snapshot", "snapshot.test")]:
        subprocess.run(["go", "test", "-c", "-trimpath", "-buildvcs=false", *compile_options,
                        "-o", str(output / binary_name), package],
                       cwd=context, env=env, check=True, timeout=120)
    subprocess.run(["go", "build", "-trimpath", "-buildvcs=false", *compile_options, "-o",
                    str(output / "factorybox"), "./cmd/factorybox"],
                   cwd=context, env=env, check=True, timeout=120)
    result["phase"] = "preflight"
    result["binaries_sha256"] = {p.name: reference.sha(p) for p in output.iterdir() if p.is_file()}
    # Run the immutable configuration identity directly. An extra tag inspection
    # adds a daemon round trip and cannot strengthen this exact --pull=never input.
    result["base_image_config"] = image
    result["platform"] = target
    result["candidate_platform"] = bool(options.candidate_platform and not options.ordinary_build)
    result["ordinary_build"] = options.ordinary_build
    command = [
        *reference.DOCKER, "run", "--pull=never", "--platform=" + target, "--name", container_name,
        "--label", "com.factorybox.run=" + owner, "--cgroupns=private",
        "--cpus=1", "--memory=128m", "--pids-limit=64", "--network=none",
        "--read-only", "--cap-drop=ALL", "--cap-add=SYS_RESOURCE",
        "--security-opt=no-new-privileges",
        "--tmpfs", "/tmp:rw,nosuid,nodev,size=16m",
        "--tmpfs", "/experiment:rw,nosuid,nodev,size=16m",
        "--env", "FACTORYBOX_NATIVE=1",
        "--env", "FACTORYBOX_EXECUTION_NATIVE=" + ("1" if options.candidate_platform else "0"),
        "--env", "DESCENDANT_READER_NATIVE=1",
        "--env", "GOMAXPROCS=1", "--env", "GOMEMLIMIT=96MiB",
    ]
    for file in ["platform.test", "pageview.test", "snapshot.test", "factorybox"]:
        command += ["--mount", f"type=bind,source={output / file},target=/{file},readonly"]
    policy_test = "OrdinaryExecutionRequiresReference" if options.ordinary_build else "CandidateExecutionRequiresExactLinkedTuple"
    platform_pattern = ("^Test(CurrentExecutionWorker|RejectsUntestedPlatforms|" + policy_test + ")$"
                        if options.candidate_platform else
                        "^Test(CurrentReferenceWorker|RejectsUntestedPlatforms|OrdinaryExecutionRequiresReference)$")
    command += ["--entrypoint", "/bin/sh", image, "-ec",
                "/factorybox doctor\n/platform.test -test.v -test.timeout=15s -test.run='" + platform_pattern + "'\n"
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
        if options.candidate_platform:
            result["inputs_unchanged"] = upstream_unit.context_manifest(context) == result["source_context"]
            result["passed"] = result["passed"] and result["inputs_unchanged"]
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
