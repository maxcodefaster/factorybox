#!/usr/bin/env python3
"""Compare the actual pinned original/patched consumer feature-control behavior."""
import argparse
import json
from pathlib import Path
import shutil
import subprocess
import uuid

import devcontainer_empty_features
import devx_inputs
import reference
import reference_lifecycle
import upstream_unit


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--consumer-inputs", required=True)
    parser.add_argument("--source-artifact", required=True)
    options = parser.parse_args()
    if Path(options.source_artifact).name != options.source_artifact:
        parser.error("expected one artifact name")
    source = reference.ROOT / "artifacts" / options.source_artifact
    verdict = json.loads((source / "result.json").read_text())
    if not verdict.get("passed") or not verdict.get("cleanup"):
        raise ValueError("feature probe needs a complete passing source extraction")
    patch_path = reference.ROOT / "tests/consumer-optimizations/0001-devcontainer-empty-features.json"
    patch = json.loads(patch_path.read_text())
    before = (source / "before.js").read_bytes()
    after = devcontainer_empty_features.transform(before, patch)
    if after != (source / "after.js").read_bytes():
        raise ValueError("extracted consumer patch differs")
    _, consumers = devx_inputs.load(options.consumer_inputs, rootfs=True, platform="linux/amd64")
    owner = uuid.uuid4().hex
    name = "factorybox-devcontainer-feature-probe-" + owner
    artifact = reference.new_artifact(name)
    artifact.mkdir()
    inputs = artifact / "input"
    inputs.mkdir()
    # Small source copies: these inputs are an extraction, not a component
    # build eligible for the shared immutable-build-input optimization.
    (inputs / "before.js").write_bytes(before)
    (inputs / "after.js").write_bytes(after)
    shutil.copyfile(patch_path, inputs / "patch.json")
    shutil.copyfile(reference.ROOT / "tests/consumer-optimizations/probe.cjs", inputs / "probe.cjs")
    shutil.copyfile(__file__, artifact / "runner.py")
    result = {"schema": 1, "owner": owner, "source_artifact": options.source_artifact,
              "passed": False, "cleanup": False, "selected": False, "production_qualified": False,
              "inputs": upstream_unit.context_manifest(inputs)}
    command = [*reference.DOCKER, "run", "--pull=never", "--name", name, "--label", "com.factorybox.run=" + owner,
               "--cpus=3", "--memory=3g", "--pids-limit=64", "--network=none", "--read-only", "--cap-drop=ALL",
               "--security-opt=no-new-privileges", "--tmpfs", "/var/lib/docker:rw,nosuid,nodev,size=16m",
               "--mount", f"type=bind,source={inputs},target=/input,readonly", "--entrypoint", "/usr/local/bin/node",
               consumers["images"]["build-runtime-host"]["image"], "/input/probe.cjs"]
    result["command"] = command
    try:
        with (artifact / "stdout.json").open("x") as out, (artifact / "stderr.txt").open("x") as err:
            done = subprocess.run(command, stdout=out, stderr=err, timeout=20)
        done.check_returncode()
        observed = json.loads((artifact / "stdout.json").read_text())
        result["cases"] = len(observed["cases"])
        result["inputs_unchanged"] = result["inputs"] == upstream_unit.context_manifest(inputs)
        result["passed"] = observed["passed"] and result["cases"] == 9 and result["inputs_unchanged"]
    except Exception as error:
        result["error"] = str(error)
    finally:
        reference.owned_delete("container", name, owner)
        result["cleanup"] = True
        reference_lifecycle.write_json(artifact / "result.json", result)
        print(json.dumps({"evidence": str(artifact), "passed": result["passed"], "cases": result.get("cases"), "error": result.get("error")}), flush=True)
    return 0 if result["passed"] else 1


if __name__ == "__main__":
    raise SystemExit(main())
