#!/usr/bin/env python3
"""Prove one pinned OpenCode agent's actual local tool and result ingestion."""
import argparse
import json
import shutil
import subprocess
import uuid

import devx_inputs
import reference
import reference_lifecycle
import upstream_unit


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--consumer-inputs", required=True)
    options = parser.parse_args()
    _, consumers = devx_inputs.load(options.consumer_inputs, rootfs=True, platform="linux/amd64")
    image = consumers["images"]["build-runtime-host"]
    owner = uuid.uuid4().hex
    name = "factorybox-opencode-tool-"+owner
    artifact = reference.new_artifact(name); artifact.mkdir()
    shutil.copytree(reference.ROOT/"tests/opencode-flow", artifact/"input")
    shutil.copyfile(__file__, artifact/"runner.py")
    result = {"schema":1,"owner":owner,"passed":False,"cleanup":False,"production_qualified":False,
              "lifecycle_qualified":False,"consumer_inputs":options.consumer_inputs,"image":image,
              "inputs":upstream_unit.context_manifest(artifact/"input"),
              "scope":"Native pinned OpenCode 2 API, real foreground shell tool, local deterministic model and result/exit ingestion only. No inference or checkpoint/restore."}
    attempted = False
    try:
        reference.docker(["volume","create","--label","com.factorybox.run="+owner,name]); attempted=True
        command = [*reference.DOCKER,"run","--pull=never","--name",name,"--label","com.factorybox.run="+owner,
                   "--cpus=3","--memory=3g","--pids-limit=128","--network=none","--read-only",
                   "--cap-drop=ALL","--security-opt=no-new-privileges","--tmpfs","/run:rw,nosuid,nodev,size=4m",
                   "--tmpfs","/tmp:rw,nosuid,nodev,size=64m","--tmpfs","/var/lib/docker:rw,nosuid,nodev,size=16m",
                   "--mount",f"type=volume,source={name},target=/experiment",
                   "--mount",f"type=bind,source={artifact/'input'},target=/opt/factorybox/opencode-flow,readonly",
                   "--entrypoint","/usr/local/bin/node",image["image"],"/opt/factorybox/opencode-flow/diagnostic.cjs"]
        result["command"] = command
        reference_lifecycle.write_json(artifact/"intent.json",result)
        print(json.dumps({"running":str(artifact)}),flush=True)
        with (artifact/"stdout.txt").open("x") as out,(artifact/"stderr.txt").open("x") as err:
            done = subprocess.run(command,stdout=out,stderr=err,timeout=80)
        result["exit_code"] = done.returncode
        copied = subprocess.run([*reference.DOCKER,"cp",name+":/experiment/.",str(artifact/"output")],capture_output=True,text=True,timeout=10)
        (artifact/"copy.stderr").write_text(copied.stderr)
        result["state"] = json.loads(reference.docker(["container","inspect",name]))[0]["State"]
        result["inputs_unchanged"] = result["inputs"] == upstream_unit.context_manifest(artifact/"input")
        result["passed"] = done.returncode == 0 and not result["state"]["OOMKilled"] and result["inputs_unchanged"]
        if not result["passed"]: raise ValueError("actual pinned OpenCode tool/ingestion probe failed")
    except Exception as error:
        result["error"] = str(error); raise
    finally:
        if attempted:
            reference.owned_delete("container",name,owner)
            reference.owned_delete("volume",name,owner)
        result["cleanup"] = True
        reference_lifecycle.write_json(artifact/"result.json",result)
        print(json.dumps({"evidence":str(artifact),"passed":result["passed"],"cleanup":result["cleanup"]}),flush=True)


if __name__ == "__main__": main()
