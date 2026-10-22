#!/usr/bin/env python3
"""Verify the complete local pilot evidence selection without changing its inputs."""
import argparse
import json
from pathlib import Path
import re
import subprocess
import sys
import uuid

import benchmark_lifecycle
import build_worker
import criu_candidate
import pilot_integration_gates
import pilot_native_gates
import reference
import reference_lifecycle
import upstream_unit
import verify_fixture_extensions


CONSUMERS = {"factory-planned-loss", "factory-abrupt-loss", "minimal-incomplete-pause",
             "factory-incomplete-pause", "recovery-negative", "attached-exec",
             "minimal-preparation", "factory-preparation", "compose-example"}
UPSTREAM = {"sysbox-modules", "sysbox-basic", "sysbox-systemd"}


def core(manifest):
    return {name: value for name, value in manifest.items() if not name.endswith("_test.go")}


def read_evidence(row):
    name = row["artifact"]
    if not re.fullmatch(r"factorybox-[a-z0-9-]+-[0-9a-f]{32}", name):
        raise ValueError("evidence requires one exact artifact identity")
    directory = reference.ROOT / "artifacts" / name
    result = directory / "result.json"
    if directory.is_symlink() or result.is_symlink() or reference.sha(result) != row["result_sha256"]:
        raise ValueError("pilot evidence identity or result bytes changed: " + name)
    return directory, json.loads(result.read_text())


