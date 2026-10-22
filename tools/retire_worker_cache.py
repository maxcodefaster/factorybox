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
import sources


def saved(path, value):
    with path.open("x") as stream:
        json.dump(value,stream,indent=2); stream.write("\n"); stream.flush(); os.fsync(stream.fileno())
    parent=os.open(path.parent,os.O_RDONLY|os.O_DIRECTORY)
    try: os.fsync(parent)
    finally: os.close(parent)


def frozen_runtime(build_name):
    if not re.fullmatch(r"factorybox-build-runtime-[0-9a-f]{32}",build_name):
        raise ValueError("requires one exact legacy runtime package build")
    build=reference.ROOT/"artifacts"/build_name
    if any(path.is_symlink() or path.resolve()!=path for path in [build,build/"result.json"]):
        raise ValueError("runtime cache inputs cannot be redirected")
    result=json.loads((build/"result.json").read_text())
    package=build/"factorybox-v0.1-linux-amd64.tar"
    manifest=build/"bundle/manifest.json"
    if (result.get("passed") is not True or result.get("inputs_unchanged") is not True or
            reference.sha(package)!=result["package_sha256"] or
            upstream_unit.context_manifest(build/"context")!=result["source_context"] or
            reference.sha(build/"bundle/profile.json")!=result["profile_sha256"]):
        raise ValueError("legacy frozen package/source/profile changed")
    if manifest.is_symlink() or json.loads(manifest.read_text())["binary_sha256"]!=result["binary_sha256"]:
        raise ValueError("legacy runtime manifest changed")
    for name,digest in result["binary_sha256"].items():
        path=build/"bundle"/name
        if path.is_symlink() or reference.sha(path)!=digest:
            raise ValueError("legacy runnable component bundle changed")
    return build,result


