#!/usr/bin/env python3
"""Retire only an optional closed worker build's generated Go cache."""
import argparse
import json
import os
from pathlib import Path
import re
import shutil
import uuid

import build_worker
import reference
import upstream_unit


def saved(path, value):
    with path.open("x") as stream:
        json.dump(value,stream,indent=2); stream.write("\n"); stream.flush(); os.fsync(stream.fileno())
    parent=os.open(path.parent,os.O_RDONLY|os.O_DIRECTORY)
    try: os.fsync(parent)
    finally: os.close(parent)


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--build",required=True)
    parser.add_argument("--native-proof",required=True)
    args=parser.parse_args()
    if not re.fullmatch(r"factorybox-consumer-worker-loss-[0-9a-f]{32}",args.native_proof):
        raise ValueError("requires one exact completed consumer proof")
    build,result=build_worker.load(args.build)
    proof=reference.ROOT/"artifacts"/args.native_proof/"result.json"
    if proof.is_symlink() or proof.resolve()!=proof: raise ValueError("native proof cannot be redirected")
    native=json.loads(proof.read_text())
    if native.get("passed") is not True or native.get("cleanup") is not True or native.get("worker_build")!=args.build or native.get("worker_image_id")!=result["image_id"]:
        raise ValueError("native proof does not exercise this exact delivered image")
    cache=build/"go-cache"
    if cache.resolve()!=cache or not cache.is_dir(): raise ValueError("exact generated cache is absent or redirected")
    for path in cache.rglob("*"):
        if path.is_symlink() or not (path.is_dir() or path.is_file()): raise ValueError("generated cache has an unexpected special entry")
    reference.artifact_storage.require_capacity(reference.ROOT,run_reserve=0)
    artifact=reference.ROOT/"artifacts"/("factorybox-storage-retire-cache-"+uuid.uuid4().hex); artifact.mkdir()
    shutil.copyfile(__file__,artifact/"runner.py")
    manifest=upstream_unit.context_manifest(cache)
    receipt={"schema":1,"operation":"retire_optional_generated_go_build_cache","build":args.build,
             "native_proof":args.native_proof,"native_result_sha256":reference.sha(proof),
             "build_result_sha256":reference.sha(build/"result.json"),"cache":str(cache),
             "generated_cache_manifest":manifest,"allocated_bytes":reference.artifact_storage.allocated_bytes(cache),
             "source_and_bundle_preserved":True,"reconstruction":"Fresh offline Go compilation from the preserved frozen source and exact toolchain; generated cache bytes are optional and intentionally removed."}
    saved(artifact/"receipt.json",receipt)
    if upstream_unit.context_manifest(cache)!=manifest: raise ValueError("closed cache changed before retirement")
    shutil.rmtree(cache)
    # Recheck every frozen source, binary, package and image after retirement.
    build_worker.load(args.build)
    saved(artifact/"result.json",{"schema":1,"passed":True,"removed":str(cache),
                                "receipt_sha256":reference.sha(artifact/"receipt.json"),"reclaimed_allocated_bytes":receipt["allocated_bytes"]})
    print(json.dumps({"evidence":str(artifact),"reclaimed_allocated_bytes":receipt["allocated_bytes"]}),flush=True)


if __name__=="__main__": main()
