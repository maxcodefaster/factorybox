#!/usr/bin/env python3
"""Verify bounded worker shutdown and independent kernel release natively."""
import argparse
import json
from pathlib import Path
import shutil
import subprocess
import sys
import time
import uuid

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / "tools"))
import build_worker
import consumer_worker_loss
import descriptor_transfer_proof
import native_base
import reference
import upstream_bats
import worker_storage_release

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument("--worker-build", required=True)
args = parser.parse_args()
_, worker = build_worker.load(args.worker_build)
native_base.require_native_host(worker["platform"], json.loads(reference.docker(["info", "--format", "{{json .}}"])))
if descriptor_transfer_proof.go_source_context(ROOT) != worker["source_context"]:
    raise ValueError("storage-exit proof requires the exact current delivered Go source")
OWNER = uuid.uuid4().hex
NAME = "factorybox-storage-exit-native-" + OWNER
ARTIFACT = reference.new_artifact(NAME)
ARTIFACT.mkdir(mode=0o700)
shutil.copyfile(__file__, ARTIFACT / "runner.py")
shutil.copyfile(worker_storage_release.__file__, ARTIFACT / "kernel-release.py")
result = {"schema": 1, "owner": OWNER, "workerBuild": args.worker_build,
          "image": worker["image"], "passed": False, "cleanup": False, "productionQualified": False,
          "scope": "Real bounded-worker shutdown with independently held original block FD. Require refusal while live and while the stopped original association is held, exact post-exit completion after release, and automatic completion before a later epoch. Preserve image identity and a durable canary across three epochs. No forced detach, host setting or inference.",
          "runnerSHA256": reference.sha(ARTIFACT / "runner.py"),
          "releaseHelperSHA256": reference.sha(ARTIFACT / "kernel-release.py"),
          "epochs": []}
(ARTIFACT / "intent.json").write_text(json.dumps(result, indent=2))
held_program = r'''
import fcntl,json,os,signal,stat,struct,sys,time
request=json.loads(sys.argv[1]);path='/dev/factorybox-held-'+request['owner']
os.mknod(path,stat.S_IFBLK|0o600,os.makedev(7,request['number']))
original=os.lstat(path);fd=os.open(path,os.O_RDONLY|os.O_NOFOLLOW|os.O_CLOEXEC)
data=bytearray(struct.calcsize('<QQQQQIIII64s64s32sQQ'));fcntl.ioctl(fd,0x4c05,data,True)
fields=struct.unpack('<QQQQQIIII64s64s32sQQ',data)
assert fields[0]==request['image']['device'] and fields[1]==request['image']['inode']
assert fields[4]==request['image']['bytes'] and fields[5]==request['number']
running=True
def stop(*unused):
 global running
 running=False
signal.signal(signal.SIGTERM,stop)
print(json.dumps({'ready':True,'exactOriginalHeld':True}),flush=True)
while running:time.sleep(.02)
os.close(fd);now=os.lstat(path)
assert (now.st_dev,now.st_ino,now.st_rdev)==(original.st_dev,original.st_ino,original.st_rdev)
os.unlink(path)
'''
created = []
volume = False
preserve = False
deadline = time.monotonic() + 420


def exact(name, identity):
    return consumer_worker_loss.exact_worker(name, OWNER, identity)


def start_epoch(number):
    name = NAME + "-" + str(number)
    identity = reference.docker([
        "run", "--detach", "--pull=never", "--name", name, "--label", "com.factorybox.run=" + OWNER,
        "--privileged", "--cgroupns=private", "--network=none", "--stop-timeout=45",
        "--cpus=3", "--memory=3g", "--pids-limit=512", "--env", "GOMAXPROCS=2", "--env", "GOMEMLIMIT=256MiB",
        "--mount", f"type=volume,source={NAME},target=/var/lib/sysbox",
        worker["image"], "--store-id", OWNER, "--bounded-root", "/var/lib/sysbox/fixed-store",
        "--bounded-bytes", str(512 << 20), "--health-listen", "127.0.0.1:8081",
    ])
    created.append((name, identity))
    ready = consumer_worker_loss.ready(name, OWNER, identity, deadline)
    result["epochs"].append(ready["status"])
    return name, identity, ready["status"]


def stop_epoch(name, identity):
    exact(name, identity)
    reference.docker(["stop", "--timeout=45", identity], timeout=50)
    state = exact(name, identity)["State"]
    if state["Running"] or state["OOMKilled"] or state["ExitCode"] != 0:
        raise ValueError("Original clean worker-exit assertion failed")


def expect_release_refusal(status, phase, fragment):
    directory = ARTIFACT / phase
    directory.mkdir()
    try:
        worker_storage_release.verify(worker, NAME, OWNER, status, directory)
    except ValueError:
        errors = list(directory.glob("*.stderr"))
        if len(errors) != 1 or fragment not in errors[0].read_text():
            raise ValueError("Storage release failed for another reason")
        return
    raise ValueError("An unconfirmed original storage association was promoted")


