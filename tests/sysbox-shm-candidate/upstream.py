#!/usr/bin/env python3
"""Run the unchanged full Sysbox-fs unit module with the exact SHM dependency fix."""
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
import reference
import reference_lifecycle
import upstream_unit


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--shm-build", required=True)
    parser.add_argument("--offline-build", action="store_true",
                        help="Require every pinned module to exist in the retained BuildKit cache")
    parser.add_argument("--prepared-from",
                        help="Reuse an exact prior run's verified compiled test image")
    options = parser.parse_args()
    if not re.fullmatch(r"factorybox-build-shm-[0-9a-f]{32}", options.shm_build):
        parser.error("requires one exact SHM candidate build")
    candidate = ROOT / "artifacts" / options.shm_build
    saved = json.loads((candidate / "result.json").read_text())
    if (not saved["passed"] or not saved["cleanup"] or saved["variant"] != "candidate" or
            upstream_unit.context_manifest(candidate / "inputs") != saved["source_inputs"]):
        raise ValueError("candidate source inputs changed")
    baseline, original = reference_lifecycle.component_build(
        saved["selected_sysbox_build"], "factorybox-build-sysbox-")
    image = saved["builder_image"]
    observed = json.loads(reference.docker(["image", "inspect", image]))[0]
    if (image not in observed.get("RepoDigests", []) or observed["Id"] != saved["builder_config"] or
            observed["Architecture"] != "arm64" or observed["Os"] != "linux"):
        raise ValueError("retained native builder identity changed")
    owner = uuid.uuid4().hex
    name = "factorybox-upstream-shm-" + owner
    archive = reference.new_artifact(name)
    output = archive / "output"
    output.mkdir(parents=True)
    shutil.copyfile(__file__, archive / "runner.py")
    shutil.copyfile(ROOT / "tests/private-cgroup-delegation.sh", archive / "delegate.sh")
    tests = {path: row["sha256"] for path, row in
             upstream_unit.context_manifest(baseline / "context/sysbox-fs").items()
             if path.endswith("_test.go")}
    checksums = archive / "upstream-tests.sha256"
    checksums.write_text("".join(f"{digest}  /src/sysbox-fs/{path}\n"
                                 for path, digest in sorted(tests.items())))
    context = archive / "context"
    shutil.copytree(candidate / "inputs", context / "candidate")
    shutil.copyfile(checksums, context / "upstream-tests.sha256")
    dockerfile = Path(__file__).with_name("upstream.Dockerfile")
    shutil.copyfile(dockerfile, context / "Dockerfile")
    context_before = upstream_unit.context_manifest(context)
    script = archive / "run.sh"
    script.write_text(
        "set -euo pipefail\n"
        ". /delegate.sh > /output/cgroup-delegation.txt\n"
        "sha256sum -c /upstream-tests.sha256 > /output/upstream-tests-before.txt\n"
        "sha256sum /out/sysbox-fs > /output/original-binary.sha256\n"
        "sha256sum /out/sysbox-fs-candidate > /output/candidate-binary.sha256\n"
        "sha256sum -c /out/fs-unit-binaries.sha256 > /output/test-binaries-before.txt\n"
        "cp /out/fs-all-packages.txt /out/fs-test-packages.txt /out/fs-unit-binaries.sha256 /output/\n"
        "status=0\n"
        ": > /output/fs.jsonl\n"
        ": > /output/fs.stderr\n"
        "while IFS='|' read -r package directory index; do\n"
        "  (cd \"$directory\" && go tool test2json -t -p \"$package\" "
        "/out/fs-unit/\"$index\".test -test.v -test.timeout=3m) "
        ">> /output/fs.jsonl 2>> /output/fs.stderr || status=1\n"
        "done < /out/fs-test-packages.txt\n"
        "printf '%s\\n' \"$status\" > /output/fs.exit-code\n"
        "sha256sum -c /upstream-tests.sha256 > /output/upstream-tests-after.txt\n"
        "sha256sum -c /out/fs-unit-binaries.sha256 > /output/test-binaries-after.txt\n"
        "exit \"$status\"\n")
    result = {
        "owner": owner, "passed": False, "cleanup": False, "production_qualified": False,
        "candidate_selected": False, "candidate": options.shm_build,
        "base_sysbox_build": baseline.name, "builder_image": image,
        "candidate_source_inputs": saved["source_inputs"], "upstream_test_files": tests,
        "build_context": context_before,
        "offline_build": options.offline_build,
        "scope": "Full original Sysbox-fs Go unit module with the exact rebuilt FS's patched "
                 "linuxUtils dependency. Unchanged upstream test files checked before/after. "
                 "Pinned retained Linux ARM64 builder, 3 CPU/3 GiB. "
                 "No complete developer, Bats/systemd or production qualification.",
    }
    attempted, failure = False, None
    try:
        if options.prepared_from:
            if not re.fullmatch(r"factorybox-upstream-shm-[0-9a-f]{32}", options.prepared_from):
                raise ValueError("requires one exact prepared FS test run")
            prior_path = ROOT / "artifacts" / options.prepared_from
            prior = json.loads((prior_path / "result.json").read_text())
            if (not prior["cleanup"] or prior["candidate"] != options.shm_build or
                    prior["build_context"] != context_before or not prior["original_binary_unchanged"] or
                    not prior["candidate_binary_reproduced"] or not prior["upstream_tests_unchanged"] or
                    not prior["inputs_unchanged"]):
                raise ValueError("prior compiled test provenance is incomplete or changed")
            image = prior["test_image"]
            actual = json.loads(reference.docker(["image", "inspect", image]))[0]
            if (image not in actual.get("RepoDigests", []) or actual["Architecture"] != "arm64" or
                    actual["Os"] != "linux" or
                    actual["Config"].get("Labels", {}).get("com.factorybox.run") != prior["owner"]):
                raise ValueError("prior compiled test image identity changed")
            result["prepared_from"] = options.prepared_from
            result["prepared_result_sha256"] = reference.sha(prior_path / "result.json")
        else:
            build_name = name + "-tests"
            build_command = [
                *reference.DOCKER, "build", "--provenance=false", "--progress=plain",
                "--platform=linux/arm64",
                "--network=" + ("none" if options.offline_build else "default"),
                "--build-arg", "BASE_IMAGE=" + image, "--build-arg", "RUN_OWNER=" + owner,
                "--output", "type=image,compression=uncompressed", "--tag", build_name,
                "--iidfile", str(archive / "builder-id.txt"),
                "--metadata-file", str(archive / "builder-metadata.json"), str(context),
            ]
            if options.offline_build:
                position = len(reference.DOCKER) + 1
                build_command[position:position] = [
                    "--build-arg", "TEST_GOPROXY=off", "--build-arg", "TEST_GOSUMDB=off"]
            result["build_command"] = build_command
            with (archive / "build.log").open("x") as log:
                subprocess.run(build_command, stdout=log, stderr=subprocess.STDOUT, check=True, timeout=420)
            metadata = json.loads((archive / "builder-metadata.json").read_text())
            image = build_name + "@" + metadata["containerimage.digest"]
        result["test_image"] = image
        command = [
            *reference.DOCKER, "run", "--pull=never", "--name", name,
            "--label", "com.factorybox.run=" + owner, "--privileged", "--cgroupns=private",
            "--cpus=3", "--memory=3g", "--pids-limit=512",
            "--network=none",
            "--env", "FACTORYBOX_UPSTREAM_CONTAINER=1",
            "--env", "GOMAXPROCS=2", "--env", "GOMEMLIMIT=512MiB", "--env", "GOFLAGS=-p=2",
            "--mount", f"type=bind,source={candidate / 'inputs'},target=/candidate,readonly",
            "--mount", f"type=bind,source={output},target=/output",
            "--mount", f"type=bind,source={checksums},target=/upstream-tests.sha256,readonly",
            "--mount", f"type=bind,source={script},target=/run.sh,readonly",
            "--mount", f"type=bind,source={archive / 'delegate.sh'},target=/delegate.sh,readonly",
            "--entrypoint", "/bin/bash", image, "/run.sh",
        ]
        result["command"] = command
        (archive / "intent.json").write_text(json.dumps(result, indent=2) + "\n")
        print(json.dumps({"running": str(archive)}), flush=True)
        attempted = True
        with (archive / "stdout.txt").open("x") as stdout, (archive / "stderr.txt").open("x") as stderr:
            done = subprocess.run(command, stdout=stdout, stderr=stderr, timeout=420)
        result["exit_code"] = done.returncode
        state = json.loads(reference.docker(["container", "inspect", name]))[0]["State"]
        result["container_state"] = state
        result["suites"] = upstream_unit.observations(output)
        result["original_binary_unchanged"] = (
            (output / "original-binary.sha256").read_text().split()[0] ==
            original["binary_sha256"]["sysbox-fs"])
        result["candidate_binary_reproduced"] = (
            (output / "candidate-binary.sha256").read_text().split()[0] ==
            saved["binary_sha256"]["sysbox-fs"])
        result["upstream_tests_unchanged"] = all(
            len((output / f"upstream-tests-{phase}.txt").read_text().splitlines()) == len(tests) and
            all(line.endswith(": OK") for line in
                (output / f"upstream-tests-{phase}.txt").read_text().splitlines())
            for phase in ["before", "after"])
        result["inputs_unchanged"] = (
            upstream_unit.context_manifest(candidate / "inputs") == saved["source_inputs"] and
            upstream_unit.context_manifest(context) == context_before)
        suite = result["suites"].get("fs", {})
        result["passed"] = (
            done.returncode == 0 and not state["OOMKilled"] and
            result["original_binary_unchanged"] and result["upstream_tests_unchanged"] and
            result["candidate_binary_reproduced"] and
            result["inputs_unchanged"] and suite.get("exit_code") == 0 and
            suite.get("passed_top_level_tests") == 67 and suite.get("passed_subtests") == 73 and
            not suite.get("failed") and not suite.get("skipped_tests"))
        if not result["passed"]:
            raise ValueError("complete candidate Sysbox-fs unit module failed")
    except Exception as error:
        failure = error
        result["error"] = str(error)
        result["suites"] = upstream_unit.observations(output)
    finally:
        if attempted:
            reference.owned_delete("container", name, owner)
        result["cleanup"] = True
        (archive / "result.json").write_text(json.dumps(result, indent=2) + "\n")
        print(json.dumps({"evidence": str(archive), **{key: result.get(key) for key in [
            "passed", "cleanup", "upstream_tests_unchanged", "suites", "error"]}}), flush=True)
    if failure:
        raise RuntimeError("candidate upstream FS qualification failed: " + str(archive)) from failure


if __name__ == "__main__":
    main()
