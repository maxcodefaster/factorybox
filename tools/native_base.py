"""Resolve a successful explicit build of the public-input native fixture."""
import json
import re

import reference
import build_config


def require_native_host(target, host):
    build_config.architecture(target)
    machines = {"linux/arm64": {"aarch64", "arm64"},
                "linux/amd64": {"x86_64", "amd64"}}
    if host["OSType"] != "linux" or host["Architecture"] not in machines[target]:
        raise ValueError("native tests require a matching Linux Docker host; emulation is not qualification")


def bind_image_identity(result, metadata, inspected):
    # Recent Docker image stores omit config.digest from Buildx metadata.
    # Inspect the exact build manifest, then pin the daemon's reported identity
    # and require its original repository digest and target architecture.
    configured = metadata.get("containerimage.config.digest")
    identity = configured if configured is not None else inspected["Id"]
    if not isinstance(identity, str) or not re.fullmatch(r"sha256:[0-9a-f]{64}", identity):
        raise ValueError("native base image identity is not a SHA-256 digest")
    result["image_config"] = identity
    result["image_identity_source"] = "build_metadata" if configured is not None else "manifest_inspection"
    verify_image(result, inspected)


def verify_image(result, inspected):
    manifest = result["image"].split("@", 1)[1]
    architecture = build_config.architecture(result.get("platform", "linux/arm64"))
    if (inspected["Id"] not in {manifest, result["image_config"]} or
            result["image"] not in inspected.get("RepoDigests", []) or
            inspected["Architecture"] != architecture or inspected["Os"] != "linux"):
        raise ValueError("native base image identity/platform differs")


def load(name, *, for_build=False, candidate_platform=None):
    if not re.fullmatch(r"factorybox-build-native-base-[0-9a-f]{32}", name):
        raise ValueError("native base must name one exact local build artifact")
    archive = reference.ROOT / "artifacts" / name
    result = json.loads((archive / "result.json").read_text())
    if not result.get("passed") or not result.get("cleanup"):
        raise ValueError("native base build or inspection cleanup did not pass")
    platform = result.get("platform", "linux/arm64")
    build_config.architecture(platform)
    if candidate_platform is not None:
        build_config.architecture(candidate_platform)
        if candidate_platform != platform:
            raise ValueError("native base differs from the explicit candidate platform")
    if platform != "linux/arm64" and not for_build and candidate_platform is None:
        raise ValueError("AMD64 native lifecycle is unqualified; this base is available only to build "
                         "commands or explicit candidate tests")
    dockerfile = reference.ROOT / "build/native-base.Dockerfile"
    if reference.sha(dockerfile) != result["dockerfile_sha256"]:
        raise ValueError("native base recipe changed since its build")
    if reference.sha(archive / "packages.tsv") != result["packages_sha256"]:
        raise ValueError("native base package manifest changed")
    image = result["image"]
    if not re.fullmatch(re.escape(name) + r"@sha256:[0-9a-f]{64}", image):
        raise ValueError("native base image is not its pinned build output")
    inspected = json.loads(reference.docker(["image", "inspect", image]))[0]
    verify_image(result, inspected)
    return image, result
