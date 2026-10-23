#!/usr/bin/env python3
"""Force a bounded consumer's replacement onto a different owned loop slot."""
import subprocess
import uuid

import reference


def new_case(parent):
    return {"name": parent + "-loop-holder", "owner": uuid.uuid4().hex,
            "volume_created": False, "attempted": False, "passed": False, "cleanup": False,
            "scope": "An empty separate 3 CPU/3 GiB worker with an owned 512-MiB store holds the old loop slot. No sandbox, foreign loop manipulation or host setting change."}


def start(row, image, previous, deadline, ready):
    old = previous["lifecycle"]["bounded_store"]
    row["previous_active"] = old
    name, owner = row["name"], row["owner"]
    reference.docker(["volume", "create", "--label", "com.factorybox.run=" + owner, name])
    row["volume_created"] = True
    command = ["run", "--detach", "--pull=never", "--name", name,
               "--label", "com.factorybox.run=" + owner, "--privileged", "--cgroupns=private",
               "--cpus=3", "--memory=3g", "--pids-limit=512", "--network=none", "--stop-timeout=45",
               "--env", "GOMAXPROCS=2", "--env", "GOMEMLIMIT=256MiB",
               "--mount", "type=volume,source=" + name + ",target=/var/lib/sysbox",
               image, "--store-id", owner, "--bounded-root", "/var/lib/sysbox/fixed-store",
               "--bounded-bytes", str(512 << 20)]
    row["command"] = command
    row["attempted"] = True
    identity = reference.docker(command)
    row["identity"] = identity
    row["startup"] = ready(name, owner, identity, deadline)
    active = row["startup"]["status"]["lifecycle"]["bounded_store"]
    if (active["loop_number"] != old["loop_number"] or active["block_device"] != old["block_device"] or
            active["image"]["owner"] != owner or active["image"]["owner"] == old["image"]["owner"] or
            active["image"]["filesystem_uuid"] == old["image"]["filesystem_uuid"]):
        raise ValueError("owned empty worker did not acquire the released old loop slot")
    row["old_slot_held"] = True


def verify_replacement(row, current):
    old = row["previous_active"]
    active = current["lifecycle"]["bounded_store"]
    if (row.get("old_slot_held") is not True or active["image"] != old["image"] or
            active["loop_number"] == old["loop_number"] or active["block_device"] == old["block_device"]):
        raise ValueError("replacement did not remount the exact retained image on a different loop device")
    row["replacement_active"] = active
    row["replacement_verified"] = True


def close(row, exact_worker, artifact):
    try:
        if row.get("identity"):
            identity = row["identity"]
            before = exact_worker(row["name"], row["owner"], identity)
            if not before["State"]["Running"]:
                raise ValueError("empty loop holder exited before the replacement proof ended")
            reference.docker(["container", "stop", "--time=45", identity], timeout=50)
            state = exact_worker(row["name"], row["owner"], identity)["State"]
            row["state"] = state
            logs = subprocess.run([*reference.DOCKER, "logs", identity], capture_output=True, text=True, timeout=5)
            (artifact / "loop-holder.log").write_text(logs.stdout + logs.stderr)
            if state["Running"] or state["Pid"] or state["OOMKilled"] or state["ExitCode"]:
                raise ValueError("empty loop holder failed its clean owned Stop")
            row["passed"] = row.get("replacement_verified") is True
    except Exception as error:
        row["error"] = str(error)
    finally:
        try:
            if row["attempted"]:
                reference.owned_delete("container", row["name"], row["owner"])
            if row["volume_created"]:
                reference.owned_delete("volume", row["name"], row["owner"])
            row["cleanup"] = True
        except Exception as error:
            row["cleanup_error"] = str(error)
    row["passed"] = row["passed"] and row["cleanup"] and not row.get("error")