def verify(profile):
    if (profile["schema"] != 1 or profile["status"] != "pilot-reference"
            or profile["production_qualified"] is not False
            or profile["platform"] != {"os": "linux", "architecture": "amd64", "page_size": 4096,
                                      "kernel": "7.2.8-9-omarchy"}):
        raise ValueError("pilot profile changes its qualification boundary")
    if reference.sha(reference.ROOT / "upstream/lock.json") != profile["base_source_lock_sha256"]:
        raise ValueError("pilot base source lock differs")
    _, worker = build_worker.load(profile["worker_build"])
    _, reproduced = build_worker.load(profile["reproduction_build"])
    for key in ("package_sha256", "image_id", "layers", "bundle_sha256", "source_context"):
        if worker[key] != reproduced[key]:
            raise ValueError("independent worker reproduction differs: " + key)
    if (worker["image_id"] != profile["worker_image_id"]
            or worker["package_sha256"] != profile["package_sha256"]
            or worker["bundle_sha256"]["factorybox"] != profile["cli_sha256"]):
        raise ValueError("profile differs from delivered worker/package")
    binaries = {name.removeprefix("components/"): value for name, value in worker["bundle_sha256"].items()
                if name.startswith("components/")}
    if binaries != profile["binary_sha256"]:
        raise ValueError("profile component hashes differ from delivered bundle")
    _, criu, _, _ = criu_candidate.load(worker["criu_build"])
    sysbox_repro_dir, sysbox_repro = read_evidence(profile["component_reproductions"]["sysbox"])
    if (sysbox_repro.get("passed") is not True or sysbox_repro.get("matches_comparison") is not True
            or sysbox_repro.get("inputs_unchanged") is not True
            or sysbox_repro["comparison_build"] != worker["sysbox_build"]
            or upstream_unit.context_manifest(sysbox_repro_dir / "context") != sysbox_repro["source_context"]):
        raise ValueError("independent offline Sysbox reproduction differs")
    for component, digest in sysbox_repro["binary_sha256"].items():
        if binaries[component] != digest or reference.sha(sysbox_repro_dir / "output" / component) != digest:
            raise ValueError("reproduced Sysbox executable differs")
    criu_repro_dir, criu_repro = read_evidence(profile["component_reproductions"]["criu"])
    _, verified_criu_repro, _, _ = criu_candidate.load(criu_repro_dir.name)
    if (criu_repro.get("passed") is not True or verified_criu_repro != criu_repro
            or any(criu_repro[key] != criu[key] for key in
                   ("binary_sha256", "context_sha256", "source_lock_sha256", "candidate_patch_sha256", "candidate_overlay_sha256"))):
        raise ValueError("independent CRIU rebuild differs")
    for patch in profile["additional_criu_patches"]:
        if (reference.sha(reference.ROOT / patch["path"]) != patch["sha256"]
                or criu["candidate_patch_sha256"][patch["build_name"]] != patch["sha256"]):
            raise ValueError("ordered CRIU patch input differs")
    if [row["build_name"] for row in profile["additional_criu_patches"]] != list(criu["candidate_patch_sha256"]):
        raise ValueError("CRIU patch order or coverage differs")
    current = {}
    for directory in ("internal", "cmd/factorybox"):
        current.update({directory + "/" + name: value for name, value in
                        upstream_unit.context_manifest(reference.ROOT / directory).items()})
    current.update({name: {"sha256": reference.sha(reference.ROOT / name)} for name in ("go.mod", "go.sum")})
    if current != worker["source_context"]:
        raise ValueError("current Go source differs from the reproduced delivered worker")
    expected = ({"native-" + name for name in pilot_native_gates.SUITES}
                | {"integration-" + name for name in pilot_integration_gates.SUITES}
                | CONSUMERS | UPSTREAM | {"comparison", "portable"})
    if set(profile["gates"]) != expected:
        raise ValueError("pilot evidence selection omits or adds a required gate")
    accepted = []
    for name, row in profile["gates"].items():
        directory, saved = read_evidence(row)
        negative = name == "integration-retained-corruption"
        if negative:
            if not pilot_integration_gates.corruption_oracle_rejected(saved, (directory / "stdout.txt").read_text(), 1):
                raise ValueError("deliberate RAM corruption did not trip only its original assertion")
        elif saved.get("passed") is not True:
            raise ValueError("required pilot evidence failed: " + name)
        if name not in {"comparison", "portable"} and saved.get("cleanup") is not True:
            raise ValueError("required exact cleanup is incomplete: " + name)
        if name.startswith("native-") and name != "native-criu-core":
            if core(saved["source_context"]) != core(worker["source_context"]):
                raise ValueError("native regression core differs from delivered worker: " + name)
            if saved.get("skipped_tests") != 0:
                raise ValueError("native regression contains a skip: " + name)
            if any(binaries.get(component) != digest for component, digest in saved["component_sha256"].items()):
                raise ValueError("native regression component differs: " + name)
        if name == "native-criu-core":
            if (saved["criu_sha256"] != binaries["criu"] or saved["native"]["cycles"] != 2
                    or len(saved["native"]["cases"]) != 18 or saved["unchanged_upstream_test_files"] != 1030):
                raise ValueError("CRIU core identity or complete selected coverage differs")
        if name.startswith("integration-docker-") or name.startswith("integration-retained-"):
            context = directory / "context"
            # Consumer fixtures also compile actors from tests/. Compare the
            # complete runtime source closure, including any unexpected runtime
            # file, while keeping those separate consumer inputs out of it.
            manifest = {path: value for path, value in upstream_unit.context_manifest(context).items()
                        if path.startswith(("internal/", "cmd/factorybox/")) or path in {"go.mod", "go.sum"}}
            if core(manifest) != core(worker["source_context"]):
                raise ValueError("native fault core differs from delivered worker: " + name)
            if saved["component_sha256"]["criu"] != profile["binary_sha256"]["criu"]:
                raise ValueError("native fault uses different CRIU: " + name)
            if name.startswith("integration-docker-") and (saved["inputs_sha256"]["factorybox"] != profile["cli_sha256"]
                    or reference.sha(directory / "factorybox") != profile["cli_sha256"]):
                raise ValueError("Docker fault did not execute the delivered CLI bytes")
        if name in CONSUMERS or name in {"integration-factory-agent", "integration-minimal-planned-loss"}:
            if saved.get("worker_image_id") != worker["image_id"]:
                raise ValueError("consumer did not use delivered/reproduced image: " + name)
        if name in {"integration-libcontainer", "integration-runc-integration"}:
            suite = name.removeprefix("integration-")
            if (saved.get("component_build") != worker["sysbox_build"] or
                    not pilot_integration_gates.original_upstream_coverage(suite, saved.get("suites"))):
                raise ValueError("upstream core coverage/components/original skips differ")
        if name == "sysbox-modules":
            suites = saved["suites"]
            if (saved["component_build"] != worker["sysbox_build"]
                    or sum(s["passed_top_level_tests"] for s in suites.values()) != 399
                    or sum(s["passed_subtests"] for s in suites.values()) != 323
                    or sum(len(s["skipped_tests"]) for s in suites.values()) != 18):
                raise ValueError("unchanged Sysbox module evidence differs")
        if name in {"sysbox-basic", "sysbox-systemd"}:
            passes, skips = (7, []) if name == "sysbox-basic" else (4, ["systemd ubuntu bionic", "systemd archlinux"])
            if (saved["sysbox_build"] != worker["sysbox_build"]
                    or saved["observed"]["executed_passes"] != passes
                    or sorted(saved["observed"]["skipped"]) != sorted(skips)):
                raise ValueError("unchanged Sysbox Bats coverage/original skips differ")
        if name == "comparison":
            for observation in saved["rows"]:
                child_name = observation["native_artifact"]
                if not re.fullmatch(r"factorybox-consumer-worker-loss-[0-9a-f]{32}", child_name):
                    raise ValueError("comparison requires an exact local native artifact name")
                child = reference.ROOT / "artifacts" / child_name
                if (reference.sha(child / "result.json") != observation["observation"]["result_sha256"]
                        or benchmark_lifecycle.observations(child) != observation["observation"]):
                    raise ValueError("comparison observation changed")
            report = benchmark_lifecycle.summarize(saved["rows"])
            if (report != saved["report"] or not all(report[key] for key in
                    ("complete_five_per_configuration", "factory_halving_target_met", "regression_limits_met"))):
                raise ValueError("matched performance acceptance differs")
        if name == "portable" and (len(saved["checks"]) != 6 or any(item["exit_code"] for item in saved["checks"])):
            raise ValueError("portable checks incomplete")
        accepted.append({"gate": name, "artifact": directory.name, "expected_negative_oracle": negative})
    fixture = verify_fixture_extensions.verify(reference.ROOT,
        reference.ROOT / "artifacts" / "factorybox-pilot-baseline-8829afb65616415585502684623e6e5c")
    return {"schema": 1, "passed": True, "pilot_functional_qualified": True,
            "matched_speed_target_met": True, "production_qualified": False,
            "gates": accepted, "fixture_integrity": fixture,
            "scope": "Exact local pilot evidence and reproduced image only; original upstream skips remain production blockers."}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--profile", type=Path, default=reference.ROOT / "upstream/amd64-pilot-profile.json")
    options = parser.parse_args()
    artifact = reference.new_artifact("factorybox-pilot-qualification-" + uuid.uuid4().hex)
    artifact.mkdir()
    (artifact / "runner.py").write_bytes(Path(__file__).read_bytes())
    (artifact / "profile.json").write_bytes(options.profile.read_bytes())
    result = {"schema": 1, "passed": False, "production_qualified": False}
    try:
        with (artifact / "sources.log").open("x") as log:
            subprocess.run([sys.executable, str(reference.ROOT / "tools/sources.py"), "verify", "--offline"],
                           stdout=log, stderr=subprocess.STDOUT, check=True, timeout=60)
        result = verify(json.loads((artifact / "profile.json").read_text()))
        result["profile_sha256"] = reference.sha(artifact / "profile.json")
        result["source_verification_sha256"] = reference.sha(artifact / "sources.log")
    except Exception as error:
        result["error"] = str(error)
        raise
    finally:
        reference_lifecycle.write_json(artifact / "result.json", result)
        print(json.dumps({"evidence": str(artifact), "passed": result["passed"], "error": result.get("error")}), flush=True)


if __name__ == "__main__":
    main()
