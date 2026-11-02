"""Verify exact stopped-worker kernel storage release without changing backing."""
import json
import re
import subprocess
import time
import uuid

import consumer_worker_loss
import reference

def pending_kernel_owner(receipt, diagnostic):
    observed = receipt.get("helper_exit", {})
    return (
        receipt.get("cleanup") is True and receipt.get("passed") is False and
        observed.get("running") is False and observed.get("oom_killed") is False and
        isinstance(observed.get("command_exit"), int) and observed["command_exit"] != 0 and
        isinstance(observed.get("exit_code"), int) and observed["exit_code"] != 0 and
        diagnostic.strip() == "fixed filesystem still has an image owner: resource temporarily unavailable"
    )


def observe(worker, volume, owner, status, artifact, *, deadline):
    """Keep single observations immutable while the stopped original releases."""
    epoch = status.get("epoch")
    if not isinstance(epoch, str) or not re.fullmatch(r"[a-f0-9]{32}", epoch):
        raise ValueError("storage release requires its exact original epoch")
    observation = 0
    while True:
        if time.monotonic() >= deadline:
            raise TimeoutError("original kernel release was not confirmed within the native lifecycle")
        selected = artifact / "storage-release-observations" / epoch / str(observation)
        selected.mkdir(parents=True)
        try:
            receipt = verify(worker, volume, owner, status, selected)
            return {**receipt, "observations": observation + 1}
        except ValueError:
            path = selected / ("storage-release-" + status["epoch"])
            if not path.with_suffix(".json").is_file() or not path.with_suffix(".stderr").is_file():
                raise
            receipt = json.loads(path.with_suffix(".json").read_text())
            if not pending_kernel_owner(receipt, path.with_suffix(".stderr").read_text()):
                raise
            # Only reobserve this completed known refusal, never an uncertain
            # lifecycle command or a failed/changed/foreign ownership result.
            observation += 1
            time.sleep(.02)


def verify(worker, volume, owner, status, artifact):
    active = status.get("lifecycle", {}).get("bounded_store")
    if not isinstance(active, dict):
        raise ValueError("storage release requires the original bounded-worker status")
    image = active.get("image")
    if (not isinstance(image, dict) or image.get("owner") != status.get("store_id") or
            image.get("path") != "/var/lib/sysbox/fixed-store/store.img" or
            not re.fullmatch(r"[a-f0-9]{32}", status.get("epoch", ""))):
        raise ValueError("storage release original identity differs")
    declared = json.loads(reference.docker(["volume", "inspect", volume]))[0]
    if declared.get("Name") != volume or declared.get("Labels", {}).get("com.factorybox.run") != owner:
        raise ValueError("storage release must use only the exact owned volume")
    name = "factorybox-storage-release-" + uuid.uuid4().hex
    identity = None
    receipt = {"volume": volume, "owner": owner, "epoch": status["epoch"], "image": image,
               "helper": name, "passed": False, "kernel_state_changed": False}
    path = artifact / ("storage-release-" + status["epoch"])
    if path.with_suffix(".json").exists():
        raise ValueError("observe the existing storage-release command")
    path.with_suffix(".intent.json").write_text(json.dumps(receipt, indent=2))
    try:
        identity = reference.docker([
            "create", "--pull=never", "--name", name, "--label", "com.factorybox.run=" + owner,
            "--privileged", "--network=none", "--read-only", "--cpus=1", "--memory=128m", "--pids-limit=64",
            "--mount", f"type=volume,source={volume},target=/var/lib/sysbox",
            "--entrypoint", "/opt/factorybox/factorybox", worker["image"],
            "worker-reconcile-storage", "--bounded-root", "/var/lib/sysbox/fixed-store",
            "--store-id", status["store_id"],
        ])
        consumer_worker_loss.exact_worker(name, owner, identity)
        done = subprocess.run([*reference.DOCKER, "start", "--attach", identity],
                              capture_output=True, text=True, timeout=30)
        path.with_suffix(".stdout").write_text(done.stdout)
        path.with_suffix(".stderr").write_text(done.stderr)
        state = consumer_worker_loss.exact_worker(name, owner, identity)["State"]
        receipt["helper_exit"] = {"command_exit": done.returncode, "running": state["Running"],
                                "exit_code": state["ExitCode"], "oom_killed": state["OOMKilled"]}
        if done.returncode or state["Running"] or state["ExitCode"] != 0 or state["OOMKilled"]:
            raise ValueError("post-exit storage release is unconfirmed")
        observed = json.loads(done.stdout)
        if (observed.get("store_id") != status["store_id"] or observed.get("image") != image or
                observed.get("kernel_release_verified") is not True or
                status["epoch"] not in observed.get("completed_epochs", [])):
            raise ValueError("post-exit storage proof lost its exact original image or epoch")
        receipt.update(passed=True, observation=observed, helper_id=identity)
        return receipt
    finally:
        if identity:
            consumer_worker_loss.exact_worker(name, owner, identity)
            reference.owned_delete("container", name, owner)
        receipt["cleanup"] = True
        path.with_suffix(".json").write_text(json.dumps(receipt, indent=2))
