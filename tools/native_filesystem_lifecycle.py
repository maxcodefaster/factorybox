#!/usr/bin/env python3
"""Exercise a delivered worker's common-subvolume Sandbox through Pause/Resume."""
import argparse
import json
import math
import os
import shutil
import subprocess
import time
import uuid

import bounded_store_native
import build_config
import build_worker
import consumer_worker_loss
import native_base
import reference
import upstream_unit
import worker_storage_release


def require_complete(proof, deleted, child_deleted):
    if (proof.get("passed") is not True or proof.get("same_private_ram") is not True or
            proof.get("useful_after_resume") is not True or proof.get("native_roots") != 8 or
            proof.get("pause", {}).get("after", {}).get("anon") != 0 or
            proof.get("pause", {}).get("snapshot", {}).get("resident_bytes_after") != 0):
        raise ValueError("native lifecycle omitted native capture, released RAM or useful resumed work")
    if (any(proof.get(key) is not True for key in
            ["independent_fork_ram", "child_resume_preserves_work", "child_useful_after_source_delete"]) or
            proof.get("child_pause", {}).get("after", {}).get("anon") != 0 or
            proof.get("child_pause", {}).get("snapshot", {}).get("resident_bytes_after") != 0):
        raise ValueError("native lifecycle omitted its independently useful child Pause/Resume")
    record = deleted.get("record", {})
    if (record.get("phase") != "deleted" or record.get("owner") != proof.get("owner") or
            not record.get("delete", {}).get("completed_at") or deleted.get("runtime") is not None):
        raise ValueError("native lifecycle lacks its exact public Delete")
    record = child_deleted.get("record", {})
    if (record.get("phase") != "deleted" or record.get("owner") != proof.get("child_owner") or
            not record.get("delete", {}).get("completed_at") or child_deleted.get("runtime") is not None):
        raise ValueError("native lifecycle lacks its exact independent child Delete")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--worker-build", required=True)
    args = parser.parse_args()
    build, worker = build_worker.load(args.worker_build)
    host = json.loads(reference.docker(["info", "--format", "{{json .}}"]))
    native_base.require_native_host(worker["platform"], host)
    compile_options = bounded_store_native.worker_compile_options(worker, host)
    image, _ = build_config.local_pinned_image(worker["image"], worker["image_id"], reference.docker)
    owner = uuid.uuid4().hex
    name = "factorybox-native-filesystem-" + owner
    artifact = reference.new_artifact(name)
    artifact.mkdir()
    source, output = artifact / "source", artifact / "output"
    output.mkdir()
    for directory in ["internal", "cmd/factorybox"]:
        shutil.copytree(build / "source" / directory, source / directory)
    for file in ["go.mod", "go.sum"]:
        shutil.copyfile(build / "source" / file, source / file)
    for directory in ["internal", "cmd/factorybox"]:
        for path in (reference.ROOT / directory).rglob("*_test.go"):
            target = source / path.relative_to(reference.ROOT)
            target.parent.mkdir(parents=True, exist_ok=True)
            shutil.copyfile(path, target)
    manifest = upstream_unit.context_manifest(source)
    core = lambda rows: {k: v for k, v in rows.items() if not k.endswith("_test.go")}
    if core(manifest) != core(worker["source_context"]):
        raise ValueError("native proof differs from the delivered worker core")
    shutil.copyfile(__file__, artifact / "runner.py")
    busybox = json.loads((reference.ROOT / "upstream/test-inputs.json").read_text())[
        "busybox-" + build_config.architecture(worker["platform"])]
    rootfs = reference.ROOT / ".cache/downloads" / busybox["archive"]
    if reference.sha(rootfs) != busybox["sha256"]:
        raise ValueError("native fixture rootfs changed")
    result = {
        "schema": 1, "owner": owner, "worker_build": args.worker_build, "image": image,
        "source_context": manifest, "rootfs_sha256": busybox["sha256"],
        "passed": False, "cleanup": False, "native_lifecycle_qualified": False, "production_qualified": False,
        "scope": "Delivered CLI native-layout Start, Pause/Resume and independent Fork; child Pause/Resume and useful work after original deletion; public Delete and clean same-worker store release. Not ACME or worker-replacement qualification.",
    }
    volume_attempted = container_attempted = False
    identity, deadline = None, None
    try:
        binary = artifact / "probe.test"
        command = bounded_store_native.probe_build_command(binary, compile_options, capacity=True)
        result["compile_command"] = command
        with (artifact / "compile.log").open("x") as log:
            subprocess.run(command, cwd=source, env=dict(os.environ, GOOS="linux",
                           GOARCH=build_config.architecture(worker["platform"]), CGO_ENABLED="0"),
                           stdout=log, stderr=subprocess.STDOUT, check=True, timeout=120)
        result["probe_sha256"] = reference.sha(binary)
        before = consumer_worker_loss.sysctls(
            docker_image=worker["native_base_image"], owner=owner, artifact=artifact, phase="before")
        command = [
            "run", "--detach", "--pull=never", "--name", name, "--label", "com.factorybox.run=" + owner,
            "--privileged", "--cgroupns=private", "--cpus=3", "--memory=3g", "--pids-limit=512", "--network=none",
            "--env", "GOMAXPROCS=2", "--env", "GOMEMLIMIT=256MiB",
            "--mount", "type=volume,source=" + name + ",target=/var/lib/sysbox",
            "--mount", "type=bind,source=" + str(binary) + ",target=/probe.test,readonly",
            "--mount", "type=bind,source=" + str(rootfs) + ",target=/busybox-rootfs.tar.gz,readonly",
            "--mount", "type=bind,source=" + str(output) + ",target=/output",
            image, "--store-id", owner,
            "--bounded-root", "/var/lib/sysbox/fixed-store", "--bounded-bytes", str(1 << 30),
        ]
        result["command"] = command
        (artifact / "intent.json").write_text(json.dumps(result, indent=2) + "\n")
        volume_attempted = True
        reference.docker(["volume", "create", "--label", "com.factorybox.run=" + owner, name])
        container_attempted = True
        identity = reference.docker(command)
        deadline = time.monotonic() + 420
        result["startup"] = consumer_worker_loss.ready(name, owner, identity, deadline)
        remaining = max(.001, deadline - time.monotonic())
        setup = "set -eu; mkdir /busybox; tar -xzf /busybox-rootfs.tar.gz -C /busybox; exec /probe.test -test.v -test.run=^TestNativeFilesystemLifecycle$ -test.timeout=" + str(remaining) + "s"
        with (artifact / "stdout.txt").open("x") as out, (artifact / "stderr.txt").open("x") as err:
            done = subprocess.run([*reference.DOCKER, "exec", "--env", "FACTORYBOX_NATIVE_FILESYSTEM_LIFECYCLE=1",
                                   identity, "/bin/bash", "-ec", setup],
                                  stdout=out, stderr=err, timeout=max(.001, deadline - time.monotonic()))
        result["exit_code"] = done.returncode
        if done.returncode or "--- SKIP:" in (artifact / "stdout.txt").read_text():
            raise ValueError("native Sandbox filesystem lifecycle failed; retain original owned resources")
        proof = json.loads((output / "native-filesystem-lifecycle.json").read_text())
        deleted = json.loads((output / "native-delete.json").read_text())
        child_deleted = json.loads((output / "native-child-delete.json").read_text())
        require_complete(proof, deleted, child_deleted)
        remaining = deadline - time.monotonic()
        if remaining <= 0:
            raise TimeoutError("native lifecycle deadline expired before worker closure")
        reference.docker(["container", "stop", "--time=" + str(math.ceil(remaining)), identity], timeout=remaining)
        state = consumer_worker_loss.exact_worker(name, owner, identity)["State"]
        result["worker_exit"] = state
        if state["Running"] or state["Pid"] or state["OOMKilled"] or state["ExitCode"]:
            raise ValueError("native worker did not close its original store cleanly")
        result["storage_release"] = worker_storage_release.observe(
            worker, name, owner, result["startup"]["status"], artifact, deadline=deadline)
        after = consumer_worker_loss.sysctls(
            docker_image=worker["native_base_image"], owner=owner, artifact=artifact, phase="after")
        result["global_sysctls_unchanged"] = before == after
        result["inputs_unchanged"] = upstream_unit.context_manifest(source) == manifest
        result["timings_us"] = proof["timings_us"]
        result["passed"] = result["global_sysctls_unchanged"] and result["inputs_unchanged"]
    except Exception as error:
        result["error"] = str(error)
    finally:
        if container_attempted:
            logs = subprocess.run([*reference.DOCKER, "logs", name], capture_output=True, text=True, timeout=5)
            (artifact / "worker.log").write_text(logs.stdout + logs.stderr)
        if result["passed"]:
            reference.owned_delete("container", name, owner)
            reference.owned_delete("volume", name, owner)
            result["cleanup"] = True
        elif container_attempted or volume_attempted:
            result["preserved_resources"] = {"container": name if container_attempted else None,
                                             "volume": name if volume_attempted else None}
        (artifact / "result.json").write_text(json.dumps(result, indent=2) + "\n")
        print(json.dumps({"evidence": str(artifact), "passed": result["passed"],
                          "cleanup": result["cleanup"], "error": result.get("error"),
                          "preserved_resources": result.get("preserved_resources")}), flush=True)
    if not result["passed"]:
        raise SystemExit(1)


if __name__ == "__main__":
    main()
