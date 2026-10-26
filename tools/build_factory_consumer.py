#!/usr/bin/env python3
"""Build exact local Factory consumer and Linux Bun fixture images; never publish."""
import argparse
import json
from pathlib import Path
import re
import shutil
import subprocess
import uuid

import build_config
import native_base
import reference
import reference_lifecycle
import upstream_unit


INPUTS = [
    ".dockerignore",
    "upstream.json",
    "deploy/runtime-host/Dockerfile",
    "deploy/language-tools/package.json",
    "deploy/language-tools/package-lock.json",
    "deploy/opencode-server",
    "services/runtime-connector",
]
DIRECTORIES = {"deploy/opencode-server", "services/runtime-connector"}
REQUIRED_RUNTIME_FILES = [
    "services/runtime-connector/go.mod",
    "services/runtime-connector/go.sum",
    "deploy/opencode-server/package.json",
    "deploy/opencode-server/bun.lock",
    "deploy/opencode-server/server.mjs",
    "deploy/opencode-server/retained-events.mjs",
    "deploy/opencode-server/install-skills.mjs",
    "deploy/opencode-server/config/AGENTS.md",
    "deploy/opencode-server/config/opencode.json",
]
TARGETS = {"runtime": "runtime-host", "hub": "bun-tool"}
BUN = "/opt/factory/opencode/musl/bun"


def source_files(repo):
    repo = Path(repo).resolve()
    paths = subprocess.check_output(
        ["git", "-C", str(repo), "ls-files", "--cached", "--others", "--exclude-standard",
         "-z", "--", *INPUTS]).decode().split("\0")
    selected = sorted(set(path for path in paths if path))
    required = [path for path in INPUTS if path not in DIRECTORIES]
    required += REQUIRED_RUNTIME_FILES
    if any(path not in selected for path in required):
        raise ValueError("Factory consumer source is missing an exact Dockerfile/toolchain input")
    for name in selected:
        path = repo / name
        if path.is_symlink() or path.resolve() != path or not path.is_file():
            raise ValueError("Factory consumer source must contain regular owned files")
    return selected


