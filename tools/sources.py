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
import uuid

ROOT = pathlib.Path(__file__).resolve().parents[1]
CACHE_RECORD = ".factorybox-source.json"


def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def recipe_sha(component):
    return hashlib.sha256(json.dumps(
        {"assembly_version": 2, "component": component}, sort_keys=True).encode()).hexdigest()


def tree_sha(tree):
    entries = {}
    for path in tree.rglob("*"):
        name = str(path.relative_to(tree))
        if name == CACHE_RECORD:
            continue
        if path.is_symlink():
            entries[name] = {"link": str(path.readlink())}
        elif path.is_file():
            entries[name] = {"sha256": sha(path)}
        elif path.is_dir():
            entries[name] = {"directory": True}
        else:
            raise ValueError("unexpected source tree entry: " + name)
    return hashlib.sha256(json.dumps(entries, sort_keys=True).encode()).hexdigest()


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
    declared = component.get("test_file_changes", {})
    seen = set()
    with tarfile.open(archive) as tf:
        for member in tf.getmembers():
            if not member.isfile():
                continue
            name = pathlib.PurePosixPath(member.name).relative_to(component["prefix"])
            if not (str(name).startswith(("test/", "tests/")) or
                    name.name.endswith(("_test.go", ".bats"))):
                continue
            original_tests += 1
            seen.add(str(name))
            path = tree / name
            original = tf.extractfile(member).read()
            expected = hashlib.sha256(original).hexdigest()
            if str(name) in declared:
                change = declared[str(name)]
                if change["original_sha256"] != expected:
                    raise ValueError(f"declared upstream test base differs: {name}")
                expected = change["prepared_sha256"]
            if not path.is_file() or sha(path) != expected:
                raise ValueError(f"upstream test changed or missing: {name}")
    if set(declared) - seen:
        raise ValueError("declared test change does not name an original upstream test")
    return original_tests


def test_counts(component, total):
    changes = len(component.get("test_file_changes", {}))
    return {"upstream_test_files_verified": total,
            "unchanged_upstream_test_files": total - changes,
            "declared_test_format_changes": changes}


def prepare(name, component, offline, refresh=False):
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
    record = destination / CACHE_RECORD
    if destination.exists() and record.exists() and not refresh:
        cached = json.loads(record.read_text())
        if cached["recipe_sha256"] != recipe_sha(component):
            raise ValueError(f"cached source recipe changed: {name}; prepare --component {name} --refresh")
        if cached["tree_sha256"] != tree_sha(destination):
            raise ValueError(f"cached source tree changed: {name}; preserve it before refreshing")
        tests = check_source(destination, archive, component)
        print(json.dumps({"component": name, "status": "verified",
                          **test_counts(component, tests)}), flush=True)
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
                ["patch", "--batch", "-E", "-F", "0", "-p1", "-i", str(ROOT / relative(patch))],
                cwd=tree, capture_output=True, text=True, timeout=30)
            if result.returncode:
                raise ValueError(f"patch failed for {name}: {patch}\n"
                                 f"{result.stdout}\n{result.stderr}")
        tests = check_source(tree, archive, component)
        metadata = {"recipe_sha256": recipe_sha(component), "tree_sha256": tree_sha(tree)}
        if destination.exists() and not refresh:
            # Old unmarked caches are adopted only after full comparison with a
            # fresh assembly of their immutable archive, overlays and patches.
            if tree_sha(destination) != metadata["tree_sha256"]:
                raise ValueError(f"cached source differs from canonical recipe: {name}; "
                                 f"prepare --component {name} --refresh")
            (destination / CACHE_RECORD).write_text(json.dumps(metadata, sort_keys=True) + "\n")
            print(json.dumps({"component": name, "status": "verified_complete_tree",
                              **test_counts(component, tests)}), flush=True)
            return
        (tree / CACHE_RECORD).write_text(json.dumps(metadata, sort_keys=True) + "\n")
        if destination.exists():
            retired = ROOT / ".cache/retired" / (name + "-" + uuid.uuid4().hex)
            retired.parent.mkdir(parents=True, exist_ok=True)
            destination.rename(retired)
        tree.rename(destination)
    print(json.dumps({"component": name, "status": "prepared",
                      **test_counts(component, tests)}), flush=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("operation", choices=["prepare", "verify"])
    parser.add_argument("--offline", action="store_true")
    parser.add_argument("--component")
    parser.add_argument("--refresh", action="store_true",
                        help="assemble a replacement source tree; preserve the previous cache")
    args = parser.parse_args()
    if args.refresh and (args.operation != "prepare" or not args.component):
        parser.error("--refresh requires prepare and one exact --component")
    components = json.loads((ROOT / "upstream/lock.json").read_text())["components"]
    if args.component:
        if args.component not in components:
            parser.error("unknown component")
        components = {args.component: components[args.component]}
    for name, component in components.items():
        if args.operation == "verify" and not (ROOT / ".cache/sources" / name).exists():
            raise ValueError(f"source not prepared: {name}")
        prepare(name, component, args.offline or args.operation == "verify", args.refresh)


if __name__ == "__main__":
    main()
