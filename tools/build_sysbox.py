#!/usr/bin/env python3
"""Build selected Sysbox components from pinned source and dated APT inputs."""
import argparse
import datetime
import hashlib
import json
import pathlib
import shutil
import subprocess
import uuid

import reference
import reference_lifecycle
import sources

ROOT = reference.ROOT
COMPONENTS = ["sysbox-runc", "sysbox-fs", "sysbox-ipc", "sysbox-libs", "sysbox-mgr"]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--clean", action="store_true",
                        help="Disable layer reuse and use new empty Go module/build caches")
    parser.add_argument("--compare", help="Require outputs identical to one exact successful build")
    options = parser.parse_args()
    comparison = None
    if options.compare:
        previous, comparison = reference_lifecycle.component_build(
            options.compare, "factorybox-build-sysbox-")
        if comparison["dockerfile_sha256"] != reference.sha(ROOT / "build/sysbox.Dockerfile"):
            parser.error("--compare requires the same pinned build recipe")
        for name, expected in comparison["binary_sha256"].items():
            if reference.sha(previous / "output" / name) != expected:
                raise ValueError("comparison binary changed: " + name)
    owner = uuid.uuid4().hex
    archive = ROOT / "artifacts" / ("factorybox-build-sysbox-" + owner)
    context, output = archive / "context", archive / "output"
    context.mkdir(parents=True)
    output.mkdir()
    lock_path = ROOT / "upstream/lock.json"
    shutil.copyfile(lock_path, archive / "source-lock.json")
    shutil.copyfile(ROOT / "build/sysbox.Dockerfile", archive / "Dockerfile")
    lock = json.loads(lock_path.read_text())["components"]
    result = {
        "schema": 1, "owner": owner, "started_at": datetime.datetime.now(
            datetime.timezone.utc).isoformat(),
        "source_lock_sha256": reference.sha(lock_path),
        "dockerfile_sha256": reference.sha(ROOT / "build/sysbox.Dockerfile"),
        "clean": options.clean, "comparison_build": options.compare,
        "passed": False, "production_qualified": False,
        "scope": "Native component compilation, a natural-module libcontainer test compile gate "
                 "and selected inherited regression tests. "
                 "Not the complete upstream unit/integration suites or full lifecycle qualification. "
                 "Signed APT inputs use the dated Ubuntu snapshot 20261018T000000Z. "
                 "A matching comparison proves these outputs on this builder; "
                 "it does not establish another architecture or runtime qualification.",
    }
    failure = None
    try:
        for name in COMPONENTS + ["sysbox-fuse"]:
            sources.prepare(name, lock[name], offline=True)
        for name in COMPONENTS:
            shutil.copytree(ROOT / ".cache/sources" / name, context / name, symlinks=True)
        shutil.copytree(ROOT / ".cache/sources/sysbox-fuse",
                        context / "sysbox-fs/bazil", dirs_exist_ok=True, symlinks=True)
        shutil.copyfile(ROOT / ".cache/downloads" / lock["sysbox-runc"]["archive"],
                        context / "sysbox-runc-pristine.tar.gz")
        context_files = {}
        for path in context.rglob("*"):
            relative = str(path.relative_to(context))
            if path.is_symlink():
                context_files[relative] = {"symlink": str(path.readlink())}
            elif path.is_file():
                context_files[relative] = {"sha256": reference.sha(path)}
        result["context_sha256"] = hashlib.sha256(
            json.dumps(context_files, sort_keys=True).encode()).hexdigest()
        (archive / "context-manifest.json").write_text(
            json.dumps(context_files, indent=2, sort_keys=True) + "\n")
        command = [
            *reference.DOCKER, "build", "--provenance=false", "--progress=plain",
            "--platform=linux/arm64", "-f", str(ROOT / "build/sysbox.Dockerfile"),
            "--output", "type=local,dest=" + str(output), str(context),
        ]
        if options.clean:
            position = len(reference.DOCKER) + 1
            command[position:position] = ["--no-cache", "--build-arg", "FACTORYBOX_BUILD_CACHE=" + owner]
        result["command"] = command
        (archive / "intent.json").write_text(json.dumps(result, indent=2) + "\n")
        print(json.dumps({"building": str(archive)}), flush=True)
        with (archive / "build.log").open("x") as log:
            done = subprocess.run(command, stdout=log, stderr=subprocess.STDOUT, timeout=900)
        result["exit_code"] = done.returncode
        if done.returncode:
            raise ValueError("Sysbox component build failed; inspect build.log")
        result["binary_sha256"] = {name: reference.sha(output / name) for name in COMPONENTS
                                  if name in ["sysbox-runc", "sysbox-fs", "sysbox-mgr"]}
        result["toolchain"] = (output / "toolchain.txt").read_text()
        result["packages_sha256"] = reference.sha(output / "packages.tsv")
        result["apt_sources_sha256"] = reference.sha(output / "apt-sources.list")
        if comparison:
            result["matches_comparison"] = result["binary_sha256"] == comparison["binary_sha256"]
            result["packages_match_comparison"] = result["packages_sha256"] == comparison["packages_sha256"]
            if not result["matches_comparison"] or not result["packages_match_comparison"]:
                raise ValueError("clean build differs from the declared comparison")
        result["passed"] = True
    except Exception as error:
        result["error"] = str(error)
        failure = error
    finally:
        (archive / "result.json").write_text(json.dumps(result, indent=2) + "\n")
        print(json.dumps({"evidence": str(archive), **result}), flush=True)
    if failure:
        raise RuntimeError("Sysbox build gate failed; evidence: " + str(archive)) from failure


if __name__ == "__main__":
    main()
