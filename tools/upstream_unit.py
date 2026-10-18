#!/usr/bin/env python3
"""Run complete upstream Go unit suites inside an owned Linux container."""
import argparse
import datetime
import json
import pathlib
import subprocess
import uuid
import urllib.request

import reference
import reference_lifecycle

ROOT = reference.ROOT


def context_manifest(directory):
    entries = {}
    for path in directory.rglob("*"):
        name = str(path.relative_to(directory))
        if path.is_symlink():
            entries[name] = {"symlink": str(path.readlink())}
        elif path.is_file():
            entries[name] = {"sha256": reference.sha(path)}
    return entries


def observations(output):
    suites = {}
    for path in sorted(output.glob("*.exit-code")):
        name = path.name.removesuffix(".exit-code")
        records = []
        for line in (output / (name + ".jsonl")).read_text().splitlines():
            try:
                records.append(json.loads(line))
            except json.JSONDecodeError:
                continue
        failed = [r.get("Test", r.get("Package")) for r in records if r.get("Action") == "fail"]
        suites[name] = {
            "exit_code": int(path.read_text()),
            "passed_tests": sum(r.get("Action") == "pass" and "Test" in r for r in records),
            "skipped_tests": [r["Test"] for r in records
                              if r.get("Action") == "skip" and "Test" in r],
            "failed": failed,
        }
    return suites


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--sysbox-build", required=True)
    parser.add_argument("--suite", choices=["all", "libcontainer"], default="all")
    options = parser.parse_args()
    build, build_result = reference_lifecycle.component_build(
        options.sysbox_build, "factorybox-build-sysbox-")
    owner = uuid.uuid4().hex
    name = "factorybox-upstream-" + owner
    archive = ROOT / "artifacts" / name
    output = archive / "output"
    output.mkdir(parents=True)
    result = {"schema": 1, "owner": owner, "container_name": name,
              "started_at": datetime.datetime.now(datetime.timezone.utc).isoformat(),
              "passed": False, "cleanup": False, "production_qualified": False,
              "suite": options.suite,
              "scope": ("Pinned libcontainer unit package only; executes the independently compiled binary."
                        if options.suite == "libcontainer" else
                        "Complete pinned runc/fs/mgr/ipc/libs Go unit suites. "
                        "Sysbox umbrella/Bats/systemd and CRIU integration gates are separate.")}
    attempted = False
    failure = None
    try:
        dockerfile = ROOT / "build/sysbox.Dockerfile"
        if reference.sha(dockerfile) != build_result["dockerfile_sha256"]:
            raise ValueError("builder recipe changed since its component build")
        expected = json.loads((build / "context-manifest.json").read_text())
        if context_manifest(build / "context") != expected:
            raise ValueError("verified build context changed")
        result["component_build"] = options.sysbox_build
        result["test_script_sha256"] = reference.sha(ROOT / "tests/upstream-unit.sh")
        busybox = json.loads((ROOT / "upstream/test-inputs.json").read_text())["busybox-arm64"]
        rootfs = ROOT / ".cache/downloads" / busybox["archive"]
        if not rootfs.exists():
            with urllib.request.urlopen(busybox["url"], timeout=30) as response:
                rootfs.write_bytes(response.read())
        if reference.sha(rootfs) != busybox["sha256"]:
            raise ValueError("upstream Busybox test rootfs differs")
        result["busybox_rootfs_sha256"] = busybox["sha256"]
        command = [*reference.DOCKER, "build", "--provenance=false", "--progress=plain",
                   "--platform=linux/arm64", "--target", "build",
                   "--output", "type=image,compression=uncompressed",
                   "--tag", name, "--iidfile", str(archive / "builder-id.txt"),
                   "--metadata-file", str(archive / "builder-metadata.json"),
                   "-f", str(dockerfile), str(build / "context")]
        with (archive / "builder.log").open("x") as log:
            subprocess.run(command, stdout=log, stderr=subprocess.STDOUT, check=True, timeout=120)
        result["builder_image_config"] = (archive / "builder-id.txt").read_text().strip()
        metadata = json.loads((archive / "builder-metadata.json").read_text())
        image = name + "@" + metadata["containerimage.digest"]
        result["builder_image_manifest"] = image
        command = [*reference.DOCKER, "run", "--pull=never", "--name", name,
                   "--label", "com.factorybox.run=" + owner, "--privileged",
                   "--cgroupns=private", "--cpus=2", "--memory=2g", "--pids-limit=512",
                   "--env", "FACTORYBOX_UPSTREAM_CONTAINER=1",
                   "--env", "FACTORYBOX_UPSTREAM_SUITE=" + options.suite,
                   "--env", "GOMAXPROCS=2", "--env", "GOFLAGS=-p=2",
                   "--mount", f"type=bind,source={build / 'context'},target=/input,readonly",
                   "--mount", f"type=bind,source={output},target=/output",
                   "--mount", f"type=bind,source={ROOT / 'tests/upstream-unit.sh'},target=/run-tests.sh,readonly",
                   "--mount", f"type=bind,source={rootfs},target=/busybox-rootfs.tar.gz,readonly",
                   "--entrypoint", "/bin/bash", image, "/run-tests.sh"]
        result["command"] = command
        reference_lifecycle.write_json(archive / "intent.json", result)
        print(json.dumps({"running": str(archive)}), flush=True)
        attempted = True
        with (archive / "stdout.txt").open("x") as log, (archive / "stderr.txt").open("x") as errors:
            done = subprocess.run(command, stdout=log, stderr=errors, timeout=900)
        result["exit_code"] = done.returncode
        result["suites"] = observations(output)
        if done.returncode == 125:
            raise ValueError("upstream test container did not start: " +
                             (archive / "stderr.txt").read_text()[-4096:])
        required = ({"libcontainer"} if options.suite == "libcontainer" else
                    {"runc", "fs", "mgr", "ipc"} | {
                        "libs-" + p.parent.name
                        for p in (build / "context/sysbox-libs").glob("*/go.mod")})
        state = json.loads(reference.docker(["container", "inspect", name]))[0]["State"]
        result["container_state"] = state
        result["passed"] = (done.returncode == 0 and not state["OOMKilled"] and
                            required == result["suites"].keys() and
                            all(v["exit_code"] == 0 and not v["failed"]
                                for v in result["suites"].values()))
        if not result["passed"]:
            raise ValueError("upstream unit suites failed or did not all finish")
    except Exception as error:
        failure = error
        result["error"] = str(error)
        result["suites"] = observations(output)
    finally:
        try:
            if attempted:
                reference.owned_delete("container", name, owner)
            result["cleanup"] = True
        except Exception as error:
            result["cleanup_error"] = str(error)
            failure = failure or error
        reference_lifecycle.write_json(archive / "result.json", result)
        print(json.dumps({"evidence": str(archive), **result}), flush=True)
    if failure:
        raise RuntimeError("upstream qualification failed: " + str(archive)) from failure


if __name__ == "__main__":
    main()
