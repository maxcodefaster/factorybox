"""Resolve a successful explicit build of the public-input native fixture."""
import json
import re

import reference


def verify_image(result, inspected):
    manifest = result["image"].split("@", 1)[1]
    if (inspected["Id"] not in {manifest, result["image_config"]} or
            result["image"] not in inspected.get("RepoDigests", []) or
            inspected["Architecture"] != "arm64" or inspected["Os"] != "linux"):
        raise ValueError("native base image identity/platform differs")


def load(name):
    if not re.fullmatch(r"factorybox-build-native-base-[0-9a-f]{32}", name):
        raise ValueError("native base must name one exact local build artifact")
    archive = reference.ROOT / "artifacts" / name
    result = json.loads((archive / "result.json").read_text())
    if not result.get("passed") or not result.get("cleanup"):
        raise ValueError("native base build or inspection cleanup did not pass")
    dockerfile = reference.ROOT / "build/native-base.Dockerfile"
    if reference.sha(dockerfile) != result["dockerfile_sha256"]:
        raise ValueError("native base recipe changed since its build")
    if reference.sha(archive / "packages.tsv") != result["packages_sha256"]:
        raise ValueError("native base package manifest changed")
    image = result["image"]
    if not re.fullmatch(re.escape(name) + r"@sha256:[0-9a-f]{64}", image):
        raise ValueError("native base image is not its pinned build output")
    return image, result
