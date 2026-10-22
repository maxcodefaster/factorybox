#!/usr/bin/env python3
"""Serialize final native suites against the delivered worker's exact core source."""
import argparse
import json
from pathlib import Path
import subprocess
import sys
import uuid

import build_worker
import reference
import reference_lifecycle
import upstream_unit

SUITES = ("lifecycle", "guard", "capture", "pause", "base", "base-pause",
          "delegation", "auxiliary", "namespaces", "time", "mountfd", "overlay",
          "admission", "worker-loss", "criu-core")


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--worker-build",required=True)
    parser.add_argument("--suite", action="append", choices=SUITES,
                        help="Run only these final gates, in the declared order; prior results stay separate")
    options=parser.parse_args()
    selected = options.suite or list(SUITES)
    if len(selected) != len(set(selected)):
        parser.error("duplicate suite")
    _,worker=build_worker.load(options.worker_build)
    artifact=reference.new_artifact("factorybox-pilot-native-gates-"+uuid.uuid4().hex);artifact.mkdir()
    (artifact/"runner.py").write_bytes(Path(__file__).read_bytes())
    core=lambda manifest:{name:value for name,value in manifest.items() if not name.endswith("_test.go")}
    def check_core():
        manifest={}
        for directory in ["internal","cmd/factorybox"]:
            manifest.update({directory+"/"+name:value for name,value in upstream_unit.context_manifest(reference.ROOT/directory).items()})
        manifest.update({name:{"sha256":reference.sha(reference.ROOT/name)} for name in ["go.mod","go.sum"]})
        if core(manifest)!=core(worker["source_context"]):raise ValueError("native core differs from delivered worker")
    result={"schema":1,"worker_build":options.worker_build,"worker_image_id":worker["image_id"],"passed":False,"rows":[],"selected_suites":selected,
            "scope":"Final in-process native regression binaries from the delivered image's exact core source, using the same pinned Sysbox/CRIU. Explicit native test-platform build; actual delivered CLI/image proofs remain separate.","production_qualified":False}
    commands=[]
    for suite in selected:
        if suite == "criu-core":
            commands.append((suite,[sys.executable,str(reference.ROOT/"tools/criu_core.py"),"--retained-candidate",worker["criu_build"],
                                   "--native-base-build",worker["native_base_build"],"--candidate-platform","linux/amd64"]))
            continue
        command=[sys.executable,str(reference.ROOT/"tools/lifecycle_native.py"),"--sysbox-build",worker["sysbox_build"],
                 "--native-base-build",worker["native_base_build"],"--candidate-platform","linux/amd64","--suite",suite]
        if suite not in {"lifecycle","guard"}:command += ["--retained-candidate",worker["criu_build"]]
        commands.append((suite,command))
    try:
        for name,command in commands:
            check_core()
            row={"suite":name,"command":command,"passed":False};result["rows"].append(row)
            reference_lifecycle.write_json(artifact/"result.json",result)
            log=artifact/(name+".log")
            print(json.dumps({"native_gates":str(artifact),"starting":name}),flush=True)
            with log.open("x") as stream:
                completed=subprocess.run(command,stdout=stream,stderr=subprocess.STDOUT)
            row["exit_code"]=completed.returncode
            for line in log.read_text().splitlines():
                try:value=json.loads(line)
                except json.JSONDecodeError:continue
                if isinstance(value,dict) and "evidence" in value:row["evidence"]=value["evidence"]
            if row.get("evidence"):
                child=Path(row["evidence"])/"result.json"
                if not child.resolve().is_relative_to(reference.ROOT/"artifacts"):raise ValueError("native evidence escaped artifacts")
                saved=json.loads(child.read_text())
                row.update(result_sha256=reference.sha(child),passed=completed.returncode==0 and saved.get("passed") is True and saved.get("cleanup") is True,
                           passed_tests=saved.get("passed_tests"),passed_subtests=saved.get("passed_subtests"))
            reference_lifecycle.write_json(artifact/"result.json",result)
            print(json.dumps({"suite":name,"passed":row["passed"],"evidence":row.get("evidence")}),flush=True)
            if not row["passed"]:raise RuntimeError("final native gate failed: "+name)
        result["passed"]=True
    except Exception as error:
        result["error"]=str(error)
        raise
    finally:
        reference_lifecycle.write_json(artifact/"result.json",result)
        print(json.dumps({"evidence":str(artifact),"passed":result["passed"]}),flush=True)


if __name__=="__main__":main()
