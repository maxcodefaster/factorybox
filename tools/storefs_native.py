#!/usr/bin/env python3
"""Test the new fixed-filesystem component on this native host in one owned fixture."""
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
    parser.add_argument("--worker-build", required=True, help="Exact existing native image supplying the formatter and libraries")
    args = parser.parse_args()
    _, worker = build_worker.load(args.worker_build)
    native_base.require_native_host("linux/amd64", json.loads(reference.docker(["info", "--format", "{{json .}}"])))
    if reference.docker(["ps", "--filter", "label=com.factorybox.run", "--format", "{{.ID}}"]):
        raise ValueError("other Factorybox runs must finish before this serialized storage test")
    owner = uuid.uuid4().hex
    name = "factorybox-storefs-native-" + owner
    artifact = reference.new_artifact(name)
    artifact.mkdir()
    source = artifact / "source"
    shutil.copytree(reference.ROOT / "internal/storefs", source / "internal/storefs")
    for file in ["go.mod", "go.sum"]:
        shutil.copyfile(reference.ROOT / file, source / file)
    shutil.copyfile(__file__, artifact / "runner.py")
    manifest = upstream_unit.context_manifest(source)
    output = artifact / "output"
    output.mkdir()
    result = {"schema": 1, "owner": owner, "passed": False, "cleanup": False,
              "production_qualified": False, "source_context": manifest,
              "runtime_image_build": args.worker_build, "image_id": worker["image_id"],
              "runner_sha256": reference.sha(artifact / "runner.py"),
              "global_sysctls_before": consumer_worker_loss.sysctls(),
              "loop_module_before": Path("/sys/module/loop").exists(),
              "scope": "New runtime component, built from its frozen source with vet. Actual fixed-size filesystem ENOSPC, reserve rollback, data persistence, changed image/mount refusal, duplicate/orphan association refusal and last-reference autoclear lease. Existing exact native image supplies tools/libraries only. Not a delivered-worker, Sysbox lifecycle, worker-replacement or production qualification."}
    reference_lifecycle.write_json(artifact / "intent.json", result)
    volume = container = preserve = False
    identity = None
    started = time.monotonic()
    print(json.dumps({"running": str(artifact)}), flush=True)
    try:
        binary = artifact / "storefs.test"
        env = dict(os.environ, GOOS="linux", GOARCH="amd64", CGO_ENABLED="0")
        command = ["go", "test", "-c", "-mod=readonly", "-trimpath", "-buildvcs=false", "-o", str(binary), "./internal/storefs"]
        with (artifact / "build.log").open("x") as log:
            subprocess.run(command, cwd=source, env=env, stdout=log, stderr=subprocess.STDOUT, check=True, timeout=120)
        result["test_binary_sha256"] = reference.sha(binary)
        volume = True
        reference.docker(["volume", "create", "--label", "com.factorybox.run=" + owner, name])
        command = ["create", "--pull=never", "--name", name, "--label", "com.factorybox.run=" + owner,
                   "--privileged", "--cgroupns=private", "--cpus=3", "--memory=3g", "--pids-limit=128", "--network=none", "--read-only",
                   "--env", "GOMAXPROCS=2", "--env", "GOMEMLIMIT=256MiB", "--env", "FACTORYBOX_NATIVE_STOREFS=1",
                   "--mount", f"type=volume,source={name},target=/store",
                   "--mount", f"type=bind,source={binary},target=/probe.test,readonly",
                   "--mount", f"type=bind,source={output},target=/output",
                   "--entrypoint", "/probe.test", worker["image"],
                   "-test.v", "-test.run=^TestNativeStoreFS", "-test.timeout=410s"]
        result["command"] = command
        container = True
        identity = reference.docker(command)
        result["container_id"] = identity
        consumer_worker_loss.exact_worker(name, owner, identity)
        native_started = time.monotonic()
        with (artifact / "stdout.txt").open("x") as stdout, (artifact / "stderr.txt").open("x") as stderr:
            done = subprocess.run([*reference.DOCKER, "start", "--attach", identity], stdout=stdout, stderr=stderr, timeout=420)
        result["native_seconds"] = time.monotonic() - native_started
        result["exit_code"] = done.returncode
        state = consumer_worker_loss.exact_worker(name, owner, identity)["State"]
        result["state"] = state
        log = (artifact / "stdout.txt").read_text()
        result["passed_tests"] = sum(line.startswith("--- PASS: TestNativeStoreFS") for line in log.splitlines())
        result["failed_tests"] = sum(line.startswith("--- FAIL:") for line in log.splitlines())
        result["skipped_tests"] = sum(line.startswith("--- SKIP:") for line in log.splitlines())
        if (done.returncode or state["Running"] or state["OOMKilled"] or state["ExitCode"] != 0 or
                result["passed_tests"] != 4 or result["failed_tests"] or result["skipped_tests"]):
            raise ValueError("native fixed-filesystem assertions failed or were skipped")
        required = {
            "capacity": ["real_enospc", "boundary_canary_enospc", "rollback_after_reserve_release", "retained_unchanged",
                         "remount_persistence", "existing_initialization_refused", "exact_autoclear_cleanup",
                         "loop_direct_io", "independent_verification"],
            "identity": ["second_owner_refused", "shared_inode_refused", "symlink_substitution_refused",
                         "existing_kernel_association_refused", "changed_mount_preserved", "original_data_preserved", "exact_autoclear_cleanup",
                         "unheld_image_lease_refused", "independent_changed_mount_refused"],
            "autoclear": ["unconfirmed_release_refused", "lease_retained_until_release", "cleanup_retry_completed", "exact_reopen"],
            "initialization": ["wrong_formatter_refused_before_creation", "failed_initialization_retained", "uncertain_image_not_reformatted",
                               "uninitialized_attachment_refused", "retained_image_unchanged"],
        }
        result["proof_sha256"] = {}
        for proof_name, keys in required.items():
            file = output / (proof_name + ".json")
            proof = json.loads(file.read_text())
            if proof.get("passed") is not True or any(proof.get(key) is not True for key in keys):
                raise ValueError("native storage proof incomplete: " + proof_name)
            result["proof_sha256"][proof_name] = reference.sha(file)
        result["passed"] = True
    except Exception as error:
        result["error"] = str(error)
    finally:
        try:
            if container:
                if identity:
                    consumer_worker_loss.exact_worker(name, owner, identity)
                reference.owned_delete("container", name, owner)
            if volume and not result["passed"]:
                try:
                    upstream_bats.archive_stopped_volumes(artifact, name, owner, [name], worker["image"], result,
                                                          timeout=120, compressed=True)
                except Exception as error:
                    preserve = True
                    result["archive_error"] = str(error)
            if volume and not preserve:
                reference.owned_delete("volume", name, owner)
            result["cleanup"] = not preserve
        finally:
            result["inputs_unchanged"] = (upstream_unit.context_manifest(source) == manifest and
                                          reference.sha(artifact / "runner.py") == result["runner_sha256"])
            result["global_sysctls_after"] = consumer_worker_loss.sysctls()
            result["loop_module_after"] = Path("/sys/module/loop").exists()
            result["passed"] = (result["passed"] and result["cleanup"] and result["inputs_unchanged"] and
                                result["global_sysctls_after"] == result["global_sysctls_before"])
            result["outer_seconds"] = time.monotonic() - started
            reference_lifecycle.write_json(artifact / "result.json", result)
            print(json.dumps({"evidence": str(artifact), "passed": result["passed"], "tests": result.get("passed_tests"),
                              "error": result.get("error")}), flush=True)
    if not result["passed"]:
        raise SystemExit(1)


if __name__ == "__main__":
    main()
