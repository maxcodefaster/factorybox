#!/usr/bin/env python3
"""Execute the exact optional Compose integration client on an owned worker."""
import argparse
import difflib
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import time
import uuid

import build_worker
import consumer_worker_loss
import consumer_bounded_store
import devx_inputs
import native_base
import reference
import reference_lifecycle
import upstream_bats
import upstream_unit
import worker_readiness


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--worker-build",required=True)
    parser.add_argument("--consumer-inputs",required=True)
    parser.add_argument("--bounded-bytes",type=int,help="Execute the bounded Compose override with an exact 4-32 GiB store")
    args=parser.parse_args()
    if args.bounded_bytes is not None and not 4 << 30 <= args.bounded_bytes <= 32 << 30:
        parser.error("bounded store must be between 4 and 32 GiB")
    worker_path,worker=build_worker.load(args.worker_build)
    inputs,consumer=devx_inputs.load(args.consumer_inputs,rootfs=True,platform="linux/amd64")
    host=json.loads(reference.docker(["info","--format","{{json .}}"])); native_base.require_native_host("linux/amd64",host)
    owner=uuid.uuid4().hex
    name="factorybox-worker-example-"+owner
    artifact=reference.new_artifact(name); artifact.mkdir()
    shutil.copyfile(__file__,artifact/"runner.py")
    shutil.copytree(reference.ROOT/"examples/worker",artifact/"example")
    shutil.copyfile(reference.ROOT/"tests/worker-example-setup.sh",artifact/"setup.sh")
    for directory in ["tests/minimal-actor"]:
        shutil.copytree(reference.ROOT/directory,artifact/"source"/directory)
    for file in ["go.mod","go.sum"]:shutil.copyfile(reference.ROOT/file,artifact/"source"/file)
    env=dict(os.environ,FACTORYBOX_WORKER_IMAGE=worker["image"],FACTORYBOX_STORE_ID=owner,FACTORYBOX_STORE_VOLUME=name)
    compose_env=artifact/"compose.env"
    fd=os.open(compose_env,os.O_WRONLY|os.O_CREAT|os.O_EXCL,0o600)
    with os.fdopen(fd,"w") as stream:
        stream.write(f"FACTORYBOX_WORKER_IMAGE={worker['image']}\nFACTORYBOX_STORE_ID={owner}\nFACTORYBOX_STORE_VOLUME={name}\n")
        if args.bounded_bytes is not None:
            stream.write(f"FACTORYBOX_STORE_BYTES={args.bounded_bytes}\n")
        stream.flush();os.fsync(stream.fileno())
    compose=[*reference.DOCKER,"compose","--env-file",str(compose_env),"--project-name",name,"--file",str(artifact/"example/compose.yaml")]
    if args.bounded_bytes is not None:
        compose += ["--file",str(artifact/"example/compose.bounded.yaml")]
    result={"schema":1,"passed":False,"cleanup":False,"production_qualified":False,"selected":False,
            "owner":owner,"worker_build":args.worker_build,"worker_image_id":worker["image_id"],
            "bounded_bytes":args.bounded_bytes,
            "rootfs_sha256":consumer["rootfs_tar_sha256"],"example_manifest":upstream_unit.context_manifest(artifact/"example"),
            "source_context":upstream_unit.context_manifest(artifact/"source"),"commands":[],
            "scope":"Exact supplied Compose file and workflow.py with an administrator-prepared independent 32-MiB test actor. Consumer first-use/RAM independence qualification remains in the full native consumer gates."}
    before=consumer_worker_loss.sysctls(); result["global_sysctls_before"]=before
    attempted=False; deadline=None

    def command(arguments,*,timeout=60,expect_failure=False):
        limit=timeout if deadline is None else min(timeout,max(.1,deadline-time.monotonic()))
        started=time.monotonic()
        done=subprocess.run(arguments,capture_output=True,text=True,env=env,timeout=limit)
        result["commands"].append({"arguments":arguments,"seconds":time.monotonic()-started,"exit":done.returncode,"stdout":done.stdout,"stderr":done.stderr})
        if bool(done.returncode)!=expect_failure:raise ValueError("owned example command outcome differed: "+" ".join(arguments)+": "+done.stderr[-4096:])
        return done.stdout.strip()

    def exact(identity):
        row=json.loads(reference.docker(["container","inspect",identity]))[0]
        labels=row["Config"].get("Labels",{})
        mounts=[mount for mount in row["Mounts"] if mount["Destination"]=="/var/lib/sysbox"]
        config=row["HostConfig"]
        if (row["Id"]!=identity or row["Image"]!=worker["image_id"] or
                labels.get("com.docker.compose.project")!=name or labels.get("com.docker.compose.service")!="worker" or labels.get("io.factorybox.store-id")!=owner or
                len(mounts)!=1 or mounts[0].get("Name")!=name or not mounts[0]["RW"] or
                config["Memory"]!=3<<30 or config["NanoCpus"]!=3_000_000_000 or config["PidsLimit"]!=512 or
                not config["Privileged"] or config["CgroupnsMode"]!="private" or row["Config"].get("StopTimeout")!=45):
            raise ValueError("exact Compose worker image, ownership, persistent mount or ceilings changed")
        expected=["--store-id",owner]
        if args.bounded_bytes is not None:
            expected += ["--bounded-root",consumer_bounded_store.BOUNDED_ROOT,"--bounded-bytes",str(args.bounded_bytes)]
        if row["Config"]["Cmd"]!=expected:
            raise ValueError("Compose worker command differs from its exact store configuration")
        return row

    def identities():
        text=command(compose+["ps","--all","--quiet","worker"])
        return text.splitlines() if text else []

    try:
        bundle="/var/lib/sysbox/input/bundle"
        if args.bounded_bytes is not None:
            declaration=reference.ROOT/"tests/compatibility/worker-example-bounded-paths.json"
            fixture=json.loads(declaration.read_text())
            setup=artifact/"setup.sh"
            before_bytes=setup.read_bytes()
            if (fixture["schema"]!=1 or fixture["file"]!="tests/worker-example-setup.sh" or
                    fixture["assertions_preserved"] is not True or fixture["upstream_assertions_modified"] is not False or
                    reference.sha(setup)!=fixture["before_sha256"] or
                    before_bytes.count(fixture["old_path"].encode())!=fixture["replacements"]):
                raise ValueError("bounded example preparation differs from the exact path-only declaration")
            (artifact/"setup.before.sh").write_bytes(before_bytes)
            setup.write_bytes(before_bytes.replace(fixture["old_path"].encode(),fixture["new_path"].encode()))
            if reference.sha(setup)!=fixture["after_sha256"]:
                raise ValueError("bounded example preparation after hash differs")
            patch=artifact/"bounded-setup.patch"
            patch.write_text(''.join(difflib.unified_diff(before_bytes.decode().splitlines(True),setup.read_text().splitlines(True),
                                                       fromfile="a/"+fixture["file"],tofile="b/"+fixture["file"])))
            shutil.copyfile(declaration,artifact/declaration.name)
            result["bounded_fixture"]={**fixture,"declaration_sha256":reference.sha(declaration),"patch_sha256":reference.sha(patch)}
            bundle=fixture["new_path"]+"/bundle"
        subprocess.run(["go","build","-trimpath","-buildvcs=false","-o",str(artifact/"minimal-actor"),"./tests/minimal-actor"],
                       cwd=artifact/"source",env=dict(os.environ,CGO_ENABLED="0",GOOS="linux",GOARCH="amd64"),check=True,timeout=120)
        result["actor_sha256"]=reference.sha(artifact/"minimal-actor")
        reference.docker(["volume","create","--label","com.factorybox.run="+owner,"--label","io.factorybox.store-id="+owner,name]); attempted=True
        deadline=time.monotonic()+420
        command(compose+["up","--detach","--pull","never","worker"])
        found=identities()
        if len(found)!=1:raise ValueError("example requires one exact owned Compose worker")
        identity=found[0]; exact(identity)
        for _ in range(150):
            if not exact(identity)["State"]["Running"]:raise ValueError("Compose worker exited before readiness")
            if worker_readiness.published(reference.DOCKER,identity,owner):break
            time.sleep(.1)
        else:raise TimeoutError("Compose readiness publication timeout")
        result["initial_status"]=json.loads(command([*reference.DOCKER,"exec",identity,"/opt/factorybox/factorybox","worker-status"]))
        if args.bounded_bytes is not None:
            capacity=json.loads(command([*reference.DOCKER,"exec",identity,"/opt/factorybox/factorybox",
                                         "--worker-run-root","/run/factorybox","capacity"]))
            result["bounded_capacity"]=consumer_bounded_store.verify_capacity(result["initial_status"],capacity,args.bounded_bytes,owner)
        preparation=time.monotonic()
        for source,target in [(inputs/"runtime-rootfs.tar","/var/lib/sysbox/native-rootfs.tar"),(artifact/"minimal-actor","/var/lib/sysbox/native-minimal-actor"),(artifact/"setup.sh","/var/lib/sysbox/native-setup.sh")]:
            command([*reference.DOCKER,"cp",str(source),identity+":"+target],timeout=90)
        command([*reference.DOCKER,"exec","--env","FACTORYBOX_WORKER_EXAMPLE_NATIVE=1",identity,"/bin/bash","/var/lib/sysbox/native-setup.sh",consumer["rootfs_tar_sha256"],result["actor_sha256"]],timeout=180)
        result["administrator_preparation_seconds"]=time.monotonic()-preparation
        helper=(artifact/"example/bundle_mounts.py").read_text()
        mounts_command=[*reference.DOCKER,"exec",identity,"python3","-c",helper,"inspect",bundle]
        original_mounts=json.loads(command(mounts_command))
        changed=json.loads(json.dumps(original_mounts))
        changed["mounts"][bundle+"/lower"]["inode"]+=1
        command([*reference.DOCKER,"exec",identity,"python3","-c",helper,"release",bundle,json.dumps(changed)],expect_failure=True)
        if ("input mount ownership changed" not in result["commands"][-1]["stderr"] or
                json.loads(command(mounts_command))!=original_mounts):
            raise ValueError("changed input identity did not refuse without changing the original mounts")
        result["changed_input_identity_refused_unchanged"]=True
        client=[sys.executable,str(artifact/"example/workflow.py"),"--project",name,"--bundle",bundle,"--context",os.getenv("FACTORYBOX_DOCKER_CONTEXT","default"),"--output",str(artifact/"responses")]
        if args.bounded_bytes is not None:client += ["--bounded-bytes",str(args.bounded_bytes)]
        if reference.DOCKER[0]=="sudo":client.append("--sudo")
        command(client,timeout=420)
        journal=json.loads((artifact/"responses/workflow.json").read_text())
        if not journal.get("passed") or journal["old_worker"]["container_id"]==journal["new_worker"]["container_id"]:raise ValueError("example did not complete whole worker replacement")
        found=identities()
        if found!=[journal["new_worker"]["container_id"]]:raise ValueError("example left another owned worker")
        exact(found[0])
        command(compose+["stop","worker"],timeout=60)
        result["final_stopped_state"]=exact(found[0])["State"]
        if (result["final_stopped_state"]["Running"] or result["final_stopped_state"]["ExitCode"]!=0 or
                result["final_stopped_state"]["OOMKilled"]):
            raise ValueError("replacement worker did not complete a clean final drain")
        if upstream_unit.context_manifest(artifact/"example")!=result["example_manifest"] or upstream_unit.context_manifest(artifact/"source")!=result["source_context"]:raise ValueError("frozen example or actor source changed")
        result["workflow_sha256"]=reference.sha(artifact/"responses/workflow.json")
        result["passed"]=True
    except Exception as error:
        result["error"]=str(error)
        raise
    finally:
        deadline=None  # Cleanup and evidence retention have their own bounds.
        preserve=False
        try:
            if attempted:
                for identity in identities():
                    exact(identity)
                    # Explicit exact-ID cleanup after project, store, image and
                    # mount checks; no project-wide down or volume pruning.
                    reference.docker(["container","rm","--force",identity])
                if not result["passed"]:
                    try:upstream_bats.archive_stopped_volumes(artifact,name,owner,[name],worker["image"],result,timeout=600,compressed=True)
                    except Exception as error:preserve=True;result["archive_error"]=str(error)
                if not preserve:reference.owned_delete("volume",name,owner)
            result["cleanup"]=not preserve
            result["global_sysctls_after"]=consumer_worker_loss.sysctls()
            result["passed"]=result["passed"] and result["cleanup"] and result["global_sysctls_after"]==before
        finally:
            reference_lifecycle.write_json(artifact/"result.json",result)
            print(json.dumps({"evidence":str(artifact),"passed":result["passed"],"cleanup":result["cleanup"],"error":result.get("error")}),flush=True)
    if not result["passed"]:raise RuntimeError("native Compose example failed: "+str(artifact))


if __name__=="__main__":main()
