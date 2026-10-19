#!/usr/bin/env python3
"""Freeze the example's exact public project images for offline native use."""
import datetime
import json
import re
import subprocess
import uuid

import devx_inputs
import reference
import reference_lifecycle

ROOT = reference.ROOT


def load(name):
    if not re.fullmatch(r"factorybox-devx-project-images-[0-9a-f]{32}", name):
        raise ValueError("requires one exact project-image archive")
    archive = ROOT / "artifacts" / name
    result = json.loads((archive / "result.json").read_text())
    compose = ROOT / "examples/factory/.devcontainer/compose.yaml"
    images = list(dict.fromkeys(re.findall(r"^\s+image:\s+(\S+)\s*$", compose.read_text(), re.MULTILINE)))
    if (not result.get("passed") or result["compose_sha256"] != reference.sha(compose) or
            [row["image"] for row in result["images"]] != images or
            any(row["canonical_image"] != devx_inputs.canonical_digest(row["image"])
                for row in result["images"]) or
            devx_inputs.file_sha(archive / "images.tar") != result["archive_sha256"]):
        raise ValueError("project image archive or selected recipe changed")
    return archive, result


def main():
    compose = ROOT / "examples/factory/.devcontainer/compose.yaml"
    images = re.findall(r"^\s+image:\s+(\S+)\s*$", compose.read_text(), re.MULTILINE)
    if len(images) != 2 or any(not devx_inputs.PIN.fullmatch(i) for i in images):
        raise ValueError("example must name exact image digests for both services")
    images = list(dict.fromkeys(images))
    owner = uuid.uuid4().hex
    archive = ROOT / "artifacts" / ("factorybox-devx-project-images-" + owner)
    archive.mkdir()
    result = {
        "schema": 1, "owner": owner, "started_at": datetime.datetime.now(datetime.timezone.utc).isoformat(),
        "compose_sha256": reference.sha(compose), "images": [], "passed": False,
        "production_qualified": False,
        "scope": "Read/acquire and save the example's exact public image digests. "
                 "No mutable image tag or running installation is replaced. "
                 "Offline digest retention must still be checked against the consumer daemon.",
    }
    failure = None
    try:
        for number, image in enumerate(images):
            try:
                facts = devx_inputs.inspect_image(image)
            except subprocess.CalledProcessError:
                with (archive / ("pull-%d.log" % number)).open("x") as log:
                    subprocess.run([*reference.DOCKER, "pull", "--platform=linux/arm64", image],
                                   stdout=log, stderr=subprocess.STDOUT, check=True, timeout=120)
                facts = devx_inputs.inspect_image(image)
            result["images"].append(facts)
        command = [*reference.DOCKER, "image", "save", "--output", str(archive / "images.tar"), *images]
        result["command"] = command
        reference_lifecycle.write_json(archive / "intent.json", result)
        print(json.dumps({"saving": str(archive)}), flush=True)
        with (archive / "save.log").open("x") as log:
            subprocess.run(command, stdout=log, stderr=subprocess.STDOUT, check=True, timeout=120)
        result["archive_sha256"] = devx_inputs.file_sha(archive / "images.tar")
        result["archive_bytes"] = (archive / "images.tar").stat().st_size
        result["passed"] = True
    except Exception as error:
        failure = error
        result["error"] = str(error)
    finally:
        reference_lifecycle.write_json(archive / "result.json", result)
        print(json.dumps({"evidence": str(archive), **result}), flush=True)
    if failure:
        raise RuntimeError("example image archive failed: " + str(archive)) from failure


if __name__ == "__main__":
    main()
