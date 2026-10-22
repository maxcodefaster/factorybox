#!/usr/bin/env python3
"""Retain a predetermined interleaved comparison of both complete consumers."""
import argparse
import json
import math
from pathlib import Path
import re
import statistics
import subprocess
import sys
import time
import uuid

import reference
import reference_lifecycle
import upstream_unit


ORDER = ["reference", "candidate", "candidate", "reference", "reference",
         "candidate", "candidate", "reference", "reference", "candidate"]
FORKS = {"a": "fork-first-use", "b": "fork-of-fork-first-use", "d": "fork-of-resumed-first-use"}
CHECKPOINTS = {"a": "checkpoint-source", "b": "checkpoint-fork", "d": "checkpoint-resumed"}


def snapshot():
    return {name: Path("/proc"+name).read_text() for name in
            ["/loadavg", "/meminfo", "/pressure/cpu", "/pressure/io", "/pressure/memory"]}


def code_identity():
    names = ["internal", "cmd", "tests/devx-lifecycle", "tests/opencode-flow", "examples/minimal", "examples/factory"]
    files = ["tools/consumer_worker_loss.py", "tools/worker_readiness.py", "tests/consumer-worker-setup.sh",
             "tests/minimal-sandbox.sh", "tests/devx-sandbox.sh", "tests/devx-devcontainer.sh", "go.mod", "go.sum"]
    value = {name: upstream_unit.context_manifest(reference.ROOT/name) for name in names}
    value.update({name: reference.sha(reference.ROOT/name) for name in files})
    return value


def observations(artifact):
    artifact = Path(artifact)
    result = json.loads((artifact/"result.json").read_text())
    if not result.get("passed") or not result.get("cleanup") or result.get("probe") != "lifecycle" or len(result["phases"]) != 1:
        raise ValueError("not a complete passing ordinary lifecycle observation")
    phase = result["phases"][0]
    values = {row["phase"]: row["milliseconds"]/1000 for row in phase["observations"]}
    if len(values) != 8:
        raise ValueError("useful lifecycle observations are missing or duplicated")
    metrics = dict(values)
    metrics.update({"preparation": phase["preparation"]["seconds"], "worker-startup": phase["startup"]["seconds"]})
    metrics["cold-worker-to-useful"] = metrics["worker-startup"]+metrics["preparation"]+metrics["prepared-start-first-use"]
    output, captured, pauses, payloads = artifact/"prepare-output", {}, [], {}
    for path in sorted(output.glob("cli-*.timing.json")):
        row = json.loads(path.read_text())
        args = row["arguments"]
        if args[0] == "checkpoint":
            token = args[args.index("--snapshot-id")+1]
            if token not in [letter*32 for letter in CHECKPOINTS] or token in captured:
                raise ValueError("checkpoint timing differs from the complete fixed lifecycle")
            captured[token] = row["seconds"]
            checkpoint = json.loads(path.with_name(path.name.removesuffix(".timing.json")+".json").read_text())
            roots = checkpoint["filesystem"]["roots"]
            payloads[CHECKPOINTS[token[0]]] = {
                "checkpoint_metadata_sha256": reference.sha(path.with_name(path.name.removesuffix(".timing.json")+".json")),
                "roots": [{key: tree.get(key) for key in ["name","entries","file_bytes","hashed_bytes","capture_us","source_read_us","copy_us","target_read_us"]} | {"shared_immutable": tree.get("immutable") is not None} for tree in roots],
                "independent_mutable_file_bytes": sum(tree["file_bytes"] for tree in roots if tree.get("immutable") is None),
                "image_file_bytes": sum(file["bytes"] for file in checkpoint["files"] if file["name"].startswith("images/"))}
        if args[0] == "pause":
            pauses.append(row["seconds"])
            receipt = json.loads(path.with_name(path.name.removesuffix(".timing.json")+".json").read_text())["record"]["pause"]
    if len(captured) != 3 or len(pauses) != 1:
        raise ValueError("checkpoint or pause timings missing or duplicated")
    metrics["pause"] = pauses[0]
    for letter, name in CHECKPOINTS.items():
        metrics[name] = captured[letter*32]
        metrics["capture-plus-"+FORKS[letter]] = captured[letter*32]+metrics[FORKS[letter]]
    theia = output/"theia-observations.json"
    if result["example"] == "factory":
        rows = json.loads(theia.read_text())
        for name in values:
            matches = [row["milliseconds"]/1000 for row in rows if row["phase"] == name]
            if len(matches) != 1:
                raise ValueError("separate IDE omitted or duplicated a lifecycle observation")
            metrics["theia-"+name] = matches[0]
    return {"artifact": artifact.name, "result_sha256": reference.sha(artifact/"result.json"),
            "metrics_seconds": metrics, "pause_memory": receipt, "checkpoint_payloads": payloads,
            "cli_sha256": result["lifecycle_cli_sha256"], "criu_sha256": result["lifecycle_criu_sha256"],
            "worker_image_id": result["worker_image_id"], "storage_driver": result["storage_driver"],
            "preparation": phase["preparation"],
            "worker_resources": [{"phase": row["phase"], "resources": row["worker_resources"]} for row in phase["observations"]]}


