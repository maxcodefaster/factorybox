#!/usr/bin/env python3
"""Verify failed-capture cleanup after a real same-store worker replacement."""
import argparse
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import time
import uuid

import build_config
import build_worker
import capture_budget_native
import consumer_worker_loss
import native_base
import reference
import upstream_bats
import upstream_unit
import worker_storage_release


PROOFS = [
    "legacy_receipt_preserved", "legacy_record_preserved", "failure_evidence_preserved",
    "same_store_new_epoch", "failed_payload_removed", "actual_capacity_returned",
    "wrong_owner_refused", "exact_replay", "original_storage_released",
    "successor_storage_released", "host_sysctls_unchanged", "inputs_unchanged",
    "pending_resume_preserved", "pending_resume_continued",
]


def require_complete(result):
    if any(result.get(key) is not True for key in PROOFS):
        raise ValueError("retained failed-capture cleanup omitted an ownership or preservation proof")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--worker-build", required=True)
    parser.add_argument("--legacy-cli-build", required=True)
    args = parser.parse_args()
    _, worker = build_worker.load(args.worker_build)
    if not re.fullmatch(r"factorybox-build-worker-[0-9a-f]{32}", args.legacy_cli_build):
        raise ValueError("requires one exact completed legacy CLI build")
    legacy_root = reference.ROOT / "artifacts" / args.legacy_cli_build
    legacy = json.loads((legacy_root / "result.json").read_text())
    legacy_cli = legacy_root / "image-context/bundle/factorybox"
    if (legacy.get("passed") is not True or legacy["platform"] != worker["platform"] or
            legacy_cli.is_symlink() or reference.sha(legacy_cli) != legacy["bundle_sha256"]["factorybox"] or
            any(legacy["bundle_sha256"][name] != worker["bundle_sha256"][name]
                for name in ["components/sysbox-runc", "components/criu"])):
        raise ValueError("legacy cleanup CLI differs from the exact declared native input")
    build_config.require_elf(legacy_cli, worker["platform"])
    native_base.require_native_host(worker["platform"], json.loads(reference.docker(["info", "--format", "{{json .}}"])))
    if reference.docker(["ps", "--filter", "label=com.factorybox.run", "--format", "{{.ID}}"]):
        raise ValueError("other native fixtures must finish before this serialized proof")
    owner = uuid.uuid4().hex
    name = "factorybox-retained-failed-capture-" + owner
    artifact = reference.new_artifact(name)
    artifact.mkdir(mode=0o700)
    shutil.copyfile(__file__, artifact / "runner.py")
    source = artifact / "source"
    shutil.copytree(reference.ROOT / "internal", source / "internal")
    for file in ["go.mod", "go.sum"]:
        shutil.copyfile(reference.ROOT / file, source / file)
    manifest = upstream_unit.context_manifest(source)
    core = lambda rows: {k: v for k, v in rows.items() if not k.endswith("_test.go")}
    expected = {k: v for k, v in worker["source_context"].items()
                if k.startswith("internal/") or k in {"go.mod", "go.sum"}}
    if core(manifest) != core(expected):
        raise ValueError("retained cleanup probe core differs from its delivered worker")
    binary = artifact / "lifecycle.test"
    with (artifact / "build.log").open("x") as log:
        subprocess.run(["go", "test", "-c", "-mod=readonly", "-trimpath", "-buildvcs=false",
                        "-o", str(binary), "./internal/lifecycle"], cwd=source, check=True,
                       env=dict(os.environ, GOOS="linux", GOARCH=build_config.architecture(worker["platform"]),
                                CGO_ENABLED="0", GOMAXPROCS="3", GOMEMLIMIT="2GiB"),
                       stdout=log, stderr=subprocess.STDOUT)
    rootfs = json.loads((reference.ROOT / "upstream/test-inputs.json").read_text())[
        "busybox-" + build_config.architecture(worker["platform"])]
    archive = reference.ROOT / ".cache/downloads" / rootfs["archive"]
    if reference.sha(archive) != rootfs["sha256"]:
        raise ValueError("native rootfs differs from the pinned input")
    output = artifact / "output"
    output.mkdir()
    result = {"schema": 1, "owner": owner, "worker_build": args.worker_build,
              "legacy_cli_build": args.legacy_cli_build, "legacy_cli_sha256": reference.sha(legacy_cli),
              "image": worker["image"], "platform": worker["platform"], "passed": False, "cleanup": False,
              "production_qualified": False, "source": manifest,
              "binary_sha256": reference.sha(binary), "runner_sha256": reference.sha(artifact / "runner.py"),
              "scope": "One owned 1-GiB store and a real abandoned guarded capture. An exact older cleanup CLI completes its original retirement without capture cleanup. Replace the worker on the same store, then require exact retained cleanup, byte-identical old receipts/records/diagnostics and actual capacity return. No retained installation, model calls, publishing or host settings."}
    deadline = time.monotonic() + 420
    volume = False
    workers = []
    pending = None

    def run(arguments, body=None):
        remaining = deadline - time.monotonic()
        if remaining <= 0:
            raise TimeoutError("retained cleanup exhausted its 420-second native deadline")
        done = subprocess.run([*reference.DOCKER, *arguments], input=body,
                              capture_output=True, text=True, timeout=remaining)
        with (artifact / "commands.jsonl").open("a") as log:
            log.write(json.dumps({"arguments": arguments, "exit_code": done.returncode,
                                  "stdout": done.stdout, "stderr": done.stderr}) + "\n")
        if done.returncode:
            raise ValueError("owned retained cleanup command failed: " + done.stderr[-2500:])
        return done.stdout.strip()

    def release(status, destination):
        observation = 0
        while True:
            selected = destination / str(observation)
            selected.mkdir(parents=True)
            try:
                return worker_storage_release.verify(worker, name, owner, status, selected)
            except ValueError:
                path = selected / ("storage-release-" + status["epoch"])
                receipt = json.loads(path.with_suffix(".json").read_text())
                if (not capture_budget_native.pending_kernel_owner(receipt, path.with_suffix(".stderr").read_text()) or
                        time.monotonic() >= deadline):
                    raise
                observation += 1
                time.sleep(.02)

    command = [
        "run", "--detach", "--pull=never", "--name", name, "--label", "com.factorybox.run=" + owner,
        "--privileged", "--cgroupns=private", "--network=none", "--stop-timeout=45",
        "--cpus=3", "--memory=3g", "--pids-limit=512", "--env", "GOMAXPROCS=2",
        "--env", "GOMEMLIMIT=256MiB", "--mount", f"type=volume,source={name},target=/var/lib/sysbox",
        "--mount", f"type=bind,source={archive},target=/input/rootfs.tar.gz,readonly",
        "--mount", f"type=bind,source={binary},target=/input/lifecycle.test,readonly",
        "--mount", f"type=bind,source={legacy_cli},target=/input/legacy-cli,readonly",
        "--mount", f"type=bind,source={output},target=/output",
        worker["image"], "--store-id", owner, "--bounded-root", "/var/lib/sysbox/fixed-store",
        "--bounded-bytes", str(1 << 30),
    ]
    try:
        before_sysctls = consumer_worker_loss.sysctls(
            docker_image=worker["native_base_image"], owner=owner, artifact=artifact, phase="before")
        run(["volume", "create", "--label", "com.factorybox.run=" + owner, name])
        volume = True
        workers.append(name)
        identity = run(command)
        status = consumer_worker_loss.ready(name, owner, identity, deadline)["status"]
        run(["exec", identity, "sh", "-ec",
             "mkdir /busybox; tar -xzf /input/rootfs.tar.gz -C /busybox"])
        run(["exec", "--env", "FACTORYBOX_NATIVE_FAILED_CAPTURE_RETIREMENT=1",
             "--env", "FACTORYBOX_NATIVE_RETAINED_FAILED_CAPTURE_PREPARE=1",
             "--env", "FACTORYBOX_RETIREMENT_CLI=/input/legacy-cli", identity,
             "/input/lifecycle.test", "-test.v", "-test.run=^TestNativeRetainedFailedCapturePrepare$",
             "-test.timeout=120s"])
        prepared = json.loads((output / "retained-failed-capture-prepare.json").read_text())
        run(["stop", "--timeout=45", identity])
        exited = consumer_worker_loss.exact_worker(name, owner, identity)["State"]
        if exited["Running"] or exited["OOMKilled"] or exited["ExitCode"] != 0:
            raise ValueError("original worker did not stop cleanly")
        result["original_storage_released"] = release(status, artifact / "original-release").get("passed") is True
        successor_name = name + "-successor"
        successor_command = list(command)
        successor_command[successor_command.index(name)] = successor_name
        workers.append(successor_name)
        identity = run(successor_command)
        successor = consumer_worker_loss.ready(successor_name, owner, identity, deadline)["status"]
        result["same_store_new_epoch"] = successor["store_id"] == status["store_id"] and successor["epoch"] != status["epoch"]
        run(["exec", identity, "sh", "-ec",
             "mkdir /busybox; tar -xzf /input/rootfs.tar.gz -C /busybox"])
        pending_stdout = (artifact / "pending-resume.stdout").open("x")
        pending_stderr = (artifact / "pending-resume.stderr").open("x")
        pending = subprocess.Popen([*reference.DOCKER, "exec", "--env",
            "FACTORYBOX_NATIVE_RETAINED_PENDING_RESUME=1", identity,
            "/input/lifecycle.test", "-test.v",
            "-test.run=^TestNativeRetainedCleanupWhileResumeUnattempted$", "-test.timeout=180s"],
            stdout=pending_stdout, stderr=pending_stderr)
        pending_stdout.close()
        pending_stderr.close()
        while not (output / "pending-resume-ready.json").exists():
            if pending.poll() is not None or time.monotonic() >= deadline:
                raise ValueError("owned pending Resume did not reach its real copy failure")
            time.sleep(.02)
        arguments = ["exec", identity, "/opt/factorybox/factorybox", "--worker-run-root", "/run/factorybox",
                     "retire-sandbox", "--source-root", prepared["source_root"],
                     "--sandbox-owner", prepared["request"]["sandbox_owner"],
                     "--input-sha256", prepared["request"]["input_sha256"], "failed-capture"]
        wrong = list(arguments)
        wrong[wrong.index(prepared["request"]["sandbox_owner"])] = "f"*32
        try:
            run(wrong)
        except ValueError:
            result["wrong_owner_refused"] = True
        else:
            raise ValueError("retained cleanup accepted another owner")
        capacity = lambda: json.loads(run(["exec", identity, "/opt/factorybox/factorybox",
            "--worker-run-root", "/run/factorybox", "capacity"]))["filesystem_available_bytes"]
        before = capacity()
        receipt = json.loads(run(arguments))
        after = capacity()
        result["legacy_receipt_preserved"] = receipt == prepared["receipt"]
        result["exact_replay"] = json.loads(run(arguments)) == receipt
        observed = json.loads(run(["exec", identity, "python3", "-c", """
import hashlib,json,pathlib,sys
p=json.loads(sys.argv[1]);root=pathlib.Path(p['source_root'])/'objects/failed-capture'
cap=root/'captures'/p['snapshot_id']
print(json.dumps({'record':hashlib.sha256((root/'record.json').read_bytes()).hexdigest()==p['record_sha256'],
 'evidence':all(hashlib.sha256((cap/name).read_bytes()).hexdigest()==digest for name,digest in p['metadata_sha256'].items()),
 'images_removed':not (cap/'images').exists()}))
""", json.dumps(prepared)]))
        result["legacy_record_preserved"] = observed["record"]
        result["failure_evidence_preserved"] = observed["evidence"]
        result["failed_payload_removed"] = observed["images_removed"]
        result["actual_capacity_returned"] = after-before >= prepared["retained_payload_bytes"]
        result["available_bytes_before"], result["available_bytes_after"] = before, after
        run(["exec", identity, "touch", "/output/continue-pending-resume"])
        if pending.wait(timeout=max(.1, deadline-time.monotonic())) != 0:
            raise ValueError("original pending Resume did not continue after unrelated cleanup")
        resumed = json.loads((output / "pending-resume-completed.json").read_text())
        result["pending_resume_preserved"] = (
            resumed.get("original_record_preserved") is True and resumed.get("checkpoint_preserved") is True)
        result["pending_resume_continued"] = resumed.get("same_resume_continued") is True
        run(["stop", "--timeout=45", identity])
        exited = consumer_worker_loss.exact_worker(successor_name, owner, identity)["State"]
        if exited["Running"] or exited["OOMKilled"] or exited["ExitCode"] != 0:
            raise ValueError("successor worker did not stop cleanly")
        result["successor_storage_released"] = release(successor, artifact / "successor-release").get("passed") is True
        result["host_sysctls_unchanged"] = before_sysctls == consumer_worker_loss.sysctls(
            docker_image=worker["native_base_image"], owner=owner, artifact=artifact, phase="after")
        result["inputs_unchanged"] = (
            reference.sha(Path(__file__)) == result["runner_sha256"] and
            reference.sha(binary) == result["binary_sha256"] and
            reference.sha(legacy_cli) == result["legacy_cli_sha256"] and reference.sha(archive) == rootfs["sha256"])
        require_complete(result)
        result["passed"] = True
    except (Exception, KeyboardInterrupt) as error:
        result["error"] = str(error)
    finally:
        try:
            for created in reversed(workers):
                logs = subprocess.run([*reference.DOCKER, "logs", created], capture_output=True, text=True)
                (artifact / (created + ".log")).write_text(logs.stdout + logs.stderr)
                reference.owned_delete("container", created, owner)
            if pending is not None:
                pending.wait(timeout=10)
            if volume and not result["passed"]:
                upstream_bats.archive_stopped_volumes(artifact, name, owner, [name], worker["image"], result, compressed=True)
            if volume:
                reference.owned_delete("volume", name, owner)
            result["cleanup"] = True
        except Exception as error:
            result.update(cleanup=False, passed=False, cleanup_error=str(error))
        (artifact / "result.json").write_text(json.dumps(result, indent=2) + "\n")
        print(json.dumps({"evidence": str(artifact), "passed": result["passed"],
                          "cleanup": result["cleanup"], "error": result.get("error")}), flush=True)
    if not result["passed"]:
        raise SystemExit(1)


if __name__ == "__main__":
    main()