def qualified_runtime(build_name, proof_name):
    build,result=frozen_runtime(build_name)
    if not re.fullmatch(r"factorybox-v01-qualification-[0-9a-f]{32}",proof_name or ""):
        raise ValueError("requires its exact completed local runtime qualification")
    proof=reference.ROOT/"artifacts"/proof_name/"result.json"
    if proof.is_symlink() or proof.resolve()!=proof:
        raise ValueError("runtime qualification cannot be redirected")
    native=json.loads(proof.read_text())
    if (
            native.get("passed") is not True or native.get("native_lifecycle_qualified") is not True or
            native.get("exact_packaged_cli_tested") is not True or
            build_name not in {Path(native["runtime_package"]).parent.name,native["independent_component_rebuild_comparison"]} or
            result["package_sha256"]!=native["runtime_package_sha256"] or
            result["binary_sha256"]!=native["binary_sha256"] or
            upstream_unit.context_manifest(build/"context")!=result["source_context"] or
            sources.tree_sha(build/"context")!=native["go_source_sha256"] or
            reference.sha(build/"bundle/profile.json")!=native["profile_sha256"]):
        raise ValueError("legacy package, sources and native qualification do not match")
    return build,result,proof,native


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--build",required=True)
    proofs=parser.add_mutually_exclusive_group(required=True)
    proofs.add_argument("--native-proof")
    proofs.add_argument("--bounded-store-proof", help="Complete fixed-filesystem lifecycle/ENOSPC proof of this exact delivered worker")
    proofs.add_argument("--replacement-proof",help="Passing exact storage regression for a replacement of the failed build")
    proofs.add_argument("--qualified-runtime-proof",help="Completed native qualification for an older runtime package's optional cache")
    proofs.add_argument("--qualified-successor-runtime-proof",help="Native-qualified successor with identical pinned components; retire only the superseded runtime's optional compiler cache, preserving its runnable package")
    parser.add_argument("--failed-proof",help="Closed failing storage regression for the superseded build")
    args=parser.parse_args()
    if args.qualified_runtime_proof or args.qualified_successor_runtime_proof:
        if args.failed_proof:raise ValueError("failed proof is only for replacement storage evidence")
        if args.qualified_runtime_proof:
            build,result,proof,native=qualified_runtime(args.build,args.qualified_runtime_proof)
        else:
            if not re.fullmatch(r"factorybox-v01-qualification-[0-9a-f]{32}",args.qualified_successor_runtime_proof):
                raise ValueError("requires one exact qualified runtime successor")
            candidate=reference.ROOT/"artifacts"/args.qualified_successor_runtime_proof/"result.json"
            if candidate.is_symlink() or candidate.resolve()!=candidate:raise ValueError("successor cannot be redirected")
            successor_name=Path(json.loads(candidate.read_text())["runtime_package"]).parent.name
            _,successor,proof,native=qualified_runtime(successor_name,args.qualified_successor_runtime_proof)
            build,result=frozen_runtime(args.build)
            components=lambda row:{k:v for k,v in row["binary_sha256"].items() if k!="factorybox"}
            if (successor_name==args.build or result["source_lock_sha256"]!=successor["source_lock_sha256"] or
                    components(result)!=components(successor)):
                raise ValueError("successor must be separate and retain exact pinned components/source lock")
    else:
        build,result=build_worker.load(args.build)
    replacement=None
    def evidence(name,pattern):
        if not re.fullmatch(pattern,name or ""):raise ValueError("requires one exact completed native proof")
        path=reference.ROOT/"artifacts"/name/"result.json"
        if path.is_symlink() or path.resolve()!=path:raise ValueError("native proof cannot be redirected")
        return path,json.loads(path.read_text())
    if args.qualified_runtime_proof:
        replacement={"qualified_runtime_proof":args.qualified_runtime_proof,
                     "scope":"Only optional compiler cache retirement; qualified source, original native evidence, runnable components and complete package stay directly available."}
    elif args.qualified_successor_runtime_proof:
        replacement={"qualified_successor_proof":args.qualified_successor_runtime_proof,
                     "qualified_successor_build":successor_name,"qualified_successor_package_sha256":successor["package_sha256"],
                     "scope":"Only the superseded runtime's optional compiler cache. Identical pinned components and a fully native-qualified successor are verified. All original frozen sources, runnable binaries, complete packages, negative controls and native evidence stay directly available."}
    elif args.bounded_store_proof:
        if args.failed_proof: raise ValueError("failed proof requires a replacement storage proof")
        proof,native=evidence(args.bounded_store_proof,r"factorybox-bounded-store-[0-9a-f]{32}")
        if (native.get("passed") is not True or native.get("cleanup") is not True or
                native.get("inputs_unchanged") is not True or native.get("passed_tests") != 1 or
                native.get("worker_build") != args.build or native.get("worker_image_id") != result["image_id"]):
            raise ValueError("bounded storage proof does not exercise this exact delivered image")
        core=lambda rows:{key:value for key,value in rows.items() if not key.endswith("_test.go")}
        if (upstream_unit.context_manifest(proof.parent/"source") != native["source_context"] or
                core(native["source_context"]) != core(result["source_context"])):
            raise ValueError("bounded storage proof core differs from delivered bytes")
        checked=proof.parent/"output/bounded-store.json"
        content=json.loads(checked.read_text())
        if (checked.is_symlink() or reference.sha(checked) != native["proof_sha256"] or
                any(content.get(key) is not True for key in ["passed","direct_io","checkpoint_fork",
                    "branch_ram_independent","resume_ram_preserved","capture_refused_at_limit","prior_checkpoint_preserved",
                    "external_writable_bind_refused","owned_truncate_released_blocks"]) or
                content["full_capacity"]["filesystem_available_bytes"] != 0):
            raise ValueError("bounded store proof is incomplete or changed")
        replacement={"bounded_store_proof":args.bounded_store_proof,"proof_sha256":native["proof_sha256"],
                     "scope":"Only optional compiler cache retirement; exact delivered image, package, source, native failures and passing bounded-store lifecycle remain preserved. No production qualification."}
    elif args.native_proof:
        if args.failed_proof:raise ValueError("failed proof requires a replacement storage proof")
        proof,native=evidence(args.native_proof,r"factorybox-consumer-worker-loss-[0-9a-f]{32}")
        if native.get("passed") is not True or native.get("cleanup") is not True or native.get("worker_build")!=args.build or native.get("worker_image_id")!=result["image_id"]:
            raise ValueError("native proof does not exercise this exact delivered image")
    else:
        pattern=r"factorybox-storage-admission-[0-9a-f]{32}"
        failed_path,failed=evidence(args.failed_proof,pattern)
        proof,native=evidence(args.replacement_proof,pattern)
        if (failed.get("passed") is not False or failed.get("cleanup") is not True or failed.get("worker_build")!=args.build or
                failed.get("worker_image_id")!=result["image_id"] or native.get("passed") is not True or native.get("cleanup") is not True or
                native.get("inputs_unchanged") is not True or native.get("worker_build")==args.build):
            raise ValueError("requires this closed failure and its separately passing replacement")
        _,new_build=build_worker.load(native["worker_build"])
        if (native["worker_image_id"]!=new_build["image_id"] or
                any(new_build[key]!=result[key] for key in ["sysbox_build","criu_build","native_base_build"])):
            raise ValueError("replacement changed component inputs or image identity")
        core=lambda manifest:{key:value for key,value in manifest.items() if not key.endswith("_test.go")}
        for path,row,delivered in [(failed_path,failed,result),(proof,native,new_build)]:
            if (upstream_unit.context_manifest(path.parent/"source")!=row["source_context"] or
                    core(row["source_context"])!=core(delivered["source_context"])):
                raise ValueError("storage proof sources differ from the delivered bytes")
        for file,key in [("storage.json","pressure_result_sha256"),("resume-admission.json","resume_result_sha256"),("reserve-ownership.json","reserve_result_sha256")]:
            path=proof.parent/"output"/file
            if path.is_symlink() or path.resolve()!=path or reference.sha(path)!=native[key] or json.loads(path.read_text()).get("passed") is not True:
                raise ValueError("replacement lacks its actual checked storage regression")
        replacement={"failed_proof":args.failed_proof,"failed_result_sha256":reference.sha(failed_path),
                     "replacement_proof":args.replacement_proof,"replacement_build":native["worker_build"],
                     "scope":"Only the superseded failed build's optional compiler cache. Complete failed native inputs, logs, archive, old source/binary/package and passing replacement are preserved."}
    cache=build/"go-cache"
    if cache.resolve()!=cache or not cache.is_dir(): raise ValueError("exact generated cache is absent or redirected")
    for path in cache.rglob("*"):
        if path.is_symlink() or not (path.is_dir() or path.is_file()): raise ValueError("generated cache has an unexpected special entry")
    reference.artifact_storage.require_capacity(reference.ROOT,run_reserve=0)
    artifact=reference.ROOT/"artifacts"/("factorybox-storage-retire-cache-"+uuid.uuid4().hex); artifact.mkdir()
    shutil.copyfile(__file__,artifact/"runner.py")
    manifest=upstream_unit.context_manifest(cache)
    receipt={"schema":1,"operation":"retire_optional_generated_go_build_cache","build":args.build,
             "native_proof":args.native_proof,"replacement":replacement,"native_result_sha256":reference.sha(proof),
             "build_result_sha256":reference.sha(build/"result.json"),"cache":str(cache),
             "generated_cache_manifest":manifest,"allocated_bytes":reference.artifact_storage.allocated_bytes(cache),
             "source_and_bundle_preserved":True,"reconstruction":"Fresh offline Go compilation from the preserved frozen source and exact toolchain; generated cache bytes are optional and intentionally removed."}
    saved(artifact/"receipt.json",receipt)
    if upstream_unit.context_manifest(cache)!=manifest: raise ValueError("closed cache changed before retirement")
    allocated_before_removal=reference.artifact_storage.allocated_bytes(reference.ROOT/"artifacts")
    shutil.rmtree(cache)
    # Recheck frozen sources, binaries and packages, plus the worker image when present.
    if args.qualified_runtime_proof:qualified_runtime(args.build,args.qualified_runtime_proof)
    elif args.qualified_successor_runtime_proof:
        frozen_runtime(args.build)
        qualified_runtime(successor_name,args.qualified_successor_runtime_proof)
    else:build_worker.load(args.build)
    allocated_after_removal=reference.artifact_storage.allocated_bytes(reference.ROOT/"artifacts")
    reclaimed=allocated_before_removal-allocated_after_removal
    saved(artifact/"result.json",{"schema":1,"passed":True,"removed":str(cache),
                                "receipt_sha256":reference.sha(artifact/"receipt.json"),"cache_logical_allocated_bytes":receipt["allocated_bytes"],
                                "allocated_before_removal":allocated_before_removal,"allocated_after_removal":allocated_after_removal,
                                "reclaimed_allocated_bytes":reclaimed})
    print(json.dumps({"evidence":str(artifact),"reclaimed_allocated_bytes":reclaimed}),flush=True)


if __name__=="__main__": main()
