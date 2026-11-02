#!/usr/bin/env python3
"""Measure the actual filesystem copier on an owned host and bounded store."""
import argparse
import json
import os
import shutil
import subprocess
import time
import uuid

import build_config
import build_worker
import consumer_worker_loss
import native_base
import reference
import upstream_unit


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--worker-build", required=True)
    args = parser.parse_args()
    build, worker = build_worker.load(args.worker_build)
    host = json.loads(reference.docker(["info", "--format", "{{json .}}"]))
    platform = native_base.native_platform(host)
    native_base.require_native_host(platform, host)
    if platform != worker["platform"]:
        raise ValueError("the copy proof must execute on its real native platform")
    owner = uuid.uuid4().hex
    name = "factorybox-filesystem-cow-" + owner
    artifact = reference.new_artifact(name)
    artifact.mkdir()
    source = artifact / "source"
    shutil.copytree(reference.ROOT / "internal", source / "internal")
    for filename in ["go.mod", "go.sum"]:
        shutil.copyfile(reference.ROOT / filename, source / filename)
    shutil.copyfile(__file__, artifact / "runner.py")
    manifest = upstream_unit.context_manifest(source)
    current_core = {key: value for key, value in manifest.items() if not key.endswith("_test.go")}
    built_core = {key: value for key, value in worker["source_context"].items()
                  if key.startswith("internal/") or key in ("go.mod", "go.sum")}
    built_core = {key: value for key, value in built_core.items() if not key.endswith("_test.go")}
    if current_core != built_core:
        raise ValueError("the component proof must use the delivered worker core")
    result = {
        "schema": 1, "owner": owner, "platform": platform, "worker_build": args.worker_build,
        "image": worker["image"], "source_context": manifest,
        "scope": "Complete verified filesystem copies on the host filesystem and owned bounded store",
        "sandbox_created": False, "native_lifecycle_qualified": False,
        "production_qualified": False, "passed": False, "cleanup": False,
    }
    attempted = volume_created = False
    try:
        binary = artifact / "probe.test"
        subprocess.run(["go", "test", "-c", "-trimpath", "-buildvcs=false",
                        "-o", str(binary), "./internal/sysboxcapture"], cwd=source,
                       env=dict(os.environ, CGO_ENABLED="0", GOOS="linux",
                                GOARCH=platform.split("/")[1]), check=True, timeout=120)
        build_config.require_elf(binary, platform)
        result["binary_sha256"] = reference.sha(binary)
        reference.docker(["volume", "create", "--label", "com.factorybox.run=" + owner, name])
        volume_created = True
        command = [
            "run", "--detach", "--pull=never", "--name", name,
            "--label", "com.factorybox.run=" + owner, "--privileged", "--cgroupns=private",
            "--cpus=3", "--memory=3g", "--pids-limit=512", "--network=none",
            "--env", "GOMAXPROCS=2", "--env", "GOMEMLIMIT=256MiB",
            "--mount", "type=volume,source=" + name + ",target=/var/lib/sysbox",
            "--mount", "type=bind,source=" + str(binary) + ",target=/probe.test,readonly",
            worker["image"], "--store-id", owner,
            "--bounded-root", "/var/lib/sysbox/fixed-store", "--bounded-bytes", str(512 << 20),
        ]
        result["command"] = command
        attempted = True
        identity = reference.docker(command)
        result["identity"] = identity
        deadline = time.monotonic() + 420
        result["startup"] = consumer_worker_loss.ready(name, owner, identity, deadline)
        probe = [*reference.DOCKER, "exec", "--env", "FACTORYBOX_NATIVE_COW_COPY=1",
                 identity, "/probe.test", "-test.v", "-test.run=^TestFilesystemCopyCOWNative$",
                 "-test.timeout=60s"]
        result["probe_command"] = probe
        with (artifact / "stdout.txt").open("x") as out, (artifact / "stderr.txt").open("x") as err:
            done = subprocess.run(probe, stdout=out, stderr=err,
                                  timeout=min(65, deadline - time.monotonic()))
        result["exit_code"] = done.returncode
        output = (artifact / "stdout.txt").read_text()
        result["passed"] = done.returncode == 0 and "--- SKIP:" not in output
        consumer_worker_loss.exact_worker(name, owner, identity)
        reference.docker(["container", "stop", "--time=30", identity],
                         timeout=min(40, deadline - time.monotonic()))
        state = consumer_worker_loss.exact_worker(name, owner, identity)["State"]
        if state["Running"] or state["Pid"] or state["OOMKilled"] or state["ExitCode"]:
            raise ValueError("owned copy fixture did not close its bounded store cleanly")
        result["inputs_unchanged"] = upstream_unit.context_manifest(source) == manifest
        result["passed"] = result["passed"] and result["inputs_unchanged"]
    finally:
        if attempted:
            logs = subprocess.run([*reference.DOCKER, "logs", name], capture_output=True,
                                  text=True, timeout=5)
            (artifact / "worker.log").write_text(logs.stdout + logs.stderr)
            reference.owned_delete("container", name, owner)
        if volume_created:
            # The probe owns deterministic source files, creates no Sandbox or
            # checkpoint, and removes its file roots before closing the worker.
            reference.owned_delete("volume", name, owner)
        result["cleanup"] = True
        (artifact / "result.json").write_text(json.dumps(result, indent=2) + "\n")
        print(json.dumps({"evidence": str(artifact), "passed": result["passed"],
                          "cleanup": result["cleanup"]}), flush=True)
    if not result["passed"]:
        raise SystemExit(1)


if __name__ == "__main__":
    main()
