#!/usr/bin/env python3
"""Check native fault-injection waiting cost and deadlines without a Docker workload."""
import argparse
import json
import os
import shutil
import subprocess
import uuid

import native_base
import reference
import upstream_unit

ROOT = reference.ROOT


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--native-base-build", required=True)
    options = parser.parse_args()
    image, _ = native_base.load(options.native_base_build)
    owner = uuid.uuid4().hex
    name = "factorybox-fault-wait-" + owner
    archive = reference.new_artifact(name)
    archive.mkdir(parents=True)
    context = archive / "context"
    result = {
        "schema": 1, "owner": owner, "passed": False, "cleanup": False,
        "production_qualified": False, "native_base_build": options.native_base_build,
        "scope": "Exact-PID ptrace wait with at least 2000 syscall stops, a 1s diagnostic "
                 "cost budget and a separate no-stop/stale-notification deadline check. "
                 "20s Go / 90s outer, 3 CPU/3 GiB. No Docker recovery or runtime latency qualification.",
    }
    attempted, failure = False, None
    try:
        shutil.copyfile(__file__, archive / "runner.py")
        shutil.copytree(ROOT / "internal", context / "internal")
        shutil.copytree(ROOT / "tests/devx-lifecycle", context / "tests/devx-lifecycle")
        for filename in ["go.mod", "go.sum"]:
            shutil.copyfile(ROOT / filename, context / filename)
        result["source_context"] = upstream_unit.context_manifest(context)
        binary = archive / "lifecycle.test"
        subprocess.run(
            ["go", "test", "-c", "-trimpath", "-buildvcs=false", "-o", str(binary),
             "./tests/devx-lifecycle"], cwd=context,
            env=dict(os.environ, GOOS="linux", GOARCH="arm64", CGO_ENABLED="0"),
            check=True, timeout=120)
        shutil.copyfile(ROOT / "tests/private-cgroup-delegation.sh", archive / "delegate.sh")
        inputs = {path.name: reference.sha(path) for path in archive.iterdir() if path.is_file()}
        result["inputs_sha256"] = inputs
        command = [
            *reference.DOCKER, "run", "--pull=never", "--name", name,
            "--label", "com.factorybox.run=" + owner, "--privileged", "--cgroupns=private",
            "--cpus=3", "--memory=3g", "--pids-limit=64", "--network=none",
            "--env", "FACTORYBOX_NATIVE_FAULT_WAIT=1", "--env", "FACTORYBOX_UPSTREAM_CONTAINER=1",
            "--env", "GOMAXPROCS=2", "--env", "GOMEMLIMIT=256MiB",
            "--mount", f"type=bind,source={binary},target=/lifecycle.test,readonly",
            "--mount", f"type=bind,source={archive / 'delegate.sh'},target=/delegate.sh,readonly",
            "--entrypoint", "/bin/bash", image, "-ec",
            ". /delegate.sh\nmount --make-rprivate /\n"
            "exec setpriv --bounding-set=-sys_module,-sys_rawio,-sys_boot,-sys_time,-syslog "
            "/lifecycle.test -test.v -test.timeout=20s "
            "-test.run '^TestNativeFaultWait(Cost|Deadline)$'",
        ]
        result["command"] = command
        (archive / "intent.json").write_text(json.dumps(result, indent=2) + "\n")
        print(json.dumps({"running": str(archive)}), flush=True)
        attempted = True
        with (archive / "stdout.txt").open("x") as stdout, (archive / "stderr.txt").open("x") as stderr:
            done = subprocess.run(command, stdout=stdout, stderr=stderr, timeout=90)
        result["exit_code"] = done.returncode
        result["container_state"] = json.loads(reference.docker(["inspect", name]))[0]["State"]
        result["inputs_unchanged"] = (
            upstream_unit.context_manifest(context) == result["source_context"] and
            all(reference.sha(archive / filename) == digest for filename, digest in inputs.items()))
        text = (archive / "stdout.txt").read_text()
        result["passed"] = (
            done.returncode == 0 and result["inputs_unchanged"] and
            not result["container_state"]["OOMKilled"] and "--- SKIP:" not in text and
            all(f"--- PASS: TestNativeFaultWait{case} " in text for case in ["Cost", "Deadline"]))
        if not result["passed"]:
            raise ValueError("native tracing wait cost or deadline regression failed")
    except Exception as error:
        failure = error
        result["error"] = str(error)
    finally:
        try:
            if attempted:
                reference.owned_delete("container", name, owner)
            result["cleanup"] = True
        except Exception as error:
            failure = failure or error
            result["cleanup_error"] = str(error)
        result["passed"] = result["passed"] and failure is None and result["cleanup"]
        (archive / "result.json").write_text(json.dumps(result, indent=2) + "\n")
        print(json.dumps({"evidence": str(archive), **{key: result[key] for key in [
            "passed", "cleanup", "production_qualified"]}}), flush=True)
        reference.artifact_storage.compact_closed_run(archive, ROOT)
    if failure:
        raise RuntimeError("native tracing wait regression failed: " + str(archive)) from failure


if __name__ == "__main__":
    main()
