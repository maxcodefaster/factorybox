#!/usr/bin/env python3
"""Run one complete same-host worker-loss case with an unchanged real consumer."""
import argparse
import ast
import json
import os
from pathlib import Path
import shutil
import subprocess
import time
import uuid

import build_worker
import devx_inputs
import devx_project_images
import devx_lifecycle
import criu_candidate
import native_base
import external_lifecycle
import provider_peer
import reference
import reference_lifecycle
import theia_admission
import theia_peer
import upstream_bats
import upstream_unit
import worker_readiness
import verify_fixture_extensions


def sysctls():
    tree = ast.parse((reference.ROOT / "tests/global-sysctl-observation.py").read_text())
    names = next(ast.literal_eval(node.value) for node in tree.body if isinstance(node, ast.Assign) and
                 any(isinstance(target, ast.Name) and target.id == "NAMES" for target in node.targets))
    return {name: (Path("/proc/sys") / name).read_text().strip() for name in names}


def exact_worker(name, owner, identity):
    saved = json.loads(reference.docker(["container", "inspect", identity]))[0]
    if saved["Id"] != identity or saved["Name"] != "/"+name or saved["Config"].get("Labels", {}).get("com.factorybox.run") != owner:
        raise ValueError("consumer worker identity changed")
    return saved


def ready(name, owner, identity, deadline):
    started = time.monotonic()
    while time.monotonic() < min(started+65, deadline):
        if not exact_worker(name, owner, identity)["State"]["Running"]:
            raise ValueError("delivered consumer worker exited during startup")
        if not worker_readiness.published(reference.DOCKER, identity, owner):
            time.sleep(.1)
            continue
        done = subprocess.run([*reference.DOCKER,"exec",identity,"/opt/factorybox/factorybox","worker-status"], capture_output=True,text=True,timeout=5)
        if done.returncode == 0:
            status = json.loads(done.stdout)
            if status["schema"] != 1 or status["store_id"] != owner:
                raise ValueError("consumer worker has another store identity")
            return {"seconds": time.monotonic()-started, "status": status}
        time.sleep(.1)
    raise TimeoutError("consumer worker readiness deadline exceeded")


