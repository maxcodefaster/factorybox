#!/usr/bin/env python3
"""Predetermined full-lifecycle comparison of the optional empty-features patch.

Both configurations use the same corrected fixture, worker, CRIU and Overlay2.
The previous VFS/component comparison and archived observations stay intact.
"""
import argparse
import json
from pathlib import Path
import subprocess
import sys
import time
import uuid

import benchmark_lifecycle
import reference
import reference_lifecycle
import upstream_unit


def code_identity():
    value = benchmark_lifecycle.code_identity()
    for name in ["tests/devx-actor", "tests/devx-registry", "tests/minimal-actor", "tests/ide-filesystem", "tests/consumer-optimizations"]:
        value[name] = upstream_unit.context_manifest(reference.ROOT / name)
    for name in ["tools/benchmark_lifecycle.py", "tools/benchmark_consumer_start.py", "tools/devcontainer_empty_features.py",
                 "tools/verify_fixture_extensions.py", "tests/production-fixture-extensions.json", "tests/compatibility/actor-stream-error.patch"]:
        value[name] = reference.sha(reference.ROOT / name)
    return value


def exact_inputs(record, expected, example, configuration):
    if not record.get("passed") or not record.get("cleanup"):
        raise ValueError("a failed or incomplete native run cannot qualify")
    if (record["example"] != example or record.get("probe") != "lifecycle" or record.get("storage_driver") != "overlay2" or
            record.get("consumer_request_profile") or record.get("baseline_input") or
            record.get("consumer_empty_features") != (configuration == "candidate") or
            record.get("opencode_flow") != (example == "factory") or
            record.get("fixture_verification", {}).get("production_extensions") != 1):
        raise ValueError("native consumer configuration differs from the fixed comparison")
    identity = {key: record[key] for key in ["worker_image_id", "lifecycle_cli_sha256", "lifecycle_criu_sha256",
               "consumer_rootfs_sha256", "project_images_sha256", "example_manifest", "source_context"]}
    if expected is not None and identity != expected:
        raise ValueError("native bytes or fixture changed within one example's comparison")
    return identity


