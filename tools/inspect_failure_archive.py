#!/usr/bin/env python3
"""Retain bounded selected diagnostics after verifying an entire closed dump.

No member path is extracted. Exact requested names map to content hashes in
the new receipt. The complete original archive and encoding remain unchanged.
This is offline evidence maintenance, not a build or native workload.
"""
import argparse
import hashlib
import json
from pathlib import Path
import re
import shutil
import subprocess
import tarfile
import uuid

import reference
import reference_lifecycle


class Hashed:
    def __init__(self, stream, limit):
        self.stream, self.limit = stream, limit
        self.digest, self.count = hashlib.sha256(), 0

    def read(self, size=-1):
        data = self.stream.read(size)
        self.count += len(data)
        if self.count > self.limit:
            raise ValueError("decoded dump exceeds its original receipt")
        self.digest.update(data)
        return data


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--run", required=True)
    parser.add_argument("--member", action="append", required=True)
    args = parser.parse_args()
    if not re.fullmatch(r"factorybox-consumer-worker-loss-[0-9a-f]{32}", args.run):
        raise ValueError("requires one exact consumer failure")
    run = reference.ROOT / "artifacts" / args.run
    if run.resolve() != run or (run / "result.json").is_symlink():
        raise ValueError("failure evidence cannot be redirected")
    result_path = run / "result.json"
    closed = json.loads(result_path.read_text())
    if closed.get("passed") is not False or closed.get("cleanup") is not True:
        raise ValueError("only a closed native failure can be inspected")
    receipt_path = run / "failed-volume-0.tar.encoding.json"
    encoding = json.loads(receipt_path.read_text())
    original, encoded = encoding["original"], encoding["encoded"]
    if (not encoding.get("whole_decompressed_payload_verified") or
            original not in closed.get("failed_volume_archives", {}).values()):
        raise ValueError("native dump and encoding receipts differ")
    source = run / encoded["file"]
    if not source.exists():
        source, encoded = reference.artifact_storage.verified_replacement(source, encoded, original)
    before = reference.artifact_storage.identity(source)
    with source.open("rb") as stream:
        digest = hashlib.file_digest(stream, "sha256").hexdigest()
    if before[2] != encoded["bytes"] or digest != encoded["sha256"]:
        raise ValueError("encoded dump differs from its receipt")
    requested = set(args.member)
    if len(requested) != len(args.member) or not 1 <= len(requested) <= 128:
        raise ValueError("requires unique bounded exact member names")
    reference.artifact_storage.require_capacity(reference.ROOT, run_reserve=0)
    artifact = reference.ROOT / "artifacts" / ("factorybox-failure-inspection-" + uuid.uuid4().hex)
    artifact.mkdir(mode=0o700)
    shutil.copyfile(__file__, artifact / "runner.py")
    result = {"schema": 1, "passed": False, "source_run": args.run,
              "source_result_sha256": reference.sha(result_path),
              "encoding_receipt_sha256": reference.sha(receipt_path),
              "source_archive": str(source), "encoded_sha256": digest,
              "original": original, "members": {}, "whole_decompressed_payload_verified": False}
    print(json.dumps({"inspecting": str(artifact)}), flush=True)
    try:
        total = 0
        with (artifact / "decoder.log").open("xb") as log, subprocess.Popen(
                ["zstd", "-q", "-dc", "--long=31", "--", str(source)],
                stdout=subprocess.PIPE, stderr=log) as decoder:
            stream = Hashed(decoder.stdout, original["bytes"])
            try:
                with tarfile.open(fileobj=stream, mode="r|") as archive:
                    for member in archive:
                        if member.name not in requested:
                            continue
                        if (member.name in result["members"] or not member.isfile() or
                                member.size > 8 << 20 or total + member.size > 32 << 20):
                            raise ValueError("selected diagnostic is duplicated, special or oversized")
                        with archive.extractfile(member) as content:
                            data = content.read((8 << 20) + 1)
                        if len(data) != member.size:
                            raise ValueError("selected diagnostic has incomplete content")
                        digest = hashlib.sha256(data).hexdigest()
                        target = artifact / digest
                        if not target.exists():
                            with target.open("xb") as output:
                                output.write(data)
                            target.chmod(0o600)
                        result["members"][member.name] = {"file": digest, "sha256": digest, "bytes": len(data)}
                        total += len(data)
                while stream.read(1 << 20):
                    pass
                if (decoder.wait(timeout=30) or stream.count != original["bytes"] or
                        stream.digest.hexdigest() != original["sha256"]):
                    raise ValueError("complete decoded dump differs from its native receipt")
            finally:
                if decoder.poll() is None:
                    decoder.kill()
                    decoder.wait()
        result["whole_decompressed_payload_verified"] = True
        if reference.artifact_storage.identity(source) != before or reference.sha(result_path) != result["source_result_sha256"]:
            raise ValueError("closed evidence changed while inspecting")
        missing = requested - result["members"].keys()
        if missing:
            raise ValueError("exact requested members absent: " + repr(sorted(missing)))
        result["passed"] = True
    except Exception as error:
        result["error"] = str(error)
    finally:
        reference_lifecycle.write_json(artifact / "result.json", result)
        print(json.dumps({"evidence": str(artifact), "passed": result["passed"], "error": result.get("error")}), flush=True)
    return 0 if result["passed"] else 1


if __name__ == "__main__":
    raise SystemExit(main())
