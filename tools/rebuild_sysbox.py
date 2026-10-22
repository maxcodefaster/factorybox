#!/usr/bin/env python3
"""Recompile exact Sysbox inputs offline in a verified frozen build environment."""
import argparse
import datetime
import json
import re
import shutil
import subprocess
import uuid

import native_base
import reference
import reference_lifecycle
import upstream_unit


def offline_recipe(source):
    anchor = "COPY sysbox-runc /src/sysbox-runc\n"
    if source.count(anchor) != 1:
        raise ValueError("Sysbox recipe source boundary changed")
    body = anchor + source.split(anchor, 1)[1]
    pattern = (r"RUN --mount=type=cache,id=factorybox-go-mod-\$\{FACTORYBOX_BUILD_CACHE\},target=/root/go/pkg/mod \\\n"
               r"    --mount=type=cache,id=factorybox-go-build-\$\{FACTORYBOX_BUILD_CACHE\},target=/root/.cache/go-build \\\n    ")
    body, count = re.subn(pattern, "RUN ", body)
    if count != 6 or "--mount=" in body or "apt-get" in body:
        raise ValueError("Sysbox cache/build recipe changed; review the offline derivation")
    generator = "go install github.com/golang/protobuf/protoc-gen-go@v1.5.4"
    if body.count(generator) != 1:
        raise ValueError("pinned protobuf generator step changed")
    # The generator is part of the frozen build toolchain, like protoc and cc.
    # Reinstalling it queries uncached proxy metadata, even with a pinned version.
    body = body.replace(generator, "(go version -m /root/go/bin/protoc-gen-go | "
                        "awk '$1 == \"mod\" && $2 == \"github.com/golang/protobuf\" && "
                        "$3 == \"v1.5.4\" { found=1 } END { exit !found }') && "
                        "sha256sum /root/go/bin/protoc-gen-go > /out/protoc-gen-go.sha256")
    return (
        "ARG FROZEN_BUILDER_IMAGE\nFROM ${FROZEN_BUILDER_IMAGE} AS build\nUSER root\n"
        "ENV GOPROXY=off GOSUMDB=off GOTOOLCHAIN=local GOCACHE=/tmp/factorybox-clean-go-cache\n"
        "RUN rm -rf /src /out /tmp/factorybox-clean-go-cache && mkdir -p /src /out\n" + body)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--sysbox-build", required=True)
    parser.add_argument("--builder-run", required=True,
                        help="Exact completed upstream run containing its frozen dependency image")
    options = parser.parse_args()
    root = reference.ROOT
    original, expected = reference_lifecycle.component_build(options.sysbox_build, "factorybox-build-sysbox-")
    if not re.fullmatch(r"factorybox-upstream-[0-9a-f]{32}", options.builder_run):
        parser.error("expected one exact upstream run")
    previous = root / "artifacts" / options.builder_run
    proof = json.loads((previous / "result.json").read_text())
    if (not proof.get("passed") or not proof.get("cleanup") or
            proof["component_build"] != original.name or not proof.get("offline_image")):
        raise ValueError("builder run does not prove this exact component build")
    target = expected.get("platform", "linux/arm64")
    image = proof["offline_image"]
    inspected = json.loads(reference.docker(["image", "inspect", image]))[0]
    native_base.verify_image({"image": image, "image_config": inspected["Id"], "platform": target}, inspected)
    context_manifest = json.loads((original / "context-manifest.json").read_text())
    if upstream_unit.context_manifest(original / "context") != context_manifest:
        raise ValueError("original Sysbox context changed")
    recipe = original / "Dockerfile"
    if reference.sha(recipe) != expected["dockerfile_sha256"]:
        raise ValueError("original Sysbox recipe changed")
    for name, digest in expected["binary_sha256"].items():
        if reference.sha(original / "output" / name) != digest:
            raise ValueError("comparison executable changed")
    owner = uuid.uuid4().hex
    archive = reference.new_artifact("factorybox-build-sysbox-repro-" + owner)
    archive.mkdir()
    shutil.copytree(original / "context", archive / "context", symlinks=True)
    shutil.copyfile(original / "source-lock.json", archive / "source-lock.json")
    shutil.copyfile(recipe, archive / "original.Dockerfile")
    shutil.copyfile(__file__, archive / "runner.py")
    (archive / "Dockerfile").write_text(offline_recipe(recipe.read_text()))
    output = archive / "output"
    output.mkdir()
    result = {
        "schema": 1, "owner": owner, "comparison_build": original.name,
        "builder_run": options.builder_run, "builder_image": image, "platform": target,
        "started_at": datetime.datetime.now(datetime.timezone.utc).isoformat(),
        "source_lock_sha256": reference.sha(archive / "source-lock.json"),
        "source_context": context_manifest, "recipe_sha256": reference.sha(archive / "Dockerfile"),
        "passed": False, "production_qualified": False, "native_lifecycle_qualified": False,
        "scope": "Offline recompilation with an empty Go compiler cache, exact original runtime sources, "
                 "compiler commands, assertions and verified frozen dependency image. The pinned v1.5.4 "
                 "protobuf generator is a verified frozen build tool, alongside protoc and cc. No compiled /out or /src "
                 "from that image is retained. This does not repeat APT/network acquisition or qualify lifecycle.",
    }
    command = [*reference.DOCKER, "build", "--no-cache", "--network=none",
               "--platform=" + target, "--resource=memory=3g", "--resource=cpu-period=100000",
               "--resource=cpu-quota=300000", "--provenance=false", "--progress=plain",
               "--build-arg", "FROZEN_BUILDER_IMAGE=" + image,
               "-f", str(archive / "Dockerfile"), "--output", "type=local,dest=" + str(output),
               str(archive / "context")]
    result["command"] = command
    reference_lifecycle.write_json(archive / "intent.json", result)
    print(json.dumps({"building": str(archive)}), flush=True)
    failure = None
    try:
        with (archive / "build.log").open("x") as log:
            done = subprocess.run(command, stdout=log, stderr=subprocess.STDOUT, timeout=900)
        result["exit_code"] = done.returncode
        if done.returncode:
            raise ValueError("offline Sysbox recompilation failed")
        result["binary_sha256"] = {name: reference.sha(output / name) for name in expected["binary_sha256"]}
        result["packages_sha256"] = reference.sha(output / "packages.tsv")
        result["inputs_unchanged"] = (
            upstream_unit.context_manifest(archive / "context") == context_manifest and
            upstream_unit.context_manifest(original / "context") == context_manifest and
            reference.sha(archive / "Dockerfile") == result["recipe_sha256"])
        result["matches_comparison"] = result["binary_sha256"] == expected["binary_sha256"]
        result["packages_match_comparison"] = result["packages_sha256"] == expected["packages_sha256"]
        if not all(result[k] for k in ["inputs_unchanged", "matches_comparison", "packages_match_comparison"]):
            raise ValueError("offline Sysbox output or frozen input differs")
        result["passed"] = True
    except Exception as error:
        result["error"] = str(error)
        failure = error
    finally:
        reference_lifecycle.write_json(archive / "result.json", result)
        print(json.dumps({"evidence": str(archive), **result}), flush=True)
    if failure:
        raise RuntimeError("offline Sysbox reproducibility failed: " + str(archive)) from failure


if __name__ == "__main__":
    main()
