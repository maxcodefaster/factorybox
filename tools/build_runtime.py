#!/usr/bin/env python3
"""Package the exact application-independent AMD64 runtime composition locally."""
import argparse
import datetime
import json
import os
from pathlib import Path
import shutil
import subprocess
import tarfile
import uuid

import criu_candidate
import reference
import reference_lifecycle
import sources
import upstream_unit


def profile_inputs(profile, sysbox, criu):
    expected = profile["binary_sha256"]
    if (profile["schema"] != 1 or profile["status"] not in {"candidate", "reference"} or
            set(expected) != {"sysbox-runc", "sysbox-fs", "sysbox-mgr", "criu"} or
            sysbox.get("platform") != "linux/amd64" or criu.get("platform") != "linux/amd64" or
            sysbox["binary_sha256"] != {key: expected[key] for key in expected if key != "criu"} or
            criu["binary_sha256"] != expected["criu"] or not criu.get("exit_hold_socket_candidate") or
            criu.get("nested_overlay_candidate") or
            list(criu["candidate_patch_sha256"].values()) !=
            [row["sha256"] for row in profile["additional_criu_patches"]] or
            criu["candidate_overlay_sha256"] != {"exit-observer.h": profile["additional_criu_overlay"]["sha256"]}):
        raise ValueError("component composition differs from the exact AMD64 profile")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--sysbox-build", required=True)
    parser.add_argument("--retained-candidate", required=True)
    parser.add_argument("--compare", help="Require a byte-identical complete package from an earlier successful build")
    options = parser.parse_args()
    root = reference.ROOT
    profile_path = root / "upstream/amd64-profile.json"
    profile = json.loads(profile_path.read_text())
    if reference.sha(root / "upstream/lock.json") != profile["base_source_lock_sha256"]:
        raise ValueError("profile belongs to a different pinned source base")
    for row in [*profile["additional_criu_patches"], profile["additional_criu_overlay"]]:
        if reference.sha(root / row["path"]) != row["sha256"]:
            raise ValueError("profile patch or overlay changed")
    sysbox_path, sysbox = reference_lifecycle.component_build(options.sysbox_build, "factorybox-build-sysbox-")
    criu_path, criu, _, _ = criu_candidate.load(options.retained_candidate)
    profile_inputs(profile, sysbox, criu)
    comparison = None
    if options.compare:
        _, comparison = reference_lifecycle.component_build(options.compare, "factorybox-build-runtime-")
        if comparison["profile_sha256"] != reference.sha(profile_path):
            raise ValueError("comparison uses a different runtime profile")
    archive = reference.new_artifact("factorybox-build-runtime-" + uuid.uuid4().hex)
    archive.mkdir()
    context, bundle = archive / "context", archive / "bundle"
    bundle.mkdir()
    for directory in ["internal", "cmd/factorybox"]:
        shutil.copytree(root / directory, context / directory)
    for name in ["go.mod", "go.sum"]:
        shutil.copyfile(root / name, context / name)
    shutil.copyfile(profile_path, bundle / "profile.json")
    shutil.copyfile(root / "upstream/lock.json", archive / "source-lock.json")
    shutil.copyfile(criu_path / "source-lock.json", archive / "criu-source-lock.json")
    shutil.copyfile(__file__, archive / "runner.py")
    result = {"schema": 1, "started_at": datetime.datetime.now(datetime.timezone.utc).isoformat(),
              "profile_sha256": reference.sha(profile_path),
              "source_lock_sha256": profile["base_source_lock_sha256"], "sysbox_build": options.sysbox_build,
              "criu_build": options.retained_candidate, "comparison_build": options.compare,
              "source_context": upstream_unit.context_manifest(context),
              "passed": False, "production_qualified": False, "native_lifecycle_qualified": False,
              "scope": "Local application-independent runtime bundle and reproducible build. No installation, "
                       "daemon launch, consumer image, IDE or public publishing. Native qualification is separate."}
    failure = None
    try:
        if sources.tree_sha(context) != profile["go_source_sha256"]:
            raise ValueError("Go source differs from the reviewed native profile; requalify before packaging")
        env = dict(os.environ, GOOS="linux", GOARCH="amd64", GOAMD64="v1", GOEXPERIMENT="",
                   GOFLAGS="", GOWORK="off", CGO_ENABLED="0", GOPROXY="off",
                   GOSUMDB="off", GOTOOLCHAIN="local", GOCACHE=str(archive / "go-cache"))
        version = subprocess.check_output(["go", "version"], text=True).strip()
        if not version.startswith("go version go1.27.1 "):
            raise ValueError("runtime bundle requires the pinned Go 1.27.1 toolchain")
        result["go_version"] = version
        goroot = Path(subprocess.check_output(["go", "env", "GOROOT"], text=True).strip())
        result["go_toolchain_tree_sha256"] = sources.tree_sha(goroot)
        verified = subprocess.run(["go", "mod", "verify"], cwd=context, env=env,
                                  capture_output=True, text=True, timeout=60)
        (archive / "modules-verified.txt").write_text(verified.stdout)
        (archive / "modules-verify.stderr").write_text(verified.stderr)
        verified.check_returncode()
        compile_options = []
        if profile["status"] == "candidate":
            compile_options = ["-tags=factorybox_candidate_amd64", "-ldflags=-X=" +
                               "factorybox/internal/platform.candidateKernel=" + profile["platform"]["kernel"]]
        command = ["go", "build", "-trimpath", "-buildvcs=false", *compile_options,
                   "-o", str(bundle / "factorybox"), "./cmd/factorybox"]
        result["command"] = command
        reference_lifecycle.write_json(archive / "intent.json", result)
        done = subprocess.run(command, cwd=context, env=env, capture_output=True, text=True, timeout=180)
        (archive / "build.stdout").write_text(done.stdout)
        (archive / "build.stderr").write_text(done.stderr)
        done.check_returncode()
        for name, expected in profile["binary_sha256"].items():
            source = criu_path / "criu" if name == "criu" else sysbox_path / "output" / name
            reference.artifact_storage.share_frozen_input(source, bundle / name, root, expected)
        result["binary_sha256"] = {name: reference.sha(bundle / name)
                                   for name in ["factorybox", *profile["binary_sha256"]]}
        license_inputs = json.loads((root / "upstream/licenses.json").read_text())
        license_hashes = {}
        for name, notice in license_inputs.items():
            source = root / name
            if reference.sha(source) != notice["sha256"]:
                raise ValueError("pinned upstream license notice changed")
            relative = Path(name).relative_to("upstream/licenses")
            destination = bundle / "licenses" / relative
            destination.parent.mkdir(parents=True, exist_ok=True)
            shutil.copyfile(source, destination)
            license_hashes[str(Path("licenses") / relative)] = notice["sha256"]
        shutil.copyfile(root / "upstream/licenses.json", bundle / "licenses.json")
        dependencies = subprocess.check_output(["go", "list", "-deps", "./cmd/factorybox"],
                                               cwd=context, env=env, text=True, timeout=60).splitlines()
        if any(name.startswith("factorybox/tests/") for name in dependencies):
            raise ValueError("runtime acquired a test-consumer dependency")
        (archive / "dependencies.txt").write_text("\n".join(dependencies) + "\n")
        manifest = {"schema": 1, "profile_sha256": result["profile_sha256"],
                    "go_version": version, "go_toolchain_tree_sha256": result["go_toolchain_tree_sha256"],
                    "go_source_sha256": sources.tree_sha(context),
                    "sysbox_binary_sha256": sysbox["binary_sha256"],
                    "criu_source_lock_sha256": criu["source_lock_sha256"],
                    "binary_sha256": result["binary_sha256"], "license_sha256": license_hashes,
                    "production_qualified": False}
        reference_lifecycle.write_json(bundle / "manifest.json", manifest)
        package = archive / (profile["name"] + ".tar")
        with tarfile.open(package, "w", format=tarfile.USTAR_FORMAT) as tar:
            for source in sorted(bundle.rglob("*")):
                relative = str(source.relative_to(bundle))
                info = tarfile.TarInfo(relative)
                info.mtime = profile["source_date_epoch"]
                if source.is_dir():
                    info.type, info.mode = tarfile.DIRTYPE, 0o755
                    tar.addfile(info)
                else:
                    info.size = source.stat().st_size
                    info.mode = 0o755 if relative in result["binary_sha256"] else 0o644
                    with source.open("rb") as stream:
                        tar.addfile(info, stream)
        result["package_sha256"] = reference.sha(package)
        result["package_bytes"] = package.stat().st_size
        result["inputs_unchanged"] = (upstream_unit.context_manifest(context) == result["source_context"] and
                                      reference.sha(profile_path) == result["profile_sha256"])
        if not result["inputs_unchanged"]:
            raise ValueError("runtime build inputs changed")
        if comparison and result["package_sha256"] != comparison["package_sha256"]:
            raise ValueError("runtime package differs from clean comparison")
        result["passed"] = True
    except Exception as error:
        failure = error
        result["error"] = str(error)
    finally:
        reference_lifecycle.write_json(archive / "result.json", result)
        print(json.dumps({"evidence": str(archive), **result}), flush=True)
    if failure:
        raise RuntimeError("runtime package failed: " + str(archive)) from failure


if __name__ == "__main__":
    main()
