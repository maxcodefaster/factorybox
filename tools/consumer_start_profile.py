#!/usr/bin/env python3
"""Extract fresh-start timings from an existing completed Factory lifecycle proof."""
import argparse
import json
import pathlib
import shutil
import uuid

import reference


def parse(connector, devcontainer):
    phases = []
    for line in connector.splitlines():
        if line.startswith("factory-startup "):
            row = json.loads(line.removeprefix("factory-startup "))
            if row["durationMs"] < 0:
                raise ValueError("negative connector duration")
            phases.append(row)
    names = [row["phase"] for row in phases]
    required = ["private-docker", "checkout", "devcontainer-configuration", "project-images",
                "devcontainer-merged-configuration", "devcontainer-start", "tool-injection", "process-start", "channel-readiness"]
    if names != required:
        raise ValueError("expected exactly one complete original connector startup")
    spans, failures = [], []
    remote_start = None
    for line in devcontainer.splitlines():
        row = json.loads(line)
        if row.get("type") == "start" and row.get("text") == "Resolving Remote":
            remote_start = row["timestamp"]
        if row.get("type") == "stop" and "startTimestamp" in row:
            duration = row["timestamp"] - row["startTimestamp"]
            if duration >= 250:
                spans.append({"text": row["text"], "milliseconds": duration,
                              "note": "Nested intervals may overlap; do not sum these spans."})
        if "Failed to fetch control manifest" in row.get("text", ""):
            failures.append({"text": row["text"], "timestamp": row["timestamp"],
                             "milliseconds_since_resolving_remote": None if remote_start is None else row["timestamp"] - remote_start,
                             "note": "Observed elapsed window, not a separately instrumented request duration."})
    return {"connector_phases": phases, "connector_phase_milliseconds": sum(row["durationMs"] for row in phases),
            "devcontainer_slow_spans": spans, "control_manifest_failures": failures}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--artifact", required=True)
    options = parser.parse_args()
    if pathlib.PurePath(options.artifact).name != options.artifact:
        parser.error("expected one local completed artifact name")
    source = reference.ROOT / "artifacts" / options.artifact
    verdict = json.loads((source / "result.json").read_text())
    if not verdict.get("passed") or not verdict.get("cleanup") or verdict.get("example") != "factory" or verdict.get("probe") != "lifecycle":
        raise ValueError("profiling requires a completed passing Factory lifecycle proof")
    # The branch inherits the original fresh-start logs, retained before deletion.
    output = source / "prepare-output"
    inputs = [source / "result.json", output / "branch-factorybox-connector.log", output / "branch-factorybox-devcontainer.log",
              output / "devx-observations.json", output / "preparation.json", output / "cli-01-prepare-base.timing.json"]
    profile = parse(inputs[1].read_text(), inputs[2].read_text())
    observations = json.loads(inputs[3].read_text())
    first = [row for row in observations if row["phase"] == "prepared-start-first-use"]
    if len(first) != 1:
        raise ValueError("original first useful-work observation is ambiguous")
    artifact = reference.new_artifact("factorybox-consumer-start-profile-" + uuid.uuid4().hex)
    artifact.mkdir()
    shutil.copyfile(__file__, artifact / "runner.py")
    profile.update(schema=1, source_artifact=options.artifact,
                   source_hashes={str(path.relative_to(source)): reference.sha(path) for path in inputs},
                   prepared_start_first_use=first[0], rootfs_preparation=json.loads(inputs[4].read_text()),
                   immutable_base_preparation=json.loads(inputs[5].read_text()),
                   worker_startup=verdict["phases"][0]["startup"],
                   note="Analysis of one existing observation. No changed consumer, inferred p95 or new performance qualification.",
                   production_qualified=False)
    (artifact / "result.json").write_text(json.dumps(profile, indent=2) + "\n")
    print(json.dumps({"evidence": str(artifact), "prepared_start_ms": first[0]["milliseconds"],
                      "connector_ms": profile["connector_phase_milliseconds"], "control_manifest_failures": profile["control_manifest_failures"]}), flush=True)


if __name__ == "__main__":
    main()
