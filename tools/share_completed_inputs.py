#!/usr/bin/env python3
"""Share byte-identical immutable inputs of closed, passing native proofs."""
import argparse
import json
import os
from pathlib import Path
import re
import shutil
import uuid

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
    argparse.ArgumentParser(description=__doc__).parse_args()
    # This operates only on outputs already proven closed and byte-pinned.
    # It never opens a source/build, failure archive or active native input.
    reference.artifact_storage.require_capacity(reference.ROOT,run_reserve=0)
    artifact=reference.ROOT/"artifacts"/("factorybox-storage-share-inputs-"+uuid.uuid4().hex);artifact.mkdir()
    shutil.copyfile(__file__,artifact/"runner.py")
    groups={};actions=[];results={}
    for run in sorted((reference.ROOT/"artifacts").iterdir()):
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
            path=run/role
            if path.is_symlink() or not path.is_file():continue
            if path.resolve() != path.absolute():raise ValueError("compiled input contains a symlink")
            info=path.stat()
            if info.st_uid != os.getuid():continue
            if not expected or reference.sha(path)!=expected:raise ValueError("closed compiled input differs from its pinned hash: "+str(path))
            key=(role,expected,info.st_size,info.st_mode,info.st_uid,info.st_gid)
            if key not in groups:groups[key]=path;continue
            original=groups[key]
            if os.path.samefile(original,path):continue
            if info.st_nlink!=1:continue  # Do not count still-linked old inodes as reclaimed.
            actions.append({"file":str(path),"canonical":str(original),"sha256":expected,"bytes":info.st_size,
                            "before_identity":list(reference.artifact_storage.identity(path)),"allocated_bytes":info.st_blocks*512})
    plan={"schema":1,"operation":"share_identical_closed_native_inputs","result_sha256":results,"actions":actions,
          "scope":"Only exact compiled input roles of complete passing consumer proofs; source, results, logs and file contents are unchanged. No archived input is rewritten in place."}
    write(artifact/"plan.json",plan)
    done=[]
    try:
        for row in actions:
            target,source=Path(row["file"]),Path(row["canonical"])
            if (list(reference.artifact_storage.identity(target))!=row["before_identity"] or
                    reference.sha(source)!=row["sha256"] or reference.sha(target)!=row["sha256"]):
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
        result={"schema":1,"passed":True,"plan_sha256":reference.sha(artifact/"plan.json"),"files_shared":len(done),
                "reclaimed_allocated_bytes":sum(row["allocated_bytes"] for row in done)}
    except Exception as error:
        write(artifact/"result.json",{"schema":1,"passed":False,"error":str(error),"completed":done})
        raise
    write(artifact/"result.json",result)
    print(json.dumps({"evidence":str(artifact),**result}),flush=True)


if __name__=="__main__":main()
