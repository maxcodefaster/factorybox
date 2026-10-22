#!/usr/bin/env python3
"""Losslessly re-encode one closed evidence dump, keeping its exact raw bytes."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import resource
import re
import shutil
import subprocess
import uuid

import reference
import reference_lifecycle


def require_closed_source(run, source):
    """Bind a reconciled failure to its exact successful retention receipt."""
    closed_path = run / "result.json"
    if closed_path.is_symlink():
        raise ValueError("closed-run result cannot be redirected")
    original = json.loads(closed_path.read_text())
    if original.get("cleanup") is True:
        return closed_path
    relative = source.relative_to(run)
    owner = original.get("owner", "")
    if (not re.fullmatch(r"[0-9a-f]{32}", owner) or
            run.name != "factorybox-consumer-worker-loss-" + owner or
            original.get("passed") is not False or original.get("cleanup") is not False or
            original.get("retained_volumes") != [run.name] or
            len(relative.parts) != 3 or relative.parts[0] != "reconciliations" or
            not re.fullmatch(r"[0-9a-f]{32}", relative.parts[1])):
        raise ValueError("evidence source is not a fully closed run")
    receipt_path = source.parent / "result.json"
    if receipt_path.is_symlink():
        raise ValueError("reconciliation result cannot be redirected")
    saved = json.loads(receipt_path.read_text())
    archives = saved.get("failed_volume_archives", {})
    if (saved.get("passed") is not True or saved.get("cleanup") is not True or
            saved.get("run") != run.name or saved.get("owner") != owner or
            saved.get("volume_name") != run.name or
            saved.get("original_result_sha256") != reference.sha(closed_path) or
            set(archives) != {run.name} or
            archives[run.name].get("file") != source.name.removesuffix(".zst")):
        raise ValueError("reconciled source lacks its exact complete retention receipt")
    return receipt_path


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("source", help="One existing closed-run .tar.zst under artifacts")
    options = parser.parse_args()
    supplied = Path(options.source).absolute()
    source = supplied.resolve()
    if supplied != source or source.is_symlink() or not source.is_relative_to(reference.ROOT/"artifacts") or not source.name.endswith(".tar.zst"):
        raise ValueError("requires one canonical repository evidence archive")
    run = reference.ROOT/"artifacts"/source.relative_to(reference.ROOT/"artifacts").parts[0]
    if run.name.startswith(("factorybox-build-","factorybox-devx-inputs-","factorybox-devx-project-images-","factorybox-bats-images-")):
        raise ValueError("frozen build/consumer inputs cannot be repacked")
    closed_path = require_closed_source(run, source)
    receipt_path = source.with_name(source.name.removesuffix(".zst")+".encoding.json")
    receipt = json.loads(receipt_path.read_text())
    expected = receipt["original"]
    if closed_path != run / "result.json":
        closed = json.loads(closed_path.read_text())
        if closed["failed_volume_archives"][run.name] != expected:
            raise ValueError("encoded source differs from its completed retention receipt")
    before = reference.artifact_storage.identity(source)
    if (receipt.get("whole_decompressed_payload_verified") is not True or
            receipt["encoded"]["file"] != source.name or receipt["encoded"]["bytes"] != before[2] or
            receipt["encoded"]["sha256"] != reference.sha(source)):
        raise ValueError("original compressed evidence differs from its verified receipt")
    pointer = source.with_name(source.name+".repacked.json")
    if pointer.exists() or pointer.is_symlink():
        raise ValueError("source already has a replacement")
    # This maintenance operation creates no build or native workload. It must
    # remain possible when native-run headroom is exhausted. The encoder's
    # hard file-size limit still preserves the full 32-GiB/host-reserve limits.
    reference.artifact_storage.require_capacity(reference.ROOT, run_reserve=0)
    artifact = reference.ROOT/"artifacts"/("factorybox-storage-repack-"+uuid.uuid4().hex)
    artifact.mkdir()
    shutil.copyfile(__file__,artifact/"runner.py")
    target = artifact/source.name
    result = {"schema":1,"operation":"lossless_closed_archive_repack","source":str(source),
              "closed_receipt":str(closed_path),"closed_receipt_sha256":reference.sha(closed_path),
              "source_encoded_sha256":receipt["encoded"]["sha256"],"source_encoded_bytes":before[2],
              "raw_bytes":expected["bytes"],"raw_sha256":expected["sha256"],"new_archive":str(target),
              "passed":False,"cleanup":False,"source_removed":False,"whole_decoded_bytes_verified":False}
    print(json.dumps({"repacking":str(artifact)}),flush=True)
    try:
        maximum = min(reference.artifact_storage.ARTIFACT_LIMIT-reference.artifact_storage.allocated_bytes(reference.ROOT/"artifacts"),
                      shutil.disk_usage(reference.ROOT).free-reference.artifact_storage.FREE_RESERVE)-(1<<20)
        def cap_encoder():
            soft,hard=resource.getrlimit(resource.RLIMIT_FSIZE)
            resource.setrlimit(resource.RLIMIT_FSIZE,(min(maximum,hard) if hard!=resource.RLIM_INFINITY else maximum,hard))
        digest,count=hashlib.sha256(),0
        with (artifact/"encode.log").open("xb") as log,target.open("xb") as output:
            decoder=subprocess.Popen(["zstd","-q","-dc","--long=31","--",str(source)],stdout=subprocess.PIPE,stderr=log)
            encoder=subprocess.Popen(["zstd","-q","-T1","-9","--long=31","--stdout"],stdin=subprocess.PIPE,stdout=output,stderr=log,preexec_fn=cap_encoder)
            try:
                while data:=decoder.stdout.read(1<<20):
                    count+=len(data)
                    if count>expected["bytes"]:raise ValueError("decoded source exceeds receipt")
                    digest.update(data);encoder.stdin.write(data)
                encoder.stdin.close()
                if decoder.wait(timeout=600) or encoder.wait(timeout=600):raise ValueError("lossless encoder failed")
            finally:
                for child in [decoder,encoder]:
                    if child.poll() is None:child.kill();child.wait()
            output.flush();os.fsync(output.fileno())
        if count!=expected["bytes"] or digest.hexdigest()!=expected["sha256"]:
            raise ValueError("complete original decoded evidence differs")
        digest,count=hashlib.sha256(),0
        with subprocess.Popen(["zstd","-q","-dc","--long=31","--",str(target)],stdout=subprocess.PIPE,stderr=subprocess.DEVNULL) as decoder:
            while data:=decoder.stdout.read(1<<20):
                count+=len(data)
                if count>expected["bytes"]:raise ValueError("replacement exceeds original decoded size")
                digest.update(data)
            if decoder.wait(timeout=600) or count!=expected["bytes"] or digest.hexdigest()!=expected["sha256"]:
                raise ValueError("complete replacement decompression differs")
        if reference.artifact_storage.identity(source)!=before:
            raise ValueError("original evidence changed during repack")
        result.update(encoded_sha256=reference.sha(target),encoded_bytes=target.stat().st_size,whole_decoded_bytes_verified=True)
        if result["encoded_bytes"]>=before[2]:raise ValueError("replacement reclaims no storage; original retained")
        # Commit verified facts before removing any original. The pointer binds
        # a final receipt; the original result/encoding records stay unchanged.
        result.update(passed=True,cleanup=True,source_removed=True)
        reference_lifecycle.write_json(artifact/"result.json",result)
        with (artifact/"result.json").open("rb") as committed:
            os.fsync(committed.fileno())
        parent=os.open(artifact,os.O_RDONLY|os.O_DIRECTORY)
        try:os.fsync(parent)
        finally:os.close(parent)
        replacement={"schema":1,"original_archive":str(source),"original_encoded_sha256":result["source_encoded_sha256"],
                     "original_tar_bytes":expected["bytes"],"original_tar_sha256":expected["sha256"],
                     "replacement_archive":str(target),"replacement_encoded_sha256":result["encoded_sha256"],
                     "verified_result":str(artifact/"result.json"),"verified_result_sha256":reference.sha(artifact/"result.json")}
        reference_lifecycle.write_json(pointer,replacement)
        with pointer.open("rb") as committed:
            os.fsync(committed.fileno())
        parent=os.open(pointer.parent,os.O_RDONLY|os.O_DIRECTORY)
        try:os.fsync(parent)
        finally:os.close(parent)
        # Check through the same resolver used by existing archive consumers.
        reference.artifact_storage.verified_replacement(source,receipt["encoded"],expected)
        source.unlink()
        parent=os.open(source.parent,os.O_RDONLY|os.O_DIRECTORY)
        try:os.fsync(parent)
        finally:os.close(parent)
        print(json.dumps({"evidence":str(artifact),"reclaimed_bytes":before[2]-target.stat().st_size,"verified_raw_bytes":count}),flush=True)
    except Exception as error:
        result.update(error=str(error),passed=False,cleanup=False,source_removed=not source.exists())
        reference_lifecycle.write_json(artifact/"result.json",result)
        raise


if __name__=="__main__":main()
