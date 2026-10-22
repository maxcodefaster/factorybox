#!/usr/bin/env python3
"""Exercise the local CLI against one administrator-prepared Compose worker."""
import argparse
import io
import json
import os
from pathlib import Path, PurePosixPath
import re
import subprocess
import tarfile
import time
import uuid


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--project", required=True, help="Dedicated Compose project already running this example")
    parser.add_argument("--bundle", required=True, help="Prepared private OCI bundle under /var/lib/sysbox/input/")
    parser.add_argument("--output", type=Path, required=True, help="New private directory for responses and recovery request")
    parser.add_argument("--context", default="default")
    parser.add_argument("--sudo", action="store_true")
    args = parser.parse_args()
    store = os.environ.get("FACTORYBOX_STORE_ID", "")
    volume = os.environ.get("FACTORYBOX_STORE_VOLUME", "")
    image = os.environ.get("FACTORYBOX_WORKER_IMAGE", "")
    bundle = PurePosixPath(args.bundle)
    if (not re.fullmatch(r"[0-9a-f]{32}", store) or not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9_.-]{0,254}", volume) or
            not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9._:/-]*@sha256:[0-9a-f]{64}", image) or
            not re.fullmatch(r"[a-z][a-z0-9-]{0,62}", args.project) or
            str(bundle) != args.bundle or not bundle.is_relative_to("/var/lib/sysbox/input") or
            bundle == PurePosixPath("/var/lib/sysbox/input") or ".." in bundle.parts):
        parser.error("requires exact image/store inputs, a dedicated project and canonical private bundle")
    args.output.mkdir(mode=0o700)
    # sudo may discard the calling process environment. Explicit, validated
    # Compose inputs work with either ordinary or elevated Docker access.
    compose_env=args.output.resolve()/"compose.env"
    fd=os.open(compose_env,os.O_WRONLY|os.O_CREAT|os.O_EXCL,0o600)
    with os.fdopen(fd,"w") as stream:
        stream.write(f"FACTORYBOX_WORKER_IMAGE={image}\nFACTORYBOX_STORE_ID={store}\nFACTORYBOX_STORE_VOLUME={volume}\n")
        stream.flush();os.fsync(stream.fileno())
    docker = (["sudo", "-n"] if args.sudo else []) + ["docker", "--context", args.context]
    compose = ["compose", "--env-file",str(compose_env), "--project-name", args.project, "--file", str(Path(__file__).with_name("compose.yaml"))]
    deadline = time.monotonic() + 420
    journal = {"schema": 1, "store_id": store, "image": image, "project": args.project,
               "passed": False, "steps": []}

    def save():
        target = args.output / "workflow.json"
        with target.open("w") as stream:
            json.dump(journal, stream, indent=2)
            stream.write("\n"); stream.flush(); os.fsync(stream.fileno())

    def run(arguments, *, record=True, timeout=60, binary=False):
        started = time.monotonic()
        remaining = deadline - started
        if remaining <= 0:
            raise TimeoutError("complete example exceeded its 420-second deadline")
        completed = subprocess.run(docker + arguments, capture_output=True, text=not binary,
                                   timeout=min(timeout, remaining), check=False)
        if record:
            journal["steps"].append({"arguments": arguments, "seconds": time.monotonic()-started,
                                     "exit": completed.returncode, "stdout": completed.stdout, "stderr": completed.stderr})
            save()
        if completed.returncode:
            raise RuntimeError("administrative command failed; exact response retained: " + " ".join(arguments))
        return completed.stdout if binary else completed.stdout.strip()

    def inspect(identity):
        row = json.loads(run(["container", "inspect", identity], record=False))[0]
        labels = row["Config"].get("Labels", {})
        mounts = [m for m in row["Mounts"] if m["Destination"] == "/var/lib/sysbox"]
        if (row["Id"] != identity or labels.get("com.docker.compose.project") != args.project or
                labels.get("com.docker.compose.service") != "worker" or labels.get("io.factorybox.store-id") != store or
                row["Image"] != image_id or len(mounts) != 1 or mounts[0].get("Name") != volume or not mounts[0]["RW"]):
            raise ValueError("worker identity, image, project or persistent mount differs")
        return row

    def worker_identity():
        identity = run(compose + ["ps", "--all", "--quiet", "worker"], record=False)
        if not re.fullmatch(r"[0-9a-f]{64}", identity):
            raise ValueError("requires exactly one dedicated worker, already started by this Compose example")
        inspect(identity)
        return identity

    def status(identity):
        for _ in range(150):
            if not inspect(identity)["State"]["Running"]:
                raise ValueError("owned worker exited before readiness")
            try:
                # Reading through Docker's archive endpoint creates no exec
                # task while PID 1 is delegating its initially empty root.
                raw = run(["cp", identity+":/run/factorybox/worker.json", "-"],
                          record=False, timeout=5, binary=True)
                if len(raw) > 2 << 20:
                    raise ValueError("worker readiness archive exceeds its bound")
                with tarfile.open(fileobj=io.BytesIO(raw), mode="r:") as archive:
                    members = archive.getmembers()
                    if len(members) != 1 or not members[0].isfile() or members[0].size > 1 << 20:
                        raise ValueError("worker readiness is not one bounded regular record")
                    hint = json.load(archive.extractfile(members[0]))
                if not isinstance(hint, dict) or hint.get("schema") != 1 or hint.get("store_id") != store:
                    raise ValueError("worker readiness schema/store differs")
                if not hint.get("ready_at"):
                    time.sleep(.1)
                    continue
                # Publication is only a hint. Verify live supervisor, service
                # identities, lease and socket peers with the real CLI.
                value = json.loads(run(["exec", identity, "/opt/factorybox/factorybox", "worker-status"], record=False, timeout=5))
                if value["schema"] != 1 or value["store_id"] != store:
                    raise ValueError("worker status schema/store differs")
                return value
            except (RuntimeError, tarfile.TarError, json.JSONDecodeError):
                time.sleep(.1)
        raise TimeoutError("worker did not publish verified readiness")

    def ctl(identity, *arguments):
        inspect(identity)
        value = json.loads(run(["exec", identity, "/opt/factorybox/factorybox",
                                "--worker-run-root", "/run/factorybox", *arguments], timeout=90))
        if value.get("version", value.get("record", {}).get("version")) != 1:
            raise ValueError("unsupported lifecycle response version")
        return value

    try:
        saved_volume = json.loads(run(["volume", "inspect", volume], record=False))[0]
        if saved_volume.get("Labels", {}).get("io.factorybox.store-id") != store:
            raise ValueError("persistent volume lacks the exact administrative store label")
        image_id = json.loads(run(["image", "inspect", image], record=False))[0]["Id"]
        old = worker_identity()
        previous = status(old)
        journal["old_worker"] = {"container_id": old, "status": previous}
        token = uuid.uuid4().hex
        source, recovered, child = (prefix + token[:12] for prefix in ["source-", "recovered-", "child-"])
        first, second, third = (uuid.uuid4().hex for _ in range(3))
        ctl(old, "start", "--bundle", args.bundle, source)
        ctl(old, "pause", "--snapshot-id", first, source)
        ctl(old, "resume", "--snapshot-id", first, source)
        paused = ctl(old, "pause", "--snapshot-id", second, source)["record"]
        pause = paused["pause"]
        if (paused["phase"] != "paused" or not pause.get("completed_at") or
                not re.fullmatch(r"[0-9a-f]{64}", pause.get("checkpoint_sha256", "")) or
                pause["after"]["anon"] != 0 or pause["snapshot"]["resident_bytes_after"] != 0):
            raise ValueError("pause lacks full durable commit and measured snapshot-RAM release")
        request = ["recover-paused", "--source-root", previous["lifecycle"]["root"], "--source", source,
                   "--snapshot-id", second, "--source-owner", paused["owner"],
                   "--checkpoint-sha256", pause["checkpoint_sha256"], recovered]
        journal["recovery_request"] = request
        save()
        inspect(old)
        run(compose + ["stop", "worker"], timeout=20)
        if inspect(old)["State"]["Running"]:
            raise ValueError("old worker is still alive")
        run(compose + ["rm", "--force", "worker"])
        run(compose + ["up", "--detach", "--no-deps", "--pull", "never", "worker"])
        new = worker_identity()
        current = status(new)
        if new == old or current["epoch"] == previous["epoch"]:
            raise ValueError("worker was not fully recreated")
        journal["new_worker"] = {"container_id": new, "status": current}
        restored = ctl(new, *request)
        repeated = ctl(new, *request)
        if restored["runtime"]["pid"] != repeated["runtime"]["pid"] or restored["record"]["owner"] != repeated["record"]["owner"]:
            raise ValueError("exact recovery replay duplicated its target")
        ctl(new, "checkpoint", "--snapshot-id", third, recovered)
        ctl(new, "fork", "--source", recovered, "--snapshot-id", third, child)
        ctl(new, "delete", recovered)
        if ctl(new, "state", child)["runtime"]["status"] != "running":
            raise ValueError("descendant stopped after source deletion")
        ctl(new, "delete", child)
        journal["passed"] = True
        print(json.dumps({"passed": True, "responses": str(args.output / "workflow.json")}))
    except Exception as error:
        journal["error"] = str(error)
        raise
    finally:
        save()


if __name__ == "__main__":
    main()
