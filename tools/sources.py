#!/usr/bin/env python3
"""Prepare pinned upstream sources without touching the host runtime."""
import argparse
import hashlib
import json
import pathlib
import shutil
import subprocess
import tarfile
import tempfile
import urllib.request

ROOT = pathlib.Path(__file__).resolve().parents[1]


def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def relative(value):
    path = pathlib.PurePosixPath(value)
    if path.is_absolute() or ".." in path.parts or not path.parts:
        raise ValueError(f"unsafe relative path: {value}")
    return path


def validate_inputs(component):
    for name, expected in component["local_inputs_sha256"].items():
        path = ROOT / relative(name)
        if path.is_symlink() or sha(path) != expected:
            raise ValueError(f"local source input differs: {name}")


def check_source(tree, archive, component):
    for name, expected in component.get("prepared_files_sha256", {}).items():
        path = tree / relative(name)
        if not path.is_file() or sha(path) != expected:
            raise ValueError(f"prepared compiled source differs: {name}")
    original_tests = 0
    with tarfile.open(archive) as tf:
        for member in tf.getmembers():
            if not member.isfile():
                continue
            name = pathlib.PurePosixPath(member.name).relative_to(component["prefix"])
            if not (str(name).startswith(("test/", "tests/")) or
                    name.name.endswith(("_test.go", ".bats"))):
                continue
            original_tests += 1
            path = tree / name
            if not path.is_file() or path.read_bytes() != tf.extractfile(member).read():
                raise ValueError(f"upstream test changed or missing: {name}")
    return original_tests


def prepare(name, component, offline):
    validate_inputs(component)
    archive = ROOT / ".cache/downloads" / component["archive"]
    if not archive.exists():
        if offline:
            raise ValueError(f"missing offline archive: {archive.name}")
        archive.parent.mkdir(parents=True, exist_ok=True)
        with tempfile.NamedTemporaryFile(dir=archive.parent, delete=False) as f:
            temporary = pathlib.Path(f.name)
        try:
            with urllib.request.urlopen(component["url"], timeout=45) as response:
                with temporary.open("wb") as f:
                    shutil.copyfileobj(response, f)
            if sha(temporary) != component["sha256"]:
                raise ValueError(f"download hash differs: {name}")
            temporary.replace(archive)
        finally:
            temporary.unlink(missing_ok=True)
    if sha(archive) != component["sha256"]:
        raise ValueError(f"archive hash differs: {name}")
    destination = ROOT / ".cache/sources" / name
    if destination.exists():
        tests = check_source(destination, archive, component)
        print(json.dumps({"component": name, "status": "verified",
                          "unchanged_upstream_test_files": tests}), flush=True)
        return
    destination.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(dir=destination.parent) as temporary:
        staging = pathlib.Path(temporary)
        with tarfile.open(archive) as tf:
            tf.extractall(staging, filter="data")
        tree = staging / component["prefix"]
        for name_in_source, local in component["overlays"].items():
            target = tree / relative(name_in_source)
            target.parent.mkdir(parents=True, exist_ok=True)
            if target.is_symlink() or not target.resolve().is_relative_to(tree.resolve()):
                raise ValueError(f"overlay escapes source tree: {name_in_source}")
            shutil.copyfile(ROOT / relative(local), target)
        for patch in component["patches"]:
            result = subprocess.run(
                ["patch", "--batch", "-F", "0", "-p1", "-i", str(ROOT / relative(patch))],
                cwd=tree, capture_output=True, text=True, timeout=30)
            if result.returncode:
                raise ValueError(f"patch failed for {name}: {patch}\n"
                                 f"{result.stdout}\n{result.stderr}")
        tests = check_source(tree, archive, component)
        tree.rename(destination)
    print(json.dumps({"component": name, "status": "prepared",
                      "unchanged_upstream_test_files": tests}), flush=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("operation", choices=["prepare", "verify"])
    parser.add_argument("--offline", action="store_true")
    parser.add_argument("--component")
    args = parser.parse_args()
    components = json.loads((ROOT / "upstream/lock.json").read_text())["components"]
    if args.component:
        if args.component not in components:
            parser.error("unknown component")
        components = {args.component: components[args.component]}
    for name, component in components.items():
        if args.operation == "verify" and not (ROOT / ".cache/sources" / name).exists():
            raise ValueError(f"source not prepared: {name}")
        prepare(name, component, args.offline or args.operation == "verify")


if __name__ == "__main__":
    main()
