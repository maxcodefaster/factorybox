#!/usr/bin/env python3
"""Serialize the final pilot consumer, interruption and upstream core gates."""
import argparse
import json
from pathlib import Path
import subprocess
import sys
import uuid

import build_worker
import reference
import reference_lifecycle
import upstream_unit
import lifecycle_native


SUITES = ("factory-agent", "minimal-planned-loss", "retained-corruption",
          "retained-child", "retained-controller", "retained-guard", "retained-churn",
          "docker-root", "docker-daemon", "docker-pause", "docker-helper",
          "docker-controller", "docker-guard", "libcontainer", "runc-integration")
ORIGINAL_INTEGRATION_SKIPS = (
    "TestUsernsCheckpoint", "TestCheckpoint", "TestSystemdFreeze", "TestCpuShares",
    "TestCpuSharesSystemd", "TestPidsSystemd", "TestCgroupResourcesUnifiedErrorOnV1",
    "TestCgroupResourcesUnifiedErrorOnV1Systemd", "TestCgroupResourcesUnifiedSystemd",
    "TestRootfsPropagationSharedMount", "TestPIDHost", "TestExecInTTY")


def corruption_oracle_rejected(saved, output, exit_code):
    """Accept only the deliberately red complete-RAM assertion, not other failures."""
    oracle = "read-only diagnostic found changed original 32-MiB RAM"
    failed = [line for line in output.splitlines() if line.startswith("--- FAIL:")]
    return (exit_code != 0 and saved.get("passed") is False
            and saved.get("cleanup") is True and saved.get("inputs_unchanged") is True
            and saved.get("corruption_control") is True and saved.get("exit_code") == 1
            and not saved.get("container_state", {}).get("OOMKilled", True)
            and saved.get("passed_tests") == lifecycle_native.EXPECTED_ADAPTER_TESTS
            and saved.get("passed_subtests") == lifecycle_native.EXPECTED_ADAPTER_SUBTESTS
            and len(failed) == 1 and failed[0].startswith("--- FAIL: TestInterruptedCaptureNative ")
            and output.count(oracle) == 1 and "--- SKIP:" not in output and "panic:" not in output)


