#!/usr/bin/env python3
"""Build the pinned CRIU source in the existing isolated Linux builder."""
import datetime
import json
import pathlib
import shutil
import subprocess
import uuid

import reference
import sources

ROOT = reference.ROOT
BUILDER = reference.BASE_ID
HISTORICAL = "2b74a9712aa928245cdc43db1702083f4b87f454f4b077b7fe12b71dbfef3ab1"


def main():
    owner = uuid.uuid4().hex
    name = "factorybox-build-criu-" + owner
    output = ROOT / "artifacts" / name
    output.mkdir(parents=True)
    lock = ROOT / "upstream/lock.json"
    shutil.copyfile(lock, output / "source-lock.json")
    component = json.loads(lock.read_text())["components"]["criu"]
    result = {
        "schema": 1, "owner": owner, "container_name": name, "builder": BUILDER,
        "started_at": datetime.datetime.now(datetime.timezone.utc).isoformat(),
        "source_lock_sha256": reference.sha(lock), "passed": False,
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
            "--entrypoint", "/bin/sh", BUILDER, "-ec",
            "cc --version > /output/toolchain.txt\n"
            "make --version >> /output/toolchain.txt\n"
            "protoc --version >> /output/toolchain.txt\n"
            "cp -R /source /tmp/criu-source\n"
            "make -C /tmp/criu-source -j2 criu/criu > /output/build.log 2>&1\n"
            "cp /tmp/criu-source/criu/criu /output/criu\n"
            "/output/criu --version > /output/version.txt\n",
        ]
        result["command"] = command
        (output / "intent.json").write_text(json.dumps(result, indent=2) + "\n")
        print(json.dumps({"building": str(output), "builder": BUILDER}), flush=True)
        attempted = True
        completed = subprocess.run(command, text=True, capture_output=True, timeout=300)
        (output / "stdout.txt").write_text(completed.stdout)
        (output / "stderr.txt").write_text(completed.stderr)
        result["exit_code"] = completed.returncode
        if completed.returncode == 125:
            raise ValueError("builder container did not start: " + completed.stderr[-4096:])
        result["container_state"] = json.loads(
            reference.docker(["container", "inspect", name]))[0]["State"]
        if completed.returncode or result["container_state"]["OOMKilled"]:
            raise ValueError("native CRIU build failed")
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
