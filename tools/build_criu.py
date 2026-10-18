#!/usr/bin/env python3
"""Build pinned CRIU in an explicit isolated Linux toolchain."""
import argparse
import datetime
import json
import pathlib
import shutil
import subprocess
import uuid

import native_base
import reference
import sources

ROOT = reference.ROOT
BUILDER = reference.BASE_ID
HISTORICAL = "2b74a9712aa928245cdc43db1702083f4b87f454f4b077b7fe12b71dbfef3ab1"


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--native-base-build", help="Exact successful public-input native base build")
    options = parser.parse_args()
    builder, base_result = (native_base.load(options.native_base_build)
                            if options.native_base_build else (BUILDER, None))
    owner = uuid.uuid4().hex
    name = "factorybox-build-criu-" + owner
    output = ROOT / "artifacts" / name
    output.mkdir(parents=True)
    lock = ROOT / "upstream/lock.json"
    shutil.copyfile(lock, output / "source-lock.json")
    shutil.copyfile(ROOT / "tests/criu-owned-scope.c", output / "criu-owned-scope.c")
    component = json.loads(lock.read_text())["components"]["criu"]
    result = {
        "schema": 1, "owner": owner, "container_name": name, "builder": builder,
        "native_base_build": options.native_base_build,
        "started_at": datetime.datetime.now(datetime.timezone.utc).isoformat(),
        "source_lock_sha256": reference.sha(lock), "passed": False,
        "scope_test_sha256": reference.sha(output / "criu-owned-scope.c"),
        "cleanup": False, "production_qualified": False,
    }
    attempted = False
    failure = None
    try:
        sources.validate_inputs(component)
        tree = ROOT / ".cache/sources/criu"
        result["unchanged_upstream_test_files"] = sources.check_source(
            tree, ROOT / ".cache/downloads" / component["archive"], component)
        command = [
            *reference.DOCKER, "run", "--pull=never", "--name", name,
            "--label", "com.factorybox.run=" + owner,
            "--cpus=2", "--memory=1g", "--pids-limit=128", "--network=none",
            "--read-only", "--cgroupns=private", "--cap-drop=ALL",
            "--security-opt=no-new-privileges",
            "--tmpfs", "/tmp:rw,exec,nosuid,nodev,size=384m",
            "--mount", f"type=bind,source={tree},target=/source,readonly",
            "--mount", f"type=bind,source={output},target=/output",
            "--entrypoint", "/bin/sh", builder, "-ec",
            "cc --version > /output/toolchain.txt\n"
            "make --version >> /output/toolchain.txt\n"
            "protoc --version >> /output/toolchain.txt\n"
            "cc -std=c11 -Wall -Wextra -Werror -iquote /source/criu/include "
            "/output/criu-owned-scope.c -o /tmp/owned-scope-test\n"
            "/tmp/owned-scope-test > /output/owned-scope.log 2>&1\n"
            "cp -R /source /tmp/criu-source\n"
            "make -C /tmp/criu-source -j2 criu/criu > /output/build.log 2>&1\n"
            "cp /tmp/criu-source/criu/criu /output/criu\n"
            "/output/criu --version > /output/version.txt\n",
        ]
        result["command"] = command
        (output / "intent.json").write_text(json.dumps(result, indent=2) + "\n")
        if base_result:
            result["native_base_packages_sha256"] = base_result["packages_sha256"]
        print(json.dumps({"building": str(output), "builder": builder}), flush=True)
        attempted = True
        completed = subprocess.run(command, text=True, capture_output=True, timeout=300)
        (output / "stdout.txt").write_text(completed.stdout)
        (output / "stderr.txt").write_text(completed.stderr)
        result["exit_code"] = completed.returncode
        scope_log = output / "owned-scope.log"
        result["scope_gate_passed"] = scope_log.exists() and "owned-scope-cases=31 failures=0" in scope_log.read_text()
        if completed.returncode == 125:
            raise ValueError("builder container did not start: " + completed.stderr[-4096:])
        result["container_state"] = json.loads(
            reference.docker(["container", "inspect", name]))[0]["State"]
        if (completed.returncode or result["container_state"]["OOMKilled"] or
                not result["scope_gate_passed"]):
            raise ValueError("native CRIU build failed")
        if reference.sha(output / "criu-owned-scope.c") != result["scope_test_sha256"]:
            raise ValueError("native CRIU scope test input changed")
        result["binary_sha256"] = reference.sha(output / "criu")
        result["matches_historical_binary"] = result["binary_sha256"] == HISTORICAL
        result["version"] = (output / "version.txt").read_text().strip()
        result["toolchain"] = (output / "toolchain.txt").read_text()
        result["passed"] = True
    except Exception as error:
        result["error"] = str(error)
        failure = error
    finally:
        try:
            if attempted:
                reference.owned_delete("container", name, owner)
            result["cleanup"] = True
        except Exception as error:
            result["cleanup_error"] = str(error)
            failure = failure or error
        (output / "result.json").write_text(json.dumps(result, indent=2) + "\n")
        print(json.dumps({"evidence": str(output), **result}), flush=True)
    if failure:
        raise RuntimeError("CRIU build gate failed; evidence: " + str(output)) from failure


if __name__ == "__main__":
    main()