def original_upstream_coverage(name, suites):
    if set(suites or {}) != {name}:
        return False
    saved = suites[name]
    main, subtests, skips = (28, 30, ()) if name == "libcontainer" else (47, 0, ORIGINAL_INTEGRATION_SKIPS)
    return (saved.get("passed_top_level_tests") == main and saved.get("passed_subtests") == subtests
            and sorted(saved.get("skipped_tests", [])) == sorted(skips))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ("worker-build", "consumer-inputs", "project-images", "minimal-project-images", "theia-image"):
        parser.add_argument("--" + name, required=True)
    parser.add_argument("--suite", action="append", choices=SUITES)
    options = parser.parse_args()
    selected = options.suite or list(SUITES)
    if len(selected) != len(set(selected)):
        parser.error("duplicate suite")
    _, worker = build_worker.load(options.worker_build)
    artifact = reference.new_artifact("factorybox-pilot-integration-gates-" + uuid.uuid4().hex)
    artifact.mkdir()
    (artifact / "runner.py").write_bytes(Path(__file__).read_bytes())
    result = {"schema": 1, "worker_build": options.worker_build, "worker_image_id": worker["image_id"],
              "selected_suites": selected, "passed": False, "production_qualified": False, "rows": [],
              "scope": "Actual delivered-image consumers; exact-core native fault probes and unchanged upstream core/integration assertions are separate gates. Original upstream skips remain visible."}

    def command(name):
        python = sys.executable
        script = lambda path: str(reference.ROOT / path)
        components = ["--native-base-build", worker["native_base_build"], "--sysbox-build", worker["sysbox_build"]]
        if name in {"factory-agent", "minimal-planned-loss"}:
            common = [python, script("tools/consumer_worker_loss.py"), "--worker-build", options.worker_build,
                      "--consumer-inputs", options.consumer_inputs, "--loss", "planned"]
            if name == "factory-agent":
                return common + ["--project-images", options.project_images, "--example", "factory", "--probe", "lifecycle",
                                 "--storage-driver", "overlay2", "--theia-image", options.theia_image, "--opencode-flow"]
            return common + ["--project-images", options.minimal_project_images, "--example", "minimal"]
        if name.startswith("retained-"):
            common = [python, script("tests/criu-retained-candidate/native.py"), *components,
                      "--candidate", worker["criu_build"], "--candidate-platform", "linux/amd64"]
            if name == "retained-corruption":
                return common + ["--corruption-control"]
            common += ["--pause", "--grant-control", "--multiprocess"]
            if name != "retained-controller":
                common += ["--interrupt-child"]
            if name in {"retained-controller", "retained-guard"}:
                common += ["--" + name.removeprefix("retained-") + "-loss"]
            if name == "retained-churn":
                common += ["--thread-churn"]
            return common
        if name.startswith("docker-"):
            process = "dockerd" if name in {"docker-daemon", "docker-pause", "docker-controller", "docker-guard"} else "root"
            operation = "pause" if name in {"docker-pause", "docker-controller", "docker-guard"} else "checkpoint"
            component = name.removeprefix("docker-") if name in {"docker-helper", "docker-controller", "docker-guard"} else "tracer"
            return [python, script("tests/docker-overlay-candidate/native.py"), *components,
                    "--retained-candidate", worker["criu_build"], "--consumer-inputs", options.consumer_inputs,
                    "--project-images", options.project_images, "--candidate-platform", "linux/amd64", "--ordinary-build",
                    "--suite", "docker-interruption", "--docker-fault-process", process,
                    "--docker-fault-operation", operation, "--docker-fault-component", component]
        return [python, script("tools/upstream_unit.py"), "--sysbox-build", worker["sysbox_build"],
                "--candidate-platform", "linux/amd64", "--bounded-build", "--network", "host", "--suite", name]

    def check_core():
        manifest = {}
        for directory in ("internal", "cmd/factorybox"):
            manifest.update({directory + "/" + name: value
                             for name, value in upstream_unit.context_manifest(reference.ROOT / directory).items()})
        manifest.update({name: {"sha256": reference.sha(reference.ROOT / name)} for name in ("go.mod", "go.sum")})
        core = lambda entries: {name: value for name, value in entries.items() if not name.endswith("_test.go")}
        if core(manifest) != core(worker["source_context"]):
            raise ValueError("final gate core differs from delivered worker")

    try:
        for name in selected:
            check_core()
            row = {"suite": name, "command": command(name), "passed": False}
            result["rows"].append(row)
            reference_lifecycle.write_json(artifact / "result.json", result)
            print(json.dumps({"integration_gates": str(artifact), "starting": name}), flush=True)
            log = artifact / (name + ".log")
            with log.open("x") as stream:
                done = subprocess.run(row["command"], stdout=stream, stderr=subprocess.STDOUT)
            row["exit_code"] = done.returncode
            for line in log.read_text().splitlines():
                try:
                    value = json.loads(line)
                except json.JSONDecodeError:
                    continue
                if isinstance(value, dict) and "evidence" in value:
                    row["evidence"] = value["evidence"]
            if row.get("evidence"):
                child = Path(row["evidence"]) / "result.json"
                if not child.resolve().is_relative_to(reference.ROOT / "artifacts"):
                    raise ValueError("child evidence escaped artifacts")
                saved = json.loads(child.read_text())
                row.update(result_sha256=reference.sha(child),
                           passed=done.returncode == 0 and saved.get("passed") is True and saved.get("cleanup") is True)
                if name == "retained-corruption":
                    # This deliberately changes real source RAM while retaining
                    # the application's cached digest. The original assertion
                    # must go red; preserve its failed native result verbatim.
                    output = (child.parent / "stdout.txt").read_text()
                    oracle = "read-only diagnostic found changed original 32-MiB RAM"
                    row.update(expected_test_failure=True, oracle=oracle,
                               passed=corruption_oracle_rejected(saved, output, done.returncode))
                if name in {"libcontainer", "runc-integration"}:
                    row["upstream_suites"] = saved.get("suites")
                    row["original_skip_set_and_coverage"] = original_upstream_coverage(name, saved.get("suites"))
                    row["passed"] = row["passed"] and row["original_skip_set_and_coverage"]
            reference_lifecycle.write_json(artifact / "result.json", result)
            print(json.dumps({"suite": name, "passed": row["passed"], "evidence": row.get("evidence")}), flush=True)
            if not row["passed"]:
                raise RuntimeError("final integration gate failed: " + name)
        result["passed"] = True
    except Exception as error:
        result["error"] = str(error)
        raise
    finally:
        reference_lifecycle.write_json(artifact / "result.json", result)
        print(json.dumps({"evidence": str(artifact), "passed": result["passed"]}), flush=True)


if __name__ == "__main__":
    main()
