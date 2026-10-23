#!/usr/bin/env python3
"""Retire a superseded failure dump after retaining metadata, hashes and diagnostics."""
import argparse
import gzip
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import tarfile
import threading
import uuid

import reference
import reference_lifecycle
import upstream_unit


def sync_json(path, value):
    with path.open("x") as stream:
        json.dump(value, stream, indent=2)
        stream.write("\n"); stream.flush(); os.fsync(stream.fileno())


def verify_bats_replacement(run, proof, old, new):
    if (old.get('suite') != 'systemd' or new.get('suite') != 'systemd' or
            old.get('target_platform') != 'linux/amd64' or new.get('target_platform') != 'linux/amd64' or
            any(type(row.get('upstream_test_files_verified')) is not int or row['upstream_test_files_verified'] <= 0 or
                row['upstream_test_files_verified'] != row['unchanged_upstream_test_files'] + row['declared_upstream_test_changes'] or
                row.get('inputs_unchanged') is not True or
                row.get('components_unchanged') is not True for row in [old, new]) or
            old.get('unchanged_upstream_test_files') != new.get('unchanged_upstream_test_files') or
            old.get('declared_upstream_test_changes') != new.get('declared_upstream_test_changes') or
            not set(old['expected_titles']).issubset(new['expected_titles']) or
            old['inputs_sha256'].get('source.tar') != new['inputs_sha256'].get('source.tar')):
        raise ValueError('Bats supersession requires the same original upstream source and assertions')
    cases = {row['name']: row for row in new['observed']['cases']}
    if (new['observed'].get('complete') is not True or new['observed'].get('failed') or not old['observed']['failed'] or
            any(name not in cases or cases[name].get('passed') is not True or cases[name].get('skipped') is not False
                for name in old['observed']['failed'])):
        raise ValueError('every formerly failed Bats assertion must actually pass without a skip')
    for path, record in [(run, old), (proof, new)]:
        for name, digest in record['inputs_sha256'].items():
            supplied = path / name
            if supplied.resolve() != supplied or not supplied.is_relative_to(path) or reference.sha(supplied) != digest:
                raise ValueError('Bats frozen input changed before retention')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--source-run", required=True)
    parser.add_argument("--replacement-proof", required=True)
    parser.add_argument("--reason", required=True, help="Concrete supersession and remaining reconstruction limits")
    parser.add_argument("--verify-only",action="store_true",help="Retain and verify diagnostics while keeping the complete source archive")
    args = parser.parse_args()
    pattern = r"factorybox-(?:devx-lifecycle|consumer-worker-loss|checkpoint-retirement|bats)-[0-9a-f]{32}"
    if not all(re.fullmatch(pattern, name) for name in [args.source_run, args.replacement_proof]):
        raise ValueError("requires exact native consumer failure/proof IDs")
    run, proof = (reference.ROOT/"artifacts"/name for name in [args.source_run, args.replacement_proof])
    if run.resolve()!=run or proof.resolve()!=proof:
        raise ValueError("native evidence cannot be redirected through symlinks")
    source_result, replacement = (json.loads((path/"result.json").read_text()) for path in [run,proof])
    bats = any(name.startswith('factorybox-bats-') for name in [args.source_run, args.replacement_proof])
    if bats:
        if not all(name.startswith('factorybox-bats-') for name in [args.source_run, args.replacement_proof]):
            raise ValueError('Bats failure requires a complete Bats replacement')
        verify_bats_replacement(run, proof, source_result, replacement)
    if any(name.startswith('factorybox-checkpoint-retirement-') for name in [args.source_run, args.replacement_proof]):
        # Only supersede a fixture that failed on an invalid CLI verb before
        # its metadata assertions. Real runtime failures are not eligible.
        log = (run / 'stdout.txt').read_text()
        if (not all(name.startswith('factorybox-checkpoint-retirement-') for name in [args.source_run, args.replacement_proof]) or
                source_result.get('suite') != 'record-metadata' or replacement.get('suite') != 'record-metadata' or
                not any('unknown lifecycle command "' + verb + '"' in log for verb in ['status', 'inspect']) or
                replacement.get('passed_tests') != 1 or
                '--- PASS: TestNativeRecordMetadataResumeAdmission ' not in (proof / 'stdout.txt').read_text()):
            raise ValueError('checkpoint fixture retirement requires an invalid-verb failure and a passing corrected full metadata test')
        commands = lambda row: {key: value for key, value in row['source_context'].items()
                                if key.startswith('cmd/') and not key.endswith('_test.go')}
        if commands(source_result) != commands(replacement):
            raise ValueError('invalid-verb supersession requires the same public command implementation')
        checked = proof / 'output/record-metadata.json'
        evidence = json.loads(checked.read_text())
        if (reference.sha(checked) != replacement.get('proof_sha256') or
                any(evidence.get(key) is not True for key in ['passed', 'injected_history',
                    'resume_refused_before_transition', 'committed_pause_record_unchanged',
                    'source_still_stopped', 'no_resume_backing', 'prior_checkpoint_independent'])):
            raise ValueError('corrected metadata fixture proof is incomplete or changed')
    if any(path.is_symlink() for path in [run/"result.json",proof/"result.json",run/"failed-volume-0.tar.encoding.json"]):
        raise ValueError("native evidence receipts must not be symlinks")
    if source_result.get("passed") is not False or source_result.get("cleanup") is not True or replacement.get("passed") is not True or replacement.get("cleanup") is not True:
        raise ValueError("requires a closed failure and a complete passing replacement proof")
    old = run/"failed-volume-0.tar.zst"
    pointer = old.with_name(old.name+".retired.json")
    if pointer.exists() or pointer.is_symlink():
        raise ValueError("failure payload already has a retirement receipt")
    encoding=json.loads((run/"failed-volume-0.tar.encoding.json").read_text())
    original,encoded=encoding["original"],encoding["encoded"]
    native_originals=[row for row in source_result.get("failed_volume_archives",{}).values() if row.get("file")==original["file"]]
    if native_originals!=[original]:
        raise ValueError("encoding receipt differs from the original native failure result")
    for path,record in ([] if bats else [(run,source_result),(proof,replacement)]):
        context=path/("source" if (path/"source").is_dir() else "context")
        if upstream_unit.context_manifest(context)!=record["source_context"]:
            raise ValueError("frozen native source context changed")
    source=old
    if not source.exists():
        source,encoded=reference.artifact_storage.verified_replacement(source,encoded,original)
    before=reference.artifact_storage.identity(source)
    with source.open("rb") as stream:
        if hashlib.file_digest(stream,"sha256").hexdigest()!=encoded["sha256"]:
            raise ValueError("encoded source differs from its receipt")
    reference.artifact_storage.require_capacity(reference.ROOT,run_reserve=0)
    if min(reference.artifact_storage.ARTIFACT_LIMIT-reference.artifact_storage.allocated_bytes(reference.ROOT/"artifacts"),
           shutil.disk_usage(reference.ROOT).free-reference.artifact_storage.FREE_RESERVE) < 128<<20:
        raise ValueError("retirement needs space for bounded metadata and diagnostic retention")
    artifact=reference.ROOT/"artifacts"/("factorybox-storage-retirement-"+uuid.uuid4().hex)
    artifact.mkdir(mode=0o700);(artifact/"payloads").mkdir(mode=0o700)
    shutil.copyfile(__file__,artifact/"runner.py")
    result={"schema":1,"operation":"retire_superseded_failed_volume","source_run":args.source_run,
            "source_result_sha256":reference.sha(run/"result.json"),"replacement_proof":args.replacement_proof,
            "replacement_result_sha256":reference.sha(proof/"result.json"),"source_archive":str(source),
            "source_encoded_sha256":encoded["sha256"],"original":original,"reason":args.reason,
            "source_encoding_receipt_sha256":reference.sha(run/"failed-volume-0.tar.encoding.json"),
            "complete_archive_reconstructable_after_retirement":False,"passed":False,"retired":False}
    reference_lifecycle.write_json(artifact/"result.json",result)
    print(json.dumps({"retiring":str(artifact)}),flush=True)
    class Hashed:
        def __init__(self,stream):self.stream,self.hash,self.count=stream,hashlib.sha256(),0
        def read(self,size=-1):
            data=self.stream.read(size);self.hash.update(data);self.count+=len(data)
            if self.count>original["bytes"]:raise ValueError("source exceeds its verified decoded size")
            return data
    members,retained_bytes=0,0
    decoder=None
    def stop_decoder():
        if decoder is not None and decoder.poll() is None:decoder.kill()
    timer=threading.Timer(180,stop_decoder);timer.daemon=True;timer.start()
    try:
        with (artifact/"decode.log").open("xb") as log, gzip.open(artifact/"members.jsonl.gz","xb") as metadata, subprocess.Popen(
                ["zstd","-q","-dc","--long=31","--",str(source)],stdout=subprocess.PIPE,stderr=log) as decoder:
            stream=Hashed(decoder.stdout)
            with tarfile.open(fileobj=stream,mode="r|") as archive:
                for entry in archive:
                    members+=1
                    if members>1_000_000:raise ValueError("source member count exceeds its bound")
                    row={"name":entry.name,"size":entry.size,"type":entry.type.decode("latin1"),"mode":entry.mode,
                         "uid":entry.uid,"gid":entry.gid,"uname":entry.uname,"gname":entry.gname,"mtime":entry.mtime,
                         "linkname":entry.linkname,"devmajor":entry.devmajor,"devminor":entry.devminor,"pax_headers":entry.pax_headers}
                    if entry.isfile():
                        diagnostic=("/captures/" in entry.name and "/filesystem/" not in entry.name and "/pages-" not in entry.name or
                                    entry.name.endswith((".log","/record.json","/config.json")) or
                                    "/bundle/upper/workspace/project/" in entry.name and "/node_modules/" not in entry.name)
                        keep=diagnostic and entry.size<=4<<20
                        digest,parts,count=hashlib.sha256(),[],0
                        with archive.extractfile(entry) as content:
                            while data:=content.read(1<<20):
                                digest.update(data);count+=len(data)
                                if keep:parts.append(data)
                        if count!=entry.size:raise ValueError("incomplete member content")
                        row["sha256"]=digest.hexdigest()
                        if keep:
                            target=artifact/"payloads"/row["sha256"]
                            if not target.exists():
                                retained_bytes+=count
                                if retained_bytes>64<<20:raise ValueError("selected diagnostics exceed their retention bound")
                                with target.open("xb") as output:
                                    output.write(b"".join(parts));output.flush();os.fsync(output.fileno())
                            row["retained_content"]=str(target.relative_to(artifact))
                    metadata.write((json.dumps(row,separators=(",",":"))+"\n").encode())
            while stream.read(1<<20):pass
            if decoder.wait(timeout=30) or stream.count!=original["bytes"] or stream.hash.hexdigest()!=original["sha256"]:
                raise ValueError("complete source decompression differs from its verified original")
        if reference.artifact_storage.identity(source)!=before or reference.sha(run/"result.json")!=result["source_result_sha256"] or reference.sha(proof/"result.json")!=result["replacement_result_sha256"]:
            raise ValueError("closed source or proof changed during retirement")
        for path in [artifact/"members.jsonl.gz",artifact/"runner.py"]:
            with path.open("rb") as retained:os.fsync(retained.fileno())
        verification={**result,"passed":True,"verification_only":True,"complete_original_decompression_verified":True,"members":members,"retained_unique_bytes":retained_bytes,
                      "member_index_sha256":reference.sha(artifact/"members.jsonl.gz"),"reclaimed_encoded_bytes":before[2],
                      "scope":"Every member header and regular-file SHA-256 retained; selected controller/CRIU/workspace diagnostics retain contents. Large superseded payloads are intentionally unavailable. Frozen source, native results/logs and original consumer/build inputs are unchanged."}
        sync_json(artifact/"verification.json",verification)
        if args.verify_only:
            result.update(passed=True,retired=False,verification_only=True,complete_original_decompression_verified=True,members=members,retained_unique_bytes=retained_bytes)
            print(json.dumps({"evidence":str(artifact),"retired":False,"complete_original_decompression_verified":True,"retained_unique_bytes":retained_bytes}),flush=True)
            return
        marker={"schema":1,"source_run":args.source_run,"original":original,"retirement_verification":str(artifact/"verification.json"),
                "verification_sha256":reference.sha(artifact/"verification.json"),"complete_archive_reconstructable":False}
        for path in dict.fromkeys([pointer,source.with_name(source.name+".retired.json")]):sync_json(path,marker)
        for directory in [artifact,artifact/"payloads",run,source.parent]:
            fd=os.open(directory,os.O_RDONLY|os.O_DIRECTORY)
            try:os.fsync(fd)
            finally:os.close(fd)
        source.unlink()
        fd=os.open(source.parent,os.O_RDONLY|os.O_DIRECTORY)
        try:os.fsync(fd)
        finally:os.close(fd)
        result.update(passed=True,retired=True,complete_original_decompression_verified=True,members=members,retained_unique_bytes=retained_bytes)
        print(json.dumps({"evidence":str(artifact),"retired_encoded_bytes":before[2],"retained_unique_bytes":retained_bytes}),flush=True)
    except Exception as error:
        result["error"]=str(error)
        raise
    finally:
        timer.cancel()
        reference_lifecycle.write_json(artifact/"result.json",result)


if __name__=="__main__":main()
