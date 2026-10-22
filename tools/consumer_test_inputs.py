#!/usr/bin/env python3
"""Freeze exact already-tested consumer helpers for bounded repeated native runs."""
import argparse
import json
from pathlib import Path
import re
import shutil
import stat
import subprocess
import uuid

import build_worker
import reference
import reference_lifecycle
import upstream_unit

FILES = ("consumer-worker.test", "devx-actor", "devx-registry", "minimal-actor", "ide-filesystem")


def native_proof(name):
    if not re.fullmatch(r"factorybox-consumer-worker-loss-[0-9a-f]{32}", name):
        raise ValueError("requires one exact complete consumer lifecycle proof")
    path = reference.ROOT / "artifacts" / name
    if path.resolve() != path or (path / "result.json").is_symlink():
        raise ValueError("native input proof cannot be redirected")
    result = json.loads((path / "result.json").read_text())
    if (any(result.get(key) is not True for key in ["passed", "cleanup", "global_sysctls_unchanged"]) or
            result.get("probe") != "lifecycle" or result.get("baseline_input") is not None or
            len(result.get("phases", [])) != 1 or result["phases"][0].get("passed") is not True or
            upstream_unit.context_manifest(path / "source") != result["source_context"]):
        raise ValueError("native input proof is incomplete or its exact source changed")
    return path, result


def load(name, worker_build, source_context):
    if not re.fullmatch(r"factorybox-build-consumer-tests-[0-9a-f]{32}", name):
        raise ValueError("requires one exact completed consumer test-input build")
    path = reference.ROOT / "artifacts" / name
    if path.resolve() != path or (path / "result.json").is_symlink():
        raise ValueError("consumer test inputs cannot be redirected")
    result = json.loads((path / "result.json").read_text())
    original, native = native_proof(result["native_proof"])
    if (result.get("passed") is not True or result.get("platform") != "linux/amd64" or
            result.get("worker_build") != worker_build or native.get("worker_build") != worker_build or
            result.get("native_result_sha256") != reference.sha(original / "result.json") or
            result.get("source_context") != source_context or source_context != native["source_context"] or
            upstream_unit.context_manifest(path / "source") != source_context or
            set(result.get("binary_sha256", {})) != set(FILES)):
        raise ValueError("consumer test inputs differ from this worker or the complete current test source")
    for name in FILES:
        file = path / name
        if file.resolve() != file or file.is_symlink():
            raise ValueError("frozen consumer helper cannot be redirected")
        info = file.stat()
        expected = native["inputs_sha256"][name]
        before = reference.artifact_storage.identity(file)
        if (not stat.S_ISREG(info.st_mode) or info.st_mode & 0o777 != 0o555 or
                result["binary_sha256"][name] != expected or reference.sha(file) != expected or
                reference.artifact_storage.identity(file) != before):
            raise ValueError("frozen consumer helper differs from its native proof")
    return path, result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--native-proof", required=True)
    args = parser.parse_args()
    original, native = native_proof(args.native_proof)
    _, worker = build_worker.load(native["worker_build"])
    if (native["worker_image_id"] != worker["image_id"] or
            native["lifecycle_cli_sha256"] != worker["bundle_sha256"]["factorybox"] or
            native["lifecycle_criu_sha256"] != worker["bundle_sha256"]["components/criu"]):
        raise ValueError("native input proof differs from its exact delivered worker")
    artifact = reference.new_artifact("factorybox-build-consumer-tests-" + uuid.uuid4().hex)
    artifact.mkdir()
    shutil.copyfile(__file__, artifact / "runner.py")
    result = {"schema": 1, "passed": False, "platform": "linux/amd64", "production_qualified": False,
              "native_proof": args.native_proof, "native_result_sha256": reference.sha(original / "result.json"),
              "worker_build": native["worker_build"], "source_context": native["source_context"],
              "binary_sha256": {}, "reconstruction": {},
              "scope": "Exact already native-tested Go helper bytes and complete test/core source for this worker. "
                       "Future native fixtures share immutable read-only inputs; their stores, processes and lifecycle "
                       "assertions stay independent. No workload/base reuse, runtime optimization or release qualification."}
    try:
        shutil.copytree(original / "source", artifact / "source")
        for name in FILES:
            source, target = original / name, artifact / name
            expected = native["inputs_sha256"][name]
            if source.exists():
                if source.is_symlink() or reference.sha(source) != expected:
                    raise ValueError("original native helper changed")
                shutil.copyfile(source, target)
            else:
                receipt_path = source.with_name(source.name + ".encoding.json")
                receipt = json.loads(receipt_path.read_text())
                if receipt_path.is_symlink() or receipt["original"]["sha256"] != expected:
                    raise ValueError("native helper reconstruction receipt differs")
                encoded = reference.artifact_storage.verify_compacted(source, receipt["original"])
                payload = Path(encoded.get("artifact_path", str(source.with_name(encoded["file"]))))
                with target.open("xb") as stream:
                    subprocess.run(["zstd", "-q", "-dc", "--long=31", "--", str(payload)], stdout=stream, check=True, timeout=30)
                result["reconstruction"][name] = {"receipt_sha256": reference.sha(receipt_path),
                                                    "whole_decoded_input_verified": True}
            if reference.sha(target) != expected:
                raise ValueError("reconstructed native helper bytes differ")
            target.chmod(0o555)
            result["binary_sha256"][name] = expected
        result["passed"] = (native_proof(args.native_proof)[1] == native and
                            reference.sha(original / "result.json") == result["native_result_sha256"] and
                            upstream_unit.context_manifest(artifact / "source") == result["source_context"])
        if not result["passed"]:
            raise ValueError("frozen consumer input proof changed during assembly")
    except Exception as error:
        result["passed"] = False
        result["error"] = str(error)
        raise
    finally:
        reference_lifecycle.write_json(artifact / "result.json", result)
        print(json.dumps({"evidence": str(artifact), "passed": result["passed"], "error": result.get("error")}), flush=True)


if __name__ == "__main__":
    main()
