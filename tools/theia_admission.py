#!/usr/bin/env python3
"""Require the original small IDE setup to work before staging a developer workload."""
import argparse
import datetime
import json
from pathlib import Path
import re
import shutil
import time
import uuid

import build_config
import devx_inputs
import native_base
import reference
import reference_lifecycle
import theia_peer

ROOT = reference.ROOT


def qualify(image):
    began = time.monotonic()
    reference.verify()
    retained = []
    for command in [
        ["ps", "-a", "--filter", "label=com.factorybox.run", "--format", "{{.Names}}"],
        ["volume", "ls", "--filter", "label=com.factorybox.run", "--format", "{{.Name}}"],
    ]:
        retained.extend(name for name in reference.docker(command).splitlines()
                        if re.fullmatch(r"factorybox-theia-admission-[0-9a-f]{32}", name))
    if retained:
        raise ValueError("reconcile unreconciled IDE admission resources first: " +
                         ", ".join(sorted(set(retained))))
    owner = uuid.uuid4().hex
    name = "factorybox-theia-admission-" + owner
    archive = reference.new_artifact(name)
    archive.mkdir(mode=0o700)
    for source in [Path(__file__), ROOT / "tools/theia_peer.py"]:
        shutil.copyfile(source, archive / source.name)
    inputs = {p.name: reference.sha(p) for p in archive.iterdir() if p.is_file()}
    command = theia_peer.setup_command(name, name, owner, image, 1000, 1000)
    result = {
        "schema": 1, "owner": owner, "container_name": name, "volume_name": name,
        "started_at": datetime.datetime.now(datetime.timezone.utc).isoformat(),
        "image": image, "inputs_sha256": inputs, "setup_command": command,
        "passed": False, "cleanup": False, "production_qualified": False,
        "scope": "The original exact IDE setup recipe and 30s command deadline, on a new empty "
                 "owned volume with 1 CPU/256 MiB, 32 PIDs, no network and original capabilities. "
                 "UID/GID 1000 are disposable probe ownership, not Sysbox mapping proof. "
                 "A failure blocks rootfs staging; a pass warms this cached image and does not "
                 "qualify the full developer lifecycle or a cold-start latency.",
    }
    reference_lifecycle.write_json(archive / "intent.json", result)
    print(json.dumps({"theia_admission": str(archive)}), flush=True)
    volume_attempted = container_attempted = False
    failure = None
    try:
        volume_attempted = True
        reference.docker(["volume", "create", "--label", "com.factorybox.run=" + owner, name])
        container_attempted = True
        theia_peer.run_setup(command, archive, result)
        observed = json.loads(reference.docker(["container", "inspect", name]))[0]
        if (observed.get("Name") != "/" + name or
                observed["Config"].get("Labels", {}).get("com.factorybox.run") != owner):
            raise ValueError("IDE admission container ownership changed")
        host, state = observed["HostConfig"], observed["State"]
        result["container_state"] = state
        result["constraints"] = {key: host.get(key) for key in [
            "NanoCpus", "Memory", "PidsLimit", "ReadonlyRootfs", "NetworkMode",
            "Privileged", "CapAdd", "CapDrop", "SecurityOpt"]}
        if (observed["Image"] != image["id"] or observed["Config"]["User"] != "0:0" or
                host["NanoCpus"] != 1_000_000_000 or host["Memory"] != 256 << 20 or
                host["PidsLimit"] != 32 or not host["ReadonlyRootfs"] or host["Privileged"] or
                host["NetworkMode"] != "none" or host["CapDrop"] != ["ALL"] or
                {cap.removeprefix("CAP_") for cap in host.get("CapAdd") or []} != {"CHOWN", "DAC_OVERRIDE"} or
                "no-new-privileges" not in host["SecurityOpt"] or
                state["Running"] or state["OOMKilled"] or state["ExitCode"] != 0):
            raise ValueError("IDE admission differs from its original constraints or completed setup")
        result["passed"] = True
    except Exception as error:
        failure = error
        result["error"] = str(error)
    finally:
        try:
            if container_attempted:
                reference.owned_delete("container", name, owner)
            if volume_attempted:
                reference.owned_delete("volume", name, owner)
            result["cleanup"] = True
        except Exception as error:
            failure = failure or error
            result["cleanup_error"] = str(error)
        result["inputs_unchanged"] = all(reference.sha(archive / name) == expected
                                         for name, expected in inputs.items())
        result["passed"] = (result["passed"] and result["cleanup"] and
                            result["inputs_unchanged"] and failure is None)
        result["admission_seconds"] = time.monotonic() - began
        reference_lifecycle.write_json(archive / "result.json", result)
        print(json.dumps({"evidence": str(archive), **{key: result[key] for key in [
            "passed", "cleanup", "inputs_unchanged", "production_qualified"]}}), flush=True)
    if not result["passed"]:
        raise ValueError("IDE setup admission failed before developer rootfs staging: " + str(archive)) from failure
    return {"run": name, "result_sha256": reference.sha(archive / "result.json"),
            "image_id": image["id"], "seconds": result["admission_seconds"]}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--image", required=True, help="Exact cached native IDE image digest")
    parser.add_argument("--platform", choices=build_config.PLATFORMS, default="linux/arm64")
    options = parser.parse_args()
    host = json.loads(reference.docker(["info", "--format", "{{json .}}"]))
    native_base.require_native_host(options.platform, host)
    qualify(devx_inputs.inspect_image(options.image, platform=options.platform))


if __name__ == "__main__":
    main()