def build_command(options, role, tag, context, metadata):
    if role not in TARGETS:
        raise ValueError("unknown concrete Factory consumer image role")
    return [*reference.DOCKER, "buildx", "build", "--provenance=false", "--progress=plain",
            *build_config.arguments(options), "--target", TARGETS[role], "--load",
            "--tag", tag, "--file", str(context / "deploy/runtime-host/Dockerfile"),
            "--metadata-file", str(metadata), str(context)]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    build_config.add_arguments(parser, required_platform=True)
    parser.add_argument("--factory-repo", type=Path, required=True)
    options = parser.parse_args()
    if not options.bounded_build:
        parser.error("Factory consumer compilation requires --bounded-build and an exact --builder")
    host = json.loads(reference.docker(["info", "--format", "{{json .}}"]))
    native_base.require_native_host(options.platform, host)
    bounded = build_config.verify_builder(options, reference.DOCKER)
    repo = options.factory_repo.resolve()
    files = source_files(repo)
    owner = uuid.uuid4().hex
    archive = reference.new_artifact("factorybox-build-consumer-" + owner)
    archive.mkdir()
    context = archive / "context"
    for name in files:
        target = context / name
        target.parent.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(repo / name, target)
    shutil.copyfile(__file__, archive / "runner.py")
    manifest = upstream_unit.context_manifest(context)
    result = {"schema": 1, "owner": owner, "platform": options.platform,
              "factory_commit": subprocess.check_output(["git", "-C", str(repo), "rev-parse", "HEAD"], text=True).strip(),
              "source_context": manifest, "bounded_builder": bounded, "images": {},
              "passed": False, "cleanup": False, "native_lifecycle_qualified": False,
              "production_qualified": False,
              "scope": "Local native Factory runtime-host plus separate Linux Bun fixture image from exact frozen Factory Dockerfile, source and pins. No publish, installation or runtime/platform admission."}
    print(json.dumps({"building": str(archive)}), flush=True)
    helpers = set()
    try:
        for role in TARGETS:
            tag = "factorybox-consumer-" + role + "-" + owner
            metadata_path = archive / (role + "-metadata.json")
            command = build_command(options, role, tag, context, metadata_path)
            row = {"platform": options.platform, "command": command}
            result["images"][role] = row
            reference_lifecycle.write_json(archive / "intent.json", result)
            with (archive / (role + "-build.log")).open("x") as log:
                subprocess.run(command, stdout=log, stderr=subprocess.STDOUT,
                               check=True, timeout=900)
            metadata = json.loads(metadata_path.read_text())
            row["image"] = tag + "@" + metadata["containerimage.digest"]
            inspected = json.loads(reference.docker(["image", "inspect", row["image"]]))[0]
            native_base.bind_image_identity(row, metadata, inspected)
            row["image_id"] = inspected["Id"]
            binary = "/usr/local/bin/factory-runtime-connector" if role == "runtime" else BUN
            helper = "factorybox-consumer-inspect-" + owner + "-" + role
            script = "set -eu; sha256sum " + binary + "; head -c 20 " + binary + " | od -An -tx1"
            if role == "hub":
                script += "; " + BUN + " --version"
            volumes = inspected["Config"].get("Volumes") or {}
            if any(not re.fullmatch(r"/[A-Za-z0-9_./-]+", path) or path == "/" for path in volumes):
                raise ValueError("consumer image declares an invalid inspection volume")
            inspection = [*reference.DOCKER, "run", "--pull=never", "--name", helper,
                          "--label", "com.factorybox.run=" + owner, "--network=none",
                          "--read-only", "--cpus=.5", "--memory=128m", "--pids-limit=32",
                          "--cap-drop=ALL", "--security-opt=no-new-privileges",
                          *[argument for path in volumes for argument in
                            ["--tmpfs", path + ":rw,size=16m"]],
                          "--entrypoint", "/bin/sh", row["image"], "-ec", script]
            row["inspection_command"] = inspection
            helpers.add(helper)
            try:
                done = subprocess.run(inspection, capture_output=True, text=True, check=True, timeout=65)
                (archive / (role + "-inspection.stdout")).write_text(done.stdout)
                (archive / (role + "-inspection.stderr")).write_text(done.stderr)
                lines = done.stdout.splitlines()
                digest, filename = lines[0].split()
                if filename != binary or len(digest) != 64 or any(c not in "0123456789abcdef" for c in digest):
                    raise ValueError("native consumer executable digest differs")
                header_lines = lines[1:-1] if role == "hub" else lines[1:]
                header = archive / (role + "-elf-header")
                header.write_bytes(bytes.fromhex("".join(header_lines)))
                build_config.require_elf(header, options.platform)
                row["binary_sha256"] = digest
                if role == "hub":
                    if lines[-1] != "1.4.3":
                        raise ValueError("native consumer fixture Bun differs from Factory's exact pin")
                    row["bun_version"] = lines[-1]
            finally:
                reference.owned_delete("container", helper, owner)
                helpers.remove(helper)
        result["image"] = result["images"]["runtime"]["image"]
        result["hub_image"] = result["images"]["hub"]["image"]
        result["hub_bun"] = BUN
        result["inputs_unchanged"] = upstream_unit.context_manifest(context) == manifest
        result["builder_unchanged"] = build_config.verify_builder(options, reference.DOCKER) == bounded
        if not result["inputs_unchanged"] or not result["builder_unchanged"]:
            raise ValueError("consumer build inputs or bounded builder changed")
        result["passed"] = True
    except Exception as error:
        result["error"] = str(error)
        raise
    finally:
        for helper in list(helpers):
            try:
                reference.owned_delete("container", helper, owner)
                helpers.remove(helper)
            except Exception as error:
                result.setdefault("cleanup_errors", []).append(str(error))
        result["cleanup"] = not helpers
        result["passed"] = result["passed"] and result["cleanup"]
        reference_lifecycle.write_json(archive / "result.json", result)
        print(json.dumps({"evidence": str(archive), "passed": result["passed"],
                          "cleanup": result["cleanup"], "error": result.get("error")}), flush=True)


if __name__ == "__main__":
    main()
