#!/usr/bin/env python3
"""Compose an explicit unselected FS candidate with unchanged pinned runc/mgr inputs."""
import argparse
import json
from pathlib import Path
import re
import shutil
import sys
import uuid

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / "tools"))
import reference
import reference_lifecycle
import upstream_unit


def exact(name, prefix):
    if not re.fullmatch(re.escape(prefix) + r"[0-9a-f]{32}", name):
        raise ValueError("requires one exact " + prefix + " artifact")
    path = ROOT / "artifacts" / name
    return path, json.loads((path / "result.json").read_text())


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--sysbox-build", required=True)
    parser.add_argument("--shm-build", required=True)
    parser.add_argument("--regression-run", required=True)
    parser.add_argument("--startup-run", required=True)
    options = parser.parse_args()
    base, original = reference_lifecycle.component_build(
        options.sysbox_build, "factorybox-build-sysbox-")
    candidate, saved = exact(options.shm_build, "factorybox-build-shm-")
    if (not saved["passed"] or not saved["cleanup"] or saved["variant"] != "candidate" or
            saved["selected_sysbox_build"] != base.name or
            upstream_unit.context_manifest(candidate / "inputs") != saved["source_inputs"]):
        raise ValueError("native candidate inputs changed")
    proof, regression = exact(options.regression_run, "factorybox-shm-probe-")
    startup_path, startup = exact(options.startup_run, "factorybox-startup-manager-")
    if (not regression["passed"] or not regression["cleanup"] or
            regression["build"] != candidate.name or
            regression["completed_probes"] != 32 or regression["supported_probes"] != 32 or
            not regression["global_sysctls_unchanged"] or not regression["parent_shm_unchanged"] or
            not startup["passed"] or not startup["cleanup"] or
            startup["shm_candidate"] != candidate.name or not startup["inputs_unchanged"] or
            len(startup["observations"]) != 16 or
            not all(row["ready"] for row in startup["observations"]) or
            startup["inputs_sha256"]["sysbox-fs"] != saved["binary_sha256"]["sysbox-fs"] or
            startup["inputs_sha256"]["sysbox-mgr"] != original["binary_sha256"]["sysbox-mgr"]):
        raise ValueError("required native candidate proofs are not complete for these exact inputs")
    patch = ROOT / "upstream/patches/sysbox-libs-shm-probe-candidate.patch"
    if reference.sha(patch) != saved["patch_sha256"]:
        raise ValueError("candidate patch differs from its frozen build")
    archive = reference.new_artifact("factorybox-build-sysbox-shm-" + uuid.uuid4().hex)
    output = archive / "output"
    output.mkdir(parents=True)
    shutil.copyfile(__file__, archive / "assembler.py")
    shutil.copyfile(base / "source-lock.json", archive / "source-lock.json")
    shutil.copyfile(patch, archive / "candidate.patch")
    result = {
        "passed": False, "cleanup": True, "candidate_only": True, "candidate_selected": False,
        "scope": "Explicitly unselected native-probe composition. Reuses selected runc/mgr bytes; "
                 "only FS is rebuilt. The source lock describes BASE inputs; the supplemental "
                 "patch and FS build below are required provenance. This is not a homogeneous "
                 "full Sysbox rebuild or production qualification.",
        "source_lock_sha256": original["source_lock_sha256"], "base_sysbox_build": base.name,
        "fs_build": candidate.name, "fs_result_sha256": reference.sha(candidate / "result.json"),
        "additional_patch_sha256": reference.sha(archive / "candidate.patch"),
        "regression_result_sha256": reference.sha(proof / "result.json"),
        "startup_result_sha256": reference.sha(startup_path / "result.json"),
        "binary_sha256": {}, "per_component_build": {},
    }
    (archive / "intent.json").write_text(json.dumps(result, indent=2) + "\n")
    for component in ["sysbox-runc", "sysbox-mgr", "sysbox-fs"]:
        source, digest, build = base / "output" / component, original["binary_sha256"][component], base.name
        if component == "sysbox-fs":
            source, digest, build = candidate / "output" / component, saved["binary_sha256"][component], candidate.name
        reference.artifact_storage.share_frozen_input(source, output / component, ROOT, digest)
        result["binary_sha256"][component] = digest
        result["per_component_build"][component] = build
    result["passed"] = all(reference.sha(output / name) == digest
                            for name, digest in result["binary_sha256"].items())
    (archive / "result.json").write_text(json.dumps(result, indent=2) + "\n")
    print(json.dumps({"evidence": str(archive), "passed": result["passed"], "candidate_only": True}))


if __name__ == "__main__":
    main()
