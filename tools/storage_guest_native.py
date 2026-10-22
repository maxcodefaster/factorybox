#!/usr/bin/env python3
"""Observe bounded writes and subvolume creation in one real owned Sysbox guest."""
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
    if reference.docker(["ps", "--filter", "label=com.factorybox.run", "--format", "{{.ID}}"]):
        raise ValueError("the bounded guest diagnostic requires other Factorybox runs to finish")
    owner = uuid.uuid4().hex
    name = "factorybox-storage-guest-" + owner
    artifact = reference.new_artifact(name)
    artifact.mkdir()
    source = artifact / "source"
    shutil.copyfile(__file__, artifact / "runner.py")
    shutil.copyfile(reference.ROOT / "tests/storage-guest-setup.sh", artifact / "setup.sh")
    for directory in ["internal", "cmd/factorybox"]:
        shutil.copytree(reference.ROOT / directory, source / directory)
    for file in ["go.mod", "go.sum"]:
        shutil.copyfile(reference.ROOT / file, source / file)
    manifest = upstream_unit.context_manifest(source)
    core = lambda value: {key: item for key, item in value.items() if not key.endswith("_test.go")}
    if core(manifest) != core(worker["source_context"]):
        raise ValueError("guest probe core differs from the delivered worker")
    busybox = json.loads((reference.ROOT / "upstream/test-inputs.json").read_text())["busybox-amd64"]
    rootfs = reference.ROOT / ".cache/downloads" / busybox["archive"]
    if reference.sha(rootfs) != busybox["sha256"]:
        raise ValueError("guest rootfs input changed")
    result = {"schema": 1, "owner": owner, "passed": False, "cleanup": False,
              "production_qualified": False, "diagnostic_only": True,
              "worker_build": args.worker_build, "worker_image_id": worker["image_id"],
              "source_context": manifest, "rootfs_sha256": busybox["sha256"],
              "runner_sha256": reference.sha(artifact / "runner.py"),
              "setup_sha256": reference.sha(artifact / "setup.sh"),
              "scope": "One actual Sysbox guest, its ordinary overlay root and an explicitly supplied own Btrfs bind. Require ordinary write controls; observe subvolume creation and at most one MiB per created child. Verify exact child identity before removal. No quota setup, global quota change, guest escape claim or storage-boundary qualification."}
    before = consumer_worker_loss.sysctls()
    result["global_sysctls_before"] = before
    reference_lifecycle.write_json(artifact / "intent.json", result)
    container = volume = preserve = False
    started = time.monotonic()
    print(json.dumps({"running": str(artifact)}), flush=True)
    try:
        binary = artifact / "probe.test"
        env = dict(os.environ, GOOS="linux", GOARCH="amd64", CGO_ENABLED="0")
        command = ["go", "test", "-c", "-trimpath", "-buildvcs=false", "-tags=factorybox_worker_probe",
                   "-o", str(binary), "./internal/lifecycle"]
        with (artifact / "build.log").open("x") as stream:
            subprocess.run(command, cwd=source, env=env, stdout=stream, stderr=subprocess.STDOUT, check=True, timeout=120)
        result["probe_sha256"] = reference.sha(binary)
        output = artifact / "output"
        output.mkdir()
        volume = True
        reference.docker(["volume", "create", "--label", "com.factorybox.run=" + owner, name])
        command = ["run", "--detach", "--pull=never", "--name", name, "--label", "com.factorybox.run=" + owner,
                   "--privileged", "--cgroupns=private", "--cpus=3", "--memory=3g", "--pids-limit=512", "--network=none",
                   "--env", "GOMAXPROCS=2", "--env", "GOMEMLIMIT=256MiB", "--env", "FACTORYBOX_NATIVE_LIFECYCLE=1",
                   "--env", "FACTORYBOX_NATIVE_STORAGE_GUEST=1",
                   "--env", "FACTORYBOX_RUNTIME_SHA256=" + worker["bundle_sha256"]["components/sysbox-runc"],
                   "--mount", f"type=volume,source={name},target=/var/lib/sysbox"]
        for path, target, readonly in [(binary, "/probe.test", True), (artifact / "setup.sh", "/setup.sh", True),
                                       (rootfs, "/busybox-rootfs.tar.gz", True), (output, "/output", False)]:
            command += ["--mount", f"type=bind,source={path},target={target}" + (",readonly" if readonly else "")]
        command += [worker["image"], "--store-id", owner]
        result["command"] = command
        container = True
        identity = reference.docker(command)
        result["container_id"] = identity
        deadline = time.monotonic() + 420
        result["startup"] = consumer_worker_loss.ready(name, owner, identity, deadline)
        with (artifact / "stdout.txt").open("x") as stdout, (artifact / "stderr.txt").open("x") as stderr:
            done = subprocess.run([*reference.DOCKER, "exec", identity, "/bin/bash", "/setup.sh"], stdout=stdout, stderr=stderr,
                                  timeout=max(.1, deadline - time.monotonic()))
        result["exit_code"] = done.returncode
        result["state"] = consumer_worker_loss.exact_worker(name, owner, identity)["State"]
        if done.returncode or result["state"]["OOMKilled"] or not result["state"]["Running"]:
            raise ValueError("actual guest storage observation failed")
        proof_text = reference.docker(["exec", identity, "cat", "/output/storage-guest.json"])
        proof = json.loads(proof_text)
        observations = proof.get("observations", [])
        if (proof.get("passed") is not True or proof.get("exact_created_subvolumes_removed") is not True or
                len(observations) != 2 or [row["path"] for row in observations] != ["/tmp/storage-boundary", "/quota-bind"] or
                any(row.get("error") or not row.get("control_bytes") for row in observations)):
            raise ValueError("actual guest proof incomplete")
        with (artifact / "guest-proof.json").open("x") as stream:
            stream.write(proof_text + "\n")
        result["guest_proof_sha256"] = reference.sha(artifact / "guest-proof.json")
        result["observations"] = observations
        result["passed"] = True
    except Exception as error:
        result["error"] = str(error)
    finally:
        try:
            if container:
                logs = subprocess.run([*reference.DOCKER, "logs", name], capture_output=True, text=True, timeout=5)
                (artifact / "worker.log").write_text(logs.stdout + logs.stderr)
                consumer_worker_loss.exact_worker(name, owner, result.get("container_id"))
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
            result["global_sysctls_after"] = consumer_worker_loss.sysctls()
            result["inputs_unchanged"] = (upstream_unit.context_manifest(source) == manifest and
                                          reference.sha(artifact / "runner.py") == result["runner_sha256"] and
                                          reference.sha(artifact / "setup.sh") == result["setup_sha256"])
            result["passed"] = (result["passed"] and result["cleanup"] and result["inputs_unchanged"] and
                                result["global_sysctls_after"] == before)
            result["outer_seconds"] = time.monotonic() - started
            reference_lifecycle.write_json(artifact / "result.json", result)
            print(json.dumps({"evidence": str(artifact), "passed": result["passed"], "error": result.get("error"),
                              "observations": result.get("observations")}), flush=True)
    if not result["passed"]:
        raise SystemExit(1)


if __name__ == "__main__":
    main()
