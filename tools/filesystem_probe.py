#!/usr/bin/env python3
"""Bounded native integrity/profile loop; not full lifecycle qualification."""
import argparse
import json
import os
import pathlib
import re
import shutil
import statistics
import subprocess
import time
import uuid

import native_base
import reference
import upstream_unit


def frozen_source(name):
    if not re.fullmatch(r"factorybox-pilot-baseline-[0-9a-f]{32}", name):
        raise ValueError("expected the exact frozen pilot baseline")
    artifact = reference.ROOT / "artifacts" / name
    manifest = json.loads((artifact / "source-manifest.json").read_text())
    source = artifact / "context"
    for name, digest in manifest.items():
        path = pathlib.PurePosixPath(name)
        if path.is_absolute() or ".." in path.parts:
            raise ValueError("unsafe frozen source path")
        file = source / path
        if file.is_symlink() or reference.sha(file) != digest:
            raise ValueError("frozen baseline differs: " + name)
    expected = {name: {"sha256": digest} for name, digest in manifest.items()}
    if upstream_unit.context_manifest(source) != expected:
        raise ValueError("frozen baseline source inventory differs")
    return source


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--native-base-build", required=True)
    parser.add_argument("--baseline-artifact")
    options = parser.parse_args()
    owner = uuid.uuid4().hex
    name = "factorybox-filesystem-probe-" + owner
    evidence = reference.new_artifact(name)
    evidence.mkdir()
    result = {"schema": 1, "owner": owner, "passed": False, "cleanup": False,
              "production_qualified": False, "scope": __doc__}
    attempted = volume_created = False
    try:
        host = json.loads(reference.docker(["info", "--format", "{{json .}}" ]))
        native_base.require_native_host("linux/amd64", host)
        image, _ = native_base.load(options.native_base_build, candidate_platform="linux/amd64")
        result["host"] = {k: host[k] for k in ["OSType", "Architecture", "KernelVersion"]}
        result["image"] = image
        configurations = {"current": reference.ROOT}
        if options.baseline_artifact:
            configurations = {"baseline": frozen_source(options.baseline_artifact), **configurations}
        result["baseline_artifact"] = options.baseline_artifact
        result["contexts"] = {}
        (evidence / "output").mkdir()
        # The privileged fixture writes through a host bind. Precreate outputs
        # as the invoking user so their original ownership remains readable.
        for file in ["digest.jsonl", *[label + ".cpu.pprof" for label in configurations]]:
            (evidence / "output" / file).touch(mode=0o600)
        binaries = evidence / "binaries"
        binaries.mkdir()
        env = dict(os.environ, GOOS="linux", GOARCH="amd64", CGO_ENABLED="0")
        for label, source in configurations.items():
            context = evidence / ("context-" + label)
            for directory in ["internal", "cmd"]:
                shutil.copytree(source / directory, context / directory)
            for file in ["go.mod", "go.sum"]:
                shutil.copyfile(source / file, context / file)
            # Add exactly the same observational fixture to both contexts.
            fixture = "internal/sysboxcapture/filesystem_probe_linux_test.go"
            shutil.copyfile(reference.ROOT / fixture, context / fixture)
            fixture = "internal/sysboxcapture/materialize_probe_linux_test.go"
            shutil.copyfile(reference.ROOT / fixture, context / fixture)
            result["contexts"][label] = upstream_unit.context_manifest(context)
            subprocess.run(["go", "test", "-c", "-trimpath", "-buildvcs=false", "-o",
                            str(binaries / (label + ".test")), "./internal/sysboxcapture"],
                           cwd=context, env=env, check=True, timeout=120)
        result["binaries_sha256"] = {p.name: reference.sha(p) for p in binaries.iterdir()}
        command = [*reference.DOCKER, "run", "--pull=never", "--platform=linux/amd64",
                   "--name", name, "--label", "com.factorybox.run=" + owner,
                   "--privileged", "--cgroupns=private", "--cpus=3", "--memory=3g",
                   "--pids-limit=128", "--network=none", "--read-only",
                   "--env", "GOMAXPROCS=2", "--env", "GOMEMLIMIT=256MiB",
                   "--env", "FACTORYBOX_NATIVE_RETAINED_CAPTURE=1",
                   "--env", "FACTORYBOX_NATIVE_FILESYSTEM_PROBE=1",
                   "--tmpfs", "/tmp:rw,nosuid,nodev,size=64m",
                   "--mount", f"type=volume,source={name},target=/var/lib/sysbox",
                   "--mount", f"type=bind,source={binaries},target=/binaries,readonly",
                   "--mount", f"type=bind,source={evidence / 'output'},target=/output"]
        script = ""
        for label in configurations:
            pattern = "^TestFilesystemCopyNative$" if label == "baseline" else "^TestFilesystem(Copy|Materialization)Native$"
            script += f"/binaries/{label}.test -test.v -test.run='{pattern}' -test.timeout=30s\n"
        order = ["current"] * 5
        if options.baseline_artifact:
            order = ["baseline", "current", "current", "baseline", "baseline", "current",
                     "current", "baseline", "baseline", "current"]
        result["order"] = order
        for label in order:
            script += (f"FACTORYBOX_PROBE_CONFIGURATION={label} /binaries/{label}.test -test.run='^$' "
                       "-test.bench='^BenchmarkFilesystem(Digest|Materialize)Native$' -test.benchtime=1x -test.timeout=30s\n")
        for label in configurations:
            script += (f"FACTORYBOX_PROBE_CONFIGURATION=profile-{label} /binaries/{label}.test -test.run='^$' "
                       "-test.bench='^BenchmarkFilesystemDigestNative$' -test.benchtime=10x "
                       f"-test.cpuprofile=/output/{label}.cpu.pprof -test.timeout=60s\n")
        command += ["--entrypoint", "/bin/sh", image, "-ec", script]
        result["command"] = command
        reference.docker(["volume", "create", "--label", "com.factorybox.run=" + owner, name])
        volume_created = True
        attempted = True
        started = time.monotonic()
        with (evidence / "stdout.txt").open("wb") as out, (evidence / "stderr.txt").open("wb") as err:
            completed = subprocess.run(command, stdout=out, stderr=err, timeout=420)
        result["seconds"] = time.monotonic() - started
        result["exit_code"] = completed.returncode
        result["state"] = json.loads(reference.docker(["container", "inspect", name]))[0]["State"]
        completed.check_returncode()
        output = (evidence / "stdout.txt").read_text()
        if output.count("--- PASS: TestFilesystemCopyNative (") != len(configurations) or "--- SKIP:" in output:
            raise ValueError("required integrity test omitted or skipped")
        if (output.count("--- PASS: TestFilesystemMaterializationNative (") != 1 or
                "--- PASS: TestFilesystemMaterializationNative/source-changes-during-copy (" not in output):
            raise ValueError("required materialization/copy-time corruption regression omitted")
        observations = [json.loads(line) for line in (evidence / "output/digest.jsonl").read_text().splitlines()]
        if len({o["tree"]["sha256"] for o in observations}) != 1:
            raise ValueError("identical fixtures have different complete digests")
        result["measurements"] = {}
        for label in configurations:
            result["measurements"][label] = {}
            for operation in ["digest", "materialize"]:
                seconds = [o["seconds"] for o in observations if o["configuration"] == label and o["operation"] == operation]
                if len(seconds) != 5:
                    raise ValueError("bounded comparison requires five observations per configuration and operation")
                result["measurements"][label][operation] = {"seconds": seconds, "median": statistics.median(seconds)}
            context = evidence / ("context-" + label)
            if upstream_unit.context_manifest(context) != result["contexts"][label]:
                raise ValueError("source inputs changed during probe")
        result["passed"] = not result["state"]["OOMKilled"]
    except Exception as error:
        result["error"] = str(error)
        raise
    finally:
        try:
            if attempted:
                reference.owned_delete("container", name, owner)
            if volume_created:
                reference.owned_delete("volume", name, owner)
            result["cleanup"] = True
        finally:
            result["passed"] = result["passed"] and result["cleanup"]
            (evidence / "result.json").write_text(json.dumps(result, indent=2) + "\n")
            print(json.dumps({"evidence": str(evidence), "passed": result["passed"],
                              "measurements": result.get("measurements"), "error": result.get("error")}))


if __name__ == "__main__":
    main()