try:
    reference.docker(["volume", "create", "--label", "com.factorybox.run=" + OWNER, NAME])
    volume = True
    first_name, first, status = start_epoch(1)
    reference.docker(["exec", first, "python3", "-c",
        "import os,pathlib; p=pathlib.Path('/var/lib/sysbox/fixed-store/data/store/canary');"
        "f=p.open('x');f.write(" + repr(OWNER) + ");f.flush();os.fsync(f.fileno());f.close()"])
    image = status["lifecycle"]["bounded_store"]["image"]
    expect_release_refusal(status, "live-refusal", "bounded store still has a worker")
    result["liveWorkerRefused"] = True
    holder_name = NAME + "-holder"
    holder = reference.docker([
        "run", "--detach", "--pull=never", "--name", holder_name, "--label", "com.factorybox.run=" + OWNER,
        "--privileged", "--network=none", "--read-only", "--cpus=.25", "--memory=64m", "--pids-limit=32",
        "--tmpfs", "/var/lib/sysbox:rw,size=16m,mode=700",
        "--mount", f"type=volume,source={NAME},target=/retained,readonly",
        "--entrypoint=python3", worker["image"],
        "-u", "-c", held_program, json.dumps({"owner": OWNER, "number": status["lifecycle"]["bounded_store"]["loop_number"], "image": image}),
    ])
    created.append((holder_name, holder))
    while True:
        if not exact(holder_name, holder)["State"]["Running"]:
            raise ValueError("Exact held-reference control exited before readiness")
        if '"exactOriginalHeld": true' in reference.docker(["logs", holder]):
            break
        if time.monotonic() > deadline:
            raise TimeoutError("Held original reference is unconfirmed")
        time.sleep(.02)
    stop_epoch(first_name, first)
    expect_release_refusal(status, "held-original-refusal", "fixed filesystem still has an image owner")
    result["stoppedOriginalAssociationRefused"] = True
    pending = json.loads(reference.docker(["exec", holder, "python3", "-c",
        "import json,pathlib;root=pathlib.Path('/retained/fixed-store/worker-exits/" + status["epoch"] + "');"
        "intent=json.loads((root/'intent.json').read_text());"
        "pending=json.loads((root/'namespace-exit-required.json').read_text());"
        "assert intent==pending and not (root/'released.json').exists();"
        "print(json.dumps(pending))"]))
    if pending["epoch"] != status["epoch"] or pending["attachment"]["image"] != image:
        raise ValueError("Unconfirmed kernel release lost its original pending receipt")
    result["pendingReceiptNeverPromotedBeforeKernelRelease"] = True
    reference.docker(["stop", "--timeout=5", holder], timeout=10)
    if exact(holder_name, holder)["State"]["ExitCode"] != 0:
        raise ValueError("Exact holder did not close its own original descriptor cleanly")
    result["firstRelease"] = worker_storage_release.verify(worker, NAME, OWNER, status, ARTIFACT)
    second_name, second, second_status = start_epoch(2)
    if second_status["epoch"] == status["epoch"] or second_status["lifecycle"]["bounded_store"]["image"] != image:
        raise ValueError("Fresh epoch changed the retained image")
    canary = reference.docker(["exec", second, "cat", "/var/lib/sysbox/fixed-store/data/store/canary"])
    if canary != OWNER:
        raise ValueError("Storage release changed durable data")
    stop_epoch(second_name, second)
    third_name, third, third_status = start_epoch(3)
    completed = json.loads(reference.docker(["exec", third, "cat",
        "/var/lib/sysbox/fixed-store/worker-exits/" + second_status["epoch"] + "/released.json"]))
    if completed["epoch"] != second_status["epoch"] or completed["attachment"]["image"] != image:
        raise ValueError("Next admission did not complete the exact previous pending release")
    if reference.docker(["exec", third, "cat", "/var/lib/sysbox/fixed-store/data/store/canary"]) != OWNER:
        raise ValueError("Automatic release completion changed durable data")
    result["automaticCompletionBeforeNewAdmission"] = True
    result["sameCanaryAcrossThreeEpochs"] = True
    stop_epoch(third_name, third)
    result["finalRelease"] = worker_storage_release.verify(worker, NAME, OWNER, third_status, ARTIFACT)
    result["inputsUnchanged"] = (
        reference.sha(Path(__file__)) == result["runnerSHA256"] and
        reference.sha(Path(worker_storage_release.__file__)) == result["releaseHelperSHA256"])
    result["passed"] = result["inputsUnchanged"]
except Exception as error:
    result["error"] = str(error)
finally:
    for name, identity in reversed(created):
        state = exact(name, identity)["State"]
        log = subprocess.run([*reference.DOCKER, "logs", identity], capture_output=True, text=True)
        (ARTIFACT / (name + ".log")).write_text(log.stdout + log.stderr)
        reference.owned_delete("container", name, OWNER)
    if volume and not result["passed"]:
        try:
            upstream_bats.archive_stopped_volumes(ARTIFACT, NAME, OWNER, [NAME], worker["image"], result,
                                                  timeout=120, compressed=True)
        except Exception as error:
            result["archiveError"] = str(error)
            preserve = True
    if volume and not preserve:
        reference.owned_delete("volume", NAME, OWNER)
    result["cleanup"] = not preserve
    (ARTIFACT / "result.json").write_text(json.dumps(result, indent=2))
    print(json.dumps({"evidence": str(ARTIFACT), **{
        key: value for key, value in result.items() if key not in ["epochs", "firstRelease", "finalRelease"]}}), flush=True)
if not result["passed"]:
    raise SystemExit(1)
