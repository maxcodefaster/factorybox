#!/usr/bin/env python3
"""Freeze the example's exact public project images for offline native use."""
import argparse
import datetime
import json
import re
import subprocess
import uuid

import build_config
import devx_inputs
import reference
import reference_lifecycle

ROOT = reference.ROOT


def example_images(example):
    if example == "factory":
        recipe = ROOT / "examples/factory/.devcontainer/compose.yaml"
        images = list(dict.fromkeys(re.findall(r"^\s+image:\s+(\S+)\s*$", recipe.read_text(), re.MULTILINE)))
    elif example == "minimal":
        recipe = ROOT / "examples/minimal/.devcontainer/devcontainer.json"
        images = [json.loads(recipe.read_text())["image"]]
    else:
        raise ValueError("unknown example")
    if any(not devx_inputs.PIN.fullmatch(image) for image in images):
        raise ValueError("example images require exact digests")
    return recipe, images


def load(name, *, platform="linux/arm64", example="factory"):
    build_config.architecture(platform)
    if not re.fullmatch(r"factorybox-devx-project-images-[0-9a-f]{32}", name):
        raise ValueError("requires one exact project-image archive")
    archive = ROOT / "artifacts" / name
    result = json.loads((archive / "result.json").read_text())
    compose, images = example_images(example)
    if (not result.get("passed") or result.get("platform", "linux/arm64") != platform or
            result.get("example", "factory") != example or
            result["compose_sha256"] != reference.sha(compose) or
            [row["image"] for row in result["images"]] != images or
            any(row["canonical_image"] != devx_inputs.canonical_digest(row["image"])
                for row in result["images"]) or
            devx_inputs.file_sha(archive / "images.tar") != result["archive_sha256"]):
        raise ValueError("project image archive or selected recipe changed")
    return archive, result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--platform", choices=build_config.PLATFORMS, default="linux/arm64")
    parser.add_argument("--example", choices=["factory", "minimal"], default="factory")
    options = parser.parse_args()
    compose, images = example_images(options.example)
    if options.example == "factory" and len(images) != 2:
        raise ValueError("Factory example requires both exact service images")
    owner = uuid.uuid4().hex
    archive = reference.new_artifact("factorybox-devx-project-images-" + owner)
    archive.mkdir()
    result = {
        "schema": 1, "owner": owner, "started_at": datetime.datetime.now(datetime.timezone.utc).isoformat(),
        "compose_sha256": reference.sha(compose), "example": options.example, "images": [], "passed": False,
        "production_qualified": False,
        "platform": options.platform, "native_lifecycle_qualified": False,
        "scope": "Read/acquire and save the example's exact public image digests. "
                 "No mutable image tag or running installation is replaced. "
                 "Offline digest retention must still be checked against the consumer daemon.",
    }
    failure = None
    try:
        for number, image in enumerate(images):
            try:
                facts = devx_inputs.inspect_image(image, platform=options.platform)
            except subprocess.CalledProcessError:
                with (archive / ("pull-%d.log" % number)).open("x") as log:
                    subprocess.run([*reference.DOCKER, "pull", "--platform=" + options.platform, image],
                                   stdout=log, stderr=subprocess.STDOUT, check=True, timeout=120)
                facts = devx_inputs.inspect_image(image, platform=options.platform)
            result["images"].append(facts)
        command = [*reference.DOCKER, "image", "save", *images]
        result["command"] = command
        reference_lifecycle.write_json(archive / "intent.json", result)
        print(json.dumps({"saving": str(archive)}), flush=True)
        with (archive / "images.tar").open("xb") as payload, (archive / "save.log").open("x") as log:
            subprocess.run(command, stdout=payload, stderr=log, check=True, timeout=120)
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
