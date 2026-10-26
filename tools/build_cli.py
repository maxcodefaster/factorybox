#!/usr/bin/env python3
"""Build and execute the ordinary CLI on its matching Linux architecture."""
import argparse
import json
import os
import platform
import shutil
import subprocess
import uuid

import build_config
import native_base
import reference
import upstream_unit


def require_native_target(target):
    native_base.require_native_host(target, {
        "OSType": platform.system().lower(), "Architecture": platform.machine().lower(),
    })
    host = subprocess.check_output(
        ["go", "env", "GOHOSTOS", "GOHOSTARCH"], text=True, timeout=30).split()
    if host != target.split("/"):
        raise ValueError("CLI build requires a native Go toolchain matching the target")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--platform", choices=build_config.PLATFORMS, required=True)
    options = parser.parse_args()
    require_native_target(options.platform)
    artifact = reference.new_artifact("factorybox-build-cli-" + uuid.uuid4().hex)
    artifact.mkdir()
    shutil.copyfile(__file__, artifact / "runner.py")
    source = artifact / "source"
    for directory in ["cmd/factorybox", "internal"]:
        shutil.copytree(reference.ROOT / directory, source / directory)
    for name in ["go.mod", "go.sum"]:
        shutil.copyfile(reference.ROOT / name, source / name)
    result = {"schema": 1, "platform": options.platform, "passed": False,
              "native_lifecycle_qualified": False, "production_qualified": False,
              "source_context": upstream_unit.context_manifest(source),
              "scope": "Native CLI compilation and version execution. No Sysbox/CRIU image or lifecycle qualification."}
    try:
        env = dict(os.environ, GOOS="linux", GOARCH=build_config.architecture(options.platform),
                   GOAMD64="v1", GOARM64="v8.0", CGO_ENABLED="0", GOEXPERIMENT="", GOFLAGS="",
                   GOWORK="off", GOTOOLCHAIN="local")
        result["go_version"] = subprocess.check_output(["go", "version"], text=True).strip()
        executable = artifact / ("factorybox-" + options.platform.replace("/", "-"))
        result["compile_command"] = ["go", "build", "-trimpath", "-buildvcs=false",
                                     "-o", str(executable), "./cmd/factorybox"]
        with (artifact / "build.log").open("x") as log:
            subprocess.run(["go", "mod", "verify"], cwd=source, env=env, check=True,
                           stdout=log, stderr=subprocess.STDOUT, timeout=60)
            subprocess.run(result["compile_command"], cwd=source, env=env, check=True,
                           stdout=log, stderr=subprocess.STDOUT, timeout=180)
        build_config.require_elf(executable, options.platform)
        result["executable_sha256"] = reference.sha(executable)
        (artifact / "SHA256SUMS").write_text(result["executable_sha256"] + "  " + executable.name + "\n")
        result["version_output"] = subprocess.check_output(
            [str(executable), "version"], text=True, timeout=15).strip()
        if not result["version_output"].startswith("factorybox v"):
            raise ValueError("native CLI returned an unexpected version response")
        if upstream_unit.context_manifest(source) != result["source_context"]:
            raise ValueError("CLI source changed during the build")
        result["passed"] = True
    except Exception as error:
        result["error"] = str(error)
        raise
    finally:
        (artifact / "result.json").write_text(json.dumps(result, indent=2) + "\n")
        print(json.dumps({"evidence": str(artifact), "platform": options.platform,
                          "passed": result["passed"], "native_lifecycle_qualified": False}), flush=True)


if __name__ == "__main__":
    main()
