#!/usr/bin/env python3
"""One real pinned OpenCode external free-model tool run, without lifecycle claims."""
import argparse
import json
import os
from pathlib import Path
import shutil
import subprocess
import tarfile
import tempfile
import threading
import uuid

import consumer_worker_loss
import devx_inputs
import free_model_probe
import model_relay
import reference
import reference_lifecycle
import upstream_unit


def capture_output(name, owner, artifact):
    saved = json.loads(reference.docker(["container", "inspect", name]))[0]
    if saved["Name"] != "/" + name or saved["Config"].get("Labels", {}).get("com.factorybox.run") != owner:
        raise ValueError("refusing to copy foreign consumer output")
    if saved["State"]["Running"]:
        reference.docker(["container", "stop", "--time=2", saved["Id"]], timeout=5)
    # Retain Docker's exact transport, then extract as the local caller. Sudo
    # Docker cp otherwise leaves root-owned mode-0700 output in capacity scans.
    with (artifact / "output.tar").open("xb") as output_tar, (artifact / "copy.stderr").open("xb") as errors:
        copied = subprocess.run([*reference.DOCKER, "cp", saved["Id"] + ":/experiment/.", "-"], stdout=output_tar, stderr=errors, timeout=10)
    copied.check_returncode()
    with tarfile.open(artifact / "output.tar") as transport:
        members = transport.getmembers()
        if len(members) > 4096 or sum(member.size for member in members) > 32 << 20:
            raise ValueError("synthetic output archive exceeds its bound")
        transport.extractall(artifact / "output", filter="data")
    return copied.returncode


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--consumer-inputs", required=True)
    parser.add_argument("--model", default="glm-5.3-flash-free")
    options = parser.parse_args()
    _, consumers = devx_inputs.load(options.consumer_inputs, rootfs=True, platform="linux/amd64")
    image = consumers["images"]["build-runtime-host"]
    owner = uuid.uuid4().hex
    name = "factorybox-external-opencode-" + owner
    artifact = reference.new_artifact(name)
    artifact.mkdir()
    shutil.copytree(reference.ROOT / "tests/opencode-external", artifact / "input")
    shutil.copyfile(reference.ROOT / "tests/opencode-flow/held-tool.cjs", artifact / "input/held-tool.cjs")
    shutil.copyfile(__file__, artifact / "runner.py")
    for helper in ("model_relay.py", "free_model_probe.py"):
        shutil.copyfile(reference.ROOT / "tools" / helper, artifact / helper)
    result = {"schema": 1, "owner": owner, "passed": False, "cleanup": False, "image": image,
              "consumer_inputs": options.consumer_inputs, "model": options.model,
              "production_qualified": False, "lifecycle_qualified": False,
              "scope": "Pinned OpenCode 2.0.27 real foreground shell, private RAM/watch and external free-model result ingestion only. No checkpoint or restore."}
    attempted = False
    before = consumer_worker_loss.sysctls()
    try:
        discovery = {}
        for label, url in (("catalog", free_model_probe.CATALOG), ("pricing", free_model_probe.PRICES)):
            body, observation = free_model_probe.exchange(url)
            (artifact / (label + ".body")).write_bytes(body)
            discovery[label] = observation
            if observation["status"] != 200:
                raise ValueError("current free-model discovery failed")
        result["discovery"] = discovery
        result["free_model_evidence"] = free_model_probe.approve_free_model(options.model,
            json.loads((artifact / "catalog.body").read_text()), (artifact / "pricing.body").read_text())
        reference_lifecycle.write_json(artifact / "input/model.json", {"id": options.model, "label": result["free_model_evidence"]["identity_row"][0]})
        result["inputs"] = upstream_unit.context_manifest(artifact / "input")
        relay_output = artifact / "provider"
        relay_output.mkdir()
        with tempfile.TemporaryDirectory(prefix="fbm-") as directory:
            socket = Path(directory) / "provider.sock"
            with model_relay.Relay(socket, relay_output, options.model) as relay:
                os.chmod(socket, 0o666)
                thread = threading.Thread(target=relay.serve_forever, daemon=True)
                thread.start()
                try:
                    reference.docker(["volume", "create", "--label", "com.factorybox.run=" + owner, name])
                    attempted = True
                    command = [*reference.DOCKER, "run", "--pull=never", "--name", name, "--label", "com.factorybox.run=" + owner,
                               "--cpus=3", "--memory=3g", "--pids-limit=128", "--network=none", "--read-only",
                               "--cap-drop=ALL", "--security-opt=no-new-privileges", "--tmpfs", "/run:rw,nosuid,nodev,size=4m",
                               "--tmpfs", "/tmp:rw,nosuid,nodev,size=64m", "--tmpfs", "/var/lib/docker:rw,nosuid,nodev,size=16m",
                               "--mount", f"type=volume,source={name},target=/experiment",
                               "--mount", f"type=bind,source={artifact/'input'},target=/opt/factorybox/opencode-external,readonly",
                               "--mount", f"type=bind,source={socket},target=/relay/provider.sock,readonly",
                               "--entrypoint", "/usr/local/bin/node", image["image"], "/opt/factorybox/opencode-external/diagnostic.cjs"]
                    result["command"] = command
                    reference_lifecycle.write_json(artifact / "intent.json", result)
                    print(json.dumps({"running": str(artifact)}), flush=True)
                    with (artifact / "stdout.txt").open("x") as out, (artifact / "stderr.txt").open("x") as err:
                        done = subprocess.run(command, stdout=out, stderr=err, timeout=120)
                    result["exit_code"] = done.returncode
                    result["copy_exit"] = capture_output(name, owner, artifact)
                    result["state"] = json.loads(reference.docker(["container", "inspect", name]))[0]["State"]
                    result["inputs_unchanged"] = result["inputs"] == upstream_unit.context_manifest(artifact / "input")
                    result["requests"] = list(relay.records)
                    result["passed"] = done.returncode == 0 and not result["state"]["OOMKilled"] and result["inputs_unchanged"] and len(relay.records) >= 2 and all(record["passed"] for record in relay.records)
                    if not result["passed"]:
                        raise ValueError("actual pinned OpenCode external tool/result probe failed")
                finally:
                    relay.shutdown()
                    thread.join(timeout=2)
    except Exception as error:
        result["error"] = str(error)
    finally:
        if attempted:
            if not (artifact / "output.tar").exists():
                try:
                    result["failure_copy_exit"] = capture_output(name, owner, artifact)
                except Exception as error:
                    result["failure_copy_error"] = str(error)
            reference.owned_delete("container", name, owner)
            reference.owned_delete("volume", name, owner)
        result["global_sysctls_unchanged"] = before == consumer_worker_loss.sysctls()
        result["cleanup"] = True
        result["passed"] = result["passed"] and result["global_sysctls_unchanged"]
        reference_lifecycle.write_json(artifact / "result.json", result)
        print(json.dumps({"evidence": str(artifact), "passed": result["passed"], "cleanup": result["cleanup"], "error": result.get("error")}), flush=True)
    return 0 if result["passed"] else 1


if __name__ == "__main__":
    raise SystemExit(main())
