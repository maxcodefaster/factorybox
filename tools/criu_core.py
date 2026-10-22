#!/usr/bin/env python3
"""Qualify selected unchanged upstream CRIU C/R cases on the existing worker."""
import argparse
import datetime
import json
from pathlib import Path
import shutil
import subprocess
import tarfile
import uuid

import native_base
import criu_candidate
import reference
import reference_lifecycle
import sources

ROOT = reference.ROOT


def native_platform(base, criu, candidate_platform, host):
    target = candidate_platform or "linux/arm64"
    machine = {"linux/arm64": {"aarch64", "arm64"},
               "linux/amd64": {"x86_64", "amd64"}}
    if (target not in machine or base.get("platform", "linux/arm64") != target or
            criu.get("platform", "linux/arm64") != target):
        raise ValueError("CRIU and base builds must match the explicit native target")
    native_base.require_native_host(target, host)
    return target


def archive_volume(output, name, owner, image, result):
    container = json.loads(reference.docker(["container", "inspect", name]))[0]
    if container["Config"].get("Labels", {}).get("com.factorybox.run") != owner:
        raise ValueError("native fixture ownership changed")
    if container["State"]["Running"]:
        reference.docker(["container", "stop", "--time=2", name])
        container = json.loads(reference.docker(["container", "inspect", name]))[0]
    if container["State"]["Running"] or container["State"]["Pid"] != 0:
        raise ValueError("native fixture writers have not stopped")
    volume = json.loads(reference.docker(["volume", "inspect", name]))[0]
    if volume.get("Labels", {}).get("com.factorybox.run") != owner:
        raise ValueError("native fixture volume ownership changed")
    helper = name + "-archive"
    command = [
        *reference.DOCKER, "run", "--pull=never", "--name", helper,
        "--label", "com.factorybox.run=" + owner, "--network=none",
        "--cpus=1", "--memory=256m", "--pids-limit=16", "--read-only",
        "--cap-drop=ALL", "--cap-add=DAC_READ_SEARCH", "--security-opt=no-new-privileges",
        "--mount", f"type=volume,source={name},target=/input,readonly",
        "--entrypoint", "/bin/tar", image, "--numeric-owner", "-C", "/input",
        "-cpf", "-", "--exclude=./zdtm", ".",
    ]
    try:
        # The invoking host user writes the archive. The restricted reader needs
        # no write capability or writable host bind, including on native Linux.
        with (output / "archive.log").open("x") as log, (output / "native.tar").open("xb") as archive:
            subprocess.run(command, stdout=archive, stderr=log, check=True, timeout=60)
        result["archive_sha256"] = reference.sha(output / "native.tar")
    finally:
        reference.owned_delete("container", helper, owner)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    inputs = parser.add_mutually_exclusive_group(required=True)
    inputs.add_argument("--criu-build")
    inputs.add_argument("--retained-candidate", help="Explicitly unselected retained-context CRIU build")
    parser.add_argument("--criu-base-lock", type=Path,
                        help="Exact immutable selected-base lock for the retained candidate; current CRIU component must match in full")
    parser.add_argument("--native-base-build", required=True)
    parser.add_argument("--candidate-platform", choices=["linux/amd64"],
                        help="Run native core tests on an explicitly unqualified platform")
    parser.add_argument("--test", help="One selected C case for diagnosis")
    options = parser.parse_args()
    if options.criu_base_lock and not options.retained_candidate:
        parser.error("--criu-base-lock requires --retained-candidate")
    spec = json.loads((ROOT / "tests/criu-core-cases.json").read_text())
    expected = [c["name"] for c in spec["cases"]]
    if options.test:
        if options.test not in expected:
            parser.error("--test must name one declared core case")
        expected = [options.test]
    image, base = native_base.load(options.native_base_build, candidate_platform=options.candidate_platform)
    lock = ROOT / "upstream/lock.json"
    if options.retained_candidate:
        build, criu, component, unchanged = criu_candidate.load(options.retained_candidate, base_lock=options.criu_base_lock)
        tree, lock = build / "context", build / "source-lock.json"
    else:
        build, criu = reference_lifecycle.component_build(options.criu_build, "factorybox-build-criu-")
        if reference.sha(build / "criu") != criu["binary_sha256"]:
            raise ValueError("CRIU binary changed")
        component = json.loads(lock.read_text())["components"]["criu"]
        tree = ROOT / ".cache/sources/criu"
        sources.validate_inputs(component)
        unchanged = sources.check_source(tree, ROOT / ".cache/downloads" / component["archive"], component)
    host = json.loads(reference.docker(["info", "--format", "{{json .}}"]))
    target = native_platform(base, criu, options.candidate_platform, host)
    owner = uuid.uuid4().hex
    name = "factorybox-criu-core-" + owner
    output = reference.new_artifact(name)
    output.mkdir()
    shutil.copyfile(__file__, output / "runner.py")
    for source in ["criu-core.py", "criu-core-cases.json"]:
        shutil.copyfile(ROOT / "tests" / source, output / source)
    shutil.copyfile(lock, output / "source-lock.json")
    if options.criu_base_lock:
        shutil.copyfile(options.criu_base_lock, output / "criu-base-source-lock.json")
    # Preserve dangling and ordinary upstream symlinks across the Mac file share.
    # The native compiler receives the exact verified files on a Linux volume.
    with tarfile.open(output / "source.tar", "w", dereference=False) as archive:
        archive.add(tree / "test/zdtm", arcname="zdtm")
    result = {
        "schema": 1, "owner": owner, "container_name": name, "volume_name": name,
        "started_at": datetime.datetime.now(datetime.timezone.utc).isoformat(),
        "passed": False, "cleanup": False, "production_qualified": False,
        "platform": target, "candidate_platform": bool(options.candidate_platform),
        "native_lifecycle_qualified": False,
        "host": {key: host[key] for key in ["OSType", "Architecture", "KernelVersion"]},
        "criu_build": options.criu_build or options.retained_candidate,
        "retained_candidate": bool(options.retained_candidate), "criu_sha256": criu["binary_sha256"],
        "native_base_build": options.native_base_build, "image": image,
        "native_base_packages_sha256": base["packages_sha256"],
        "source_lock_sha256": reference.sha(output / "source-lock.json"),
        "runner_sha256": reference.sha(output / "runner.py"),
        "criu_base_source_lock_sha256": reference.sha(output / "criu-base-source-lock.json") if options.criu_base_lock else None,
        "source_transport_sha256": reference.sha(output / "source.tar"),
        "unchanged_upstream_test_files": unchanged,
        "test_inputs_sha256": {p: reference.sha(output / p)
                               for p in ["criu-core.py", "criu-core-cases.json"]},
        "expected_cases": expected, "test": options.test,
        "scope": "Selected unchanged ZDTM C tests with two ordinary dump/restore cycles and "
                 "their own final assertions, host flavor inside owned container. "
                 "No global hugepage/cgroup setup or complete ZDTM/Sysbox qualification.",
    }
    attempted = False
    volume_attempted = False
    archived = False
    failure = None
    try:
        command = [
            *reference.DOCKER, "run", "--pull=never", "--platform=" + target, "--name", name,
            "--label", "com.factorybox.run=" + owner, "--privileged",
            "--cgroupns=private", "--network=none", "--cpus=3", "--memory=3g", "--pids-limit=256",
            "--ulimit", "nofile=4096:4096", "--env", "FACTORYBOX_CRIU_CORE=1",
            "--env", "FACTORYBOX_CRIU_SOURCE_SHA256=" + result["source_transport_sha256"],
            "--mount", f"type=volume,source={name},target=/work",
            "--mount", f"type=bind,source={output / 'source.tar'},target=/source.tar,readonly",
            "--mount", f"type=bind,source={build / 'criu'},target=/criu,readonly",
            "--mount", f"type=bind,source={output},target=/output",
            "--mount", f"type=bind,source={output / 'criu-core.py'},target=/run-tests.py,readonly",
            "--mount", f"type=bind,source={output / 'criu-core-cases.json'},target=/cases.json,readonly",
            "--entrypoint", "/bin/sh", image, "-ec",
            "mount --bind /proc/sys/vm /proc/sys/vm\n"
            "mount -o remount,bind,ro /proc/sys/vm\n"
            "exec /usr/bin/python3 /run-tests.py --platform=" + target +
            (" --test=" + options.test if options.test else ""),
        ]
        result["command"] = command
        reference_lifecycle.write_json(output / "intent.json", result)
        print(json.dumps({"running": str(output), "cases": expected}), flush=True)
        volume_attempted = True
        reference.docker(["volume", "create", "--label", "com.factorybox.run=" + owner, name])
        attempted = True
        with (output / "stdout.txt").open("x") as stdout, (output / "stderr.txt").open("x") as stderr:
            done = subprocess.run(command, stdout=stdout, stderr=stderr, timeout=360)
        result["exit_code"] = done.returncode
        state = json.loads(reference.docker(["container", "inspect", name]))[0]["State"]
        result["container_state"] = state
        if not (output / "native-result.json").exists():
            raise ValueError("native fixture did not report a result: " +
                             (output / "stderr.txt").read_text()[-4096:])
        native = json.loads((output / "native-result.json").read_text())
        result["native"] = native
        result["passed"] = (done.returncode == 0 and not state["OOMKilled"] and
                            native["passed"] and native["cases"] == expected and
                            native["platform"] == target and native["page_size"] == 4096)
        if not result["passed"]:
            raise ValueError("selected upstream CRIU C/R qualification failed")
    except Exception as error:
        failure = error
        result["error"] = str(error)
    finally:
        if attempted:
            try:
                archive_volume(output, name, owner, image, result)
                archived = True
            except Exception as error:
                result["archive_error"] = str(error)
                failure = failure or error
        try:
            if attempted:
                reference.owned_delete("container", name, owner)
            if volume_attempted and archived:
                reference.owned_delete("volume", name, owner)
            result["cleanup"] = not volume_attempted or archived
        except Exception as error:
            result["cleanup_error"] = str(error)
            failure = failure or error
        result["passed"] = result["passed"] and archived and result["cleanup"]
        reference_lifecycle.write_json(output / "result.json", result)
        print(json.dumps({"evidence": str(output), **result}), flush=True)
    if failure:
        raise RuntimeError("CRIU core gate failed; preserved evidence: " + str(output)) from failure


if __name__ == "__main__":
    main()