def summarize(rows):
    report = benchmark_lifecycle.summarize(rows)
    report.pop("factory_halving_target_met")
    complete = report["complete_five_per_configuration"]
    report["fresh_start_target_met"] = complete and all(
        report["examples"][example]["prepared-start-first-use"]["reduction_fraction"] >= .2 and
        report["examples"][example]["prepared-start-first-use"]["reference"]["median_seconds"] -
        report["examples"][example]["prepared-start-first-use"]["candidate"]["median_seconds"] >= 1.
        for example in ["factory", "minimal"])
    excluded = {"preparation", "worker-startup", "cold-worker-to-useful"}
    report["regression_limits_met"] = complete and all(
        metric["within_regression_limit"] for values in report["examples"].values()
        for name, metric in values.items() if name not in excluded)
    report["predeclared_targets"] = {"prepared_start_minimum_reduction_fraction": .2,
                                     "prepared_start_minimum_saved_seconds": 1.,
                                     "other_lifecycle_regression_allowance": "max(20 percent, 250 ms)",
                                     "cold_preparation": "Report every observation; do not fold into prepared-start latency."}
    return report


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ["worker-build", "consumer-inputs", "factory-images", "minimal-images", "theia-image"]:
        parser.add_argument("--" + name, required=True)
    options = parser.parse_args()
    artifact = reference.new_artifact("factorybox-consumer-start-comparison-" + uuid.uuid4().hex)
    artifact.mkdir()
    identity = code_identity()
    reference_lifecycle.write_json(artifact / "frozen-code.json", identity)
    (artifact / "runner.py").write_bytes(Path(__file__).read_bytes())
    result = {"schema": 1, "passed": False, "qualification_complete": False, "selected": False,
              "production_qualified": False, "configuration": vars(options), "rows": [],
              "predetermined_order": benchmark_lifecycle.ORDER, "example_order": ["factory", "minimal"],
              "fixture": "Same corrected stream-error authority in both configurations; no request telemetry.",
              "candidate": "Only the declared empty-features consumer CLI patch; exact worker/CRIU/Overlay2 retained.",
              "target": "Five observations per configuration/example; >=20 percent and >=1 second prepared-start reduction in both, other lifecycle medians within max(20 percent, 250 ms).",
              "cache_conditions": "Fresh owned worker/store each run; no host tuning or cache clearing. Record uncontrolled desktop load; no p95 estimate."}
    path = artifact / "result.json"
    inputs = {}
    print(json.dumps({"running_comparison": str(artifact), "native_runs": 20}), flush=True)
    try:
        for position, configuration in enumerate(benchmark_lifecycle.ORDER):
            for example in ["factory", "minimal"]:
                if code_identity() != identity:
                    raise ValueError("comparison source changed; preserve the partial matrix")
                reference.artifact_storage.require_capacity(reference.ROOT)
                row = {"position": position, "configuration": configuration, "example": example,
                       "passed": False, "host_before": benchmark_lifecycle.snapshot()}
                result["rows"].append(row)
                reference_lifecycle.write_json(path, result)
                command = [sys.executable, str(reference.ROOT / "tools/consumer_worker_loss.py"),
                           "--worker-build", options.worker_build, "--consumer-inputs", options.consumer_inputs,
                           "--project-images", getattr(options, example + "_images"), "--example", example,
                           "--loss", "planned", "--probe", "lifecycle", "--storage-driver", "overlay2"]
                if example == "factory":
                    command += ["--theia-image", options.theia_image, "--opencode-flow"]
                if configuration == "candidate":
                    command += ["--consumer-empty-features"]
                row["command"] = command
                started = time.monotonic()
                log = artifact / f"{position:02}-{configuration}-{example}.log"
                with log.open("x") as stream:
                    done = subprocess.run(command, stdout=stream, stderr=subprocess.STDOUT, timeout=1500)
                row.update(exit_code=done.returncode, wall_seconds=time.monotonic() - started,
                           host_after=benchmark_lifecycle.snapshot())
                for line in log.read_text().splitlines():
                    try:
                        value = json.loads(line)
                    except ValueError:
                        continue
                    if isinstance(value, dict) and "running" in value:
                        row["native_artifact"] = Path(value["running"]).name
                if done.returncode != 0 or not row.get("native_artifact"):
                    raise ValueError("native comparison failed; retain its failure without replacement or omission")
                native = reference.ROOT / "artifacts" / row["native_artifact"]
                record = json.loads((native / "result.json").read_text())
                inputs[example] = exact_inputs(record, inputs.get(example), example, configuration)
                row["observation"] = benchmark_lifecycle.observations(native)
                if code_identity() != identity:
                    raise ValueError("comparison source changed during the native run")
                row["passed"] = True
                reference_lifecycle.write_json(path, result)
                print(json.dumps({"comparison": str(artifact), "position": position, "configuration": configuration,
                                  "example": example, "passed": True, "native_artifact": row["native_artifact"],
                                  "prepared_start_seconds": row["observation"]["metrics_seconds"]["prepared-start-first-use"]}), flush=True)
        result["report"] = summarize(result["rows"])
        result["passed"] = result["report"]["complete_five_per_configuration"]
        result["qualification_complete"] = result["passed"] and result["report"]["fresh_start_target_met"] and result["report"]["regression_limits_met"]
    except Exception as error:
        result["error"] = str(error)
        raise
    finally:
        reference_lifecycle.write_json(path, result)
        print(json.dumps({"evidence": str(artifact), "passed": result["passed"], "qualification_complete": result["qualification_complete"]}), flush=True)
    return 0 if result["qualification_complete"] else 1


if __name__ == "__main__":
    raise SystemExit(main())
