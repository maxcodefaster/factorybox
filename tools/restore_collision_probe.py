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
    parser.add_argument("--count", type=int, default=3)
    options = parser.parse_args()
    if not 1 <= options.count <= 10:
        parser.error("one diagnostic sequence admits at most ten complete lifecycles")
    _, worker = build_worker.load(options.worker_build)
    _, criu, _, _ = criu_candidate.load(worker["criu_build"])
    if not criu.get("pid_collision_diagnostics"):
        raise ValueError("requires an exact unselected read-only PID diagnostic build")
    artifact = reference.new_artifact("factorybox-restore-collision-probe-" + uuid.uuid4().hex)
    artifact.mkdir()
    shutil.copyfile(__file__, artifact / "runner.py")
    identity = benchmark_consumer_start.code_identity()
    reference_lifecycle.write_json(artifact / "frozen-code.json", identity)
    result = {"schema": 1, "passed": False, "selected": False, "production_qualified": False,
              "configuration": vars(options), "worker_image_id": worker["image_id"],
              "criu_sha256": criu["binary_sha256"], "rows": [],
              "historical_failure_explained": False, "scope": __doc__}
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
                                 native["lifecycle_criu_sha256"] == criu["binary_sha256"])
            print(json.dumps({"diagnostic": str(artifact), **row}), flush=True)
            if not row["passed"]:
                raise ValueError("native diagnostic failed; preserve and inspect this run before any repeat")
            if benchmark_consumer_start.code_identity() != identity:
                raise ValueError("diagnostic source changed during the native run")
        result["passed"] = True
    except Exception as error:
        result["error"] = str(error)
    finally:
        reference_lifecycle.write_json(artifact / "result.json", result)
        print(json.dumps({"evidence": str(artifact), "passed": result["passed"], "error": result.get("error")}), flush=True)
    return 0 if result["passed"] else 1


if __name__ == "__main__":
    raise SystemExit(main())