def summarize(rows):
    reports = {}
    complete = all(sum(row.get("passed") and row["example"] == example and row["configuration"] == configuration for row in rows) == 5
                   for example in ["factory", "minimal"] for configuration in ["reference", "candidate"])
    for example in ["factory", "minimal"]:
        grouped = {configuration: [row["observation"]["metrics_seconds"] for row in rows if row.get("passed") and row["example"] == example and row["configuration"] == configuration]
                   for configuration in ["reference", "candidate"]}
        observed = [value for group in grouped.values() for value in group]
        if observed and any(set(value) != set(observed[0]) for value in observed):
            raise ValueError("metric phases differ within the fixed example comparison")
        if any(not isinstance(number,(int,float)) or not math.isfinite(number) or number <= 0
               for value in observed for number in value.values()):
            raise ValueError("comparison contains a nonpositive or nonfinite duration")
        metrics = set(observed[0]) if observed and all(grouped.values()) else set()
        reports[example] = {}
        for name in sorted(metrics):
            report = {}
            for configuration, group in grouped.items():
                values = [value[name] for value in group]
                report[configuration] = {"n": len(values), "values_seconds": values,
                                         "median_seconds": statistics.median(values), "min_seconds": min(values), "max_seconds": max(values)}
            old, new = (report[key]["median_seconds"] for key in ["reference", "candidate"])
            report["reduction_fraction"] = 1-new/old
            report["regression_allowance_seconds"] = max(.25, .2*old)
            report["within_regression_limit"] = new-old <= report["regression_allowance_seconds"]
            reports[example][name] = report
    targets = list(CHECKPOINTS.values())+list(FORKS.values())+["pause"]
    speed = complete and all(reports["factory"][name]["reduction_fraction"] >= .5 for name in targets)
    limits = complete and all(value["within_regression_limit"] for name,value in reports["minimal"].items()
                              if name not in {"preparation", "worker-startup", "cold-worker-to-useful"})
    limits = limits and all(reports["factory"][name]["within_regression_limit"] for name in
                           ["prepared-start-first-use", "resume-first-use", "theia-prepared-start-first-use", "theia-resume-first-use"]) if complete else False
    return {"complete_five_per_configuration": complete, "factory_halving_target_met": speed,
            "regression_limits_met": limits, "examples": reports,
            "p95_estimated": False, "scope": "Medians and ranges of these predetermined shared-host observations only. No tail-latency or production SLA."}


