#!/usr/bin/env python3
"""Repeat the unchanged proc admission assertions; never a qualification gate."""
import argparse
import datetime
import json
from pathlib import Path
import re
import shutil
import subprocess
import uuid

import build_worker
from consumer_worker_loss import sysctls
import lifecycle_native
import native_base
import reference
import reference_lifecycle
import upstream_unit


WRAPPER = b'''#!/bin/bash
set -euo pipefail
test "$#" = 4
test "$1" = -test.v
test "$2" = -test.timeout=360s
test "$3" = -test.run
test "$4" = '^Test((Filesystem|Descriptor|ProcDirectory|DetachedFile|SocketQueue)Admission|CaptureSocketDescriptor)Native$'
exec /proc-directory.test -test.v -test.timeout=360s -test.count=20 -test.failfast -test.run '^TestProcDirectoryAdmissionNative$'
'''


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--native-input", required=True, help="Closed passing complete admission suite")
    parser.add_argument("--worker-build", required=True)
    args = parser.parse_args()
    if not re.fullmatch(r"factorybox-runtime-[0-9a-f]{32}", args.native_input):
        parser.error("requires one exact native admission artifact")
    root = reference.ROOT
    source = root / "artifacts" / args.native_input
    if source.resolve() != source or source.is_symlink():
        raise ValueError("native inputs cannot be redirected")
    original_result = reference.sha(source / "result.json")
    native = json.loads((source / "result.json").read_text())
    _, worker = build_worker.load(args.worker_build)
    if (any(native.get(key) is not True for key in
            ["passed", "cleanup", "inputs_unchanged", "source_context_unchanged", "components_unchanged"]) or
            native.get("suite") != "admission" or native.get("platform") != "linux/amd64" or
            native.get("passed_tests") != 17 or native.get("passed_subtests") != 78 or native.get("skipped_tests") != 0 or
            any(native.get(key) != worker[key] for key in ["criu_build", "native_base_build"]) or
            native["component_build"] != worker["sysbox_build"]):
        raise ValueError("requires a complete passing admission suite for these worker components")
    core = lambda manifest: {name: value for name, value in manifest.items() if not name.endswith("_test.go")}
    if (upstream_unit.context_manifest(source / "context") != native["source_context"] or
            core(native["source_context"]) != core(worker["source_context"])):
        raise ValueError("frozen native source differs from delivered core")
    for name, digest in native["component_sha256"].items():
        if reference.sha(source / "components" / name) != digest:
            raise ValueError("frozen native component changed")
    for name, digest in native["inputs_sha256"].items():
        if name == "lifecycle.test" and not (source / name).exists():
            receipt = json.loads((source / (name + ".encoding.json")).read_text())
            if receipt["original"]["sha256"] != digest:
                raise ValueError("native test reconstruction hash differs")
            reference.artifact_storage.verify_compacted(source / name, receipt["original"])
        elif reference.sha(source / name) != digest:
            raise ValueError("frozen native input changed: " + name)
    image, _ = native_base.load(worker["native_base_build"], candidate_platform="linux/amd64")
    host = json.loads(reference.docker(["info", "--format", "{{json .}}"])); native_base.require_native_host("linux/amd64", host)
    if native["host"] != {key: host[key] for key in native["host"]}:
        raise ValueError("native diagnostic host differs from the frozen test build")
    busybox = json.loads((root / "upstream/test-inputs.json").read_text())["busybox-amd64"]
    rootfs = root / ".cache/downloads" / busybox["archive"]
    if reference.sha(rootfs) != native["busybox_rootfs_sha256"] or native["busybox_rootfs_sha256"] != busybox["sha256"]:
        raise ValueError("pinned rootfs changed")
    owner = uuid.uuid4().hex
    name = "factorybox-proc-cleanup-diagnostic-" + owner
    artifact = reference.new_artifact(name); artifact.mkdir()
    (artifact / "runner.py").write_bytes(Path(__file__).read_bytes())
    (artifact / "output").mkdir()
    (artifact / "capture-archive.sh").write_bytes((source / "capture-archive.sh").read_bytes())
    (artifact / "invocation.sh").write_bytes(WRAPPER); (artifact / "invocation.sh").chmod(0o555)
    result = {"schema": 1, "owner": owner, "passed": False, "cleanup": False,
              "diagnostic_only": True, "production_qualified": False, "historical_failure_explained": False,
              "native_input": args.native_input, "native_result_sha256": original_result,
              "worker_build": args.worker_build, "predetermined_repetitions": 20,
              "scope": "Twenty fixed repetitions of both unchanged proc admission subtests, stopping at the first failure. Original setup, assertions and immediate unmount retained. No retry, signal or qualification.",
              "invocation_sha256": reference.sha(artifact / "invocation.sh"),
              "original_invocation": ["/lifecycle.test", "-test.v", "-test.timeout=360s", "-test.run", "^Test((Filesystem|Descriptor|ProcDirectory|DetachedFile|SocketQueue)Admission|CaptureSocketDescriptor)Native$"],
              "diagnostic_invocation": ["/proc-directory.test", "-test.v", "-test.timeout=360s", "-test.count=20", "-test.failfast", "-test.run", "^TestProcDirectoryAdmissionNative$"],
              "started_at": datetime.datetime.now(datetime.timezone.utc).isoformat()}
    attempted = volume_attempted = False
    failure = None
    try:
        binary = artifact / "lifecycle.test"
        with binary.open("xb") as output:
            if (source / "lifecycle.test").exists():
                with (source / "lifecycle.test").open("rb") as stream: shutil.copyfileobj(stream, output)
            else:
                subprocess.run(["zstd", "-q", "-dc", "--", str(source / "lifecycle.test.zst")], stdout=output, check=True, timeout=30)
        if reference.sha(binary) != native["inputs_sha256"]["lifecycle.test"]:
            raise ValueError("reconstructed native test differs")
        binary.chmod(0o555)
        result["reconstructed_test_sha256"] = reference.sha(binary)
        command = [*reference.DOCKER, "run", "--pull=never", "--platform=linux/amd64", "--name", name,
                   "--label", "com.factorybox.run=" + owner, "--privileged", "--cgroupns=private",
                   "--cpus=3", "--memory=3g", "--pids-limit=512", "--network=none"]
        for key, value in {"FACTORYBOX_NATIVE_LIFECYCLE": "1", "FACTORYBOX_UPSTREAM_CONTAINER": "1",
                           "FACTORYBOX_NATIVE_RETAINED_CAPTURE": "1", "FACTORYBOX_COMPONENT_INPUTS": "1",
                           "FACTORYBOX_NATIVE_FILESYSTEM_ADMISSION": "1", "FACTORYBOX_CRIU_SHA256": native["component_sha256"]["criu"],
                           "FACTORYBOX_RUNTIME_SHA256": native["component_sha256"]["sysbox-runc"],
                           "GOMAXPROCS": "2", "GOMEMLIMIT": "256MiB"}.items():
            command += ["--env", key + "=" + value]
        command += ["--mount", f"type=volume,source={name},target=/var/lib/sysbox",
                    "--mount", f"type=bind,source={artifact / 'output'},target=/output"]
        mounts = [(artifact / "invocation.sh", "/lifecycle.test"), (binary, "/proc-directory.test"),
                  (source / "factorybox", "/factorybox"), (rootfs, "/busybox-rootfs.tar.gz"),
                  (source / "lifecycle-native.sh", "/native.sh"), (source / "private-cgroup-delegation.sh", "/delegate.sh"),
                  (source / "components", "/component-input"), (source / "sysboxcapture.test", "/sysboxcapture.test")]
        for path, target in mounts: command += ["--mount", f"type=bind,source={path},target={target},readonly"]
        command += ["--entrypoint", "/bin/bash", image, "/native.sh"]
        result["command"] = command
        result["global_sysctls_before"] = sysctls()
        reference_lifecycle.write_json(artifact / "intent.json", result)
        print(json.dumps({"running": str(artifact)}), flush=True)
        volume_attempted = True
        reference.docker(["volume", "create", "--label", "com.factorybox.run=" + owner, name])
        attempted = True
        with (artifact / "stdout.txt").open("x") as log, (artifact / "stderr.txt").open("x") as errors:
            done = subprocess.run(command, stdout=log, stderr=errors, timeout=420)
        state = json.loads(reference.docker(["container", "inspect", name]))[0]["State"]
        lines = (artifact / "stdout.txt").read_text().splitlines()
        result.update(exit_code=done.returncode, container_state=state,
                      passed_repetitions=sum(line.startswith("--- PASS: TestProcDirectoryAdmissionNative ") for line in lines),
                      passed_subtests=sum(line.lstrip().startswith("--- PASS: TestProcDirectoryAdmissionNative/") for line in lines),
                      skipped_tests=sum(line.lstrip().startswith("--- SKIP: ") for line in lines))
        result["passed"] = (done.returncode == 0 and not state["OOMKilled"] and result["passed_repetitions"] == 20 and
                            result["passed_subtests"] == 40 and result["skipped_tests"] == 0)
        if not result["passed"]: raise RuntimeError("fixed proc cleanup diagnostic failed")
    except Exception as error:
        failure = error; result["error"] = str(error)
    finally:
        preserve_volume = False
        try:
            if attempted:
                try: lifecycle_native.archive_capture(artifact, name, owner, result, image)
                except Exception as error:
                    preserve_volume = True; result["archive_error"] = str(error); result["retained_source_volume"] = name
                    failure = failure or error
                reference.owned_delete("container", name, owner)
            if volume_attempted and not preserve_volume: reference.owned_delete("volume", name, owner)
            result["cleanup"] = not preserve_volume
            result["global_sysctls_after"] = sysctls()
            result["global_sysctls_unchanged"] = result.get("global_sysctls_before") == result["global_sysctls_after"]
            result["frozen_inputs_unchanged"] = (reference.sha(source / "result.json") == original_result and
                upstream_unit.context_manifest(source / "context") == native["source_context"] and
                all(reference.sha(path) == native["inputs_sha256"][path.name] for path in
                    [source / "factorybox", source / "lifecycle-native.sh", source / "private-cgroup-delegation.sh", source / "sysboxcapture.test"]) and
                all(reference.sha(source / "components" / component) == digest for component, digest in native["component_sha256"].items()) and
                reference.sha(artifact / "invocation.sh") == result["invocation_sha256"] and
                reference.sha(artifact / "lifecycle.test") == native["inputs_sha256"]["lifecycle.test"])
        except Exception as error:
            result["cleanup_error"] = str(error); failure = failure or error
        result["passed"] = bool(result["passed"] and result["cleanup"] and result.get("global_sysctls_unchanged") and
                                result.get("frozen_inputs_unchanged") and failure is None)
        reference_lifecycle.write_json(artifact / "result.json", result)
        print(json.dumps({"evidence": str(artifact), "passed": result["passed"], "error": result.get("error")}), flush=True)
        reference.artifact_storage.compact_closed_run(artifact, root)
    if failure or not result["passed"]: raise RuntimeError("proc cleanup diagnostic failed: " + str(artifact)) from failure


if __name__ == "__main__": main()
