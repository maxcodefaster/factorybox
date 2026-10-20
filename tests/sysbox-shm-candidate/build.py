#!/usr/bin/env python3
"""Build the unselected SHM startup fix and its regression on a retained native builder."""
import argparse
import json
from pathlib import Path
import re
import shutil
import subprocess
import sys
import uuid

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / "tools"))
import reference
import reference_lifecycle
import upstream_unit

PATCH = ROOT / "upstream/patches/sysbox-libs-shm-probe-candidate.patch"


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--sysbox-build", required=True)
    parser.add_argument("--builder-run", required=True,
                        help="Closed upstream run retaining this component build's native builder image")
    parser.add_argument("--baseline", action="store_true", help="Keep upstream SHM code; add only the common regression")
    options = parser.parse_args()
    original, components = reference_lifecycle.component_build(
        options.sysbox_build, "factorybox-build-sysbox-")
    if not re.fullmatch(r"factorybox-upstream-[0-9a-f]{32}", options.builder_run):
        parser.error("requires one exact closed upstream builder run")
    builder = json.loads((ROOT / "artifacts" / options.builder_run / "result.json").read_text())
    if (not builder["passed"] or not builder["cleanup"] or
            builder["component_build"] != original.name):
        raise ValueError("builder run is not a completed proof for the supplied component build")
    image = builder["builder_image_manifest"]
    observed = json.loads(reference.docker(["image", "inspect", image]))[0]
    if (image not in observed.get("RepoDigests", []) or
            observed["Id"] not in {builder["builder_image_config"], image.split("@")[1]} or
            observed["Architecture"] != "arm64" or observed["Os"] != "linux"):
        raise ValueError("retained native builder identity changed")
    owner = uuid.uuid4().hex
    name = "factorybox-build-shm-" + owner
    archive = reference.new_artifact(name)
    archive.mkdir()
    output, inputs = archive / "output", archive / "inputs"
    output.mkdir()
    shutil.copyfile(__file__, archive / "builder.py")
    shutil.copyfile(PATCH, archive / "candidate.patch")
    result = {
        "owner": owner, "variant": "baseline" if options.baseline else "candidate",
        "passed": False, "cleanup": False, "candidate_only": True,
        "selected_sysbox_build": original.name, "builder_image": image,
        "builder_config": observed["Id"], "patch_sha256": reference.sha(archive / "candidate.patch"),
        "scope": "Native compilation only. Retained pinned builder, 3 CPU/3 GiB; "
                 "only Sysbox-fs is rebuilt. No component selection or production qualification.",
    }
    (archive / "intent.json").write_text(json.dumps(result, indent=2) + "\n")
    print(json.dumps({"building": str(archive)}), flush=True)
    attempted = False
    try:
        module = original / "context/sysbox-libs/linuxUtils"
        context = archive / "context"
        shutil.copytree(module, context / "linuxUtils")
        before = upstream_unit.context_manifest(context / "linuxUtils")
        with (archive / "patch.log").open("x") as log:
            subprocess.run(["patch", "--batch", "-E", "-F", "0", "-p1", "-i",
                            str(archive / "candidate.patch")], cwd=context,
                           stdout=log, stderr=subprocess.STDOUT, check=True, timeout=15)
        after = upstream_unit.context_manifest(context / "linuxUtils")
        changed = {path for path in before if before[path] != after.get(path)}
        added = set(after) - set(before)
        if changed != {"linux.go"} or added != {
                "shm_probe_linux.go", "shm_probe_linux_test.go", "factorybox_shm_probe_linux_test.go"}:
            raise ValueError("candidate patch changed an undeclared upstream input")
        result["unchanged_upstream_tests"] = all(
            after.get(path) == value for path, value in before.items() if path.endswith("_test.go"))
        if not result["unchanged_upstream_tests"]:
            raise ValueError("original upstream unit test changed")
        shutil.copytree(module if options.baseline else context / "linuxUtils", inputs)
        if options.baseline:
            shutil.copyfile(context / "linuxUtils/factorybox_shm_probe_linux_test.go",
                            inputs / "factorybox_shm_probe_linux_test.go")
        result["source_inputs"] = upstream_unit.context_manifest(inputs)
        script = (
            "set -euo pipefail\n"
            "go version > /output/toolchain.txt\n"
            "sha256sum /out/sysbox-fs /src/sysbox-libs/linuxUtils/linux.go > /output/original-source.txt\n"
            "cp -a /inputs/. /src/sysbox-libs/linuxUtils/\n"
            "cd /src/sysbox-libs/linuxUtils\n"
            "go test -mod=readonly -c -trimpath -buildvcs=false -tags 'netgo osusergo' "
            "-ldflags '-extldflags -static' -o /output/shm.test .\n"
        )
        if not options.baseline:
            script += (
                "cd /src/sysbox-fs\n"
                "CGO_ENABLED=1 go build -mod=readonly -trimpath -buildvcs=false -tags 'netgo osusergo' "
                "-ldflags '-extldflags -static -X main.version=0.7.1-experiment "
                "-X main.commitId=c3d2ebc65102e32e74e383675f03b45556326888-experiment' "
                "-o /output/sysbox-fs ./cmd/sysbox-fs\n"
            )
        (archive / "build.sh").write_text(script)
        command = [
            *reference.DOCKER, "run", "--pull=never", "--name", name,
            "--label", "com.factorybox.run=" + owner, "--cpus=3", "--memory=3g", "--pids-limit=512",
            "--env", "GOMAXPROCS=2", "--env", "GOMEMLIMIT=512MiB", "--env", "GOFLAGS=-p=2",
            "--mount", f"type=bind,source={inputs},target=/inputs,readonly",
            "--mount", f"type=bind,source={output},target=/output",
            "--mount", f"type=bind,source={archive / 'build.sh'},target=/build.sh,readonly",
            "--entrypoint", "/bin/bash", image, "/build.sh",
        ]
        result["command"] = command
        attempted = True
        with (archive / "build.log").open("x") as log:
            done = subprocess.run(command, stdout=log, stderr=subprocess.STDOUT, timeout=420)
        result["exit_code"] = done.returncode
        state = json.loads(reference.docker(["inspect", name]))[0]["State"]
        result["container_state"] = state
        if done.returncode:
            raise ValueError("native probe compilation failed")
        recorded = (output / "original-source.txt").read_text().splitlines()
        if (recorded[0].split()[0] != components["binary_sha256"]["sysbox-fs"] or
                recorded[1].split()[0] != reference.sha(module / "linux.go")):
            raise ValueError("retained builder source/binary differs from the supplied build")
        result["binary_sha256"] = {path.name: reference.sha(path) for path in output.iterdir()
                                   if path.name in ["shm.test", "sysbox-fs"]}
        result["inputs_unchanged"] = upstream_unit.context_manifest(inputs) == result["source_inputs"]
        result["passed"] = result["inputs_unchanged"] and not state["OOMKilled"]
    except Exception as error:
        result["error"] = str(error)
    finally:
        if attempted:
            reference.owned_delete("container", name, owner)
        result["cleanup"] = True
        (archive / "result.json").write_text(json.dumps(result, indent=2) + "\n")
        print(json.dumps({"evidence": str(archive), "passed": result["passed"],
                          "cleanup": result["cleanup"], "error": result.get("error")}), flush=True)
    if not result["passed"]:
        raise ValueError("native SHM candidate build failed")


if __name__ == "__main__":
    main()
