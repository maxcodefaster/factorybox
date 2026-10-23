#!/usr/bin/env python3
"""Repeat complete owned Factory lifecycles with read-only CRIU diagnostics.

Stop at the first failed run and preserve it. Successful repeats do not explain
the historical collision or select a fix. This is not a performance comparison.
"""
import argparse
import json
from pathlib import Path
import shutil
import subprocess
import sys
import uuid

import benchmark_consumer_start
import build_worker
import criu_candidate
import reference
import reference_lifecycle


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    for argument in ["worker-build", "consumer-inputs", "project-images", "theia-image"]:
        parser.add_argument("--" + argument, required=True)
    parser.add_argument("--criu-base-lock", type=Path,
                        help="Exact frozen CRIU base lock; full current CRIU recipe must still match")
    parser.add_argument("--probe-input-build", help="Exact already native-tested consumer helper bytes")
    parser.add_argument("--bounded-bytes", type=int,
                        help="Run the complete original lifecycle in an explicit 4-32 GiB fixed store")
    parser.add_argument("--count", type=int, default=3)
    options = parser.parse_args()
    if not 1 <= options.count <= 10:
        parser.error("one diagnostic sequence admits at most ten complete lifecycles")
    if options.bounded_bytes is not None and (options.bounded_bytes % (1 << 30) or
                                             not 4 << 30 <= options.bounded_bytes <= 32 << 30):
        parser.error("bounded diagnostic requires 4-32 whole GiB")
    _, worker = build_worker.load(options.worker_build)
    _, criu, _, _ = criu_candidate.load(worker["criu_build"], base_lock=options.criu_base_lock)
    if not criu.get("pid_collision_diagnostics"):
        raise ValueError("requires an exact unselected read-only PID diagnostic build")
    artifact = reference.new_artifact("factorybox-restore-collision-probe-" + uuid.uuid4().hex)
    artifact.mkdir()
    shutil.copyfile(__file__, artifact / "runner.py")
    configuration = vars(options).copy()
    if options.criu_base_lock is not None:
        frozen_lock = artifact / "criu-base-lock.json"
        shutil.copyfile(options.criu_base_lock, frozen_lock)
        if reference.sha(frozen_lock) != criu["base_source_lock_sha256"]:
            raise ValueError("frozen CRIU base lock changed during diagnostic setup")
        configuration["criu_base_lock"] = str(options.criu_base_lock)
    identity = benchmark_consumer_start.code_identity()
    reference_lifecycle.write_json(artifact / "frozen-code.json", identity)
    result = {"schema": 1, "passed": False, "selected": False, "production_qualified": False,
              "configuration": configuration, "worker_image_id": worker["image_id"],
              "criu_sha256": criu["binary_sha256"], "rows": [],
              "stop_on_first_failure": True, "runner_sha256": reference.sha(artifact / "runner.py"),
              "historical_failure_explained": False, "scope": __doc__}
    if options.criu_base_lock is not None:
        result["criu_base_lock_sha256"] = reference.sha(frozen_lock)
    print(json.dumps({"running_diagnostic": str(artifact)}), flush=True)
    try:
        for index in range(options.count):
            if benchmark_consumer_start.code_identity() != identity:
                raise ValueError("diagnostic inputs changed; retain the incomplete sequence")
            reference.artifact_storage.require_capacity(reference.ROOT)
            command = [sys.executable, str(reference.ROOT / "tools/consumer_worker_loss.py"),
                       "--worker-build", options.worker_build, "--consumer-inputs", options.consumer_inputs,
                       "--project-images", options.project_images, "--theia-image", options.theia_image,
                       "--example", "factory", "--loss", "planned", "--probe", "lifecycle",
                       "--storage-driver", "overlay2", "--opencode-flow"]
            if options.probe_input_build:
                command += ["--probe-input-build", options.probe_input_build]
            if options.bounded_bytes is not None:
                command += ["--bounded-bytes", str(options.bounded_bytes)]
            row = {"index": index, "command": command, "passed": False}
            result["rows"].append(row)
            reference_lifecycle.write_json(artifact / "result.json", result)
            log = artifact / f"{index:02}.log"
            with log.open("x") as output:
                done = subprocess.run(command, stdout=output, stderr=subprocess.STDOUT, timeout=1500)
            row["exit_code"] = done.returncode
            for line in log.read_text().splitlines():
                try:
                    message = json.loads(line)
                except ValueError:
                    continue
                if isinstance(message, dict) and "running" in message:
                    row["native_artifact"] = Path(message["running"]).name
            if row.get("native_artifact"):
                path = reference.ROOT / "artifacts" / row["native_artifact"] / "result.json"
                native = json.loads(path.read_text())
                row["result_sha256"] = reference.sha(path)
                row["passed"] = (done.returncode == 0 and native.get("passed") is True and
                                 native.get("cleanup") is True and native["worker_image_id"] == worker["image_id"] and
                                 native["lifecycle_criu_sha256"] == criu["binary_sha256"] and
                                 native["lifecycle_cli_sha256"] == worker["bundle_sha256"]["factorybox"] and
                                 native.get("bounded_store_bytes") == options.bounded_bytes)
            print(json.dumps({"diagnostic": str(artifact), **row}), flush=True)
            if not row["passed"]:
                raise ValueError("native diagnostic failed; preserve and inspect this run before any repeat")
            if benchmark_consumer_start.code_identity() != identity:
                raise ValueError("diagnostic source changed during the native run")
            if reference.sha(artifact / "runner.py") != result["runner_sha256"] or reference.sha(Path(__file__)) != result["runner_sha256"]:
                raise ValueError("diagnostic runner changed during the native run")
            if options.criu_base_lock is not None and reference.sha(frozen_lock) != result["criu_base_lock_sha256"]:
                raise ValueError("diagnostic frozen CRIU lock changed")
        result["passed"] = True
    except Exception as error:
        result["error"] = str(error)
    finally:
        reference_lifecycle.write_json(artifact / "result.json", result)
        print(json.dumps({"evidence": str(artifact), "passed": result["passed"], "error": result.get("error")}), flush=True)
    return 0 if result["passed"] else 1


if __name__ == "__main__":
    raise SystemExit(main())
