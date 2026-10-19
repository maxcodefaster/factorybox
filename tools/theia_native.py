#!/usr/bin/env python3
"""Prove a separate pinned Theia image through its native filesystem RPC."""
import argparse
import datetime
import json
import shutil
import subprocess
import time
import uuid

import devx_inputs
import native_base
import reference
import reference_lifecycle
import upstream_bats

ROOT = reference.ROOT
BRANCHES = ["source-sysbox", "copy-sysbox"]
ROOTS = ["/experiment/sysbox/upper/workspace/fixture",
         "/experiment/sysbox-copy/upper/workspace/fixture"]


def execute(name, script, argument=None, *, timeout=15):
    command = [*reference.DOCKER, "exec", name, "node", "-e", script]
    if argument is not None:
        command.append(json.dumps(argument))
    done = subprocess.run(command, capture_output=True, text=True, timeout=timeout)
    if done.returncode:
        raise ValueError("owned Theia command failed: " + done.stderr[-4096:])
    return json.loads(done.stdout)


def request(name, operation, body):
    return execute(name, """
const input=JSON.parse(process.argv[1]);
fetch('http://127.0.0.1:4401'+input.operation, {
  method:'POST', headers:{'content-type':'application/json'},
  body:JSON.stringify(input.body), signal:AbortSignal.timeout(12000)
}).then(async response => {
  console.log(JSON.stringify({status:response.status,body:await response.json()}));
}).catch(error => {console.error(String(error));process.exit(1)});
""", {"operation": operation, "body": body})


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--image", required=True, help="Exact cached developer-ide repository digest")
    parser.add_argument("--native-base-build", required=True, help="Exact owned archive helper image")
    options = parser.parse_args()
    reference.verify()
    image = devx_inputs.inspect_image(options.image)
    archive_image, _ = native_base.load(options.native_base_build)
    owner = uuid.uuid4().hex
    name = "factorybox-theia-" + owner
    volume = name + "-work"
    setup = name + "-setup"
    archive = reference.new_artifact(name)
    archive.mkdir()
    client = archive / "theia-client.cjs"
    shutil.copyfile(ROOT / "tests/reference/fixture/theia-client.cjs", client)
    shutil.copyfile(ROOT / "tools/theia_native.py", archive / "runner.py")
    result = {
        "schema": 1, "owner": owner, "container_name": name, "volumes": [volume],
        "started_at": datetime.datetime.now(datetime.timezone.utc).isoformat(),
        "image": image, "native_base_build": options.native_base_build,
        "client_sha256": reference.sha(client),
        "runner_sha256": reference.sha(archive / "runner.py"),
        "passed": False, "cleanup": False,
        "production_qualified": False,
        "scope": "Separate 1-CPU/768-MiB Theia component using one native filesystem RPC "
                 "connection to read and edit two owned ordinary file fixtures. These directories "
                 "are not Factorybox forks. No browser editing, lifecycle handoff, shared "
                 "workspace design, external Factory authority or inference qualification.",
    }
    attempted = volume_attempted = False
    failure = None
    try:
        volume_attempted = True
        reference.docker(["volume", "create", "--label", "com.factorybox.run=" + owner, volume])
        prepare = """
const fs=require('node:fs');
for(const path of JSON.parse(process.argv[1])) {
  fs.mkdirSync(path,{recursive:true});
  fs.writeFileSync(path+'/marker.txt','component-original');
}
fs.mkdirSync('/experiment/theia/home',{recursive:true});
for(const path of ['/experiment',...JSON.parse(process.argv[1]),'/experiment/theia','/experiment/theia/home']) {
  fs.chownSync(path,1000,1000);
}
for(const path of JSON.parse(process.argv[1]))fs.chownSync(path+'/marker.txt',1000,1000);
"""
        setup_command = [
            *reference.DOCKER, "run", "--pull=never", "--name", setup,
            "--label", "com.factorybox.run=" + owner, "--cpus=1", "--memory=256m",
            "--pids-limit=32", "--network=none", "--read-only", "--user=0:0",
            "--cap-drop=ALL", "--cap-add=CHOWN", "--cap-add=DAC_OVERRIDE",
            "--security-opt=no-new-privileges",
            "--mount", f"type=volume,source={volume},target=/experiment",
            "--entrypoint", "node", options.image, "-e", prepare, json.dumps(ROOTS),
        ]
        result["setup_command"] = setup_command
        subprocess.run(setup_command, check=True, capture_output=True, timeout=30)
        reference.owned_delete("container", setup, owner)
        command = [
            *reference.DOCKER, "run", "--detach", "--pull=never", "--name", name,
            "--label", "com.factorybox.run=" + owner, "--cpus=1", "--memory=768m",
            "--pids-limit=128", "--network=none", "--read-only", "--user=1000:1000",
            "--cap-drop=ALL", "--security-opt=no-new-privileges",
            "--env", "HOME=/experiment/theia/home",
            "--env", "XDG_CONFIG_HOME=/experiment/theia/home/.config",
            "--tmpfs", "/tmp:rw,nosuid,nodev,size=67108864,mode=1777",
            "--mount", f"type=volume,source={volume},target=/experiment",
            "--mount", f"type=bind,source={client},target=/experiment/theia-client.cjs,readonly",
            "--entrypoint", "/bin/sh", options.image, "-c",
            "node /experiment/theia-client.cjs http://127.0.0.1:4400 4401 "
            "> /experiment/theia/client.log 2>&1 & "
            "exec node /opt/factory/theia/lib/backend/main.js /experiment "
            "--hostname 127.0.0.1 --port 4400 "
            "--session-preference security.workspace.trust.enabled=false",
        ]
        result["command"] = command
        reference_lifecycle.write_json(archive / "intent.json", result)
        attempted = True
        subprocess.run(command, check=True, capture_output=True, timeout=30)
        observed = json.loads(reference.docker(["container", "inspect", name]))[0]
        host = observed["HostConfig"]
        result["observed_constraints"] = {
            "image": observed["Image"], "user": observed["Config"]["User"],
            **{key: host.get(key) for key in [
                "NanoCpus", "Memory", "PidsLimit", "ReadonlyRootfs", "NetworkMode",
                "Privileged", "CapAdd", "CapDrop", "SecurityOpt",
            ]},
        }
        if (observed["Image"] != image["id"] or observed["Config"]["User"] != "1000:1000" or
                host["NanoCpus"] != 1_000_000_000 or host["Memory"] != 768 << 20 or
                host["PidsLimit"] != 128 or not host["ReadonlyRootfs"] or
                host["NetworkMode"] != "none" or host["Privileged"] or host.get("CapAdd") or
                host["CapDrop"] != ["ALL"] or "no-new-privileges" not in host["SecurityOpt"]):
            raise ValueError("separate Theia did not receive its exact identity or resource/security limits")
        print(json.dumps({"running": str(archive)}), flush=True)
        deadline = time.monotonic() + 90
        ready = None
        while ready is None:
            try:
                ready = request(name, "/ready", {
                    "branches": BRANCHES, "phase": "component",
                    "expected_marker": "component-original"})
                if ready["status"] != 200:
                    raise ValueError("native Theia readiness refused")
            except (ValueError, subprocess.SubprocessError):
                ready = None
                if time.monotonic() >= deadline:
                    raise
                state = json.loads(reference.docker(["container", "inspect", name]))[0]["State"]
                if not state["Running"]:
                    raise ValueError("separate Theia exited before its native RPC was ready")
                time.sleep(0.2)
        result["ready"] = ready
        identity = (ready["body"]["client_id"], ready["body"]["pid"])
        if (not ready["body"]["ok"] or set(ready["body"]["values"]) != set(BRANCHES) or
                any(ready["body"]["values"][branch] != {
                    "marker": "component-original", "probe": "component:" + branch}
                    for branch in BRANCHES)):
            raise ValueError("Theia native read/edit did not reach both selected fixtures")
        rejected = request(name, "/ready", {
            "branches": ["unknown"], "phase": "invalid", "expected_marker": "component-original"})
        if rejected["status"] != 503 or rejected["body"]["ok"]:
            raise ValueError("unknown fixture was accepted by the native test client")
        execute(name, """
const fs=require('node:fs');
JSON.parse(process.argv[1]).forEach((path,index)=>fs.writeFileSync(path+'/marker.txt','devcontainer-branch-'+index));
console.log('{}');
""", ROOTS)
        divergent = request(name, "/diverge", {"branches": BRANCHES})
        result["divergent"] = divergent
        if (divergent["status"] != 200 or not divergent["body"]["ok"] or
                (divergent["body"]["client_id"], divergent["body"]["pid"]) != identity):
            raise ValueError("native Theia connection changed or independent edits failed")
        saved = execute(name, """
const fs=require('node:fs');
console.log(JSON.stringify(JSON.parse(process.argv[1]).map(path=>fs.readFileSync(path+'/marker.txt','utf8'))));
""", ROOTS)
        if saved != divergent["body"]["markers"] or len(set(saved)) != 2:
            raise ValueError("direct readback differs from the selected native Theia edits")
        result["readback"] = saved
        result["container_state"] = json.loads(
            reference.docker(["container", "inspect", name]))[0]["State"]
        if (result["container_state"]["OOMKilled"] or
                reference.sha(client) != result["client_sha256"] or
                reference.sha(archive / "runner.py") != result["runner_sha256"] or
                devx_inputs.inspect_image(options.image)["id"] != image["id"]):
            raise ValueError("Theia fixture inputs changed or its bounded container was OOM-killed")
        result["passed"] = True
    except Exception as error:
        failure = error
        result["error"] = str(error)
    finally:
        preserve = False
        if attempted:
            try:
                logs = subprocess.run([*reference.DOCKER, "logs", name],
                                      capture_output=True, text=True, timeout=15)
                (archive / "theia.log").write_text(logs.stdout + logs.stderr)
                result["client_log"] = execute(name, """
console.log(JSON.stringify(require('node:fs').readFileSync('/experiment/theia/client.log','utf8').slice(-8192)));
""")
            except Exception as error:
                result["log_observation_error"] = str(error)
            if failure:
                try:
                    upstream_bats.archive_failure(archive, name, owner, [volume],
                                                 archive_image, result, timeout=60)
                except Exception as error:
                    result["archive_error"] = str(error)
                    result["retained_volumes"] = [volume]
                    preserve = True
        try:
            reference.owned_delete("container", setup, owner)
            if attempted:
                reference.owned_delete("container", name, owner)
            if volume_attempted and not preserve:
                reference.owned_delete("volume", volume, owner)
            result["cleanup"] = not preserve
        except Exception as error:
            result["cleanup_error"] = str(error)
            failure = failure or error
        result["passed"] = result["passed"] and result["cleanup"]
        reference_lifecycle.write_json(archive / "result.json", result)
        reference.artifact_storage.compact_closed_run(archive, ROOT)
        print(json.dumps({"evidence": str(archive), **result}), flush=True)
    if failure:
        raise RuntimeError("separate Theia component proof failed: " + str(archive)) from failure


if __name__ == "__main__":
    main()