def pump_peer(peer, output):
    request = output / "theia-request.json"
    if peer and not peer.started and request.exists():
        peer.start(json.loads(request.read_text()))
    if peer and not peer.closed and (output / "theia-operations-complete").exists():
        peer.close(False)
        (output / "theia-released").write_text("ok\n" if peer.result.get("passed") and peer.result.get("cleanup") else "failed\n")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--worker-build",required=True)
    parser.add_argument("--consumer-inputs",required=True)
    parser.add_argument("--project-images",required=True)
    parser.add_argument("--example",choices=["minimal","factory"],required=True)
    parser.add_argument("--loss",choices=["planned","abrupt"],required=True)
    parser.add_argument("--storage-driver",choices=["vfs","overlay2"],default="vfs")
    parser.add_argument("--theia-image")
    parser.add_argument("--opencode-flow",action="store_true",help="Preserve the real agent's running local tool and consume its actual result")
    parser.add_argument("--external-provider-proof",help="Use the actual admitted free model and helper from this exact native gateway proof")
    parser.add_argument("--external-disconnect",action="store_true",help="Drop the second completed provider response once; require exact client replay and no repeated shell")
    parser.add_argument("--consumer-empty-features",action="store_true",help="Candidate exact consumer patch: omit the feature-control lookup for zero features only")
    parser.add_argument("--consumer-request-profile",action="store_true",help="Optional copied Factory example telemetry around its first dependency fetches")
    parser.add_argument("--probe",choices=["worker-loss","incomplete-pause","attached-exec","topology","lifecycle","warm-preparation"],default="worker-loss")
    parser.add_argument("--baseline-runtime-build",help="Exact original selected packaged CLI, for a matched lifecycle control only")
    parser.add_argument("--baseline-criu-build",help="Exact original selected CRIU paired with the baseline package")
    options = parser.parse_args()
    if options.external_disconnect and (not options.external_provider_proof or options.probe != "lifecycle"):
        parser.error("the completed-response disconnect is a separate complete external-agent lifecycle")
    if options.external_provider_proof and (options.example != "factory" or not options.opencode_flow or
            options.probe not in {"lifecycle", "worker-loss"} or options.baseline_runtime_build or options.consumer_request_profile):
        parser.error("external inference requires the complete Factory agent lifecycle or committed-pause worker-loss case")
    if options.example == "minimal" and options.theia_image:
        parser.error("the unchanged minimal consumer has no IDE")
    if options.consumer_empty_features and (options.probe != "lifecycle" or options.baseline_runtime_build):
        parser.error("the consumer optimization requires the full selected-runtime lifecycle without a baseline substitution")
    if options.consumer_request_profile and (options.example != "factory" or options.probe != "lifecycle" or not options.consumer_empty_features):
        parser.error("request profiling is a separate Factory lifecycle observation of the empty-features candidate")
    if options.example == "minimal" and options.opencode_flow:
        parser.error("the unchanged minimal consumer has no agent")
    if options.probe == "attached-exec" and (options.example != "minimal" or options.loss != "planned" or options.theia_image or options.opencode_flow):
        parser.error("attached exec is a separate minimal one-worker boundary probe")
    if options.probe == "topology" and (options.loss != "planned" or options.theia_image or options.opencode_flow):
        parser.error("topology is a one-worker consumer diagnostic without optional peers")
    if options.probe == "lifecycle" and options.loss != "planned":
        parser.error("ordinary lifecycle does not inject whole-worker loss")
    if options.probe == "incomplete-pause" and options.loss != "abrupt":
        parser.error("the actual incomplete pause boundary requires whole-worker SIGKILL")
    if options.probe == "warm-preparation" and (options.loss != "planned" or options.opencode_flow):
        parser.error("warm preparation is two fresh consumer starts, without worker loss or an inherited agent tool")
    if bool(options.baseline_runtime_build)!=bool(options.baseline_criu_build):
        parser.error("a matched baseline requires both original packaged CLI and CRIU")
    if options.baseline_runtime_build and (options.probe!="lifecycle" or options.storage_driver!="vfs" or options.opencode_flow):
        parser.error("the original selected baseline is a VFS lifecycle control without the new agent-tool case")
    worker_path, worker = build_worker.load(options.worker_build)
    consumers_path, consumers = devx_inputs.load(options.consumer_inputs,rootfs=True,platform="linux/amd64")
    projects_path, projects = devx_project_images.load(options.project_images,platform="linux/amd64",example=options.example)
    host = json.loads(reference.docker(["info","--format","{{json .}}" ]))
    native_base.require_native_host("linux/amd64",host)
    baseline_binary,baseline_criu,baseline_receipt=None,None,None
    effective_criu=json.loads((worker_path/"image-context/bundle/components.json").read_text())["binary_sha256"]["criu"]
    if options.baseline_runtime_build:
        baseline_path,baseline_criu_result,_,_=criu_candidate.load(options.baseline_criu_build)
        baseline_criu=baseline_path/"criu"
        component_digests=json.loads((worker_path/"image-context/bundle/components.json").read_text())["binary_sha256"]
        effective_criu=baseline_criu_result["binary_sha256"]
        baseline_binary,baseline_receipt=devx_lifecycle.runtime_input(options.baseline_runtime_build,host,{**component_digests,"criu":effective_criu},False)
        baseline_receipt["criu_build"]=options.baseline_criu_build
        baseline_receipt["criu_sha256"]=effective_criu
    ide = devx_inputs.inspect_image(options.theia_image,platform="linux/amd64") if options.theia_image else None
    admission = theia_admission.qualify(ide) if ide else None
    owner = uuid.uuid4().hex
    name = "factorybox-consumer-worker-loss-"+owner
    artifact = reference.new_artifact(name); artifact.mkdir()
    result = {"schema":1,"owner":owner,"passed":False,"cleanup":False,"production_qualified":False,
              "selected":False,"worker_build":options.worker_build,"worker_image":worker["image"],
              "worker_image_id":worker["image_id"],"example":options.example,"loss":options.loss,
              "storage_driver":options.storage_driver,"driver_selected":False,
              "opencode_flow":options.opencode_flow,
              "external_provider_enabled":bool(options.external_provider_proof),
              "consumer_empty_features":options.consumer_empty_features,
              "consumer_request_profile":options.consumer_request_profile,
              "probe":options.probe,
              "baseline_input":baseline_receipt,"lifecycle_criu_sha256":effective_criu,
              "consumer_inputs":options.consumer_inputs,"project_images":options.project_images,
              "consumer_rootfs_sha256":consumers["rootfs_tar_sha256"],"project_images_sha256":projects["archive_sha256"],
              "phases":[],"theia_admission":admission,"scope":"One bounded real consumer proof using the delivered candidate worker. No performance-distribution or production qualification."}
    if options.probe == "attached-exec":
        result["scope"] = "Dedicated negative admission regression for a real attached cross-PID-namespace Dev Container exec. This does not exercise worker replacement."
    if options.probe == "topology":
        result["scope"] = "Owned process/session/stdio topology after useful consumer work, without checkpoint/restore or worker replacement."
    if options.probe == "lifecycle":
        result["scope"] = "Complete unchanged consumer lifecycle in the delivered Go supervisor image, with exact packaged CLI/CRIU inputs and original RAM/filesystem/IDE assertions. One observation; no latency distribution."
    if options.probe == "incomplete-pause":
        result["scope"] = "Whole-worker SIGKILL at an actual blocked filesystem-copy permission event during an unconfirmed second pause; reject incomplete recovery and recover the earlier complete independent pause."
    if options.probe == "warm-preparation":
        result["scope"] = "Compose the existing immutable-base API for two independent fresh starts; charge initial import, incremental preparation and first useful work separately. No new base-cache algorithm."
    shutil.copyfile(__file__,artifact/"runner.py")
    shutil.copyfile(reference.ROOT/"tools/worker_readiness.py", artifact/"worker_readiness.py")
    fixture_declaration=reference.ROOT/"tests/production-fixture-extensions.json"
    if fixture_declaration.exists():
        result["fixture_verification"]=verify_fixture_extensions.verify(reference.ROOT)
        shutil.copyfile(fixture_declaration,artifact/"production-fixture-extensions.json")
        for row in json.loads(fixture_declaration.read_text())["files"].values():
            shutil.copyfile(reference.ROOT/row["patch"],artifact/Path(row["patch"]).name)
    shutil.copytree(reference.ROOT/"examples"/options.example,artifact/"example")
    if options.consumer_request_profile:
        path=artifact/"example/server.mjs"
        text=path.read_text()
        anchor="    const dependency = await fetch('http://dependency:8081/health', {\n      signal: AbortSignal.timeout(2000),\n    });"
        if text.count(anchor)!=1: raise ValueError("first-request profile anchor changed")
        before_sha=reference.sha(path)
        profile="""    const dependencyStarted = performance.now();
    let dependency;
    try {
      dependency = await fetch('http://dependency:8081/health', {signal: AbortSignal.timeout(2000)});
      traceDependency({event: 'response', milliseconds: performance.now()-dependencyStarted, status: dependency.status});
    } catch(error) {
      traceDependency({event: 'failure', milliseconds: performance.now()-dependencyStarted, error: String(error)});
      throw error;
    }"""
        tracing="""import { appendFileSync } from 'node:fs';
let traceCount = 0;
function traceDependency(event) {
  if (traceCount++ < 64) appendFileSync('/workspace/.factorybox-request-profile.jsonl', JSON.stringify({at: new Date().toISOString(), ...event})+'\\n');
}
traceDependency({event: 'module-loaded'});
"""
        path.write_text(tracing+text.replace(anchor,profile))
        result["request_profile_input"]={"file":"examples/factory/server.mjs","before_sha256":before_sha,"after_sha256":reference.sha(path),
                                          "scope":"Copied example only, bounded fetch timing without changing URL, 2-second timeout, success or error behavior. Not an optimization measurement."}
    result["example_manifest"] = upstream_unit.context_manifest(artifact/"example")
    for file in ["consumer-worker-setup.sh","global-sysctl-observation.py","devx-sandbox.sh","devx-devcontainer.sh","minimal-sandbox.sh"]:
        shutil.copyfile(reference.ROOT/"tests"/file,artifact/file)
    if options.consumer_empty_features:
        patch_input = reference.ROOT/"tests/consumer-optimizations/0001-devcontainer-empty-features.json"
        shutil.copyfile(patch_input,artifact/"consumer-patch.json")
        shutil.copyfile(reference.ROOT/"tools/devcontainer_empty_features.py",artifact/"consumer-patch.py")
        path=artifact/"consumer-worker-setup.sh"
        text=path.read_text()
        anchor='  chmod 0755 "$root/devx-actor" "$root/devx-registry" "$root/bin/devcontainer"'
        if text.count(anchor)!=1: raise ValueError("consumer injection anchor changed")
        before_sha=reference.sha(path)
        path.write_text(text.replace(anchor,anchor+'\n  python3 /consumer-patch.py --root /var/lib/sysbox/devx/bundle/lower --patch /consumer-patch.json --receipt /output/consumer-optimization.json'))
        result["consumer_optimization"]={"patch":json.loads(patch_input.read_text()),"patch_sha256":reference.sha(patch_input),
                                         "setup_before_sha256":before_sha,"setup_after_sha256":reference.sha(path),"selected":False}
    if options.probe == "attached-exec":
        path=artifact/"minimal-sandbox.sh"
        text=path.read_text()
        supported="/bin/sh -ec 'setsid /workspace/.minimal-actor </dev/null >/workspace/.minimal-actor.log 2>&1 &'"
        if text.count(supported)!=1: raise ValueError("supported service launch no longer matches its exact boundary input")
        result["exec_input_before_sha256"] = reference.sha(path)
        path.write_text(text.replace(supported,"/workspace/.minimal-actor"))
        result["exec_input_after_sha256"] = reference.sha(path)
    if options.opencode_flow:
        shutil.copytree(reference.ROOT/"tests/opencode-flow",artifact/"opencode-flow")
        shutil.copyfile(reference.ROOT/"tests/opencode-probe.cjs",artifact/"opencode-probe.cjs")
        path=artifact/"devx-sandbox.sh"
        text=path.read_text()
        anchor='if test "${FACTORYBOX_DEVX_OPENCODE:-}" = 1; then'
        if text.count(anchor)!=1: raise ValueError("OpenCode service admission anchor changed")
        path.write_text(text.replace(anchor,
            'export FACTORY_OPENCODE_CONFIG="$(cat /opt/factorybox/opencode-flow/config.json)"\n'
            + ('' if options.external_provider_proof else '/usr/local/bin/node /opt/factorybox/opencode-flow/model.cjs > /var/log/factorybox-model.log 2>&1 &\n') + anchor))
    if options.storage_driver == "overlay2":
        file = artifact/("minimal-sandbox.sh" if options.example == "minimal" else "devx-sandbox.sh")
        text = file.read_text()
        if text.count("--storage-driver=vfs") != 1:
            raise ValueError("consumer no longer has the exact one-variable Docker-driver input")
        result["driver_input_before_sha256"] = reference.sha(file)
        file.write_text(text.replace("--storage-driver=vfs","--storage-driver=overlay2"))
        result["driver_input_after_sha256"] = reference.sha(file)
    reference_lifecycle.write_json(artifact/"project-images.json",[{"image":row["image"],"canonical_image":row["canonical_image"]} for row in projects["images"]])
    source = artifact/"source"
    for directory in ["internal","cmd/factorybox","tests/devx-lifecycle","tests/devx-actor","tests/devx-registry","tests/minimal-actor","tests/ide-filesystem","tests/provider-fixture"]:
        shutil.copytree(reference.ROOT/directory,source/directory)
    for file in ["go.mod","go.sum"]: shutil.copyfile(reference.ROOT/file,source/file)
    if options.consumer_request_profile:
        path=source/"tests/devx-actor/main_linux.go"
        text=path.read_text()
        before_sha=reference.sha(path)
        changes={
            '\t\tauthority.mu.Lock()\n\t\tif event.Type == "hello" {':
            '\t\tlog.Printf("profile-control role=%s type=%s phase=%s stream=%s", role, event.Type, event.Phase, event.StreamID)\n\t\tauthority.mu.Lock()\n\t\tif event.Type == "hello" {',
            '\tauthority.write.Lock()\n\terr = websocket.JSON.Send(child, message{':
            '\tlog.Printf("profile-app-send stream=%s", stream)\n\tauthority.write.Lock()\n\terr = websocket.JSON.Send(child, message{',
            '\tapp, err := authority.proxy(ctx, request)\n\tif err != nil {':
            '\tapp, err := authority.proxy(ctx, request)\n\tlog.Printf("profile-app-result error=%v", err)\n\tif err != nil {',
            'func main() {\n\tif os.Getenv':
            'func main() {\n\tlog.SetFlags(log.LstdFlags | log.Lmicroseconds)\n\tif os.Getenv',
        }
        for before_text,after_text in changes.items():
            if text.count(before_text)!=1: raise ValueError("actor timing anchor changed")
            text=text.replace(before_text,after_text)
        path.write_text(text)
        result["request_profile_actor"]={"file":"tests/devx-actor/main_linux.go","before_sha256":before_sha,"after_sha256":reference.sha(path),
                                          "assertions_preserved":True,"scope":"Copied fixture timestamps only: authenticated control event kinds, first app send and result. No protocol or readiness changes."}
    result["source_context"] = upstream_unit.context_manifest(source)
    core = lambda data: {key:value for key,value in data.items() if (key.startswith("internal/") or key.startswith("cmd/") or key in {"go.mod","go.sum"}) and not key.endswith("_test.go")}
    if core(result["source_context"]) != core(worker["source_context"]):
        raise ValueError("consumer proof core inputs differ from delivered worker")
    before = sysctls()
    result["global_sysctls_before"] = before
    containers, peer, attempted = [], None, False
    service, provider, providers = None, None, []
    print(json.dumps({"running":str(artifact),"example":options.example,"loss":options.loss}),flush=True)
    try:
        if options.external_provider_proof:
            external_lifecycle.prepare(artifact, options.external_provider_proof, worker["image_id"], result)
            service = external_lifecycle.Service(artifact, result, disconnect=options.external_disconnect)
        if options.opencode_flow:
            result["opencode_flow_inputs"] = upstream_unit.context_manifest(artifact/"opencode-flow")
        env = dict(os.environ,GOOS="linux",GOARCH="amd64",CGO_ENABLED="0")
        for file,package,test in [("consumer-worker.test","./tests/devx-lifecycle",True),("devx-actor","./tests/devx-actor",False),
                                  ("devx-registry","./tests/devx-registry",False),("minimal-actor","./tests/minimal-actor",False),
                                  ("ide-filesystem","./tests/ide-filesystem",False)]:
            command = ["go","test","-c","-tags=factorybox_worker_probe"] if test else ["go","build"]
            subprocess.run([*command,"-trimpath","-buildvcs=false","-o",str(artifact/file),package],cwd=source,env=env,check=True,timeout=120)
        binary = baseline_binary or worker_path/"image-context/bundle/factorybox"
        cli_sha=baseline_receipt["binary_sha256"] if baseline_receipt else worker["bundle_sha256"]["factorybox"]
        reference.artifact_storage.share_frozen_input(binary,artifact/"factorybox",reference.ROOT,cli_sha)
        result["lifecycle_cli_sha256"]=cli_sha
        if baseline_criu:
            reference.artifact_storage.share_frozen_input(baseline_criu,artifact/"baseline-criu",reference.ROOT,effective_criu)
        components = worker_path/"image-context/bundle/components"
        if ide:
            shutil.copyfile(reference.ROOT/"tests/reference/fixture/theia-client.cjs",artifact/"theia-client.cjs")
        result["inputs_sha256"] = {p.name:reference.sha(p) for p in artifact.iterdir() if p.is_file()}
        reference.docker(["volume","create","--label","com.factorybox.run="+owner,name]); attempted=True
        deadline = time.monotonic()+420
        for phase in (["prepare","recover"] if options.probe in {"worker-loss","incomplete-pause"} else ["prepare"]):
            container = name+"-"+phase
            output = artifact/(phase+"-output"); output.mkdir()
            phase_artifact = artifact/phase; phase_artifact.mkdir()
            if ide: shutil.copyfile(artifact/"theia-client.cjs",phase_artifact/"theia-client.cjs")
            peer = theia_peer.Peer(container,owner,ide,phase_artifact,output,worker["image"]) if ide else None
            record = {"phase":phase,"passed":False}; result["phases"].append(record)
            command = ["run","--detach","--pull=never","--name",container,"--label","com.factorybox.run="+owner,
                       "--privileged","--cgroupns=private","--cpus=3","--memory=3g","--pids-limit=512","--network=none",
                       "--env","GOMAXPROCS=2","--env","GOMEMLIMIT=256MiB","--env","FACTORYBOX_DEVX_LIFECYCLE=1",
                       "--env","FACTORYBOX_DEVX_WORKER_LOSS="+("0" if options.probe in {"lifecycle","warm-preparation"} else "1"),"--env","FACTORYBOX_DEVX_RUN="+owner,
                       "--env","FACTORYBOX_CONSUMER_WORKER_PHASE="+phase,
                       "--env","FACTORYBOX_CONSUMER_WORKER_ABRUPT="+("1" if options.loss=="abrupt" else "0"),
                       "--env","FACTORYBOX_RUNTIME_SHA256="+json.loads((worker_path/"image-context/bundle/components.json").read_text())["binary_sha256"]["sysbox-runc"],
                       "--env","FACTORYBOX_CRIU_SHA256="+effective_criu,
                       "--mount",f"type=volume,source={name},target=/var/lib/sysbox"]
            if options.example == "minimal": command += ["--env","FACTORYBOX_DEVX_MINIMAL=1"]
            if options.probe == "attached-exec": command += ["--env","FACTORYBOX_DEVX_EXEC_SESSION=1"]
            if options.probe == "topology": command += ["--env","FACTORYBOX_DEVX_SESSION_TOPOLOGY=1"]
            if options.probe in {"lifecycle","warm-preparation"}: command += ["--env","FACTORYBOX_DEVX_WORKER_LIFECYCLE=1"]
            if options.probe == "warm-preparation": command += ["--env","FACTORYBOX_DEVX_WARM_PREPARATION=1"]
            if options.probe == "incomplete-pause": command += ["--env","FACTORYBOX_DEVX_INCOMPLETE_PAUSE=1"]
            if baseline_receipt: command += ["--env","FACTORYBOX_DEVX_BASELINE_RUNTIME=1"]
            if options.opencode_flow: command += ["--env","FACTORYBOX_DEVX_OPENCODE=1","--env","FACTORYBOX_DEVX_OPENCODE_FLOW=1"]
            if service: command += ["--env","FACTORYBOX_DEVX_EXTERNAL_PROVIDER=1"]
            if peer: command += ["--env","FACTORYBOX_DEVX_THEIA=1"]
            mounts = [(output,"/output",False),(components,"/component-input",True),(artifact/"example","/example",True),
                      (consumers_path/"runtime-rootfs.tar","/runtime-rootfs.tar",True),(projects_path/"images.tar","/project-images.tar",True),
                      (artifact/"global-sysctl-observation.py","/observe-sysctls.py",True)]
            mounts += [(artifact/file,"/"+file,True) for file in ["factorybox","consumer-worker-setup.sh","consumer-worker.test","devx-actor","devx-registry",
                                                                     "minimal-actor","minimal-sandbox.sh","devx-sandbox.sh","devx-devcontainer.sh","project-images.json","ide-filesystem"]]
            if options.opencode_flow:
                mounts += [(artifact/"opencode-flow","/opencode-flow",True),(artifact/"opencode-probe.cjs","/opencode-probe.cjs",True)]
            if service:
                mounts += [(Path(service.directory.name),"/external-provider",True),(artifact/"provider-gateway","/provider-gateway",True)]
            if baseline_receipt: mounts += [(artifact/"baseline-criu","/baseline-criu",True)]
            if options.consumer_empty_features:
                mounts += [(artifact/file,"/"+file,True) for file in ["consumer-patch.py","consumer-patch.json"]]
            for source_path,target,readonly in mounts:
                command += ["--mount",f"type=bind,source={source_path},target={target}"+(",readonly" if readonly else "")]
            command += ["--entrypoint","/opt/factorybox/factorybox",worker["image"],"worker","--store-id",owner]
            record["command"] = command
            containers.append(container)
            identity = reference.docker(command)
            record["startup"] = ready(container,owner,identity,deadline)
            provider = provider_peer.Peer(container,identity,owner,phase_artifact,output,result["external_provider"]["gateway_sha256"]) if service else None
            if provider: providers.append(provider)
            probe_command = [*reference.DOCKER,"exec",identity,"/bin/bash","/consumer-worker-setup.sh"]
            probe_start = time.monotonic()
            with (phase_artifact/"stdout.txt").open("x") as stdout,(phase_artifact/"stderr.txt").open("x") as stderr:
                probe = subprocess.Popen(probe_command,stdout=stdout,stderr=stderr)
                try:
                    while probe.poll() is None:
                        pump_peer(peer,output)
                        if provider: provider.pump()
                        if options.consumer_request_profile and (output/"devx-observations.json").exists() and not (phase_artifact/"dependency-profile.jsonl").exists():
                            exact_worker(container,owner,identity)
                            # Native output is intentionally mode 0600 until
                            # setup's final chown. Read it inside the exact
                            # owned worker, not through the host user's mount.
                            program=("import pathlib,json; v=json.loads(pathlib.Path('/output/cli-02-start.json').read_text()); "
                                     "pid=v['runtime']['pid']; assert type(pid) is int and pid>1 and v['record']['id']=='source'; "
                                     "p=pathlib.Path('/proc/%d/root/workspace/project/.factorybox-request-profile.jsonl'%pid); "
                                     "d=p.read_bytes(); assert len(d)<=65536; print(d.decode(),end='')")
                            copied=subprocess.run([*reference.DOCKER,"exec",identity,"python3","-c",program],capture_output=True,text=True,timeout=5)
                            copied.check_returncode()
                            (phase_artifact/"dependency-profile.jsonl").write_text(copied.stdout)
                            record["dependency_profile"]=[json.loads(line) for line in copied.stdout.splitlines()]
                        marker = "FACTORYBOX_CONSUMER_PAUSE_INCOMPLETE=" if options.probe=="incomplete-pause" else "FACTORYBOX_CONSUMER_PAUSE_COMMITTED="
                        if phase == "prepare" and options.loss == "abrupt" and marker+owner in (phase_artifact/"stdout.txt").read_text().splitlines():
                            if peer and (not peer.closed or not peer.result.get("cleanup")): raise ValueError("old IDE helper still exists at intentional worker loss")
                            if provider:
                                provider.close()
                                record["external_provider"] = provider.result
                            if not exact_worker(container,owner,identity)["State"]["Running"]: raise ValueError("worker disappeared before intentional loss")
                            reference.docker(["container","kill","--signal=KILL",identity]); break
                        if time.monotonic() >= deadline: raise TimeoutError("consumer whole lifecycle exceeded 420 seconds")
                        time.sleep(.1)
                    code = probe.wait(timeout=max(1,deadline-time.monotonic()))
                    record["probe_seconds"],record["probe_exit"] = time.monotonic()-probe_start,code
                    expected = 137 if phase=="prepare" and options.loss=="abrupt" else 0
                    if code != expected: raise ValueError("consumer probe exit differs: "+str(code))
                    if expected == 0:
                        log = (phase_artifact/"stdout.txt").read_text()
                        test_name={"attached-exec":"TestNativeAttachedExecBoundary","topology":"TestNativeSessionTopology","worker-loss":"TestNativeConsumerWorkerLoss","incomplete-pause":"TestNativeConsumerWorkerLoss",
                                   "warm-preparation":"TestNativeWarmPreparation",
                                   "lifecycle":"TestNativeMinimalLifecycle" if options.example=="minimal" else "TestNativeDeveloperLifecycle"}[options.probe]
                        if "--- PASS: "+test_name+" " not in log or "--- SKIP:" in log: raise ValueError("consumer worker assertions omitted or skipped")
                        if options.probe=="lifecycle":
                            required_phases={"prepared-start-first-use","fork-first-use","source-advance","branch-advance","source-independent","fork-of-fork-first-use","resume-first-use","fork-of-resumed-first-use"}
                            observed=json.loads((output/"devx-observations.json").read_text())
                            if len(observed)!=len(required_phases) or {row["phase"] for row in observed}!=required_phases:
                                raise ValueError("ordinary delivered-worker lifecycle omitted a required useful-work phase")
                            record["observations"]=observed
                            record["preparation"]=json.loads((output/"preparation.json").read_text())
                            if options.consumer_empty_features:
                                receipt=json.loads((output/"consumer-optimization.json").read_text())
                                patch=result["consumer_optimization"]["patch"]
                                if receipt["before_sha256"]!=patch["before_sha256"] or receipt["after_sha256"]!=patch["after_sha256"] or not receipt["assertions_preserved"] or receipt["upstream_assertions_modified"]:
                                    raise ValueError("native consumer patch receipt differs")
                                record["consumer_optimization"]=receipt
                        if options.probe=="warm-preparation":
                            record["preparation"]=json.loads((output/"preparation.json").read_text())
                            record["warm_preparation"]=json.loads((output/"warm-preparation.json").read_text())
                        if provider:
                            provider.close()
                            record["external_provider"] = provider.result
                        reference.docker(["container","stop","--time=15",identity],timeout=20)
                finally:
                    if probe.poll() is None:
                        reference.owned_delete("container",container,owner); probe.wait(timeout=10)
            if peer:
                if not peer.closed: peer.close(False)
                record["theia"] = peer.result
                if not peer.result.get("passed") or not peer.result.get("cleanup"): raise ValueError("consumer IDE proof or cleanup failed")
            state = exact_worker(container,owner,identity)["State"]
            if state["Running"] or state["Pid"] != 0 or state["OOMKilled"] or state["ExitCode"] != expected: raise ValueError("consumer worker did not fully exit")
            logs = subprocess.run([*reference.DOCKER,"logs",identity],capture_output=True,text=True,timeout=5)
            (phase_artifact/"worker.log").write_text(logs.stdout+logs.stderr)
            reference.owned_delete("container",container,owner); containers.remove(container)
            record["worker_removed"],record["passed"] = True,True
        if (upstream_unit.context_manifest(artifact/"source") != result["source_context"] or
                upstream_unit.context_manifest(artifact/"example") != result["example_manifest"] or
                any(reference.sha(artifact/file)!=digest for file,digest in result["inputs_sha256"].items())):
            raise ValueError("frozen consumer proof inputs changed")
        if options.opencode_flow and upstream_unit.context_manifest(artifact/"opencode-flow") != result["opencode_flow_inputs"]:
            raise ValueError("frozen actual agent/tool inputs changed")
        if service: service.require_requests()
        result["passed"] = True
    except Exception as error:
        result["error"] = str(error)
        raise
    finally:
        preserve = False
        try:
            for container in containers:
                logs = subprocess.run([*reference.DOCKER,"logs",container],capture_output=True,text=True,timeout=5)
                (artifact/(container+"-final.log")).write_text(logs.stdout+logs.stderr)
                reference.owned_delete("container",container,owner)
            if peer and not peer.closed: peer.close(True)
            for owned_provider in providers:
                if not owned_provider.closed: owned_provider.close(aborted=True)
            if attempted and not result["passed"]:
                try:
                    upstream_bats.archive_stopped_volumes(artifact,name,owner,[name],worker["image"],result,timeout=600,compressed=True)
                except Exception as error:
                    preserve=True;result["archive_error"]=str(error);result["retained_volumes"]=[name]
            if attempted and not preserve: reference.owned_delete("volume",name,owner)
            result["global_sysctls_after"] = sysctls()
            result["global_sysctls_unchanged"] = before == result["global_sysctls_after"]
            result["cleanup"] = (not preserve and (peer is None or peer.result.get("cleanup") is True)
                                 and all(owned.result.get("cleanup") is True for owned in providers))
            result["passed"] = result["passed"] and result["cleanup"] and result["global_sysctls_unchanged"]
        finally:
            if service: service.close()
            reference_lifecycle.write_json(artifact/"result.json",result)
            print(json.dumps({"evidence":str(artifact),"passed":result["passed"],"cleanup":result["cleanup"],"error":result.get("error")}),flush=True)
            reference.artifact_storage.compact_closed_run(artifact, reference.ROOT)
        if not result["passed"]: raise RuntimeError("consumer worker-loss proof failed: "+str(artifact))


if __name__ == "__main__":
    main()
