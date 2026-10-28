#!/usr/bin/env python3
"""Build one local worker target. Never publish or admit a runtime platform."""
import argparse
import json
from pathlib import Path
import subprocess
import sys
import uuid

import build_config
import build_criu_shared_fds
import build_worker
import criu_candidate
import native_base
import reference
import reference_lifecycle


def commands(options, sysbox, criu):
    common = [sys.executable]
    return {
        "sysbox": [*common, "tools/build_sysbox.py", "--platform", options.platform,
                   "--network", options.network, "--shm-startup-reexec",
                   *(["--builder", options.builder] if options.builder else []),
                   *(["--bounded-build"] if options.bounded_build else [])],
        "criu": [*common, "tests/criu-retained-candidate/build.py",
                 "--native-base-build", options.native_base_build,
                 "--exit-hold", "--exit-hold-socket", "--nested-overlay",
                 "--nested-overlay-stable-paths", "--nested-overlay-shared-root"],
        "recovery": [*common, "tools/build_criu_recovery.py", "--base-candidate", criu],
        "shared-fds": [*common, "tools/build_criu_shared_fds.py",
                       "--base-candidate", criu],
        "namespace-access": [*common, "tools/build_criu_shared_fds.py",
                             "--base-candidate", criu, "--namespace-access"],
        "worker": [*common, "tools/build_worker.py", "--platform", options.platform,
                   "--native-base-build", options.native_base_build,
                   "--sysbox-build", sysbox, "--retained-candidate", criu,
                   "--discard-compiler-scratch", "--compress-package"],
    }


def shared_source_input(name, result):
    patches = result.get("candidate_patch_sha256", {})
    if "shared-fds.patch" not in patches:
        return None
    features = result.get("features", {})
    if (patches["shared-fds.patch"] != reference.sha(build_criu_shared_fds.PATCH) or
            features.get("schema") != 1 or
            any(features.get(key) is not True for key in
                ["shared_file_descriptions", "shared_source_directories"])):
        raise ValueError("supplied CRIU shared-source patch or feature receipt differs")
    return name


def namespace_access_input(name, result):
    patches = result.get("candidate_patch_sha256", {})
    if "namespace-access.patch" not in patches:
        return None
    if patches["namespace-access.patch"] != reference.sha(build_criu_shared_fds.NAMESPACE_PATCH):
        raise ValueError("supplied CRIU namespace-validation patch differs")
    return name


def completed_output(stdout):
    for line in reversed(stdout.splitlines()):
        try:
            row = json.loads(line)
        except json.JSONDecodeError:
            continue
        if isinstance(row, dict) and row.get("passed") is True and isinstance(row.get("evidence"), str):
            return row["evidence"]
    raise ValueError("build returned no successful exact artifact receipt")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    build_config.add_arguments(parser, required_platform=True)
    parser.add_argument("--native-base-build", required=True,
                        help="Exact completed native-base build for this target")
    parser.add_argument("--sysbox-build", help="Reuse one exact verified component build")
    parser.add_argument("--retained-candidate", help="Reuse one exact verified CRIU build")
    options = parser.parse_args()
    _, base = native_base.load(options.native_base_build, for_build=True,
                               candidate_platform=options.platform)
    result = {"schema": 1, "platform": options.platform, "passed": False,
              "native_lifecycle_qualified": False, "production_qualified": False,
              "native_base_build": options.native_base_build, "steps": []}
    archive = reference.new_artifact("factorybox-build-pipeline-" + uuid.uuid4().hex)
    archive.mkdir()
    (archive / "runner.py").write_bytes(Path(__file__).read_bytes())

    def step(role, command, supplied=None):
        record = {"role": role, "command": command, "supplied": supplied, "passed": False}
        result["steps"].append(record)
        reference_lifecycle.write_json(archive / "intent.json", result)
        if supplied is None:
            with (archive / f"{role}.stdout").open("x") as out, (archive / f"{role}.stderr").open("x") as err:
                done = subprocess.run(command, cwd=reference.ROOT, stdout=out, stderr=err)
            record["exit_code"] = done.returncode
            done.check_returncode()
            evidence = Path(completed_output((archive / f"{role}.stdout").read_text()))
            if evidence.parent != reference.ROOT / "artifacts":
                raise ValueError("build evidence left the owned artifact directory")
            supplied = evidence.name
        if role == "sysbox":
            _, loaded = reference_lifecycle.component_build(supplied, "factorybox-build-sysbox-")
        elif role in {"criu", "recovery", "shared-fds", "namespace-access"}:
            _, loaded, component, _ = criu_candidate.load(supplied)
            if role == "criu":
                criu_candidate.require_base_failure_correction(component)
            else:
                criu_candidate.require_recovery_corrections(loaded, component)
        else:
            _, loaded = build_worker.load(supplied)
        build_config.require_platform(options.platform, base, loaded)
        record.update(build=supplied, passed=True)
        return supplied, loaded

    print(json.dumps({"building": str(archive)}), flush=True)
    try:
        sysbox, _ = step("sysbox", commands(options, "", "")["sysbox"], options.sysbox_build)
        criu, loaded_criu = step("criu", commands(options, "", "")["criu"], options.retained_candidate)
        recovery = loaded_criu.get("candidate_patch_sha256", {})
        reused_recovery = criu if any(name in recovery for name in criu_candidate.RECOVERY_CORRECTIONS) else None
        criu, loaded_criu = step("recovery", commands(options, sysbox, criu)["recovery"], reused_recovery)
        criu, loaded_criu = step("shared-fds", commands(options, sysbox, criu)["shared-fds"],
                                 shared_source_input(criu, loaded_criu))
        if shared_source_input(criu, loaded_criu) != criu:
            raise ValueError("worker pipeline requires the complete shared-source CRIU component")
        criu, loaded_criu = step("namespace-access", commands(options, sysbox, criu)["namespace-access"],
                                 namespace_access_input(criu, loaded_criu))
        if namespace_access_input(criu, loaded_criu) != criu:
            raise ValueError("worker pipeline requires the exact namespace-validation CRIU component")
        worker, built = step("worker", commands(options, sysbox, criu)["worker"])
        result["worker_build"] = worker
        result["image"] = built["image"]
        result["executable_sha256"] = built["bundle_sha256"]["factorybox"]
        result["passed"] = True
    except Exception as error:
        result["error"] = str(error)
        raise
    finally:
        reference_lifecycle.write_json(archive / "result.json", result)
        print(json.dumps({"evidence": str(archive), **result}), flush=True)


if __name__ == "__main__":
    main()
