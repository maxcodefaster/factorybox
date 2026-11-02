#!/usr/bin/env python3
"""Verify a real Pause whose filesystem phase outlives the old inner deadline."""
import argparse
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import time
import uuid

import build_config
import build_worker
import consumer_worker_loss
import native_base
import reference
import upstream_bats
import worker_storage_release


COPY_DELAY_SECONDS = 65


def require_complete(result):
    required = [
        "copy_delay_observed", "committed_pause", "anonymous_memory_released",
        "same_ram_after_resume", "useful_after_resume", "sandbox_deleted",
        "worker_stopped_cleanly", "kernel_storage_released", "host_sysctls_unchanged",
        "inputs_unchanged",
    ]
    if (any(result.get(key) is not True for key in required) or
            result.get("copy_delay_seconds") != COPY_DELAY_SECONDS or
            result.get("pause_seconds", 0) < COPY_DELAY_SECONDS):
        raise ValueError("capture budget omitted the delayed filesystem, RAM or cleanup proof")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--worker-build", required=True)
    options = parser.parse_args()
    _, worker = build_worker.load(options.worker_build)
    native_base.require_native_host(worker["platform"], json.loads(reference.docker(["info", "--format", "{{json .}}"])))
    if worker.get("native_candidate") is not None:
        raise ValueError("capture budget requires the ordinary delivered native image")
    owner = uuid.uuid4().hex
    name = "factorybox-capture-budget-native-" + owner
    artifact = reference.new_artifact(name)
    artifact.mkdir(mode=0o700)
    shutil.copyfile(__file__, artifact / "runner.py")
    output = artifact / "output"
    output.mkdir()
    architecture = build_config.architecture(worker["platform"])
    rootfs = json.loads((reference.ROOT / "upstream/test-inputs.json").read_text())["busybox-" + architecture]
    archive = reference.ROOT / ".cache/downloads" / rootfs["archive"]
    if reference.sha(archive) != rootfs["sha256"]:
        raise ValueError("capture budget rootfs differs from the pinned input")
    source = artifact / "actor-source"
    shutil.copytree(reference.ROOT / "tests/minimal-actor", source / "tests/minimal-actor")
    for file in ["go.mod", "go.sum"]:
        shutil.copyfile(reference.ROOT / file, source / file)
    actor = artifact / "minimal-actor"
    with (artifact / "build.log").open("x") as log:
        subprocess.run(["go", "build", "-mod=readonly", "-trimpath", "-buildvcs=false",
                        "-o", str(actor), "./tests/minimal-actor"], cwd=source, check=True,
                       env=dict(os.environ, GOOS="linux", GOARCH=architecture, CGO_ENABLED="0",
                                GOMAXPROCS="3", GOMEMLIMIT="2GiB"), stdout=log, stderr=subprocess.STDOUT)
    result = {
        "schema": 1, "owner": owner, "worker_build": options.worker_build, "image": worker["image"],
        "executable_sha256": worker["bundle_sha256"]["factorybox"], "platform": worker["platform"],
        "passed": False, "cleanup": False, "production_qualified": False,
        "copy_delay_seconds": COPY_DELAY_SECONDS, "runner_sha256": reference.sha(artifact / "runner.py"),
        "actor_sha256": reference.sha(actor), "rootfs_sha256": rootfs["sha256"],
        "scope": "Exact delivered CLI with real Sysbox/CRIU, a private Go workload and a controlled 65-second filesystem-copy stall after RAM capture. Require committed Pause, released anonymous RAM, identical restored RAM and useful work. The upstream cp executable is unchanged. No retained installation, provider calls or host settings.",
    }
    deadline = time.monotonic() + 420
    identity = None
    status = None
    runtime_pid = None
    volume = attempted = False

    def save():
        (artifact / "result.json").write_text(json.dumps(result, indent=2) + "\n")

    def run(arguments, *, body=None):
        remaining = deadline - time.monotonic()
        if remaining <= 0:
            raise TimeoutError("capture budget exhausted its original 420-second lifecycle")
        done = subprocess.run([*reference.DOCKER, *arguments], input=body,
                              capture_output=True, text=True, timeout=remaining)
        with (artifact / "commands.jsonl").open("a") as log:
            log.write(json.dumps({"arguments": arguments, "exit_code": done.returncode,
                                  "stdout": done.stdout, "stderr": done.stderr}) + "\n")
        if done.returncode:
            raise ValueError("owned capture budget command failed: " + done.stderr[-2500:])
        return done.stdout.strip()

    def phase(stage):
        print(json.dumps({"evidence": str(artifact), "stage": stage}), flush=True)

    def exact():
        consumer_worker_loss.exact_worker(name, owner, identity)

    def cli(*arguments):
        exact()
        return json.loads(run(["exec", identity, "/opt/factorybox/factorybox",
                               "--worker-run-root", "/run/factorybox", *arguments]))

    def observe(payload=None):
        exact()
        program = """
import http.client,json,socket,sys
request=json.loads(sys.argv[1]);connection=http.client.HTTPConnection('localhost')
connection.sock=socket.socket(socket.AF_UNIX,socket.SOCK_STREAM)
connection.sock.connect(request['socket'])
body=json.dumps(request['body']) if request.get('body') is not None else None
connection.request('POST' if body else 'GET','/',body,{'Content-Type':'application/json'})
response=connection.getresponse()
assert response.status==200
print(response.read().decode())
"""
        return json.loads(run(["exec", identity, "python3", "-c", program,
                               json.dumps({"socket": f"/proc/{runtime_pid}/root/workspace/.minimal.sock", "body": payload})]))

    try:
        before = consumer_worker_loss.sysctls(docker_image=worker["native_base_image"],
                                              owner=owner, artifact=artifact, phase="before")
        volume = True
        run(["volume", "create", "--label", "com.factorybox.run=" + owner, name])
        command = [
            "run", "--detach", "--pull=never", "--name", name, "--label", "com.factorybox.run=" + owner,
            "--privileged", "--cgroupns=private", "--network=none", "--stop-timeout=45",
            "--cpus=3", "--memory=3g", "--pids-limit=512", "--env", "GOMAXPROCS=2",
            "--env", "GOMEMLIMIT=256MiB", "--mount", f"type=volume,source={name},target=/var/lib/sysbox",
            "--mount", f"type=bind,source={archive},target=/input/rootfs.tar.gz,readonly",
            "--mount", f"type=bind,source={actor},target=/input/minimal-actor,readonly",
            "--mount", f"type=bind,source={output},target=/output",
            worker["image"], "--store-id", owner, "--bounded-root", "/var/lib/sysbox/fixed-store",
            "--bounded-bytes", str(512 << 20),
        ]
        result["command"] = command
        save()
        attempted = True
        identity = run(command)
        result["container_id"] = identity
        status = consumer_worker_loss.ready(name, owner, identity, deadline)["status"]
        bundle = status["store"] + "/budget-bundle"
        program = """
import json,os,subprocess,sys
bundle,runtime=sys.argv[1:]
for suffix in ['', '/lower','/upper','/work','/rootfs']:os.mkdir(bundle+suffix,0o700 if not suffix else 0o755)
subprocess.run(['tar','-xzf','/input/rootfs.tar.gz','-C',bundle+'/lower'],check=True)
subprocess.run(['cp','/input/minimal-actor',bundle+'/lower/bin/minimal-actor'],check=True)
os.mkdir(bundle+'/lower/workspace')
subprocess.run(['mount','-t','overlay','overlay','-o','index=off,lowerdir='+bundle+'/lower,upperdir='+bundle+'/upper,workdir='+bundle+'/work',bundle+'/rootfs'],check=True)
subprocess.run([runtime,'spec','--bundle',bundle],check=True)
path=bundle+'/config.json';config=json.load(open(path))
config['process'].update(terminal=False,cwd='/',args=['/bin/sh','-c','exec /bin/minimal-actor </dev/null >/dev/null 2>&1'],env=['HOME=/root','PATH=/bin:/usr/bin','GOMAXPROCS=2'])
config['root']['readonly']=False
config['linux']['resources']={'pids':{'limit':128},'devices':[{'allow':True,'access':'rwm'}]}
open(path,'w').write(json.dumps(config))
"""
        run(["exec", identity, "python3", "-c", program, bundle, status["lifecycle"]["runtime"]])
        started = cli("start", "--bundle", bundle, "budget-source")
        runtime_pid = started["runtime"]["pid"]
        while True:
            try:
                initial = observe()
                break
            except ValueError:
                if time.monotonic() >= deadline:
                    raise
                time.sleep(.02)
        if initial["ram_bytes"] != 32 << 20 or initial["marker"] != "source":
            raise ValueError("native source did not establish its private RAM and files")
        snapshot = uuid.uuid4().hex
        target = status["lifecycle"]["root"] + "/objects/budget-source/captures/" + snapshot + "/filesystem/upper"
        wrapper = ("#!/bin/sh\n"
                   'target=$(/usr/bin/readlink "/proc/$$/fd/4") || exit 91\n'
                   f'if [ "$target" = "{target}" ]; then\n'
                   f'  printf \'%s\\n\' \'{owner}\' > /output/copy-delay-observed\n'
                   f"  /usr/bin/sleep {COPY_DELAY_SECONDS}\nfi\n"
                   'exec /usr/bin/cp "$@"\n')
        run(["exec", identity, "python3", "-c",
             "import os,sys;p='/usr/local/bin/cp';f=os.open(p,os.O_WRONLY|os.O_CREAT|os.O_EXCL|os.O_NOFOLLOW,0o700);os.write(f,sys.argv[1].encode());os.close(f)",
             wrapper])
        phase("pause-with-delayed-filesystem")
        began = time.monotonic()
        try:
            paused = cli("pause", "--snapshot-id", snapshot, "budget-source")
        finally:
            result["pause_seconds"] = time.monotonic() - began
            marker = output / "copy-delay-observed"
            result["copy_delay_observed"] = marker.is_file() and marker.read_text().strip() == owner
        (output / "pause.json").write_text(json.dumps(paused, indent=2) + "\n")
        pause = paused["record"]["pause"]
        result["committed_pause"] = paused["record"]["phase"] == "paused" and bool(pause.get("completed_at"))
        result["anonymous_memory_released"] = pause["before"]["anon"] >= 32 << 20 and pause["after"]["anon"] == 0
        phase("resume-and-use-original-state")
        resumed = cli("resume", "--snapshot-id", snapshot, "budget-source")
        runtime_pid = resumed["runtime"]["pid"]
        restored = observe()
        result["same_ram_after_resume"] = restored == initial and resumed["record"]["phase"] == "running"
        changed = observe({"marker": "resumed"})
        result["useful_after_resume"] = (
            changed["marker"] == "resumed" and changed["counter"] == initial["counter"] + 1 and
            changed["ram_bytes"] == initial["ram_bytes"] and changed["ram_sha256"] != initial["ram_sha256"])
        deleted = cli("delete", "budget-source")
        result["sandbox_deleted"] = deleted["record"]["phase"] == "deleted"
        run(["exec", identity, "umount", bundle + "/rootfs"])
        run(["stop", "--timeout=45", identity])
        state = consumer_worker_loss.exact_worker(name, owner, identity)["State"]
        result["worker_stopped_cleanly"] = not state["Running"] and not state["OOMKilled"] and state["ExitCode"] == 0
        release = worker_storage_release.verify(worker, name, owner, status, artifact)
        result["kernel_storage_released"] = release.get("passed") is True
        after = consumer_worker_loss.sysctls(docker_image=worker["native_base_image"],
                                             owner=owner, artifact=artifact, phase="after")
        result["host_sysctls_unchanged"] = before == after
        result["inputs_unchanged"] = (
            reference.sha(Path(__file__)) == result["runner_sha256"] and
            reference.sha(actor) == result["actor_sha256"] and reference.sha(archive) == result["rootfs_sha256"])
        require_complete(result)
        result["passed"] = True
    except (Exception, KeyboardInterrupt) as error:
        result["error"] = str(error)
    finally:
        try:
            if attempted:
                logs = subprocess.run([*reference.DOCKER, "logs", name], capture_output=True, text=True)
                (artifact / "worker.log").write_text(logs.stdout + logs.stderr)
                # Only this fixture's checked owner can be removed. Stop every
                # writer before archiving; preserve the volume if archival fails.
                reference.owned_delete("container", name, owner)
            if not result["passed"] and volume:
                upstream_bats.archive_stopped_volumes(
                    artifact, name, owner, [name], worker["image"], result, compressed=True)
            if volume:
                reference.owned_delete("volume", name, owner)
            result["cleanup"] = True
        except Exception as error:
            result["cleanup_error"] = str(error)
            result["cleanup"] = False
            result["passed"] = False
        finally:
            save()
            print(json.dumps({"evidence": str(artifact), "passed": result["passed"],
                              "cleanup": result["cleanup"], "error": result.get("error")}), flush=True)
    if not result["passed"]:
        raise SystemExit(1)


if __name__ == "__main__":
    main()
