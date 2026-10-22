#!/usr/bin/env python3
"""Prove pre-cleanup failure diagnostics in the delivered Go worker on native AMD64."""
import argparse
import io
import json
import shutil
import subprocess
import tarfile
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
    worker_path, worker = build_worker.load(args.worker_build)
    native_base.require_native_host("linux/amd64", json.loads(reference.docker(["info", "--format", "{{json .}}"])))
    # The run must exercise exactly the current implementation, not merely an
    # old image whose schema happens to look compatible.
    current = {directory + "/" + key: value for directory in ["internal", "cmd/factorybox"]
               for key, value in upstream_unit.context_manifest(reference.ROOT / directory).items()}
    current.update({file: {"sha256": reference.sha(reference.ROOT / file)} for file in ["go.mod", "go.sum"]})
    if current != worker["source_context"]:
        raise ValueError("worker build source differs from this checkout")
    owner = uuid.uuid4().hex
    name = "factorybox-worker-startup-failure-" + owner
    artifact = reference.new_artifact(name)
    artifact.mkdir(mode=0o700)
    shutil.copyfile(__file__, artifact / "runner.py")
    shutil.copyfile(reference.ROOT / "tests/worker-startup-failure.sh", artifact / "setup.sh")
    result = {"schema": 1, "passed": False, "cleanup": False, "production_qualified": False,
              "worker_build": args.worker_build, "worker_image_id": worker["image_id"],
              "factorybox_sha256": worker["bundle_sha256"]["factorybox"], "owner": owner,
              "inputs_sha256": {file.name: reference.sha(file) for file in artifact.iterdir() if file.is_file()}, "cases": [],
              "scope": "Intentional service faults with a new private fixture component manifest. "
                       "The delivered Go worker is unchanged; the real Sysbox-fs timeout cause and "
                       "production qualification remain unresolved."}
    before = consumer_worker_loss.sysctls()
    result["global_sysctls_before"] = before
    containers, volumes = [], []
    print(json.dumps({"running": str(artifact)}), flush=True)
    try:
        for fault in ["blocked-child", "service-exit"]:
            case_name = name + "-" + fault
            output = artifact / fault
            output.mkdir()
            row = {"fault": fault, "passed": False}
            result["cases"].append(row)
            volumes.append(case_name)
            reference.docker(["volume", "create", "--label", "com.factorybox.run=" + owner, case_name])
            command = ["run", "--detach", "--pull=never", "--name", case_name,
                       "--label", "com.factorybox.run=" + owner, "--privileged", "--cgroupns=private",
                       "--cpus=3", "--memory=3g", "--pids-limit=512", "--network=none",
                       "--env", "GOMAXPROCS=2", "--env", "GOMEMLIMIT=256MiB",
                       "--env", "FACTORYBOX_STARTUP_OWNER=" + owner, "--env", "FACTORYBOX_STARTUP_FAULT=" + fault,
                       "--mount", f"type=volume,source={case_name},target=/var/lib/sysbox",
                       "--mount", f"type=bind,source={artifact / 'setup.sh'},target=/startup-failure.sh,readonly",
                       "--entrypoint", "/bin/bash", worker["image"], "/startup-failure.sh"]
            row["command"] = command
            containers.append(case_name)
            started = time.monotonic()
            identity = reference.docker(command)
            row["container_id"] = identity
            if fault == "blocked-child":
                # services.log is created only after PID 1 delegates cgroups.
                # Use Docker's archive API first; an earlier exec itself could
                # occupy the cgroup root and perturb the startup being tested.
                while True:
                    if not consumer_worker_loss.exact_worker(case_name, owner, identity)["State"]["Running"]:
                        raise ValueError("native startup fixture exited before the blocked-start status check")
                    hint = subprocess.run([*reference.DOCKER, "cp", identity + ":/var/lib/sysbox/store/epochs", "-"],
                                          capture_output=True, timeout=5)
                    if hint.returncode == 0 and len(hint.stdout) <= 4 << 20:
                        with tarfile.open(fileobj=io.BytesIO(hint.stdout)) as tar:
                            if any(member.isfile() and member.name.endswith("/services.log") for member in tar):
                                break
                    if time.monotonic() - started > 10:
                        raise ValueError("native fault fixture did not reach owned service startup")
                    time.sleep(.1)
                # Early status cannot accidentally publish a failed startup.
                status = subprocess.run([*reference.DOCKER, "exec", identity, "/opt/factorybox/factorybox", "worker-status"],
                                        capture_output=True, text=True, timeout=5)
                row["early_status"] = {"exit": status.returncode, "stdout": status.stdout, "stderr": status.stderr}
                if status.returncode != 1 or status.stdout:
                    raise ValueError("blocked worker published readiness")
            reference.docker(["container", "wait", identity], timeout=80)
            row["seconds_to_exit"] = time.monotonic() - started
            saved = consumer_worker_loss.exact_worker(case_name, owner, identity)
            state, limits = saved["State"], saved["HostConfig"]
            row["state"] = state
            if (saved["Image"] != worker["image_id"] or state["Running"] or state["ExitCode"] != 1 or state["OOMKilled"] or
                    limits["Memory"] != 3 << 30 or limits["NanoCpus"] != 3_000_000_000 or limits["PidsLimit"] != 512 or
                    limits["CgroupnsMode"] != "private" or limits["NetworkMode"] != "none"):
                raise ValueError("fault worker exit or resource boundary changed")
            logs = subprocess.run([*reference.DOCKER, "logs", identity], capture_output=True, text=True, timeout=5)
            (output / "stdout.txt").write_text(logs.stdout)
            (output / "stderr.txt").write_text(logs.stderr)
            archive = output / "epochs.tar"
            with archive.open("xb") as stream:
                subprocess.run([*reference.DOCKER, "cp", identity + ":/var/lib/sysbox/store/epochs", "-"],
                               stdout=stream, stderr=subprocess.PIPE, check=True, timeout=10)
            if archive.stat().st_size > 4 << 20:
                raise ValueError("startup diagnostics unexpectedly exceeded the fixture archive bound")
            with tarfile.open(archive) as tar:
                members = tar.getmembers()
                reports = [member for member in members if member.isfile() and member.name.endswith("/startup-failure.json")]
                if len(reports) != 1 or any(member.name.endswith("/worker.json") for member in members):
                    raise ValueError("startup did not retain exactly one failure without a ready record")
                member = reports[0]
                if member.mode != 0o600 or member.size > 1 << 20:
                    raise ValueError("startup failure record privacy/size differs")
                data = tar.extractfile(member).read()
            (output / "startup-failure.json").write_bytes(data)
            report = json.loads(data)
            status = report["worker"]
            if (report["schema"] != 1 or status["store_id"] != owner or len(status["epoch"]) != 32 or
                    status["supervisor"]["pid"] != 1 or status["supervisor_sha256"] != worker["bundle_sha256"]["factorybox"] or
                    report["proc_bytes"] > 256 << 10 or report["task_count"] > 128 or len(report["processes"]) > 32 or report["truncated"]):
                raise ValueError("failure report lost owner, delivered supervisor or collection bounds")
            if fault == "blocked-child":
                if "not ready within 60 seconds" not in report["reason"] or report["reason"] not in logs.stderr:
                    raise ValueError("original startup timeout was masked")
                parent = next(process for process in report["processes"] if process["process"] == status["filesystem"])
                child_ids = {int(value) for task in parent["tasks"] for value in task["files"]["children"].split()}
                children = [process for process in report["processes"] if process["service"] == "filesystem" and process["process"]["pid"] in child_ids]
                if not children or not any("do_wait" in task["files"]["wchan"] for task in parent["tasks"]):
                    raise ValueError("waiting filesystem service and actual child were not captured before cleanup")
                row["waiting_parent_and_child_captured"] = True
            elif "owned Sysbox service exited" not in report["reason"] or report["reason"] not in logs.stderr:
                raise ValueError("original service-exit failure was masked")
            row["report_sha256"] = reference.sha(output / "startup-failure.json")
            row["passed"] = True
            reference.owned_delete("container", case_name, owner)
            containers.remove(case_name)
            reference.owned_delete("volume", case_name, owner)
            volumes.remove(case_name)
        result["passed"] = True
    except Exception as error:
        result["error"] = str(error)
        raise
    finally:
        try:
            for container in containers:
                saved = json.loads(reference.docker(["container", "inspect", container]))[0]
                consumer_worker_loss.exact_worker(container, owner, saved["Id"])
                logs = subprocess.run([*reference.DOCKER, "logs", saved["Id"]], capture_output=True, text=True, timeout=5)
                (artifact / (container + ".stdout")).write_text(logs.stdout)
                (artifact / (container + ".stderr")).write_text(logs.stderr)
                reference.owned_delete("container", container, owner)
            if volumes and not result["passed"]:
                retained = artifact / "failure-store"
                retained.mkdir()
                upstream_bats.archive_stopped_volumes(retained, name, owner, volumes, worker["image"], result,
                                                      timeout=60, compressed=True)
            for volume in volumes:
                reference.owned_delete("volume", volume, owner)
            result["cleanup"] = True
        finally:
            result["global_sysctls_after"] = consumer_worker_loss.sysctls()
            result["inputs_unchanged"] = all(reference.sha(artifact / file) == digest for file, digest in result["inputs_sha256"].items())
            result["passed"] = (result["passed"] and result["cleanup"] and result["inputs_unchanged"] and
                                result["global_sysctls_after"] == before)
            reference_lifecycle.write_json(artifact / "result.json", result)
            print(json.dumps({"evidence": str(artifact), "passed": result["passed"], "cleanup": result["cleanup"],
                              "error": result.get("error")}), flush=True)
    if not result["passed"]:
        raise ValueError("startup failure diagnostic proof failed")


if __name__ == "__main__":
    main()
