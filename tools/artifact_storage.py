#!/usr/bin/env python3
"""Bound generated test storage and compact explicitly selected offline payloads."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import stat
import subprocess

ARTIFACT_LIMIT = 64 << 30
FREE_RESERVE = 12 << 30
RUN_RESERVE = 8 << 30


def allocated_bytes(directory):
    """Count actual allocated blocks once per inode without following links."""
    total = 0
    seen = set()
    pending = [Path(directory)]
    while pending:
        current = pending.pop()
        if not current.exists():
            continue
        with os.scandir(current) as entries:
            for entry in entries:
                info = entry.stat(follow_symlinks=False)
                if stat.S_ISDIR(info.st_mode):
                    pending.append(Path(entry.path))
                elif stat.S_ISREG(info.st_mode):
                    key = (info.st_dev, info.st_ino)
                    if key not in seen:
                        seen.add(key)
                        total += info.st_blocks * 512
    return total


def require_capacity(root, *, limit=ARTIFACT_LIMIT, run_reserve=RUN_RESERVE,
                     free_reserve=FREE_RESERVE):
    root = Path(root)
    used = allocated_bytes(root / "artifacts")
    free = shutil.disk_usage(root).free
    if used + run_reserve > limit:
        raise ValueError(
            f"artifact storage uses {used / 2**30:.2f} GiB; a new run reserves "
            f"{run_reserve / 2**30:g} GiB within the {limit / 2**30:g} GiB limit. "
            "Compact or remove unneeded closed-run payloads before starting another run.")
    if free - run_reserve < free_reserve:
        raise ValueError("a new run would consume the host's reserved free disk space")
    return {"allocated_bytes": used, "limit_bytes": limit, "run_reserve_bytes": run_reserve}


def identity(path):
    info = path.lstat()
    if not stat.S_ISREG(info.st_mode):
        raise ValueError("payload must be a regular file")
    return (info.st_dev, info.st_ino, info.st_size, info.st_mtime_ns,
            info.st_ctime_ns)


def verify_compacted(path, expected):
    path = Path(path)
    receipts = [path.with_name(path.name + ".encoding.json"),
                path.parent / "archive-encoding.json"]
    for receipt in receipts:
        if receipt.is_symlink() or not receipt.is_file():
            continue
        record = json.loads(receipt.read_text())
        if record.get("original") != expected or not record.get("whole_decompressed_payload_verified"):
            continue
        encoded = record["encoded"]
        if encoded["file"] != path.name + ".zst":
            raise ValueError("encoded payload identity differs")
        payload = path.parent / encoded["file"]
        before = identity(payload)
        with payload.open("rb") as stream:
            digest = hashlib.file_digest(stream, "sha256").hexdigest()
        if before[2] != encoded["bytes"] or digest != encoded["sha256"]:
            raise ValueError("encoded payload no longer matches its receipt")
        digest = hashlib.sha256()
        count = 0
        with subprocess.Popen(["zstd", "-q", "-dc", "--", str(payload)],
                              stdout=subprocess.PIPE, stderr=subprocess.DEVNULL) as process:
            try:
                while data := process.stdout.read(1 << 20):
                    count += len(data)
                    if count > expected["bytes"]:
                        raise ValueError("decoded payload exceeds its receipt")
                    digest.update(data)
                if (process.wait(timeout=30) or count != expected["bytes"] or
                        digest.hexdigest() != expected["sha256"] or identity(payload) != before):
                    raise ValueError("decoded payload no longer matches its receipt")
            finally:
                if process.poll() is None:
                    process.kill()
        return encoded
    raise ValueError("missing verified compacted payload")


def compact(path, root):
    """Replace one explicit offline payload only after complete byte verification."""
    supplied_root = Path(root).absolute()
    path = Path(path).absolute()
    if not path.is_relative_to(supplied_root / "artifacts"):
        raise ValueError("payload must be under this repository's artifacts directory")
    relative = path.relative_to(supplied_root / "artifacts")
    root = supplied_root.resolve()
    expected = root / "artifacts" / relative
    if path.resolve() != expected:
        raise ValueError("payload path must not contain symlinks")
    path = expected
    if len(relative.parts) > 1 and relative.parts[0].startswith("factorybox-"):
        run = root / "artifacts" / relative.parts[0]
        if relative.parts[0].startswith((
                "factorybox-build-", "factorybox-devx-inputs-",
                "factorybox-devx-project-images-", "factorybox-bats-images-",
                "factorybox-docker-profile-input-")):
            raise ValueError("frozen inputs and component builds must remain directly usable")
        result_file = run / "result.json"
        if result_file.is_symlink() or not result_file.is_file():
            raise ValueError("active or unrecorded run cannot be compacted")
        result = json.loads(result_file.read_text())
        closed = result.get("cleanup") is True
        for reconciliation in (run / "reconciliations").glob("*/result.json"):
            if not reconciliation.is_symlink() and not reconciliation.parent.is_symlink():
                closed = closed or json.loads(reconciliation.read_text()).get("cleanup") is True
        if (not closed or result.get("container_state", {}).get("Running") is True or
                "reconciliations" in relative.parts and not (path.parent / "result.json").is_file()):
            raise ValueError("run has active or unresolved cleanup")
    before = identity(path)
    encoded = path.with_name(path.name + ".zst")
    partial = encoded.with_name(encoded.name + ".partial")
    receipt = path.with_name(path.name + ".encoding.json")
    if any(p.exists() or p.is_symlink() for p in [encoded, partial, receipt]):
        raise ValueError("encoded payload or receipt already exists")
    maximum = min(shutil.disk_usage(root).free - FREE_RESERVE,
                  ARTIFACT_LIMIT - allocated_bytes(root / "artifacts"))
    if maximum <= 0:
        raise ValueError("compaction would exceed the artifact budget or reserved free disk space")
    import resource
    def limit_child():
        hard = resource.getrlimit(resource.RLIMIT_FSIZE)[1]
        cap = maximum if hard == resource.RLIM_INFINITY else min(maximum, hard)
        resource.setrlimit(resource.RLIMIT_FSIZE, (cap, cap))
    try:
        with partial.open("xb") as output:
            subprocess.run(["zstd", "-q", "-T1", "-1", "--long=27", "--stdout", "--", str(path)],
                           stdout=output, check=True, timeout=600,
                           preexec_fn=limit_child)
            output.flush()
            os.fsync(output.fileno())
        digest = hashlib.sha256()
        count = 0
        with path.open("rb") as original, subprocess.Popen(
                ["zstd", "-q", "-d", "--stdout", "--", str(partial)],
                stdout=subprocess.PIPE, stderr=subprocess.DEVNULL) as process:
            try:
                while data := process.stdout.read(1 << 20):
                    if original.read(len(data)) != data:
                        raise ValueError("decompressed payload differs from original")
                    digest.update(data)
                    count += len(data)
                if original.read(1) or process.wait(timeout=30) or count != before[2]:
                    raise ValueError("decompressed payload is incomplete")
            finally:
                if process.poll() is None:
                    process.kill()
        if identity(path) != before:
            raise ValueError("original payload changed during compaction")
        with partial.open("rb") as stream:
            encoded_sha = hashlib.file_digest(stream, "sha256").hexdigest()
        record = {
            "schema": 1, "original": {"file": path.name, "bytes": count,
                                     "sha256": digest.hexdigest()},
            "codec_parameters": ["-T1", "-1", "--long=27"],
            "encoded": {"file": encoded.name, "bytes": partial.stat().st_size,
                        "sha256": encoded_sha},
            "whole_decompressed_payload_verified": True,
            "reconstruct": ["zstd", "-d", "--keep", str(encoded), "-o", str(path)],
        }
        os.rename(partial, encoded)
        with receipt.open("x") as stream:
            stream.write(json.dumps(record, indent=2) + "\n")
            stream.flush()
            os.fsync(stream.fileno())
        directory = os.open(path.parent, os.O_RDONLY)
        try:
            os.fsync(directory)
            if identity(path) != before:
                raise ValueError("original payload changed before removal")
            path.unlink()
            os.fsync(directory)
        finally:
            os.close(directory)
        return record
    finally:
        partial.unlink(missing_ok=True)


def compact_closed_run(directory, root):
    """Retain large closed-run dumps without leaving their raw copies behind."""
    directory = Path(directory)
    result_path = directory / "result.json"
    if (result_path.is_symlink() or not result_path.is_file() or
            json.loads(result_path.read_text()).get("cleanup") is not True):
        return
    candidates = [directory / "output/capture-source.tar"]
    candidates.extend(directory.glob("failed-volume-*.tar"))
    report = {"compacted": [], "errors": []}
    for path in candidates:
        if not path.is_file() or path.is_symlink() or path.stat().st_size < 8 << 20:
            continue
        try:
            report["compacted"].append(compact(path, root))
        except Exception as error:
            # The completed test outcome stays intact. compact preserves the
            # original if verification or available storage prevents compaction.
            report["errors"].append({"file": str(path), "error": str(error)})
    if report["compacted"] or report["errors"]:
        print(json.dumps({"storage_retention": str(directory), **report}), flush=True)
    return report


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("operation", choices=["status", "compact"])
    parser.add_argument("paths", nargs="*")
    options = parser.parse_args()
    root = Path(__file__).resolve().parents[1]
    if options.operation == "status":
        print(json.dumps({"allocated_bytes": allocated_bytes(root / "artifacts"),
                          "limit_bytes": ARTIFACT_LIMIT,
                          "run_reserve_bytes": RUN_RESERVE,
                          "free_bytes": shutil.disk_usage(root).free}))
    elif not options.paths:
        parser.error("compact requires explicit offline payload paths")
    else:
        for path in options.paths:
            print(json.dumps(compact(path, root)), flush=True)


if __name__ == "__main__":
    main()
