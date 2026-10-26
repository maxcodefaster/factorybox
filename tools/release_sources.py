#!/usr/bin/env python3
"""Archive the exact component sources and recipes for one completed worker."""
import argparse
import hashlib
import json
from pathlib import Path, PurePosixPath
import subprocess
import sys
import tarfile
import uuid

def sha(path):
    with Path(path).open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def add_entry(entries, name, path):
    relative = PurePosixPath(name)
    if relative.is_absolute() or ".." in relative.parts or name in entries:
        raise ValueError("unsafe or duplicate source archive member")
    path = Path(path).absolute()
    if path.is_symlink():
        target = path.readlink()
        if target.is_absolute():
            raise ValueError("source archive cannot contain an absolute link")
        # Symlinks remain symlinks. Never follow them while archiving sources.
        parts = list(relative.parent.parts)
        for part in target.parts:
            if part == "..":
                if not parts:
                    raise ValueError("source archive link escapes its root")
                parts.pop()
            elif part != ".":
                parts.append(part)
        entries[name] = {"path": str(path), "symlink": str(target)}
    elif path.is_dir() and path.resolve() == path:
        entries[name] = {"path": str(path), "directory": True}
    elif path.is_file() and path.resolve() == path:
        entries[name] = {"path": str(path), "sha256": sha(path),
                         "bytes": path.stat().st_size, "executable": bool(path.stat().st_mode & 0o111)}
    else:
        raise ValueError("source archive input must be a regular file or relative link")


def add_tree(entries, prefix, tree):
    for path in sorted(tree.rglob("*")):
        if path.is_symlink() or path.is_file():
            add_entry(entries, prefix + "/" + str(path.relative_to(tree)), path)
        elif path.is_dir():
            add_entry(entries, prefix + "/" + str(path.relative_to(tree)), path)


def emit(entries, output):
    with tarfile.open(fileobj=output, mode="w|", format=tarfile.PAX_FORMAT) as archive:
        for name, entry in sorted(entries.items()):
            path = Path(entry["path"])
            info = tarfile.TarInfo(name)
            info.uid = info.gid = info.mtime = 0
            if entry.get("directory"):
                if path.is_symlink() or path.resolve() != path or not path.is_dir():
                    raise ValueError("source archive directory changed")
                info.type, info.mode = tarfile.DIRTYPE, 0o755
                archive.addfile(info)
            elif "symlink" in entry:
                if not path.is_symlink() or str(path.readlink()) != entry["symlink"]:
                    raise ValueError("source archive link changed")
                info.type, info.linkname, info.mode = tarfile.SYMTYPE, entry["symlink"], 0o777
                archive.addfile(info)
            else:
                if (path.is_symlink() or path.resolve() != path or
                        path.stat().st_size != entry["bytes"] or sha(path) != entry["sha256"]):
                    raise ValueError("source archive input changed: " + name)
                info.mode = 0o755 if entry["executable"] else 0o644
                info.size = entry["bytes"]
                with path.open("rb") as stream:
                    archive.addfile(info, stream)


