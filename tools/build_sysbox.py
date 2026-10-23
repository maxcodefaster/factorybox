#!/usr/bin/env python3
"""Build selected Sysbox components from pinned source and dated APT inputs."""
import argparse
import datetime
import hashlib
import ipaddress
import json
import pathlib
import shutil
import subprocess
import uuid

import reference
import reference_lifecycle
import sources
import build_config
import upstream_unit

ROOT = reference.ROOT
COMPONENTS = ["sysbox-runc", "sysbox-fs", "sysbox-ipc", "sysbox-libs", "sysbox-mgr"]
SHM_PATCH = ROOT / "upstream/patches/sysbox-libs-shm-probe-candidate.patch"


def stage_shm_reexec(context, archive):
    module = context / "sysbox-libs/linuxUtils"
    before = upstream_unit.context_manifest(module)
    patch = archive / "shm-reexec.patch"
    shutil.copyfile(SHM_PATCH, patch)
    with (archive / "shm-reexec-patch.log").open("x") as log:
        subprocess.run(["patch", "--batch", "--forward", "--fuzz=0", "-p1", "-i", str(patch)],
                       cwd=context / "sysbox-libs", stdout=log, stderr=subprocess.STDOUT,
                       check=True, timeout=15)
    after = upstream_unit.context_manifest(module)
    changed = {name for name in before if before[name] != after.get(name)}
    added = set(after) - set(before)
    if changed != {"linux.go"} or added != {
            "shm_probe_linux.go", "shm_probe_linux_test.go", "factorybox_shm_probe_linux_test.go"}:
        raise ValueError("SHM re-exec patch changed an undeclared source input")
    if any(after.get(name) != row for name, row in before.items() if name.endswith("_test.go")):
        raise ValueError("SHM re-exec patch changed an original upstream assertion")
    declaration = {"schema": 1, "before": before, "after": after,
                   "original_assertions_unchanged": True, "vet_enabled": True,
                   "patch_sha256": reference.sha(patch)}
    proof = archive / "shm-reexec-source.json"
    with proof.open("x") as stream:
        json.dump(declaration, stream, indent=2, sort_keys=True)
        stream.write("\n")
    return {"shm_reexec_patch_sha256": reference.sha(patch),
            "shm_reexec_source_sha256": reference.sha(proof)}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    build_config.add_arguments(parser)
    parser.add_argument("--clean", action="store_true",
                        help="Disable layer reuse and use new empty Go module/build caches")
    parser.add_argument("--shm-startup-reexec", action="store_true",
                        help="Apply the declared fresh-executable SHM fix only to this new private build context")
    parser.add_argument("--compare", help="Require outputs identical to one exact successful build")
    parser.add_argument("--build-dns", help="One resolver IP mounted read-only into host-network build RUN steps; host DNS is unchanged")
    options = parser.parse_args()
    if options.build_dns:
        options.build_dns = str(ipaddress.ip_address(options.build_dns))
        if options.network != "host":
            parser.error("--build-dns requires --network host")
    comparison = None
    if options.compare:
        previous, comparison = reference_lifecycle.component_build(
            options.compare, "factorybox-build-sysbox-")
        if bool(comparison.get("shm_startup_reexec")) != options.shm_startup_reexec:
            parser.error("--compare requires the same explicit SHM startup profile")
        if options.shm_startup_reexec and comparison["shm_reexec_patch_sha256"] != reference.sha(SHM_PATCH):
            parser.error("--compare requires the exact same declared SHM patch")
        if comparison["dockerfile_sha256"] != reference.sha(ROOT / "build/sysbox.Dockerfile"):
            parser.error("--compare requires the same pinned build recipe")
        if comparison.get("platform", "linux/arm64") != options.platform:
            parser.error("--compare requires the same compilation platform")
        for name, expected in comparison["binary_sha256"].items():
            if reference.sha(previous / "output" / name) != expected:
                raise ValueError("comparison binary changed: " + name)
    owner = uuid.uuid4().hex
    archive = reference.new_artifact("factorybox-build-sysbox-" + owner)
    context, output = archive / "context", archive / "output"
    context.mkdir(parents=True)
    output.mkdir()
    resolvers = [options.build_dns] if options.build_dns else [line.split()[1] for line in pathlib.Path("/etc/resolv.conf").read_text().splitlines() if line.startswith("nameserver ")]
    resolvers = [str(ipaddress.ip_address(value)) for value in resolvers]
    if not resolvers:
        raise ValueError("Sysbox build requires at least one explicit resolver")
    resolver_file = context / "build-resolv.conf"
    resolver_file.write_text("".join("nameserver " + value + "\n" for value in resolvers))
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
        "shm_startup_reexec": options.shm_startup_reexec,
        "build_dns": options.build_dns,
        "build_resolvers": resolvers, "build_resolv_sha256": reference.sha(resolver_file),
        "platform": options.platform, "native_lifecycle_qualified": False,
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
        if options.shm_startup_reexec:
            result.update(stage_shm_reexec(context, archive))
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
            *build_config.arguments(options), "-f", str(ROOT / "build/sysbox.Dockerfile"),
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
        if reference.sha(resolver_file) != result["build_resolv_sha256"]:
            raise ValueError("build-only resolver input changed")
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
