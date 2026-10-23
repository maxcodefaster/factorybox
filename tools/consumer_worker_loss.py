#!/usr/bin/env python3
"""Run one complete same-host worker-loss case with an unchanged real consumer."""
import argparse
import ast
import difflib
import json
import os
from pathlib import Path
import shutil
import subprocess
import time
import uuid

import build_worker
import consumer_test_inputs
import consumer_bounded_store
import consumer_epoch_retirement
import consumer_loop_holder
import consumer_drain_interrupt
import consumer_stop_trace
import devx_inputs
import devx_project_images
import devx_lifecycle
import criu_candidate
import native_base
import external_lifecycle
import effect_lifecycle
import provider_peer
import external_cancel
import external_effect
import reference
import reference_lifecycle
import theia_admission
import theia_peer
import upstream_bats
import upstream_unit
import worker_readiness
import verify_fixture_extensions


PROBE_PHASES = {
    "worker-loss": ("prepare", "recover"),
    "incomplete-pause": ("prepare", "recover"),
    "attached-exec": ("prepare",),
    "topology": ("prepare",),
    "lifecycle": ("prepare",),
    "warm-preparation": ("prepare",),
    "agent-cancel": ("prepare", "recover"),
    "agent-effect": ("prepare", "recover"),
}


def planned_phases(probe):
    if probe not in PROBE_PHASES:
        raise ValueError("probe lacks an explicit complete phase plan")
    return PROBE_PHASES[probe]


def require_agent_cancel_recovery(result):
    phases = result["phases"]
    if [row.get("phase") for row in phases] != ["prepare", "recover"]:
        raise ValueError("agent cancellation requires both prepare and recover phases")
    if any(row.get("passed") is not True or row.get("worker_removed") is not True for row in phases):
        raise ValueError("cancellation worker phases did not completely pass and close")
    prepared, recovered = [row["agent_cancellation"] for row in phases]
    if prepared["agent"] != recovered["agent"]:
        raise ValueError("worker replacement changed the settled agent cancellation")
    if any(recovered.get(key) is not True for key in ["independent_branch", "source_deleted", "recovery_replay"]):
        raise ValueError("cancellation lacks its independent recovery descendant")
    epochs = [row["startup"]["status"]["epoch"] for row in phases]
    if not all(epochs) or epochs[0] == epochs[1]:
        raise ValueError("cancellation requires two distinct worker epochs")
    continuation = (external_cancel.require_continuation(recovered) if result.get("external_provider_enabled")
                    else require_agent_cancel_continuation(recovered))
    return {"passed": True, "phases": ["prepare", "recover"], "worker_epochs": epochs,
            "same_cancellation": True, "session_id": prepared["agent"]["session_id"],
            "call_sha256": prepared["agent"]["call_sha256"], "continuation": continuation}


def require_agent_cancel_continuation(recovered):
    agent = recovered["agent"]
    continuations = recovered.get("continuations", {})
    if set(continuations) != {"recovered", "branch"}:
        raise ValueError("cancellation requires new prompts in both independent descendants")
    tokens = []
    for receipt in continuations.values():
        if (receipt.get("session_id") != agent["session_id"] or receipt.get("call_sha256") != agent["call_sha256"] or
                receipt.get("completed") is not True or receipt.get("tool_error_consumed") is not True or
                receipt.get("launches") != 1 or receipt.get("tool_call_id") != "call_factorybox_held"):
            raise ValueError("new prompt did not consume its original cancellation without tool replay")
        token = receipt.get("prompt_token", "")
        digest = receipt.get("content_sha256", "")
        if (len(token) != 32 or len(digest) != 64 or any(char not in "0123456789abcdef" for char in token + digest) or
                receipt.get("ack") != "FACTORYBOX_CANCEL_ACK_" + token or
                any(not receipt.get(key, "").startswith("msg_") for key in ["user_message_id", "assistant_message_id", "idle_message_id"])):
            raise ValueError("new prompt lacks its actual independent response and completion")
        tokens.append(token)
    if tokens[0] == tokens[1]:
        raise ValueError("descendants reused the same continuation prompt")
    return {"passed": True, "targets": ["recovered", "branch"], "distinct_prompts": True, "launches_each": 1}


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


