#!/usr/bin/env python3
"""Exercise bounded storage pressure on the delivered worker's exact native core."""
import argparse
import json
import os
from pathlib import Path
import shutil
import subprocess
import time
import uuid

import build_worker
import consumer_worker_loss
import native_base
import reference
import reference_lifecycle
import upstream_bats
import upstream_unit


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--worker-build", required=True)
    args = parser.parse_args()
    _, worker = build_worker.load(args.worker_build)
    native_base.require_native_host("linux/amd64", json.loads(reference.docker(["info", "--format", "{{json .}}"])))
    owner = uuid.uuid4().hex
    name = "factorybox-storage-admission-" + owner
    artifact = reference.new_artifact(name)
    artifact.mkdir()
    source = artifact / "source"
    shutil.copyfile(__file__, artifact / "runner.py")
    shutil.copyfile(reference.ROOT / "tests/storage-admission-setup.sh", artifact / "setup.sh")
    for directory in ["internal", "cmd/factorybox"]:
        shutil.copytree(reference.ROOT / directory, source / directory)
    for file in ["go.mod", "go.sum"]:
        shutil.copyfile(reference.ROOT / file, source / file)
    source_context = upstream_unit.context_manifest(source)
    core = lambda manifest: {key: value for key, value in manifest.items() if not key.endswith("_test.go")}
    if core(source_context) != core(worker["source_context"]):
        raise ValueError("pressure probe core differs from the delivered worker")
    busybox = json.loads((reference.ROOT / "upstream/test-inputs.json").read_text())["busybox-amd64"]
    rootfs = reference.ROOT / ".cache/downloads" / busybox["archive"]
    if reference.sha(rootfs) != busybox["sha256"]:
        raise ValueError("native pressure rootfs input changed")
    result = {"schema": 1, "passed": False, "cleanup": False, "production_qualified": False,
              "owner": owner, "worker_build": args.worker_build, "worker_image_id": worker["image_id"],
              "source_context": source_context, "rootfs_sha256": busybox["sha256"],
              "runner_sha256": reference.sha(artifact / "runner.py"), "setup_sha256": reference.sha(artifact / "setup.sh"),
              "scope": "One source on a private 128-MiB tmpfs: preflight refusal, exact completed replay, "
                       "separate file-backed resume admission and actual post-admission ENOSPC. No host disk fill, "
                       "hard store quota, distribution or production qualification."}
    before = consumer_worker_loss.sysctls()
    result["global_sysctls_before"] = before
    container_attempted = volume_attempted = False
    preserve = False
    print(json.dumps({"running": str(artifact)}), flush=True)
    try:
        binary = artifact / "probe.test"
        env = dict(os.environ, GOOS="linux", GOARCH="amd64", CGO_ENABLED="0")
        subprocess.run(["go", "test", "-c", "-trimpath", "-buildvcs=false", "-tags=factorybox_worker_probe",
                        "-o", str(binary), "./internal/lifecycle"], cwd=source, env=env, check=True, timeout=120)
        result["probe_sha256"] = reference.sha(binary)
        output = artifact / "output"
        output.mkdir()
        volume_attempted = True
        reference.docker(["volume", "create", "--label", "com.factorybox.run=" + owner, name])
        command = ["run", "--detach", "--pull=never", "--name", name, "--label", "com.factorybox.run=" + owner,
                   "--privileged", "--cgroupns=private", "--cpus=3", "--memory=3g", "--pids-limit=512", "--network=none",
                   "--env", "GOMAXPROCS=2", "--env", "GOMEMLIMIT=256MiB", "--env", "FACTORYBOX_NATIVE_LIFECYCLE=1",
                   "--env", "FACTORYBOX_NATIVE_STORAGE=1",
                   "--env", "FACTORYBOX_RUNTIME_SHA256=" + worker["bundle_sha256"]["components/sysbox-runc"],
                   "--env", "FACTORYBOX_CRIU_SHA256=" + worker["bundle_sha256"]["components/criu"],
                   "--mount", f"type=volume,source={name},target=/var/lib/sysbox"]
        for path, target, readonly in [(binary, "/probe.test", True), (artifact / "setup.sh", "/setup.sh", True),
                                       (rootfs, "/busybox-rootfs.tar.gz", True), (output, "/output", False)]:
            command += ["--mount", f"type=bind,source={path},target={target}" + (",readonly" if readonly else "")]
        command += [worker["image"], "--store-id", owner]
        result["command"] = command
        container_attempted = True
        identity = reference.docker(command)
        result["container_id"] = identity
        deadline = time.monotonic() + 420
        result["startup"] = consumer_worker_loss.ready(name, owner, identity, deadline)
        capacity = subprocess.run([*reference.DOCKER, "exec", identity, "/opt/factorybox/factorybox",
                                   "--worker-run-root", "/run/factorybox", "capacity"], capture_output=True, text=True, check=True, timeout=5)
        result["delivered_cli_capacity"] = json.loads(capacity.stdout)
        if (result["delivered_cli_capacity"]["min_free_bytes"] != 12 << 30 or
                result["delivered_cli_capacity"]["write_reserve_bytes"] != 4 << 30 or
                not result["delivered_cli_capacity"]["policy_enabled"]):
            raise ValueError("delivered worker did not publish its required local admission policy")
        with (artifact / "stdout.txt").open("x") as stdout, (artifact / "stderr.txt").open("x") as stderr:
            done = subprocess.run([*reference.DOCKER, "exec", identity, "/bin/bash", "/setup.sh"], stdout=stdout, stderr=stderr,
                                  timeout=max(.1, deadline - time.monotonic()))
        result["exit_code"] = done.returncode
        state = consumer_worker_loss.exact_worker(name, owner, identity)
        result["state"] = state["State"]
        if done.returncode or state["State"]["OOMKilled"] or not state["State"]["Running"]:
            raise ValueError("native bounded storage-pressure assertions failed")
        proof = json.loads((output / "storage.json").read_text())
        resume = json.loads((output / "resume-admission.json").read_text())
        reserve = json.loads((output / "reserve-ownership.json").read_text())
        if (not proof.get("passed") or proof["preflight_rejections"] != 5 or "no space left" not in proof["actual_enospc"].lower() or
                not resume.get("passed") or resume["preflight_rejections"] != 1 or
                not all(reserve.get(key) is True for key in ["passed", "replacement_preserved", "shared_inode_refused", "replay_verified"])):
            raise ValueError("native pressure proof lacks actual ENOSPC and required preflight cases")
        result["pressure_result_sha256"] = reference.sha(output / "storage.json")
        result["resume_result_sha256"] = reference.sha(output / "resume-admission.json")
        result["reserve_result_sha256"] = reference.sha(output / "reserve-ownership.json")
        result["passed"] = True
    except Exception as error:
        result["error"] = str(error)
        raise
    finally:
        try:
            if container_attempted:
                logs = subprocess.run([*reference.DOCKER, "logs", name], capture_output=True, text=True, timeout=5)
                (artifact / "worker.log").write_text(logs.stdout + logs.stderr)
                reference.owned_delete("container", name, owner)
            if volume_attempted and not result["passed"]:
                try:
                    upstream_bats.archive_stopped_volumes(artifact, name, owner, [name], worker["image"], result,
                                                          timeout=120, compressed=True)
                except Exception as error:
                    preserve = True
                    result["archive_error"] = str(error)
            if volume_attempted and not preserve:
                reference.owned_delete("volume", name, owner)
            result["cleanup"] = not preserve
        finally:
            result["global_sysctls_after"] = consumer_worker_loss.sysctls()
            result["inputs_unchanged"] = (upstream_unit.context_manifest(source) == source_context and
                                          reference.sha(artifact / "runner.py") == result["runner_sha256"] and
                                          reference.sha(artifact / "setup.sh") == result["setup_sha256"])
            result["passed"] = (result["passed"] and result["cleanup"] and result["inputs_unchanged"] and
                                result["global_sysctls_after"] == before)
            reference_lifecycle.write_json(artifact / "result.json", result)
            print(json.dumps({"evidence": str(artifact), "passed": result["passed"], "cleanup": result["cleanup"],
                              "error": result.get("error")}), flush=True)


if __name__ == "__main__":
    main()
