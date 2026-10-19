#!/usr/bin/env python3
"""Run the retained source-preservation and exact-cgroup failure regressions."""
import datetime
import json
import subprocess
import uuid

import reference
import reference_lifecycle as runner

ROOT = reference.ROOT


def main():
    reference.verify()
    build = json.loads((reference.OUTPUT / "manifest.json").read_text())
    if build["source_manifest_sha256"] != reference.sha(reference.REFERENCE / "provenance.json"):
        raise ValueError("failure regression build belongs to another source reference")
    binary = reference.OUTPUT / "lab.test"
    if reference.sha(binary) != build["files_sha256"]["lab.test"]:
        raise ValueError("native failure regression binary changed")
    owner = uuid.uuid4().hex
    name = "factorybox-failure-" + owner
    archive = reference.new_artifact(name)
    archive.mkdir(parents=True)
    command = [
        *reference.DOCKER, "run", "--pull=never", "--name", name,
        "--label", "com.factorybox.run=" + owner, "--privileged",
        "--cgroupns=private", "--cpus=1", "--memory=256m", "--pids-limit=128",
        "--network=none", "--read-only", "--tmpfs", "/tmp:rw,nosuid,nodev,size=16m",
        "--tmpfs", "/experiment:rw,nosuid,nodev,size=16m",
        "--env", "EPOCH_STOP_NATIVE=1", "--env", "EPOCH_REPLAY_CGROUP_NATIVE=1",
        "--env", "DESCENDANT_GUARD_NATIVE=1", "--env", "GOMAXPROCS=1",
        "--mount", f"type=bind,source={binary},target=/lab.test,readonly",
        "--entrypoint", "/lab.test", reference.BASE_ID, "-test.v",
        "-test.run=^Test(EpochStopNative|EpochReplayEmptyCgroupNative|DescendantGuardNative)$",
        "-test.timeout=90s",
    ]
    result = {"schema": 1, "owner": owner, "container_name": name, "command": command,
              "binary_sha256": reference.sha(binary),
              "started_at": datetime.datetime.now(datetime.timezone.utc).isoformat(),
              "scope": "Retained guard/controller/tracer loss, foreign-PID rejection and "
                       "identity-safe cgroup cleanup. Not complete runtime qualification.",
              "passed": False, "cleanup": False, "production_qualified": False}
    runner.write_json(archive / "intent.json", result)
    failure = None
    try:
        print(json.dumps({"running": str(archive)}), flush=True)
        done = subprocess.run(command, text=True, capture_output=True, timeout=110)
        (archive / "stdout.txt").write_text(done.stdout)
        (archive / "stderr.txt").write_text(done.stderr)
        result["exit_code"] = done.returncode
        result["passed_top_level_tests"] = sum(
            line.startswith("--- PASS: ") for line in done.stdout.splitlines())
        result["passed_subtests"] = sum(
            line.lstrip().startswith("--- PASS: ") and line.startswith(" ")
            for line in done.stdout.splitlines())
        result["skipped_tests"] = sum("--- SKIP: " in line for line in done.stdout.splitlines())
        if done.returncode == 125:
            raise ValueError("failure fixture did not start: " + done.stderr)
        state = json.loads(reference.docker(["container", "inspect", name]))[0]["State"]
        result["container_state"] = state
        result["passed"] = (done.returncode == 0 and result["passed_top_level_tests"] == 3 and
                            result["skipped_tests"] == 0 and not state["OOMKilled"])
        if not result["passed"]:
            raise ValueError("native source-preservation/failure regressions failed")
    except Exception as error:
        result["error"] = str(error)
        failure = error
        if isinstance(error, subprocess.TimeoutExpired):
            for field, value in [("stdout", error.stdout), ("stderr", error.stderr)]:
                if value:
                    (archive / (field + ".txt")).write_text(
                        value.decode(errors="replace") if isinstance(value, bytes) else value)
    finally:
        try:
            reference.owned_delete("container", name, owner)
            result["cleanup"] = True
        except Exception as error:
            result["cleanup_error"] = str(error)
            failure = failure or error
        runner.write_json(archive / "result.json", result)
        print(json.dumps({"evidence": str(archive), **result}), flush=True)
    if failure:
        raise RuntimeError("native failure regression gate failed: " + str(archive)) from failure


if __name__ == "__main__":
    main()