def inspect_retained_checkpoint(identity, artifact, old, view, deadline):
    record = view['record']
    pause = record['pause']
    request = {'source_root': old['lifecycle']['root'], 'source_id': record['id'],
               'snapshot_id': pause['snapshot_id'], 'source_owner': record['owner'],
               'checkpoint_sha256': pause['checkpoint_sha256']}
    if record['phase'] != 'paused' or not pause.get('completed_at') or pause.get('resume'):
        raise ValueError('retained inspection does not name a committed unresumed pause')
    command = [*reference.DOCKER, 'exec', identity, '/opt/factorybox/factorybox',
               '--worker-run-root', '/run/factorybox', 'inspect-recovery-checkpoint',
               '--source-root', request['source_root'], '--snapshot-id', request['snapshot_id'],
               '--source-owner', request['source_owner'], '--checkpoint-sha256', request['checkpoint_sha256'],
               request['source_id']]
    remaining = deadline - time.monotonic()
    if remaining <= 0:
        raise TimeoutError('retained inspection exhausted the existing lifecycle deadline')
    started = time.monotonic()
    done = subprocess.run(command, capture_output=True, text=True, timeout=min(120, remaining))
    (artifact / 'retained-inspection.stdout').write_text(done.stdout)
    (artifact / 'retained-inspection.stderr').write_text(done.stderr)
    if done.returncode:
        raise ValueError('delivered retained checkpoint inspection failed')
    proof = json.loads(done.stdout)
    if (proof.get('version') != 1 or proof.get('request') != request or proof.get('boot_id') != old['boot_id'] or
            proof.get('full_content_verified') is not True or not proof.get('verified_at') or
            proof.get('files', 0) <= 0 or proof.get('file_bytes', 0) <= 0 or proof.get('filesystem_trees', 0) < 2):
        raise ValueError('retained inspection omitted exact identity or complete-content verification')
    return {'passed': True, 'command': command, 'seconds': time.monotonic()-started,
            'proof': proof, 'stdout_sha256': reference.sha(artifact / 'retained-inspection.stdout')}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--worker-build",required=True)
    parser.add_argument("--probe-input-build",help="Exact already native-tested helper bytes; complete test/core source must match")
    parser.add_argument("--bounded-bytes",type=int,help="Explicit 4-32 GiB fixed store; relocate only new fixture copies and verify actual delivered capacity")
    parser.add_argument("--drain-interrupt-inputs",help="Exact preflighted host watcher to SIGKILL the owned worker after publication of its committed exit fence")
    parser.add_argument("--replacement-loop-holder",action="store_true",help="Use a separate empty owned worker to force the retained store onto a different loop slot during recovery")
    parser.add_argument("--inspect-retained-checkpoint",action="store_true",help="Verify the complete saved generation through the delivered read-only CLI before all original recovery assertions")
    parser.add_argument("--stop-trace-inputs",help="Exact signed diagnostic fixture; trace worker file syscalls after all bounded lifecycle assertions pass")
    parser.add_argument("--consumer-inputs",required=True)
    parser.add_argument("--project-images",required=True)
    parser.add_argument("--example",choices=["minimal","factory"],required=True)
    parser.add_argument("--loss",choices=["planned","abrupt"],required=True)
    parser.add_argument("--storage-driver",choices=["vfs","overlay2"],default="vfs")
    parser.add_argument("--theia-image")
    parser.add_argument("--opencode-flow",action="store_true",help="Preserve the real agent's running local tool and consume its actual result")
    parser.add_argument("--external-provider-proof",help="Use the actual admitted free model and helper from this exact native gateway proof")
    parser.add_argument("--effect-gateway-proof",help="Exact effect-only native gateway proof for the outside-effect worker-loss case")
    parser.add_argument("--external-disconnect",action="store_true",help="Drop the second completed provider response once; require exact client replay and no repeated shell")
    parser.add_argument("--consumer-empty-features",action="store_true",help="Candidate exact consumer patch: omit the feature-control lookup for zero features only")
    parser.add_argument("--consumer-request-profile",action="store_true",help="Optional copied Factory example telemetry around its first dependency fetches")
    parser.add_argument("--probe",choices=list(PROBE_PHASES),default="worker-loss")
    parser.add_argument("--epoch-retirement",action="store_true",help="Replace direct old-source removal with public epoch retirement and a committed independent successor")
    parser.add_argument("--baseline-runtime-build",help="Exact original selected packaged CLI, for a matched lifecycle control only")
    parser.add_argument("--baseline-criu-build",help="Exact original selected CRIU paired with the baseline package")
    options = parser.parse_args()
    if options.epoch_retirement and (options.bounded_bytes is None or options.probe != "worker-loss" or
            options.drain_interrupt_inputs or options.external_provider_proof or options.baseline_runtime_build):
        parser.error("epoch retirement requires the complete bounded worker-loss fixture without unrelated substitutions")
    if options.drain_interrupt_inputs and (options.bounded_bytes is None or options.probe != "worker-loss" or options.loss != "planned" or options.stop_trace_inputs or options.external_provider_proof or options.baseline_runtime_build):
        parser.error("drain interruption requires a complete bounded planned fixture and no separate diagnostic/provider substitution")
    if options.replacement_loop_holder and (options.bounded_bytes is None or options.probe != "worker-loss"):
        parser.error("replacement loop holder requires a complete bounded worker-loss case")
    if options.inspect_retained_checkpoint and (options.bounded_bytes is None or options.probe != "worker-loss" or options.baseline_runtime_build):
        parser.error("retained inspection requires the current bounded worker-loss fixture")
    if options.stop_trace_inputs and (options.bounded_bytes is None or options.probe != "lifecycle" or
            options.theia_image or options.opencode_flow or options.external_provider_proof or options.baseline_runtime_build):
        parser.error("stop tracing is a separate bounded lifecycle diagnostic without optional peers")
    if options.bounded_bytes is not None and (options.bounded_bytes % (1 << 30) or not 4 << 30 <= options.bounded_bytes <= 32 << 30 or
            options.baseline_runtime_build or options.probe not in {"lifecycle", "worker-loss", "incomplete-pause", "warm-preparation", "agent-cancel", "agent-effect"}):
        parser.error("bounded consumers require 4-32 whole GiB and a selected-runtime lifecycle/recovery fixture")
    if options.external_disconnect and (not options.external_provider_proof or options.probe != "lifecycle"):
        parser.error("the completed-response disconnect is a separate complete external-agent lifecycle")
    if options.external_provider_proof and (options.example != "factory" or not options.opencode_flow or
            options.probe not in {"lifecycle", "worker-loss", "agent-cancel", "agent-effect"} or options.baseline_runtime_build or options.consumer_request_profile):
        parser.error("external inference requires the complete Factory agent lifecycle or committed-pause worker-loss case")
    if options.example == "minimal" and options.theia_image:
        parser.error("the unchanged minimal consumer has no IDE")
    if options.probe == "agent-cancel" and (options.example != "factory" or not options.opencode_flow or
            options.theia_image or options.bounded_bytes is None or options.baseline_runtime_build):
        parser.error("agent cancellation requires the bounded Factory agent replacement case without an IDE or baseline substitution")
    if bool(options.effect_gateway_proof) != (options.probe == "agent-effect"):
        parser.error("the outside-effect case and its exact effect-only gateway proof must be selected together")
    if options.probe == "agent-effect" and (options.example != "factory" or not options.opencode_flow or
            options.bounded_bytes is None or options.theia_image or options.baseline_runtime_build):
        parser.error("outside effects require the bounded Factory agent worker replacement without an IDE or baseline substitution")
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
              "bounded_store_bytes":options.bounded_bytes,
              "inspect_retained_checkpoint":options.inspect_retained_checkpoint,
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
    if options.probe == "agent-cancel":
        result["scope"] = "Actual shipped OpenCode user interruption and settled tool cancellation through committed pause, same-boot worker replacement and independent fork. Local launch counter only; no uncertain external-effect or general exactly-once qualification."
    if options.probe == "agent-effect":
        result["scope"] = "Actual shipped agent's one uncertain outside effect through committed pause, worker replacement and independent fork. Reconcile applied status outside captured state before completing each tool result. Every original consumer assertion remains; local deterministic model, no general exactly-once or real external inference qualification."
    if options.probe == "warm-preparation":
        result["scope"] = "Compose the existing immutable-base API for two independent fresh starts; charge initial import, incremental preparation and first useful work separately. No new base-cache algorithm."
    if options.drain_interrupt_inputs:
        result["scope"] = "Whole-worker pidfd SIGKILL after the actual committed-pause exit fence appears; require incomplete exit with no completion record, then the full original consumer recovery assertions."
    shutil.copyfile(__file__,artifact/"runner.py")
    if options.drain_interrupt_inputs:
        result["drain_interrupt_inputs"] = consumer_drain_interrupt.freeze(options.drain_interrupt_inputs, artifact)
    if options.replacement_loop_holder:
        shutil.copyfile(Path(consumer_loop_holder.__file__), artifact/"consumer_loop_holder.py")
    if options.stop_trace_inputs:
        result["stop_trace_inputs"] = consumer_stop_trace.freeze(options.stop_trace_inputs, worker, artifact)
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
        if options.probe == "agent-cancel":
            model = artifact/"opencode-flow/model.cjs"
            before = model.read_text()
            old = "/opt/factorybox/opencode-flow/held-tool.cjs "
            if before.count(old) != 1:
                raise ValueError("exact held-tool model command changed")
            after = before.replace(old, "/opt/factorybox/opencode-flow/cancel-held-tool.cjs ")
            anchor = "      if (results.length) {"
            if after.count(anchor) != 1:
                raise ValueError("exact original model result boundary changed")
            after = after.replace(anchor,
                "      const cancellation = require('./cancel-model.cjs')(input, results, sequence);\n"
                "      if (cancellation) {\n"
                "        observed.push(cancellation.observation);\n"
                "        ({delta, finish} = cancellation);\n"
                "      } else if (results.length) {")
            (artifact/"cancel-model.before.cjs").write_text(before)
            patch = artifact/"cancel-model.patch"
            patch.write_text(''.join(difflib.unified_diff(before.splitlines(True), after.splitlines(True), fromfile="a/model.cjs", tofile="b/model.cjs")))
            model.write_text(after)
            result["agent_cancel_model"] = {"before_sha256": reference.sha(artifact/"cancel-model.before.cjs"),
                "after_sha256": reference.sha(model), "patch_sha256": reference.sha(patch), "assertions_preserved": True,
                "scope": "Only the cancellation case wraps its held command with a launch counter and adds explicit post-cancellation replies that require the actual tool error. All original assertions and held-tool source remain unchanged."}
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
    if options.bounded_bytes is not None:
        result["bounded_fixture"] = consumer_bounded_store.apply_fixture(artifact, source, options.consumer_empty_features)
    if options.epoch_retirement:
        result["epoch_fixture"] = consumer_epoch_retirement.apply_fixture(artifact, source)
        shutil.copyfile(Path(consumer_epoch_retirement.__file__), artifact/"consumer_epoch_retirement.py")
    if options.external_provider_proof and options.effect_gateway_proof:
        result["external_effect_fixture"] = external_effect.apply_fixture(artifact, source)
    if options.probe == "agent-cancel":
        setup = artifact/"consumer-worker-setup.sh"
        before = setup.read_text()
        anchor = '/consumer-worker.test -test.v -test.run="^${test_name}$" -test.timeout=390s'
        if before.count(anchor) != 1:
            raise ValueError("exact native test selection changed")
        after = before.replace(anchor, 'test_name=TestNativeAgentCancellation\n' + anchor)
        (artifact/"cancel-setup.before.sh").write_text(before)
        patch = artifact/"cancel-setup.patch"
        patch.write_text(''.join(difflib.unified_diff(before.splitlines(True), after.splitlines(True), fromfile="a/consumer-worker-setup.sh", tofile="b/consumer-worker-setup.sh")))
        setup.write_text(after)
        result["agent_cancel_setup"] = {"before_sha256": reference.sha(artifact/"cancel-setup.before.sh"),
            "after_sha256": reference.sha(setup), "patch_sha256": reference.sha(patch), "assertions_preserved": True,
            "scope": "After the declared bounded-path patch, select only the additive cancellation test in this new probe. Ordinary lifecycle/replacement assertions retain their original selections."}
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
    loop_holder = None
    service, provider, providers = None, None, []
    print(json.dumps({"running":str(artifact),"example":options.example,"loss":options.loss}),flush=True)
    try:
        if options.external_provider_proof:
            external_lifecycle.prepare(artifact, options.external_provider_proof, worker["image_id"], result)
        if options.effect_gateway_proof:
            effect_lifecycle.prepare(artifact, options.effect_gateway_proof, worker["image_id"], result)
        if options.external_provider_proof and options.effect_gateway_proof:
            service = external_effect.Service(artifact, result)
        elif options.external_provider_proof:
            service = external_lifecycle.Service(artifact, result, disconnect=options.external_disconnect)
        elif options.effect_gateway_proof:
            service = effect_lifecycle.Service(artifact, result)
        if options.opencode_flow:
            result["opencode_flow_inputs"] = upstream_unit.context_manifest(artifact/"opencode-flow")
        inputs_started = time.monotonic()
        if options.probe_input_build:
            inputs_path, input_build = consumer_test_inputs.load(options.probe_input_build, options.worker_build, result["source_context"])
            for file, digest in input_build["binary_sha256"].items():
                reference.artifact_storage.share_frozen_input(inputs_path/file,artifact/file,reference.ROOT,digest)
            result["probe_input_build"] = {"artifact": options.probe_input_build, "result_sha256": reference.sha(inputs_path/"result.json"),
                                          "native_proof": input_build["native_proof"], "native_result_sha256": input_build["native_result_sha256"]}
        else:
            env = dict(os.environ,GOOS="linux",GOARCH="amd64",CGO_ENABLED="0")
            for file,package,test in [("consumer-worker.test","./tests/devx-lifecycle",True),("devx-actor","./tests/devx-actor",False),
                                      ("devx-registry","./tests/devx-registry",False),("minimal-actor","./tests/minimal-actor",False),
                                      ("ide-filesystem","./tests/ide-filesystem",False)]:
                command = ["go","test","-c","-tags=factorybox_worker_probe"] if test else ["go","build"]
                subprocess.run([*command,"-trimpath","-buildvcs=false","-o",str(artifact/file),package],cwd=source,env=env,check=True,timeout=120)
        result["probe_input_preparation"] = {"mode": "verified_native_inputs" if options.probe_input_build else "compile",
                                             "seconds": time.monotonic()-inputs_started,
                                             "scope": "Host-side Go test-helper input preparation only; workload preparation and lifecycle timings remain separate."}
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
        for phase in planned_phases(options.probe):
            if phase == "recover" and options.replacement_loop_holder:
                loop_holder = consumer_loop_holder.new_case(name)
                result["replacement_loop_holder"] = loop_holder
                consumer_loop_holder.start(loop_holder, worker["image"], result["phases"][0]["startup"]["status"], deadline, ready)
            container = name+"-"+phase
            output = artifact/(phase+"-output"); output.mkdir()
            phase_artifact = artifact/phase; phase_artifact.mkdir()
            if ide: shutil.copyfile(artifact/"theia-client.cjs",phase_artifact/"theia-client.cjs")
            peer = theia_peer.Peer(container,owner,ide,phase_artifact,output,worker["image"]) if ide else None
            record = {"phase":phase,"passed":False}; result["phases"].append(record)
            command = ["run","--detach","--pull=never","--name",container,"--label","com.factorybox.run="+owner,
                       "--privileged","--cgroupns=private","--cpus=3","--memory=3g","--pids-limit=512","--network=none","--stop-timeout=45",
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
            if options.external_provider_proof and options.effect_gateway_proof:
                command += ["--env", "FACTORYBOX_DEVX_EXTERNAL_EFFECT=1"]
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
                if options.external_provider_proof and options.effect_gateway_proof:
                    mounts += [(Path(service.effect_directory.name), "/outside-effect", True)]
            if baseline_receipt: mounts += [(artifact/"baseline-criu","/baseline-criu",True)]
            if options.consumer_empty_features:
                mounts += [(artifact/file,"/"+file,True) for file in ["consumer-patch.py","consumer-patch.json"]]
            for source_path,target,readonly in mounts:
                command += ["--mount",f"type=bind,source={source_path},target={target}"+(",readonly" if readonly else "")]
            command += ["--entrypoint","/opt/factorybox/factorybox",worker["image"],"worker","--store-id",owner]
            if options.bounded_bytes is not None:
                command += ["--bounded-root", consumer_bounded_store.BOUNDED_ROOT, "--bounded-bytes", str(options.bounded_bytes)]
            record["command"] = command
            containers.append(container)
            identity = reference.docker(command)
            record["startup"] = ready(container,owner,identity,deadline)
            if phase == "recover" and loop_holder is not None:
                consumer_loop_holder.verify_replacement(loop_holder, record["startup"]["status"])
            if phase == "recover" and options.inspect_retained_checkpoint:
                record['retained_checkpoint_inspection'] = inspect_retained_checkpoint(
                    identity, phase_artifact, result['phases'][0]['startup']['status'],
                    json.loads((artifact / 'prepare-output/cli-03-pause.json').read_text()), deadline)
            if options.bounded_bytes is not None:
                capacity = json.loads(reference.docker(["exec", identity, "/opt/factorybox/factorybox", "--worker-run-root", "/run/factorybox", "capacity"]))
                record["bounded_store_before"] = consumer_bounded_store.verify_capacity(record["startup"]["status"], capacity, options.bounded_bytes, owner)
            gateway_key = "effect_oracle" if options.effect_gateway_proof else "external_provider"
            peer_key = "effect_gateway" if options.effect_gateway_proof else "external_provider"
            if options.external_provider_proof and options.effect_gateway_proof:
                provider = external_effect.Peer(container, identity, owner, phase_artifact, output, result[gateway_key]["gateway_sha256"])
            else:
                provider = provider_peer.Peer(container,identity,owner,phase_artifact,output,result[gateway_key]["gateway_sha256"],
                                              effect_only=bool(options.effect_gateway_proof)) if service else None
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
                                record[peer_key] = provider.result
                            if not exact_worker(container,owner,identity)["State"]["Running"]: raise ValueError("worker disappeared before intentional loss")
                            reference.docker(["container","kill","--signal=KILL",identity]); break
                        if time.monotonic() >= deadline: raise TimeoutError("consumer whole lifecycle exceeded 420 seconds")
                        time.sleep(.1)
                    code = probe.wait(timeout=max(1,deadline-time.monotonic()))
                    record["probe_seconds"],record["probe_exit"] = time.monotonic()-probe_start,code
                    expected = 137 if phase=="prepare" and options.loss=="abrupt" else 0
                    if code != expected: raise ValueError("consumer probe exit differs: "+str(code))
                    if options.probe == "agent-cancel":
                        receipt = json.loads((output/("opencode-cancel-"+phase+".json")).read_text())
                        agent = receipt["agent"]
                        if (receipt.get("passed") is not True or receipt.get("phase") != phase or
                                agent.get("canceled") is not True or agent.get("launches") != 1 or agent.get("tool_status") != "error" or
                                agent.get("idle_repeat") is not True or agent.get("original_process_gone") is not True or agent.get("execution_interrupted") is not True):
                            raise ValueError("missing settled actual tool cancellation")
                        if phase == "recover" and any(receipt.get(key) is not True for key in ["independent_branch", "source_deleted", "recovery_replay"]):
                            raise ValueError("cancellation omitted independent descendant or recovery replay")
                        record["agent_cancellation"] = receipt
                    if expected == 0:
                        log = (phase_artifact/"stdout.txt").read_text()
                        test_name={"attached-exec":"TestNativeAttachedExecBoundary","topology":"TestNativeSessionTopology","worker-loss":"TestNativeConsumerWorkerLoss","incomplete-pause":"TestNativeConsumerWorkerLoss",
                                   "warm-preparation":"TestNativeWarmPreparation", "agent-cancel":"TestNativeAgentCancellation", "agent-effect":"TestNativeConsumerWorkerLoss",
                                   "lifecycle":"TestNativeMinimalLifecycle" if options.example=="minimal" else "TestNativeDeveloperLifecycle"}[options.probe]
                        if "--- PASS: "+test_name+" " not in log or "--- SKIP:" in log: raise ValueError("consumer worker assertions omitted or skipped")
                        if options.epoch_retirement and phase == "recover":
                            old_root = result["phases"][0]["startup"]["status"]["lifecycle"]["root"]
                            record["epoch_retirement"] = consumer_epoch_retirement.verify_output(output, old_root)
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
                            record[peer_key] = provider.result
                        if options.bounded_bytes is not None:
                            capacity = json.loads(reference.docker(["exec", identity, "/opt/factorybox/factorybox", "--worker-run-root", "/run/factorybox", "capacity"]))
                            record["bounded_store_after"] = consumer_bounded_store.verify_capacity(record["startup"]["status"], capacity, options.bounded_bytes, owner)
                            if phase == "prepare":
                                record["bounded_source"] = consumer_bounded_store.verify_source(json.loads((output / "cli-02-start.json").read_text()))
                            code = 'import json,sys; target=sys.argv[1]; print(json.dumps({"target":target,"mountinfo":[line.strip() for line in open("/proc/self/mountinfo") if line.split()[4]==target or line.split()[4].startswith(target+"/")]}))'
                            mounts = json.loads(reference.docker([
                                "exec", identity, "python3", "-c", code,
                                record["startup"]["status"]["lifecycle"]["bounded_store"]["target"]]))
                            (phase_artifact / "bounded-before-worker-stop.json").write_text(json.dumps(mounts, indent=2) + "\n")
                            record["bounded_before_worker_stop"] = mounts
                        if options.stop_trace_inputs:
                            record["stop_tracer"] = consumer_stop_trace.attach(identity, artifact, phase_artifact, result["stop_trace_inputs"])
                        stop_started = time.monotonic()
                        remaining = deadline - stop_started
                        if remaining <= 2:
                            raise TimeoutError("consumer has no remaining lifecycle budget for worker Stop")
                        grace = min(45, int(remaining) - 1)
                        record["worker_stop_grace_seconds"] = grace
                        record["worker_stop_remaining_lifecycle_seconds"] = remaining
                        try:
                            if options.drain_interrupt_inputs and phase == "prepare":
                                record["drain_interruption"] = consumer_drain_interrupt.stop(
                                    container, owner, identity, worker, record["startup"]["status"],
                                    artifact, phase_artifact, output, deadline, exact_worker)
                            else:
                                reference.docker(["container","stop","--time=" + str(grace),identity],timeout=remaining)
                        finally:
                            record["worker_stop_seconds"] = time.monotonic() - stop_started
                finally:
                    if probe.poll() is None:
                        reference.owned_delete("container",container,owner); probe.wait(timeout=10)
            if peer:
                if not peer.closed: peer.close(False)
                record["theia"] = peer.result
                if not peer.result.get("passed") or not peer.result.get("cleanup"): raise ValueError("consumer IDE proof or cleanup failed")
            state = exact_worker(container,owner,identity)["State"]
            record["worker_exit"] = state
            logs = subprocess.run([*reference.DOCKER,"logs",identity],capture_output=True,text=True,timeout=5)
            (phase_artifact/"worker.log").write_text(logs.stdout+logs.stderr)
            if record.get("stop_tracer"):
                record["stop_trace"] = consumer_stop_trace.collect(identity, phase_artifact)
            worker_expected = 137 if options.drain_interrupt_inputs and phase == "prepare" else expected
            record["expected_worker_exit"] = worker_expected
            if state["Running"] or state["Pid"] != 0 or state["OOMKilled"] or state["ExitCode"] != worker_expected: raise ValueError("consumer worker did not fully exit")
            reference.owned_delete("container",container,owner); containers.remove(container)
            record["worker_removed"],record["passed"] = True,True
            if options.effect_gateway_proof:
                service.snapshot(phase)
        if (upstream_unit.context_manifest(artifact/"source") != result["source_context"] or
                upstream_unit.context_manifest(artifact/"example") != result["example_manifest"] or
                any(reference.sha(artifact/file)!=digest for file,digest in result["inputs_sha256"].items())):
            raise ValueError("frozen consumer proof inputs changed")
        if options.stop_trace_inputs:
            consumer_stop_trace.validate_root(artifact / "stop-trace-rootfs", result["stop_trace_inputs"]["files"])
        if options.opencode_flow and upstream_unit.context_manifest(artifact/"opencode-flow") != result["opencode_flow_inputs"]:
            raise ValueError("frozen actual agent/tool inputs changed")
        if service: service.require_requests()
        if options.probe == "agent-cancel":
            result["agent_cancellation_complete"] = require_agent_cancel_recovery(result)
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
            if loop_holder is not None:
                consumer_loop_holder.close(loop_holder, exact_worker, artifact)
                if not loop_holder["passed"]:
                    result["passed"] = False
                    result.setdefault("error", "replacement loop holder proof failed")
                if not loop_holder["cleanup"]:
                    preserve = True
            if attempted and not result["passed"]:
                try:
                    upstream_bats.archive_stopped_volumes(
                        artifact,name,owner,[name],worker["image"],result,timeout=600,compressed=True,
                        decoded_limit=options.bounded_bytes + (1 << 30) if options.bounded_bytes else None)
                except Exception as error:
                    preserve=True;result["archive_error"]=str(error);result["retained_volumes"]=[name]
            if attempted and not preserve: reference.owned_delete("volume",name,owner)
            result["global_sysctls_after"] = sysctls()
            result["global_sysctls_unchanged"] = before == result["global_sysctls_after"]
            result["cleanup"] = (not preserve and (peer is None or peer.result.get("cleanup") is True)
                                 and all(owned.result.get("cleanup") is True for owned in providers)
                                 and (loop_holder is None or loop_holder.get("cleanup") is True))
            result["passed"] = result["passed"] and result["cleanup"] and result["global_sysctls_unchanged"]
        finally:
            if service: service.close()
            if options.effect_gateway_proof and result.get("effect_oracle", {}).get("cleanup") is not True:
                result["passed"] = result["cleanup"] = False
                result.setdefault("error", "outside effect service did not close")
            reference_lifecycle.write_json(artifact/"result.json",result)
            print(json.dumps({"evidence":str(artifact),"passed":result["passed"],"cleanup":result["cleanup"],"error":result.get("error")}),flush=True)
            reference.artifact_storage.compact_closed_run(artifact, reference.ROOT)
        if not result["passed"]: raise RuntimeError("consumer worker-loss proof failed: "+str(artifact))


if __name__ == "__main__":
    main()
