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


def pending_kernel_owner(receipt, diagnostic):
    observed = receipt.get("helper_exit", {})
    return (
        receipt.get("cleanup") is True and receipt.get("passed") is False and
        observed.get("running") is False and observed.get("oom_killed") is False and
        isinstance(observed.get("command_exit"), int) and observed["command_exit"] != 0 and
        isinstance(observed.get("exit_code"), int) and observed["exit_code"] != 0 and
        diagnostic.strip() == "fixed filesystem still has an image owner: resource temporarily unavailable"
    )


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

def require_failed_capture_complete(result):
    required = [
        "failed_copy_observed", "recovered_failed_pause", "same_ram_after_failed_capture",
        "useful_after_failed_capture", "incomplete_alias_mounted",
        "sandbox_deleted", "worker_stopped_cleanly", "kernel_storage_released",
        "same_store_new_epoch", "successor_storage_released", "host_sysctls_unchanged", "inputs_unchanged",
    ]
    required += ([
        "failed_payload_retired", "cleanup_evidence_preserved", "retirement_replay_exact",
        "registered_base_preserved", "foreign_successor_preserved", "invalid_abort_refused",
        "published_generation_refused", "foreign_hardlink_refused", "foreign_mount_refused",
    ] if result.get("retire_recovered") else [
        "failed_payload_retired", "retained_cleanup_replay_exact", "retained_original_receipt_preserved",
        "retained_wrong_owner_refused", "retained_current_epoch_refused", "retained_record_unchanged",
        "registered_base_preserved",
    ] if result.get("retire_retained_recovered") else ["failed_payload_preserved"])
    if result.get("standalone_cleanup_cli"):
        required.append("worker_executable_preserved")
    if any(result.get(key) is not True for key in required):
        raise ValueError("failed capture exit omitted recovery, retained bytes or clean storage release")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--worker-build", required=True)
    parser.add_argument("--failed-copy", action="store_true",
                        help="Fail mutable copying after real RAM capture and immutable alias binding; require safe rollback and clean worker exit")
    parser.add_argument("--retire-recovered", action="store_true",
                        help="After proved rollback/Delete, require ordinary exact retirement of partial RAM/filesystem backing")
    parser.add_argument("--retire-retained-recovered", action="store_true",
                        help="After replacing an older worker, require exact retained capture cleanup without rewriting its old retirement")
    parser.add_argument("--predecessor-worker-build", help="Exact older worker for retained cleanup qualification")
    parser.add_argument("--retirement-cli", type=Path,
                        help="Exact selected-build cleanup CLI, run without replacing the older worker executable")
    options = parser.parse_args()
    if options.retire_recovered and not options.failed_copy:
        parser.error("--retire-recovered requires --failed-copy")
    if options.retire_retained_recovered and (not options.failed_copy or options.retire_recovered or not options.predecessor_worker_build):
        parser.error("--retire-retained-recovered requires --failed-copy and --predecessor-worker-build, without --retire-recovered")
    if options.retirement_cli and not options.retire_retained_recovered:
        parser.error("--retirement-cli requires --retire-retained-recovered")
    _, worker = build_worker.load(options.worker_build)
    predecessor = build_worker.load(options.predecessor_worker_build)[1] if options.predecessor_worker_build else worker
    if predecessor["platform"] != worker["platform"] or any(
            predecessor["bundle_sha256"][name] != value for name, value in worker["bundle_sha256"].items()
            if name not in {"factorybox", "build.json"}):
        raise ValueError("retained cleanup requires identical pinned native components and architecture")
    if options.retirement_cli:
        cli_path = options.retirement_cli.resolve()
        if not cli_path.is_relative_to(reference.ROOT / "artifacts") or reference.sha(cli_path) != worker["bundle_sha256"]["factorybox"]:
            raise ValueError("cleanup CLI differs from the exact selected native build")
        build_config.require_elf(cli_path, worker["platform"])
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
        "copy_delay_seconds": 0 if options.failed_copy else COPY_DELAY_SECONDS,
        "retire_recovered": options.retire_recovered,
        "retire_retained_recovered": options.retire_retained_recovered,
        "predecessor_worker_build": options.predecessor_worker_build,
        "standalone_cleanup_cli": bool(options.retirement_cli),
        "runner_sha256": reference.sha(artifact / "runner.py"),
        "actor_sha256": reference.sha(actor), "rootfs_sha256": rootfs["sha256"],
        "scope": ("Exact delivered CLI with real Sysbox/CRIU and an immutable base. Fail only the owned mutable copy after real RAM capture, require original-process rollback, useful work, clean worker exit and retained failed bytes across same-store restart. No retained installation, provider calls or host settings."
                  if options.failed_copy else
                  "Exact delivered CLI with real Sysbox/CRIU, a private Go workload and a controlled 65-second filesystem-copy stall after RAM capture. Require committed Pause, released anonymous RAM, identical restored RAM and useful work. The upstream cp executable is unchanged. No retained installation, provider calls or host settings."),
    }
    deadline = time.monotonic() + 420
    identity = None
    status = None
    runtime_pid = None
    volume = attempted = False
    worker_name = name
    attempted_names = []

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
        consumer_worker_loss.exact_worker(worker_name, owner, identity)

    def cli(*arguments):
        exact()
        return json.loads(run(["exec", identity, "/opt/factorybox/factorybox",
                               "--worker-run-root", "/run/factorybox", *arguments]))

    def retained_cli(*arguments):
        exact()
        executable = "/input/retirement-cli" if options.retirement_cli else "/opt/factorybox/factorybox"
        return json.loads(run(["exec", identity, executable, "--worker-run-root", "/run/factorybox", *arguments]))

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

    def release_storage(original, destination):
        observation = 0
        while True:
            selected = destination if observation == 0 else destination / ("pending-owner-" + str(observation))
            selected.mkdir(exist_ok=True)
            try:
                return worker_storage_release.verify(worker, name, owner, original, selected)
            except ValueError:
                path = selected / ("storage-release-" + original["epoch"])
                receipt = json.loads(path.with_suffix(".json").read_text())
                diagnostic = path.with_suffix(".stderr").read_text()
                if not pending_kernel_owner(receipt, diagnostic) or time.monotonic() >= deadline:
                    raise
                # The exact helper exited with a known lease refusal. Observe
                # namespace teardown within the same 420-second native deadline,
                # preserving every refusal; never detach or promote it to success.
                observation += 1
                result.setdefault("pending_kernel_owner_observations", []).append(str(selected.relative_to(artifact)))
                time.sleep(.02)

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
            predecessor["image"], "--store-id", owner, "--bounded-root", "/var/lib/sysbox/fixed-store",
            "--bounded-bytes", str(512 << 20),
        ]
        if options.retirement_cli:
            image_index = command.index(predecessor["image"])
            command[image_index:image_index] = ["--mount",
                f"type=bind,source={cli_path},target=/input/retirement-cli,readonly"]
        result["command"] = command
        save()
        attempted = True
        attempted_names.append(name)
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
        if options.failed_copy:
            base = cli("prepare-base", "--source", bundle + "/lower", "failed-copy-base")
            run(["exec", identity, "python3", "-c", """
import json,os,subprocess,sys
bundle,base=sys.argv[1:];subprocess.run(['umount',bundle+'/rootfs'],check=True)
os.rename(bundle+'/lower',bundle+'/lower-input');os.mkdir(bundle+'/lower',0o700)
subprocess.run(['mount','--bind',base,bundle+'/lower'],check=True)
subprocess.run(['mount','-o','remount,bind,ro',bundle+'/lower'],check=True)
subprocess.run(['mount','-t','overlay','overlay','-o','index=off,lowerdir='+bundle+'/lower,upperdir='+bundle+'/upper,workdir='+bundle+'/work',bundle+'/rootfs'],check=True)
path=bundle+'/config.json';config=json.load(open(path));config.setdefault('annotations',{})['io.factorybox.base']='failed-copy-base'
open(path,'w').write(json.dumps(config))
""", bundle, base["tree"]["saved"]])
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
                   f'  printf \'%s\\n\' \'{owner}\' > /output/copy-delay-observed\n' +
                   ("  exit 74\n" if options.failed_copy else f"  /usr/bin/sleep {COPY_DELAY_SECONDS}\n") +
                   "fi\n"
                   'exec /usr/bin/cp "$@"\n')
        run(["exec", identity, "python3", "-c",
             "import os,sys;p='/usr/local/bin/cp';f=os.open(p,os.O_WRONLY|os.O_CREAT|os.O_EXCL|os.O_NOFOLLOW,0o700);os.write(f,sys.argv[1].encode());os.close(f)",
             wrapper])
        phase("pause-with-failed-filesystem" if options.failed_copy else "pause-with-delayed-filesystem")
        began = time.monotonic()
        try:
            try:
                paused = cli("pause", "--snapshot-id", snapshot, "budget-source")
            except ValueError as error:
                if not options.failed_copy or "incomplete checkpoint after source process recovery" not in str(error):
                    raise
                paused = cli("state", "budget-source")
        finally:
            result["pause_seconds"] = time.monotonic() - began
            marker = output / "copy-delay-observed"
            result["copy_delay_observed"] = marker.is_file() and marker.read_text().strip() == owner
        (output / "pause.json").write_text(json.dumps(paused, indent=2) + "\n")
        capture = status["lifecycle"]["root"] + "/objects/budget-source/captures/" + snapshot
        if options.failed_copy:
            history = paused["record"].get("pause_history", [])
            result["failed_copy_observed"] = result["copy_delay_observed"]
            result["recovered_failed_pause"] = (
                paused["record"]["phase"] == "running" and not paused["record"].get("pause") and
                len(history) == 1 and history[0]["snapshot_id"] == snapshot and
                bool(history[0].get("rejected_at")) and not history[0].get("completed_at"))
            before_failure = json.loads(run(["exec", identity, "python3", "-c", """
import hashlib,json,pathlib,sys
root=pathlib.Path(sys.argv[1])
assert not (root/'checkpoint.json').exists()
paths=[root/'source-state.json',root/'guard/capture-abort-intent.json',root/'guard/guard-capture-aborted.json']
paths+=sorted((root/'images').iterdir())
hashes={str(p.relative_to(root)):hashlib.sha256(p.read_bytes()).hexdigest() for p in paths if p.is_file()}
mounted=any(str(root/'filesystem/lower')==line.split()[4] for line in pathlib.Path('/proc/self/mountinfo').read_text().splitlines())
print(json.dumps({'hashes':hashes,'alias_mounted':mounted}))
""", capture]))
            result["incomplete_alias_mounted"] = before_failure["alias_mounted"]
            result["failed_capture_hashes"] = before_failure["hashes"]
        else:
            pause = paused["record"]["pause"]
            result["committed_pause"] = paused["record"]["phase"] == "paused" and bool(pause.get("completed_at"))
            result["anonymous_memory_released"] = pause["before"]["anon"] >= 32 << 20 and pause["after"]["anon"] == 0
            phase("resume-and-use-original-state")
            resumed = cli("resume", "--snapshot-id", snapshot, "budget-source")
            runtime_pid = resumed["runtime"]["pid"]
        restored = observe()
        result["same_ram_after_failed_capture" if options.failed_copy else "same_ram_after_resume"] = (
            restored == initial and (options.failed_copy or resumed["record"]["phase"] == "running"))
        changed = observe({"marker": "resumed"})
        result["useful_after_failed_capture" if options.failed_copy else "useful_after_resume"] = (
            changed["marker"] == "resumed" and changed["counter"] == initial["counter"] + 1 and
            changed["ram_bytes"] == initial["ram_bytes"] and changed["ram_sha256"] != initial["ram_sha256"])
        deleted = cli("delete", "budget-source")
        result["sandbox_deleted"] = deleted["record"]["phase"] == "deleted"
        run(["exec", identity, "umount", bundle + "/rootfs"])
        if options.failed_copy:
            run(["exec", identity, "umount", bundle + "/lower"])
        if options.retire_retained_recovered:
            original_retirement = cli("retire-sandbox", "--sandbox-owner", deleted["record"]["owner"],
                "--input-sha256", deleted["record"]["input_sha256"], "budget-source")
            metadata = {key: value for key, value in result["failed_capture_hashes"].items()
                        if not key.startswith("images/")}
            result["predecessor_retirement"] = original_retirement
            base_canary = base["tree"]["saved"] + "/bin/minimal-actor"
            original_base_sha = run(["exec", identity, "sha256sum", base_canary]).split()[0]
            old_record = status["lifecycle"]["root"] + "/objects/budget-source/record.json"
            old_record_sha = run(["exec", identity, "sha256sum", old_record]).split()[0]
        if options.retire_recovered:
            phase("ordinary-recovered-capture-retirement")
            arguments = ("retire-sandbox", "--sandbox-owner", deleted["record"]["owner"],
                         "--input-sha256", deleted["record"]["input_sha256"], "budget-source")
            metadata = {key: value for key, value in result["failed_capture_hashes"].items()
                        if not key.startswith("images/")}
            base_canary = base["tree"]["saved"] + "/bin/minimal-actor"
            original_base_sha = run(["exec", identity, "sha256sum", base_canary]).split()[0]

            def refused():
                try:
                    cli(*arguments)
                except ValueError:
                    return True
                raise ValueError("unproved recovered-capture retirement was accepted")

            run(["exec", identity, "python3", "-c",
                 "import pathlib,sys;p=pathlib.Path(sys.argv[1]);"
                 "original=p.read_bytes();p.with_suffix('.original').write_bytes(original);"
                 "p.write_text('{\"token\":\"foreign\",\"capture_in_flight\":false}')",
                 capture + "/guard/guard-capture-aborted.json"])
            result["invalid_abort_refused"] = refused()
            run(["exec", identity, "python3", "-c",
                 "import pathlib,sys;p=pathlib.Path(sys.argv[1]);"
                 "p.write_bytes(p.with_suffix('.original').read_bytes());p.with_suffix('.original').unlink()",
                 capture + "/guard/guard-capture-aborted.json"])
            run(["exec", identity, "python3", "-c",
                 "import pathlib,sys;pathlib.Path(sys.argv[1]).write_text('{}')", capture + "/checkpoint.json"])
            result["published_generation_refused"] = refused()
            run(["exec", identity, "rm", capture + "/checkpoint.json"])
            first_image = next(key for key in result["failed_capture_hashes"] if key.startswith("images/") and key.endswith(".img"))
            foreign_link = status["store"] + "/foreign-recovered-capture-link"
            run(["exec", identity, "ln", capture + "/" + first_image, foreign_link])
            result["foreign_hardlink_refused"] = refused()
            run(["exec", identity, "rm", foreign_link])
            run(["exec", identity, "mount", "-t", "tmpfs", "-o", "size=1m,mode=0700",
                 "tmpfs", capture + "/filesystem/upper"])
            result["foreign_mount_refused"] = refused()
            run(["exec", identity, "umount", capture + "/filesystem/upper"])
            retired = cli(*arguments)
            replay = cli(*arguments)
            result["retirement_replay_exact"] = retired == replay
            result["registered_base_preserved"] = run(["exec", identity, "sha256sum", base_canary]).split()[0] == original_base_sha
            result["cleanup_evidence_preserved"] = json.loads(run(["exec", identity, "python3", "-c", """
import hashlib,json,pathlib,sys
root=pathlib.Path(sys.argv[1]);expected=json.loads(sys.argv[2])
assert not (root/'checkpoint.json').exists()
actual={name:hashlib.sha256((root/name).read_bytes()).hexdigest() for name in expected}
print(json.dumps(actual==expected and not (root/'images').exists() and not (root/'filesystem').exists()))
""", capture, json.dumps(metadata)]))
            run(["exec", identity, "python3", "-c",
                 "import pathlib,sys;p=pathlib.Path(sys.argv[1]);p.mkdir(mode=0o700);"
                 "(p/'foreign-successor').write_text('preserve foreign successor')", capture + "/images"])
            refused()
            result["foreign_successor_preserved"] = run(["exec", identity, "cat",
                capture + "/images/foreign-successor"]) == "preserve foreign successor"
            run(["exec", identity, "rm", capture + "/images/foreign-successor"])
            run(["exec", identity, "rmdir", capture + "/images"])
        run(["stop", "--timeout=45", identity])
        state = consumer_worker_loss.exact_worker(name, owner, identity)["State"]
        result["worker_stopped_cleanly"] = not state["Running"] and not state["OOMKilled"] and state["ExitCode"] == 0
        if not result["worker_stopped_cleanly"]:
            raise ValueError(f"owned worker exit was not clean: exit={state['ExitCode']}, oom={state['OOMKilled']}")
        release = release_storage(status, artifact)
        result["kernel_storage_released"] = release.get("passed") is True
        if options.failed_copy:
            worker_name = name + "-preservation"
            replacement_command = list(command)
            replacement_command[replacement_command.index(name)] = worker_name
            replacement_command[replacement_command.index(predecessor["image"])] = (
                predecessor["image"] if options.retirement_cli else worker["image"])
            result["preservation_command"] = replacement_command
            attempted_names.append(worker_name)
            identity = run(replacement_command)
            result["preservation_container_id"] = identity
            successor = consumer_worker_loss.ready(worker_name, owner, identity, deadline)["status"]
            if successor["epoch"] == status["epoch"] or successor["store_id"] != status["store_id"]:
                raise ValueError("failed-capture preservation requires the same retained store and a new epoch")
            result["same_store_new_epoch"] = True
            if options.retire_retained_recovered:
                phase("retained-recovered-capture-retirement")
                arguments = ("retire-sandbox", "--source-root", status["lifecycle"]["root"],
                             "--sandbox-owner", deleted["record"]["owner"],
                             "--input-sha256", deleted["record"]["input_sha256"], "budget-source")
                try:
                    retained_cli(*[value if value != deleted["record"]["owner"] else "f"*32 for value in arguments])
                except ValueError:
                    result["retained_wrong_owner_refused"] = True
                else:
                    raise ValueError("retained cleanup accepted another original owner")
                try:
                    retained_cli(*[value if value != status["lifecycle"]["root"] else successor["lifecycle"]["root"]
                          for value in arguments])
                except ValueError:
                    result["retained_current_epoch_refused"] = True
                else:
                    raise ValueError("retained cleanup adopted the current controller epoch")
                retired = retained_cli(*arguments)
                result["retained_original_receipt_preserved"] = retired == original_retirement
                result["retained_cleanup_replay_exact"] = retained_cli(*arguments) == retired
                result["retained_record_unchanged"] = run(["exec", identity, "sha256sum", old_record]).split()[0] == old_record_sha
                result["registered_base_preserved"] = run(["exec", identity, "sha256sum", base_canary]).split()[0] == original_base_sha
                if options.retirement_cli:
                    result["worker_executable_preserved"] = (
                        successor["supervisor_sha256"] == predecessor["bundle_sha256"]["factorybox"] and
                        run(["exec", identity, "sha256sum", "/proc/1/exe"]).split()[0] ==
                        predecessor["bundle_sha256"]["factorybox"])
            retired_mode = options.retire_recovered or options.retire_retained_recovered
            result["failed_payload_retired" if retired_mode else "failed_payload_preserved"] = json.loads(run(["exec", identity, "python3", "-c", """
import hashlib,json,pathlib,sys
root=pathlib.Path(sys.argv[1]);expected=json.loads(sys.argv[2]);retired=sys.argv[3]=='true'
assert not (root/'checkpoint.json').exists()
actual={name:hashlib.sha256((root/name).read_bytes()).hexdigest() for name in expected}
print(json.dumps(actual==expected and (not retired or not (root/'images').exists() and not (root/'filesystem').exists())))
""", capture, json.dumps(metadata if retired_mode else result["failed_capture_hashes"]),
                json.dumps(retired_mode)]))
            run(["stop", "--timeout=45", identity])
            final = consumer_worker_loss.exact_worker(worker_name, owner, identity)["State"]
            if final["Running"] or final["OOMKilled"] or final["ExitCode"] != 0:
                raise ValueError("preservation epoch did not stop cleanly")
            successor_output = artifact / "successor-release"
            successor_output.mkdir()
            result["successor_storage_release"] = release_storage(successor, successor_output)
            result["successor_storage_released"] = result["successor_storage_release"].get("passed") is True
        after = consumer_worker_loss.sysctls(docker_image=worker["native_base_image"],
                                             owner=owner, artifact=artifact, phase="after")
        result["host_sysctls_unchanged"] = before == after
        result["inputs_unchanged"] = (
            reference.sha(Path(__file__)) == result["runner_sha256"] and
            reference.sha(actor) == result["actor_sha256"] and reference.sha(archive) == result["rootfs_sha256"])
        if options.failed_copy:
            require_failed_capture_complete(result)
        else:
            require_complete(result)
        result["passed"] = True
    except (Exception, KeyboardInterrupt) as error:
        result["error"] = str(error)
    finally:
        try:
            for created_name in reversed(attempted_names):
                logs = subprocess.run([*reference.DOCKER, "logs", created_name], capture_output=True, text=True)
                filename = "worker.log" if created_name == name else "preservation-worker.log"
                (artifact / filename).write_text(logs.stdout + logs.stderr)
                # Only this fixture's checked owner can be removed. Stop every
                # writer before archiving; preserve the volume if archival fails.
                reference.owned_delete("container", created_name, owner)
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
