#!/usr/bin/env python3
"""Share byte-identical immutable inputs of closed, passing native proofs."""
import argparse
import json
import os
from pathlib import Path
import re
import shutil
import uuid

import build_worker
import reference


ROLES=("consumer-worker.test","devx-actor","devx-registry","minimal-actor","ide-filesystem",
       "factorybox","sysboxcapture.test","theia-client.cjs")
RUN_NAME = re.compile(r"factorybox-(?:consumer-worker-loss|devx-lifecycle|runtime|runtime-retained)-([0-9a-f]{32})")


def write(path,value):
    with path.open("x") as stream:
        json.dump(value,stream,indent=2);stream.write("\n");stream.flush();os.fsync(stream.fileno())
    directory=os.open(path.parent,os.O_RDONLY|os.O_DIRECTORY)
    try:os.fsync(directory)
    finally:os.close(directory)


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--include-completed-worker-components', action='store_true',
                        help='Also share identical pinned component binaries of fully verified completed worker builds; source, CLI, packages and image identities stay unchanged')
    parser.add_argument('--include-completed-worker-packages', action='store_true',
                        help='Also share byte-identical tar packages of independently completed verified worker builds; keep every path directly usable and preserve build results')
    args=parser.parse_args()
    if reference.docker(['ps', '--filter', 'label=com.factorybox.run', '--format', '{{.ID}}']):
        raise ValueError('immutable input sharing requires all Factorybox runs to finish')
    # Only closed, byte-pinned outputs are eligible. Full worker provenance
    # is read for verification; failure archives and active inputs stay out.
    reference.artifact_storage.require_capacity(reference.ROOT,run_reserve=0)
    artifact=reference.ROOT/"artifacts"/("factorybox-storage-share-inputs-"+uuid.uuid4().hex);artifact.mkdir()
    shutil.copyfile(__file__,artifact/"runner.py")
    groups={};actions=[];results={};workers=[]
    def consider(path, role, expected):
        if path.is_symlink() or not path.is_file():return
        if path.resolve() != path.absolute():raise ValueError('compiled input contains a symlink')
        info=path.stat()
        if info.st_uid != os.getuid():return
        if not expected or reference.sha(path)!=expected:raise ValueError('closed compiled input differs from its pinned hash: '+str(path))
        key=(role,expected,info.st_size,info.st_mode,info.st_uid,info.st_gid)
        if key not in groups:groups[key]=path;return
        original=groups[key]
        if os.path.samefile(original,path) or info.st_nlink!=1:return
        actions.append({'file':str(path),'canonical':str(original),'sha256':expected,'bytes':info.st_size,
                        'before_identity':list(reference.artifact_storage.identity(path)),
                        'mode':info.st_mode,'uid':info.st_uid,'gid':info.st_gid,
                        'original_mtime_ns':info.st_mtime_ns,'shared_mtime_ns':original.stat().st_mtime_ns,
                        'allocated_bytes':info.st_blocks*512})
    for run in sorted((reference.ROOT/"artifacts").iterdir()):
        if (args.include_completed_worker_components or args.include_completed_worker_packages) and re.fullmatch(r'factorybox-build-worker-[0-9a-f]{32}',run.name):
            # load verifies the complete frozen source, runnable bundle,
            # recipe, tar package, Docker image and layer identities.
            result_path=run/'result.json'
            if run.is_symlink() or result_path.is_symlink() or not result_path.is_file():continue
            if json.loads(result_path.read_text()).get('passed') is not True:continue
            build,result=build_worker.load(run.name)
            workers.append(run.name)
            results[str(result_path)]=reference.sha(result_path)
            if args.include_completed_worker_components:
                for role,expected in result['bundle_sha256'].items():
                    if role.startswith('components/'):
                        consider(build/'image-context/bundle'/role,role,expected)
            if args.include_completed_worker_packages:
                consider(build/'factorybox-worker-linux-amd64.tar',
                         'worker-package',result['package_sha256'])
            continue
        match = RUN_NAME.fullmatch(run.name)
        if not match:
            continue
        result_path=run/"result.json"
        if run.is_symlink() or result_path.is_symlink() or not result_path.is_file():continue
        result=json.loads(result_path.read_text())
        if result.get("passed") is not True or result.get("cleanup") is not True:continue
        if result.get("owner") != match[1]:
            raise ValueError("closed native proof owner differs from its directory")
        results[str(result_path)]=reference.sha(result_path)
        expected_inputs = {role: result.get("inputs_sha256", {}).get(role) for role in ROLES}
        expected_inputs.update({"components/" + role: result.get("component_sha256", {}).get(role)
                                for role in ("sysbox-runc", "sysbox-fs", "sysbox-mgr", "criu")})
        for role, expected in expected_inputs.items():
            consider(run/role,role,expected)
    plan={"schema":1,"operation":"share_identical_closed_native_inputs","result_sha256":results,"actions":actions,
          'verified_worker_builds':workers,
          "scope":"Only exact compiled input roles of complete passing consumer proofs and explicitly included completed worker builds. Source, results, logs, executable bytes, modes, ownership, packages and image identities are unchanged. Identical files adopt the canonical inode's mtime; both original and shared mtimes are retained in this receipt. No archived input is rewritten in place."}
    write(artifact/"plan.json",plan)
    done=[]
    try:
        for row in actions:
            target,source=Path(row["file"]),Path(row["canonical"])
            metadata=lambda path:(path.stat().st_mode,path.stat().st_uid,path.stat().st_gid)
            if (list(reference.artifact_storage.identity(target))!=row["before_identity"] or
                    reference.sha(source)!=row["sha256"] or reference.sha(target)!=row["sha256"] or
                    metadata(source)!=metadata(target) or metadata(target)!=(row['mode'],row['uid'],row['gid'])):
                raise ValueError("closed input changed before immutable sharing")
            temporary=target.with_name(target.name+".sharing-"+uuid.uuid4().hex)
            try:
                os.link(source,temporary,follow_symlinks=False)
                os.replace(temporary,target)
            finally:temporary.unlink(missing_ok=True)
            if not os.path.samefile(source,target) or reference.sha(target)!=row["sha256"]:raise ValueError("shared immutable input differs")
            directory=os.open(target.parent,os.O_RDONLY|os.O_DIRECTORY)
            try:os.fsync(directory)
            finally:os.close(directory)
            done.append(row)
        for path,digest in results.items():
            if reference.sha(Path(path))!=digest:raise ValueError("closed native result changed")
        for name in workers:
            build_worker.load(name)
        result={"schema":1,"passed":True,"plan_sha256":reference.sha(artifact/"plan.json"),"files_shared":len(done),
                "reclaimed_allocated_bytes":sum(row["allocated_bytes"] for row in done)}
    except Exception as error:
        write(artifact/"result.json",{"schema":1,"passed":False,"error":str(error),"completed":done})
        raise
    write(artifact/"result.json",result)
    print(json.dumps({"evidence":str(artifact),**result}),flush=True)


if __name__=="__main__":main()
