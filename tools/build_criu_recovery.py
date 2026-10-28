#!/usr/bin/env python3
"""Build the exact ordered recovery corrections on a fresh retained CRIU input."""
import argparse
import json
import os
from pathlib import Path
import shutil
import subprocess
import uuid

import criu_candidate
import native_base
import reference
import sources


def selected_patches(previous, component):
    criu_candidate.require_base_failure_correction(component)
    if any(name in previous.get("candidate_patch_sha256", {})
           for name in criu_candidate.RECOVERY_CORRECTIONS):
        raise ValueError("recovery build requires a plain retained input; reuse an exact complete recovery input through the pipeline")
    return list(criu_candidate.RECOVERY_CORRECTIONS.items())


def targets(patches):
    changed = set()
    for _, patch in patches:
        for line in patch.read_text().splitlines():
            if line.startswith("+++ b/"):
                name = line.removeprefix("+++ b/")
                if (Path(name).is_absolute() or ".." in Path(name).parts or
                        not name.startswith(("criu/", "images/"))):
                    raise ValueError("recovery patch changed an undeclared source boundary")
                changed.add(name)
    if not changed:
        raise ValueError("recovery patches contain no production targets")
    return changed


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--base-candidate", required=True)
    args = parser.parse_args()
    original, previous, component, original_tests = criu_candidate.load(args.base_candidate)
    patches = selected_patches(previous, component)
    changed = targets(patches)
    builder, base = native_base.load(previous["native_base_build"], for_build=True)
    owner = uuid.uuid4().hex
    artifact = reference.new_artifact("factorybox-build-criu-retained-" + owner)
    artifact.mkdir()
    shutil.copyfile(__file__, artifact / "runner.py")
    result = {"schema": 1, "owner": owner, "passed": False, "cleanup": False,
              "diagnostic_only": True, "production_qualified": False,
              "native_lifecycle_qualified": False, "platform": base["platform"],
              "base_candidate": original.name,
              "base_candidate_result_sha256": reference.sha(original / "result.json"),
              "base_source_lock_sha256": previous["base_source_lock_sha256"],
              "native_base_build": previous["native_base_build"], "builder": builder,
              "native_base_packages_sha256": base["packages_sha256"], "scope": __doc__}
    (artifact / "intent.json").write_text(json.dumps(result, indent=2) + "\n")
    attempted = False
    try:
        def copy(source, target):
            name = str(Path(source).relative_to(original / "context"))
            if name in changed or name.endswith((".orig", ".rej")):
                return shutil.copy2(source, target)
            os.link(source, target)
            return target
        shutil.copytree(original / "context", artifact / "context", copy_function=copy, symlinks=True)
        for name in {"retained.patch", "criu-build.json", *previous["candidate_patch_sha256"],
                     *previous.get("candidate_overlay_sha256", {})}:
            shutil.copyfile(original / name, artifact / name)
        lock = json.loads((original / "source-lock.json").read_text())
        selected = lock["components"]["criu"]
        hashes = dict(previous["candidate_patch_sha256"])
        for short, patch in patches:
            shutil.copyfile(patch, artifact / short)
            done = subprocess.run(["patch", "--batch", "--forward", "--fuzz=0",
                "--no-backup-if-mismatch", "-p1", "-i", str(artifact / short)],
                cwd=artifact / "context", capture_output=True, text=True, timeout=30)
            (artifact / (short + ".log")).write_text(done.stdout + done.stderr)
            done.check_returncode()
            relative = str(patch.relative_to(reference.ROOT))
            selected["patches"].append(relative)
            selected["local_inputs_sha256"][relative] = reference.sha(patch)
            hashes[short] = reference.sha(patch)
        selected["prepared_files_sha256"].update({name: reference.sha(artifact / "context" / name)
                                                   for name in changed})
        sources.validate_inputs(selected)
        unchanged = sources.check_source(artifact / "context", reference.ROOT / ".cache/downloads" / selected["archive"], selected)
        if unchanged != original_tests:
            raise ValueError("recovery build changed an upstream assertion")
        (artifact / "source-lock.json").write_text(json.dumps(lock, indent=2) + "\n")
        recipe = json.loads((artifact / "criu-build.json").read_text())
        recipe["source_recipe_sha256"] = sources.recipe_sha(selected)
        (artifact / "criu-build.json").write_text(json.dumps(recipe, indent=2) + "\n")
        shutil.copyfile(reference.ROOT / "tests/criu-pstree-dump.c", artifact / "pstree-regression.c")
        text = (artifact / "context/criu/pstree.c").read_text()
        start = text.index("int dump_pstree(struct pstree_item *root_item)\n{")
        (artifact / "dump-pstree.inc").write_text(text[start:text.index("\n}\n", start) + 3])
        command = [*reference.DOCKER, "run", "--pull=never", "--name", artifact.name,
            "--label", "com.factorybox.run=" + owner, "--user", f"{os.getuid()}:{os.getgid()}",
            "--cpus=2", "--memory=1g", "--pids-limit=128", "--network=none", "--read-only",
            "--cgroupns=private", "--cap-drop=ALL", "--security-opt=no-new-privileges",
            "--env", "SOURCE_DATE_EPOCH=" + str(recipe["source_date_epoch"]),
            "--tmpfs", "/tmp:rw,exec,nosuid,nodev,size=384m",
            "--mount", f"type=bind,source={artifact / 'context'},target=/source,readonly",
            "--mount", f"type=bind,source={artifact},target=/output", "--entrypoint", "/bin/sh", builder, "-ec",
            "cc --version > /output/toolchain.txt\n"
            "cc -std=c11 -Wall -Wextra -Werror /output/pstree-regression.c -o /tmp/pstree-regression\n"
            "/tmp/pstree-regression > /output/pstree-regression.log 2>&1\n"
            "cp -R /source /tmp/criu-source\n"
            "make -C /tmp/criu-source -j2 criu/criu > /output/build.log 2>&1\n"
            "cp /tmp/criu-source/criu/criu /output/criu\n/output/criu --version > /output/version.txt\n"]
        result.update(command=command, source_lock_sha256=reference.sha(artifact / "source-lock.json"),
                      patch_sha256=reference.sha(artifact / "retained.patch"), candidate_patch_sha256=hashes,
                      candidate_overlay_sha256=previous.get("candidate_overlay_sha256", {}),
                      context_sha256=sources.tree_sha(artifact / "context"),
                      build_inputs_sha256=reference.sha(artifact / "criu-build.json"),
                      unchanged_upstream_test_files=unchanged,
                      pstree_fixture_sha256=reference.sha(artifact / "pstree-regression.c"))
        (artifact / "intent.json").write_text(json.dumps(result, indent=2) + "\n")
        print(json.dumps({"building": str(artifact), "unchanged_upstream_tests": unchanged}), flush=True)
        attempted = True
        with (artifact / "stdout.txt").open("x") as out, (artifact / "stderr.txt").open("x") as err:
            done = subprocess.run(command, stdout=out, stderr=err, timeout=300)
        result["exit_code"] = done.returncode
        result["container_state"] = json.loads(reference.docker(["container", "inspect", artifact.name]))[0]["State"]
        done.check_returncode()
        result["function_gate_passed"] = "pstree-cases=7 failures=0" in (artifact / "pstree-regression.log").read_text()
        result["inputs_unchanged"] = (sources.tree_sha(artifact / "context") == result["context_sha256"] and
                                      sources.tree_sha(original / "context") == previous["context_sha256"])
        if result["container_state"]["OOMKilled"] or not result["function_gate_passed"] or not result["inputs_unchanged"]:
            raise ValueError("recovery build, original assertions or frozen inputs failed verification")
        result["binary_sha256"] = reference.sha(artifact / "criu")
        result["passed"] = True
    except Exception as error:
        result["error"] = str(error)
        raise
    finally:
        try:
            if attempted:
                reference.owned_delete("container", artifact.name, owner)
            result["cleanup"] = True
        finally:
            result["passed"] = result["passed"] and result["cleanup"]
            (artifact / "result.json").write_text(json.dumps(result, indent=2) + "\n")
            print(json.dumps({"evidence": str(artifact), "passed": result["passed"], "cleanup": result["cleanup"]}), flush=True)


if __name__ == "__main__":
    main()
