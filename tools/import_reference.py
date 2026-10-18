#!/usr/bin/env python3
"""One-time, read-only import from the existing, hash-pinned experiment."""
import argparse
import difflib
import hashlib
import json
import pathlib
import re
import shutil
import tarfile

ROOT = pathlib.Path(__file__).resolve().parents[1]
DRIVER = "descendant-driver-build-9b81ea22faeb443a81f5c880e0dba5eb"
CRIU = "criu-descendant-build-187bd5d1ef7d4790ac4b75e0f835edc0"
KEEPER = "criu-descendant-build-99e75d415a5c4f63ac0851bbc1e700a3"
SYSBOX = "descendant-sysbox-build-d59698275e9d424fb3ccd62d58b67391"
FULL = "sysbox-lazy-inherit-epoch-retirement-full-1-9aff8b93-55b6ee4798654813b8c12563da8bb22c"
ARCHIVES = [
    ("sysbox-runc", "nestybox/sysbox-runc", "sysbox-runc-081856cc.tar.gz"),
    ("sysbox-fs", "nestybox/sysbox-fs", "sysbox-fs-c3d2ebc6.tar.gz"),
    ("sysbox-ipc", "nestybox/sysbox-ipc", "sysbox-ipc-f05151f4.tar.gz"),
    ("sysbox-libs", "nestybox/sysbox-libs", "sysbox-libs-cacac193.tar.gz"),
    ("sysbox-fuse", "nestybox/fuse", "sysbox-fuse-5ce31943.tar.gz"),
    ("sysbox-mgr", "nestybox/sysbox-mgr", "sysbox-mgr-2d45af01.tar.gz"),
    ("criu", "castai/criu", "criu-castai-086df34a2da27125fa05e66ba4d6b63cbb220b2e.tar.gz"),
]


def digest(data):
    return hashlib.sha256(data).hexdigest()


def read_json(path):
    return json.loads(path.read_text())


def write_json(path, data):
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(data, indent=2, sort_keys=True) + "\n")


def copy_verified(source, target, expected):
    data = source.read_bytes()
    if digest(data) != expected:
        raise ValueError(f"source pin differs: {source}")
    target.parent.mkdir(parents=True, exist_ok=True)
    target.write_bytes(data)
    return expected