def resume_prefix(result):
    expected=[(position,configuration,example) for position,configuration in enumerate(ORDER) for example in ["factory","minimal"]]
    rows=result["rows"]
    if (result.get("passed") is not False or not result.get("error","").startswith("artifact storage uses ") or
            not rows or len(rows)>=len(expected) or not all(row.get("passed") is True for row in rows) or
            [(row["position"],row["configuration"],row["example"]) for row in rows]!=expected[:len(rows)] or
            len({row["native_artifact"] for row in rows})!=len(rows) or result.get("predetermined_order")!=ORDER):
        raise ValueError("resume requires the exact completed prefix interrupted before a native run by the storage guard")
    return len(rows)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    fields=["worker-build", "baseline-runtime-build", "baseline-criu-build", "consumer-inputs", "factory-images", "minimal-images", "theia-image"]
    for name in fields:
        parser.add_argument("--"+name)
    parser.add_argument("--resume",help="Resume the exact successful predetermined prefix after a between-run storage guard")
    parser.add_argument("--maintenance-receipt",help="Verified storage-maintenance artifact retained in the comparison interruption record")
    args = parser.parse_args()
    prefix=0
    if args.resume:
        if not re.fullmatch(r"factorybox-lifecycle-comparison-[0-9a-f]{32}",args.resume) or not args.maintenance_receipt or not re.fullmatch(r"factorybox-storage-share-inputs-[0-9a-f]{32}",args.maintenance_receipt):
            parser.error("resume requires exact comparison and closed-input maintenance artifacts")
        artifact=reference.ROOT/"artifacts"/args.resume
        if artifact.resolve()!=artifact or (artifact/"result.json").is_symlink():raise ValueError("comparison cannot be redirected")
        result=json.loads((artifact/"result.json").read_text())
        prefix=resume_prefix(result)
        identity=json.loads((artifact/"frozen-code.json").read_text())
        if code_identity()!=identity:raise ValueError("comparison source changed during the maintenance interruption")
        for field in fields:
            key=field.replace("-","_")
            if getattr(args,key) not in {None,result["configuration"][key]}:raise ValueError("resumed comparison input differs: "+key)
            setattr(args,key,result["configuration"][key])
        for row in result["rows"]:
            if observations(reference.ROOT/"artifacts"/row["native_artifact"])!=row["observation"]:
                raise ValueError("completed observation changed during maintenance")
        receipt=reference.ROOT/"artifacts"/args.maintenance_receipt/"result.json"
        if receipt.resolve()!=receipt or json.loads(receipt.read_text()).get("passed") is not True:raise ValueError("maintenance receipt is not complete")
        reference.artifact_storage.require_capacity(reference.ROOT)
        step=len(result.get("interruptions",[]))+1
        old=artifact/f"before-resume-{step}.json"
        with old.open("xb") as stream:stream.write((artifact/"result.json").read_bytes())
        runner=artifact/f"resume-runner-{step}.py"
        with runner.open("xb") as stream:stream.write(Path(__file__).read_bytes())
        result.setdefault("interruptions",[]).append({"reason":result.pop("error"),"before_result":old.name,"before_result_sha256":reference.sha(old),
            "completed_prefix":prefix,"maintenance_receipt":args.maintenance_receipt,"maintenance_result_sha256":reference.sha(receipt),
            "resume_runner_sha256":reference.sha(runner),"host_after_maintenance":snapshot(),
            "scope":"No native measurement overlapped maintenance. All prior source identities, native result hashes and observations were reverified; retain the original order and all prior values. Cache conditions may change across this recorded gap."})
    else:
        if args.maintenance_receipt or any(getattr(args,name.replace("-","_")) is None for name in fields):parser.error("new comparisons require every exact input")
        artifact = reference.new_artifact("factorybox-lifecycle-comparison-"+uuid.uuid4().hex); artifact.mkdir()
        identity = code_identity()
        reference_lifecycle.write_json(artifact/"frozen-code.json", identity)
        (artifact/"runner.py").write_bytes(Path(__file__).read_bytes())
        result = {"schema": 1, "passed": False, "qualification_complete": False, "selected": False,
                  "configuration": {name.replace("-","_"):getattr(args,name.replace("-","_")) for name in fields}, "predetermined_order": ORDER, "example_order": ["factory", "minimal"],
                  "cache_conditions": "No cache clearing, warming pass or host tuning. Each worker/store is fresh; immutable input bytes are identical. The host page cache and other desktop activity are uncontrolled and observed.",
                  "rows": [], "production_qualified": False}
    path = artifact/"result.json"
    try:
        for position, configuration in enumerate(ORDER):
            for example in ["factory", "minimal"]:
                if position*2+(example=="minimal")<prefix:continue
                if code_identity() != identity:
                    raise ValueError("comparison source changed; retain the partial matrix without mixing inputs")
                reference.artifact_storage.require_capacity(reference.ROOT)
                row = {"position": position, "configuration": configuration, "example": example,
                       "passed": False, "host_before": snapshot()}
                result["rows"].append(row)
                reference_lifecycle.write_json(path, result)
                command = [sys.executable, str(reference.ROOT/"tools/consumer_worker_loss.py"),
                           "--worker-build", args.worker_build, "--consumer-inputs", args.consumer_inputs,
                           "--project-images", getattr(args, example+"_images"), "--example", example,
                           "--loss", "planned", "--probe", "lifecycle", "--storage-driver",
                           "overlay2" if configuration == "candidate" and example == "factory" else "vfs"]
                if example == "factory": command += ["--theia-image", args.theia_image]
                if configuration == "reference":
                    command += ["--baseline-runtime-build", args.baseline_runtime_build, "--baseline-criu-build", args.baseline_criu_build]
                row["command"] = command
                log = artifact/(f"{position:02d}-"+configuration+"-"+example+".log")
                started = time.monotonic()
                with log.open("x") as stream:
                    done = subprocess.run(command, stdout=stream, stderr=subprocess.STDOUT, timeout=1500)
                row.update(exit_code=done.returncode, wall_seconds=time.monotonic()-started, host_after=snapshot())
                for line in log.read_text().splitlines():
                    try: value=json.loads(line)
                    except json.JSONDecodeError: continue
                    if isinstance(value,dict) and "running" in value: row["native_artifact"] = Path(value["running"]).name
                if done.returncode == 0 and row.get("native_artifact"):
                    row["observation"] = observations(reference.ROOT/"artifacts"/row["native_artifact"])
                    row["passed"] = True
                reference_lifecycle.write_json(path, result)
                print(json.dumps({"comparison": str(artifact), "position": position, "configuration": configuration,
                                  "example": example, "passed": row["passed"], "native_artifact": row.get("native_artifact")}), flush=True)
        result["report"] = summarize(result["rows"])
        result["passed"] = result["report"]["complete_five_per_configuration"]
        result["qualification_complete"] = result["passed"] and result["report"]["factory_halving_target_met"] and result["report"]["regression_limits_met"]
    except Exception as error:
        result["error"] = str(error)
        raise
    finally:
        reference_lifecycle.write_json(path, result)
        print(json.dumps({"evidence": str(artifact), "passed": result["passed"], "qualification_complete": result["qualification_complete"]}), flush=True)


if __name__ == "__main__": main()