def collect(worker_path, worker):
    import criu_candidate
    import reference
    import reference_lifecycle
    import upstream_unit
    root = reference.ROOT
    # A clean commit makes the original source release independently retrievable.
    if subprocess.check_output(["git", "status", "--porcelain", "--untracked-files=normal"], cwd=root):
        raise ValueError("commit release sources before creating their distribution archive")
    commit = subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=root, text=True).strip()
    if upstream_unit.context_manifest(root / "internal") != upstream_unit.context_manifest(worker_path / "source/internal"):
        raise ValueError("release source commit differs from the worker runtime source")
    if upstream_unit.context_manifest(root / "cmd/factorybox") != upstream_unit.context_manifest(worker_path / "source/cmd/factorybox"):
        raise ValueError("release source commit differs from the worker CLI source")
    for name in ["go.mod", "go.sum", "build/worker.Dockerfile"]:
        frozen = worker_path / ("image-context/Dockerfile" if name == "build/worker.Dockerfile" else "source/" + name)
        if reference.sha(root / name) != reference.sha(frozen):
            raise ValueError("release source commit differs from the worker executable source")
    for name in ["LICENSE", "NOTICE", "LICENSING.md"]:
        original = root / name
        if (original.is_symlink() or
                reference.sha(original) != worker["bundle_sha256"]["licenses/factorybox/" + name]):
            raise ValueError("release source notice differs from the delivered worker: " + name)
    sysbox_path, sysbox = reference_lifecycle.component_build(worker["sysbox_build"], "factorybox-build-sysbox-")
    manifest = json.loads((sysbox_path / "context-manifest.json").read_text())
    if (upstream_unit.context_manifest(sysbox_path / "context") != manifest or
            hashlib.sha256(json.dumps(manifest, sort_keys=True).encode()).hexdigest() != sysbox["context_sha256"] or
            sysbox["binary_sha256"] != {name: worker["bundle_sha256"]["components/" + name]
                                      for name in ["sysbox-runc", "sysbox-fs", "sysbox-mgr"]}):
        raise ValueError("Sysbox source or binary provenance differs")
    base_lock = worker_path / "criu-base-source-lock.json" if "criu_base_source_lock_sha256" in worker else None
    criu_path, criu, _, _ = criu_candidate.load(worker["criu_build"], base_lock=base_lock)
    if criu["binary_sha256"] != worker["bundle_sha256"]["components/criu"]:
        raise ValueError("CRIU binary differs from the source build")
    entries = {}
    tracked = subprocess.check_output(["git", "ls-files", "-z"], cwd=root).decode().split("\0")
    for name in filter(None, tracked):
        add_entry(entries, "factorybox/" + name, root / name)
    for label, path in [("sysbox", sysbox_path), ("criu", criu_path)]:
        add_tree(entries, label + "/source", path / "context")
        for file in sorted(path.iterdir()):
            if file.is_file() and (file.suffix in {".json", ".py", ".patch", ".h", ".c"} or
                                   file.name in {"Dockerfile", "toolchain.txt"}):
                add_entry(entries, label + "/recipes/" + file.name, file)
    add_tree(entries, "worker/source", worker_path / "source")
    for name in ["runner.py", "source-lock.json", "result.json", "image-context/Dockerfile", "image-context/bundle/build.json"]:
        add_entry(entries, "worker/" + name, worker_path / name)
    lock = json.loads((worker_path / "source-lock.json").read_text())
    for component in lock["components"].values():
        path = root / ".cache/downloads" / component["archive"]
        if reference.sha(path) != component["sha256"]:
            raise ValueError("original upstream archive differs from the source lock")
        add_entry(entries, "upstream-archives/" + path.name, path)
    return commit, entries


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--worker-build")
    parser.add_argument("--emit", type=Path, help=argparse.SUPPRESS)
    options = parser.parse_args()
    if options.emit:
        emit(json.loads(options.emit.read_text()), sys.stdout.buffer)
        return
    if not options.worker_build:
        parser.error("--worker-build is required")
    import build_worker
    import reference
    import reference_lifecycle
    path, worker = build_worker.load(options.worker_build)
    commit, entries = collect(path, worker)
    artifact = reference.new_artifact("factorybox-release-sources-" + uuid.uuid4().hex)
    artifact.mkdir()
    (artifact / "runner.py").write_bytes(Path(__file__).read_bytes())
    result = {"schema": 1, "passed": False, "worker_build": options.worker_build,
              "version": worker["version"], "platform": worker["platform"], "source_commit": commit,
              "worker_result_sha256": reference.sha(path / "result.json"),
              "worker_image_id": worker["image_id"], "worker_package_sha256": worker["package_sha256"],
              "scope": "Matching Factorybox, modified Sysbox and CRIU sources and recipes. Operating-system package source distribution is separate."}
    try:
        # The public inventory has no machine-local paths.
        public = {name: {k: v for k, v in row.items() if k != "path"} for name, row in entries.items()}
        inventory = {key: value for key, value in result.items() if key != "passed"}
        reference_lifecycle.write_json(artifact / "sources.json", {**inventory, "members": public})
        add_entry(entries, "sources.json", artifact / "sources.json")
        reference_lifecycle.write_json(artifact / "inputs.json", entries)
        package = artifact / ("factorybox-" + worker["version"] + "-" + worker["platform"].replace("/", "-") + "-sources.tar")
        with (artifact / "archive.log").open("xb") as log:
            encoded = reference.artifact_storage.archive_stream(
                [sys.executable, str(artifact / "runner.py"), "--emit", str(artifact / "inputs.json")],
                package, reference.ROOT, log, maximum=1 << 30, timeout=600, decoded_limit=4 << 30)
        result.update(passed=True, archive_encoding=encoded, source_inventory_sha256=reference.sha(artifact / "sources.json"))
    except Exception as error:
        result["error"] = str(error)
        raise
    finally:
        reference_lifecycle.write_json(artifact / "result.json", result)
        print(json.dumps({"evidence": str(artifact), "passed": result["passed"]}), flush=True)


if __name__ == "__main__":
    main()
