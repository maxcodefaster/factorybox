#!/usr/bin/env python3
"""Pass an exact local native-base OCI image to BuildKit on disposable CI runners."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import shlex
import subprocess
import sys
import tarfile

BASE = re.compile(r"factorybox-build-native-base-[a-f0-9]{32}@sha256:[a-f0-9]{64}")
BLOB = re.compile(r"blobs/sha256/([a-f0-9]{64})")


def extract_layout(stream, directory, digest):
    """Accept only OCI descriptors and content-addressed regular blobs."""
    directory = Path(directory)
    directory.mkdir()
    (directory / "blobs" / "sha256").mkdir(parents=True)
    seen = set()
    with tarfile.open(fileobj=stream, mode="r|") as archive:
        for member in archive:
            name = member.name
            if name in seen:
                raise ValueError("duplicate OCI export entry")
            seen.add(name)
            if member.isdir() and name in {"blobs", "blobs/sha256"}:
                continue
            blob = BLOB.fullmatch(name)
            if not member.isfile() or (not blob and name not in {"index.json", "oci-layout", "manifest.json"}):
                raise ValueError("unexpected OCI export entry")
            source = archive.extractfile(member)
            total = 0
            checksum = hashlib.sha256()
            with (directory / name).open("xb") as output:
                while chunk := source.read(1024 * 1024):
                    total += len(chunk)
                    checksum.update(chunk)
                    output.write(chunk)
            if total != member.size or (blob and checksum.hexdigest() != blob.group(1)):
                raise ValueError("OCI blob content differs from its digest")
    if json.loads((directory / "oci-layout").read_text()) != {"imageLayoutVersion": "1.0.0"}:
        raise ValueError("unsupported OCI layout")
    index = json.loads((directory / "index.json").read_text())
    if (index.get("schemaVersion") != 2 or
            not any(item.get("digest") == digest for item in index.get("manifests", [])) or
            not (directory / "blobs" / "sha256" / digest.removeprefix("sha256:")).is_file()):
        raise ValueError("OCI export does not contain the exact native-base digest")


def inspect_base(docker, image, architecture):
    rows = json.loads(subprocess.check_output([docker, "--context", "default", "image", "inspect", image], text=True))
    if len(rows) != 1:
        raise ValueError("expected one exact native-base image")
    value = rows[0]
    if (image not in value.get("RepoDigests", []) or value.get("Os") != "linux" or
            value.get("Architecture") != architecture):
        raise ValueError("native-base image identity or platform changed")
    return {"id": value["Id"], "layers": value["RootFS"]["Layers"]}


def build_arguments(arguments, receipt):
    """Leave all commands alone except builds consuming this exact base."""
    if len(arguments) < 3 or arguments[:3] != ["--context", "default", "build"]:
        return arguments
    values = []
    for index, argument in enumerate(arguments):
        if argument == "--build-arg" and index + 1 < len(arguments):
            values.append(arguments[index + 1])
        elif argument.startswith("--build-arg="):
            values.append(argument.removeprefix("--build-arg="))
    bases = [value.removeprefix("NATIVE_BASE=") for value in values if value.startswith("NATIVE_BASE=")]
    if not bases:
        return arguments
    if bases != [receipt["image"]] or not BASE.fullmatch(receipt["image"]):
        raise ValueError("build requested a different or mutable native-base image")
    digest = receipt["image"].split("@", 1)[1]
    layout = Path(receipt["layout"])
    if not layout.is_absolute() or not (layout / "blobs" / "sha256" / digest.removeprefix("sha256:")).is_file():
        raise ValueError("verified native-base OCI layout is unavailable")
    context = receipt["image"] + "=oci-layout://" + str(layout) + "@" + digest
    return [*arguments[:3], "--build-context", context, *arguments[3:]]


def prepare(docker, image, architecture):
    if not BASE.fullmatch(image) or architecture not in {"arm64", "amd64"}:
        raise ValueError("expected one pinned native-base image and matching Linux architecture")
    # Keep the CLI's argv[0]. Some operator Docker clients are symlinks to a
    # multi-call executable which dispatches on the original basename.
    path = Path(docker).absolute()
    if not path.resolve(strict=True).is_file() or not os.access(path, os.X_OK):
        raise ValueError("Docker client is not an executable file")
    docker = str(path)
    before = inspect_base(docker, image, architecture)
    # This helper is checked out separately from the immutable release source.
    # Use the release's unchanged capacity admission before writing an export.
    sys.path.insert(0, str(Path.cwd() / "tools"))
    import reference
    import uuid
    archive = reference.new_artifact("factorybox-ci-local-base-" + uuid.uuid4().hex)
    archive.mkdir()
    layout = archive / "layout"
    child = subprocess.Popen([docker, "--context", "default", "image", "save", image], stdout=subprocess.PIPE)
    try:
        extract_layout(child.stdout, layout, image.split("@", 1)[1])
        if child.wait() != 0:
            raise ValueError("native-base OCI export failed")
    except BaseException:
        child.kill()
        child.wait()
        raise
    finally:
        child.stdout.close()
    if inspect_base(docker, image, architecture) != before:
        raise ValueError("native-base image changed during OCI export")
    receipt = {"schema": 1, "image": image, "architecture": architecture, "docker": docker,
               "layout": str(layout), "script_sha256": hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),
               "source": before}
    path = archive / "context.json"
    path.write_text(json.dumps(receipt) + "\n")
    binary = archive / "bin"
    binary.mkdir()
    proxy = binary / "docker"
    proxy.write_text("#!/bin/sh\nexec " + " ".join(shlex.quote(value) for value in
                    [sys.executable, str(Path(__file__).resolve()), "proxy", str(path)]) + ' "$@"\n')
    proxy.chmod(0o755)
    print(json.dumps({"archive": str(archive), "bin": str(binary), **receipt}))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest="command", required=True)
    export = commands.add_parser("prepare")
    export.add_argument("--docker", required=True)
    export.add_argument("--image", required=True)
    export.add_argument("--architecture", required=True)
    proxy = commands.add_parser("proxy")
    proxy.add_argument("receipt", type=Path)
    proxy.add_argument("arguments", nargs=argparse.REMAINDER)
    options = parser.parse_args()
    if options.command == "prepare":
        prepare(options.docker, options.image, options.architecture)
    else:
        receipt = json.loads(options.receipt.read_text())
        arguments = build_arguments(options.arguments, receipt)
        os.execv(receipt["docker"], [receipt["docker"], *arguments])


if __name__ == "__main__":
    main()