def import_tree(source, target, hashes):
    return {
        str((target / relative).relative_to(ROOT)): copy_verified(
            source / relative, target / relative, expected)
        for relative, expected in sorted(hashes.items())
    }


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("experiment", type=pathlib.Path)
    parser.add_argument("--refresh-upstream", action="store_true",
                        help="regenerate source recipes only while all reference bytes remain pinned")
    args = parser.parse_args()
    source = args.experiment.resolve()
    if (ROOT / "tests/reference/provenance.json").exists():
        if not args.refresh_upstream:
            raise SystemExit("reference already imported; do not overwrite retained inputs")
        previous = read_json(ROOT / "tests/reference/provenance.json")
        for name, expected in previous["files_sha256"].items():
            if digest((ROOT / name).read_bytes()) != expected:
                raise ValueError(f"existing reference changed: {name}")
    artifacts = source / "artifacts"
    files = {}
    driver = read_json(artifacts / DRIVER / "result.json")
    files.update(import_tree(artifacts / DRIVER / "inputs",
                             ROOT / "tests/reference/driver", driver["source_sha256"]))
    files.update(import_tree(artifacts / DRIVER / "pageview",
                             ROOT / "tests/reference/pageview", driver["pageview_sha256"]))
    keeper_manifest = read_json(artifacts / KEEPER / "manifest.json")
    keeper_hashes = {k.removeprefix("inputs/keeper/"): v for k, v in keeper_manifest.items()
                     if k.startswith("inputs/keeper/")}
    if not keeper_hashes:
        raise ValueError("keeper source manifest is empty")
    files.update(import_tree(artifacts / KEEPER / "inputs/keeper",
                             ROOT / "tests/reference/keeper", keeper_hashes))
    native = ["pipe-read.c", "pipe-read.h", "read-probe.c", "sysbox-scope-probe.c",
              "sysbox-owned-scope.h", "pipe-capture.c", "pipe-capture.h", "cow-pin-guard.h"]
    for name in native:
        relative = "inputs/" + name
        target = ROOT / "tests/reference/native" / name
        files[str(target.relative_to(ROOT))] = copy_verified(
            artifacts / KEEPER / relative, target, keeper_manifest[relative])

    components = {}
    for name, repo, archive_name in ARCHIVES:
        archive = artifacts / archive_name
        with tarfile.open(archive) as tf:
            prefix = tf.getmembers()[0].name
        commit = prefix.rsplit("-", 1)[1]
        entry = {"repository": repo, "commit": commit, "archive": archive_name,
                 "url": f"https://codeload.github.com/{repo}/tar.gz/{commit}",
                 "sha256": digest(archive.read_bytes()), "prefix": prefix,
                 "patches": [], "overlays": {}}
        components[name] = entry
        cache = ROOT / ".cache/downloads" / archive_name
        cache.parent.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(archive, cache)

    # Export the exact compiled CRIU delta against its pinned upstream archive.
    # Original archive symlinks are preserved by source preparation, not turned into patches.
    current = artifacts / "descendant-fork-prototype/source"
    criu_pins = read_json(artifacts / CRIU / "intent.json")["sources_sha256"]
    with tarfile.open(artifacts / components["criu"]["archive"]) as tf:
        members = {str(pathlib.PurePosixPath(m.name).relative_to(components["criu"]["prefix"])): m
                   for m in tf.getmembers() if m.isfile() or m.issym() or m.islnk()}
        patch = []
        compiled_pins = {}
        for relative, expected in sorted(criu_pins.items()):
            p = pathlib.PurePosixPath(relative)
            if not (p.parts[0] in {"criu", "compel", "images", "include"} or
                    (len(p.parts) == 1 and p.name.startswith("Makefile"))):
                continue
            if not (p.suffix in {".c", ".h", ".proto", ".S"} or p.name.startswith("Makefile")):
                continue
            data = (current / relative).read_bytes()
            if digest(data) != expected:
                raise ValueError(f"compiled CRIU source differs: {relative}")
            member = members.get(relative)
            original = tf.extractfile(member).read() if member else b""
            # Build-created symlinks are regenerated by upstream Makefiles.
            # Exporting an alias as a new file could modify its original target.
            if not member and ((current / relative).is_symlink() or any(
                    parent.is_symlink() for parent in (current / relative).parents
                    if parent.is_relative_to(current))):
                continue
            if member and (member.issym() or member.islnk()):
                if original != data:
                    raise ValueError(f"upstream source link changed: {relative}")
                continue
            compiled_pins[relative] = expected
            if original != data:
                patch.extend(difflib.unified_diff(
                    original.decode().splitlines(keepends=True),
                    data.decode().splitlines(keepends=True),
                    fromfile="a/" + relative if member else "/dev/null",
                    tofile="b/" + relative))
        # The existing upstream test files must remain unchanged.
        for relative, member in members.items():
            if relative.startswith("test/") and member.isfile() and (current / relative).is_file():
                if tf.extractfile(member).read() != (current / relative).read_bytes():
                    raise ValueError(f"upstream CRIU test changed: {relative}")
    patch_path = ROOT / "upstream/patches/criu.patch"
    patch_path.parent.mkdir(parents=True, exist_ok=True)
    patch_path.write_text("".join(patch))
    components["criu"]["patches"] = ["upstream/patches/criu.patch"]
    components["criu"]["prepared_files_sha256"] = compiled_pins

    # Preserve the tested Sysbox patch ordering and the exact injected source files.
    full = artifacts / FULL
    full_manifest = read_json(full / "manifest.json")
    dockerfile = (artifacts / SYSBOX / "inputs/Dockerfile").read_text()
    sysbox_sources = read_json(artifacts / SYSBOX / "result.json")["source_sha256"]
    destinations = {}
    for line in dockerfile.splitlines():
        if not line.startswith("COPY ") or line.startswith("COPY --"):
            continue
        _, original, target = line.split()
        if original.endswith(".patch"):
            destination = ROOT / "upstream/patches" / pathlib.Path(original).name
            files[str(destination.relative_to(ROOT))] = copy_verified(
                full / original, destination, full_manifest[original])
            destinations[target] = str(destination.relative_to(ROOT))
        elif original.endswith(".go"):
            match = re.fullmatch(r"/src/(sysbox-[^/]+)/(.*)", target)
            if not match:
                raise ValueError(f"unrecognized Sysbox source destination: {target}")
            component, relative = match.groups()
            destination = ROOT / "upstream/overlays" / component / relative
            if original.startswith("artifacts/descendant-fork-prototype/"):
                original_key = original.removeprefix("artifacts/descendant-fork-prototype/")
                pin = sysbox_sources[original_key]
                original_path = artifacts / SYSBOX / "inputs" / original_key
            else:
                pin = full_manifest[original]
                original_path = full / original
            files[str(destination.relative_to(ROOT))] = copy_verified(original_path, destination, pin)
            components[component]["overlays"][relative] = str(destination.relative_to(ROOT))
    for component, temporary in re.findall(
            r"patch -F 0 -d /src/(sysbox-[^ ]+) -p1 < (/tmp/[a-z-]+\.patch)", dockerfile):
        components[component]["patches"].append(destinations[temporary])

    # Full proof results stay in the historical experiment; only identities travel.
    provenance = {
        "schema": 1, "driver_build": DRIVER, "criu_build": CRIU,
        "keeper_build": KEEPER, "sysbox_build": SYSBOX,
        "full_lifecycle_ancestor": FULL,
        "driver_qualification": "host checks and fifty native top-level regression tests; "
                                "new repository still requires independent native lifecycle qualification",
        "files_sha256": files,
        "historical_binary_sha256": driver["binary_sha256"],
        "historical_base_image_config": "sha256:041140e968e8efdbc6b2f33651f84bc19c13593c6d3f527e90af648f967dd52f",
        "production_qualified": False,
    }
    # Patch and overlay checksums make preparation independent of the old workspace.
    for component in components.values():
        component["local_inputs_sha256"] = {
            rel: digest((ROOT / rel).read_bytes())
            for rel in component["patches"] + list(component["overlays"].values())
        }
    write_json(ROOT / "upstream/lock.json", {"schema": 1, "components": components})
    write_json(ROOT / "tests/reference/provenance.json", provenance)
    print(json.dumps({"imported_reference_files": len(files),
                      "upstream_components": len(components),
                      "criu_compiled_files": len(compiled_pins),
                      "criu_patch_bytes": patch_path.stat().st_size,
                      "production_qualified": False}))


if __name__ == "__main__":
    main()
