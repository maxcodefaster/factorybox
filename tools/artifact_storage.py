#!/usr/bin/env python3
"""Bound generated test storage and compact explicitly selected offline payloads."""
import argparse
import errno
import hashlib
import json
import os
from pathlib import Path
import shutil
import stat
import subprocess
import threading
import time

ARTIFACT_LIMIT = 32 << 30
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


def share_frozen_input(source, destination, root, expected_sha256):
    """Share a verified completed build output with a new read-only fixture input."""
    supplied_root = Path(root).absolute()
    root = supplied_root.resolve()
    source, destination = Path(source).absolute(), Path(destination).absolute()
    checked = []
    for path in [source, destination]:
        if not path.is_relative_to(supplied_root / "artifacts"):
            raise ValueError("shared build inputs must stay within the real artifact directory")
        expected = root / "artifacts" / path.relative_to(supplied_root / "artifacts")
        if path.resolve() != expected:
            raise ValueError("shared build inputs must stay within the real artifact directory")
        checked.append(expected)
    source, destination = checked
    relative = source.relative_to(root / "artifacts")
    if len(relative.parts) < 2 or not relative.parts[0].startswith("factorybox-build-"):
        raise ValueError("shared input must belong to a completed component build")
    result = root / "artifacts" / relative.parts[0] / "result.json"
    if result.is_symlink() or json.loads(result.read_text()).get("passed") is not True:
        raise ValueError("shared input build did not pass")
    before = identity(source)
    with source.open("rb") as stream:
        digest = hashlib.file_digest(stream, "sha256").hexdigest()
    if digest != expected_sha256 or identity(source) != before:
        raise ValueError("shared build input differs from its pinned digest")
    try:
        os.link(source, destination, follow_symlinks=False)
    except OSError as error:
        if error.errno not in {errno.EPERM, errno.EACCES, errno.EXDEV}:
            raise
        # Native Linux protects hardlinks to another user's build outputs.
        # Copy into a new file rather than changing frozen ownership or a
        # global kernel setting; retain the same whole-file digest checks.
        try:
            with destination.open("xb") as output, source.open("rb") as stream:
                shutil.copyfileobj(stream, output)
            with destination.open("rb") as stream:
                copied_digest = hashlib.file_digest(stream, "sha256").hexdigest()
            if identity(source) != before or copied_digest != expected_sha256:
                raise ValueError("frozen build input changed while copying")
            destination.chmod(source.stat().st_mode & 0o555)
        except FileExistsError:
            raise
        except Exception:
            destination.unlink(missing_ok=True)
            raise
        return
    try:
        linked = identity(destination)
        if linked[:4] != before[:4]:
            raise ValueError("shared build input identity changed before linking")
    except Exception:
        destination.unlink()
        raise


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
        repacked = not payload.exists() and not payload.is_symlink()
        if repacked:
            payload, encoded = verified_replacement(payload, encoded, expected)
        before = identity(payload)
        with payload.open("rb") as stream:
            digest = hashlib.file_digest(stream, "sha256").hexdigest()
        if before[2] != encoded["bytes"] or digest != encoded["sha256"]:
            raise ValueError("encoded payload no longer matches its receipt")
        digest = hashlib.sha256()
        count = 0
        with subprocess.Popen(["zstd", "-q", "-dc", *(["--long=31"] if repacked else []),
                               "--", str(payload)],
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


def verified_replacement(payload, encoded, expected):
    """Resolve one pinned lossless replacement; never execute receipt commands."""
    artifact_root = next((parent for parent in payload.parents if parent.name == "artifacts"), None)
    if artifact_root is None:
        raise ValueError("replacement archive must remain within artifacts")
    real_root = artifact_root.resolve()

    def checked_path(value):
        path = Path(value)
        if not path.is_absolute() or not path.is_relative_to(artifact_root):
            raise ValueError("replacement path must remain within artifacts")
        canonical = real_root / path.relative_to(artifact_root)
        if path.resolve() != canonical or path.is_symlink():
            raise ValueError("replacement path must not contain symlinks")
        return path

    def read_receipt(path, expected_sha=None):
        checked_path(path)
        before = identity(path)
        if before[2] > 1 << 20:
            raise ValueError("replacement receipt exceeds its size limit")
        content = path.read_bytes()
        digest = hashlib.sha256(content).hexdigest()
        if (identity(path) != before or
                expected_sha is not None and digest != expected_sha):
            raise ValueError("replacement receipt no longer matches")
        return json.loads(content), digest

    pointer_path = payload.with_name(payload.name + ".repacked.json")
    pointer, pointer_sha = read_receipt(pointer_path)
    if (pointer.get("schema") != 1 or pointer.get("original_archive") != str(payload) or
            pointer.get("original_encoded_sha256") != encoded["sha256"] or
            pointer.get("original_tar_bytes") != expected["bytes"] or
            pointer.get("original_tar_sha256") != expected["sha256"]):
        raise ValueError("replacement pointer differs from original archive receipt")
    replacement = checked_path(pointer["replacement_archive"])
    result_path = checked_path(pointer["verified_result"])
    if (replacement == payload or replacement.parent != result_path.parent or
            result_path.name != "result.json" or
            not replacement.parent.name.startswith("factorybox-storage-repack-")):
        raise ValueError("replacement must belong to its lossless repack result")
    result, _ = read_receipt(result_path, pointer["verified_result_sha256"])
    if (result.get("operation") != "lossless_closed_archive_repack" or
            any(result.get(key) is not True for key in
                ["passed", "cleanup", "source_removed", "whole_decoded_bytes_verified"]) or
            result.get("source") != str(payload) or
            result.get("source_encoded_sha256") != encoded["sha256"] or
            result.get("source_encoded_bytes") != encoded["bytes"] or
            result.get("new_archive") != str(replacement) or
            result.get("encoded_sha256") != pointer["replacement_encoded_sha256"] or
            result.get("raw_bytes") != expected["bytes"] or
            result.get("raw_sha256") != expected["sha256"] or
            type(result.get("encoded_bytes")) is not int or result["encoded_bytes"] <= 0):
        raise ValueError("replacement result differs from original archive receipt")
    # Only one replacement is admitted. Missing/repacked replacements fail here;
    # this cannot chase an unbounded chain or a cycle.
    identity(replacement)
    return replacement, {
        "file": replacement.name, "bytes": result["encoded_bytes"],
        "sha256": result["encoded_sha256"], "artifact_path": str(replacement),
        "repacked_from": str(payload), "pointer_sha256": pointer_sha,
        "repack_result_sha256": pointer["verified_result_sha256"],
    }


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
    """Retain large closed-run dumps and unique tests without raw duplicates."""
    directory = Path(directory)
    result_path = directory / "result.json"
    if (result_path.is_symlink() or not result_path.is_file() or
            json.loads(result_path.read_text()).get("cleanup") is not True):
        return
    candidates = [directory / "output/capture-source.tar"]
    candidates.extend(directory.glob("failed-volume-*.tar"))
    # Shared immutable inputs already occupy one inode. Encoding each link
    # separately could use more storage than leaving that shared input alone.
    candidates.extend(path for name in ("lifecycle.test", "devx-lifecycle.test", "consumer-worker.test", "probe.test",
                                        "devx-actor", "devx-registry", "minimal-actor")
                      if (path := directory / name).is_file() and path.stat().st_nlink == 1)
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


def archive_stream(command, path, root, log, *, maximum, timeout, decoded_limit=None):
    """Save a stopped source's complete tar stream without a raw disk copy."""
    if decoded_limit is None:
        decoded_limit = ARTIFACT_LIMIT
    if type(decoded_limit) is not int or not 0 < decoded_limit <= 64 << 30:
        raise ValueError("decoded observation bound must be positive and at most 64 GiB")
    supplied_root, path = Path(root).absolute(), Path(path).absolute()
    if not path.is_relative_to(supplied_root / "artifacts"):
        raise ValueError("stream archive must belong to this repository's artifacts")
    relative = path.relative_to(supplied_root / "artifacts")
    root = supplied_root.resolve()
    expected = root / "artifacts" / relative
    if path.resolve() != expected:
        raise ValueError("stream archive path must not contain symlinks")
    path = expected
    encoded = path.with_name(path.name + ".zst")
    partial = encoded.with_name(encoded.name + ".partial")
    receipt = path.with_name(path.name + ".encoding.json")
    if any(p.exists() or p.is_symlink() for p in [path, encoded, partial, receipt]):
        raise ValueError("stream archive payload or receipt already exists")
    maximum = min(maximum, ARTIFACT_LIMIT - allocated_bytes(root / "artifacts"),
                  shutil.disk_usage(root).free - FREE_RESERVE) - (1 << 20)
    if maximum <= 0:
        raise ValueError("stream archive has no remaining storage budget")
    import resource
    def cap_encoder():
        hard = resource.getrlimit(resource.RLIMIT_FSIZE)[1]
        cap = maximum if hard == resource.RLIM_INFINITY else min(maximum, hard)
        resource.setrlimit(resource.RLIMIT_FSIZE, (cap, cap))
    children, workers, failures = [], [], []
    expired, encoder_finished, lock = threading.Event(), threading.Event(), threading.Lock()
    def stop_children():
        with lock:
            for child in children:
                if child.poll() is None:
                    child.kill()
    def kill_children():
        expired.set()
        stop_children()
    def launch(args, **kwargs):
        child = subprocess.Popen(args, **kwargs)
        with lock:
            children.append(child)
            if (expired.is_set() or failures) and child.poll() is None:
                child.kill()
        return child
    def work(function):
        def guarded():
            try:
                function()
            except Exception as error:
                failures.append(error)
                stop_children()
        worker = threading.Thread(target=guarded, daemon=True)
        workers.append(worker)
        worker.start()
    timer = threading.Timer(timeout, kill_children)
    timer.daemon = True
    started = time.monotonic()
    digest, count = hashlib.sha256(), 0
    decoded, encoded_digest = hashlib.sha256(), hashlib.sha256()
    observed = {"decoded_bytes": 0, "encoded_bytes": 0}
    def progress(phase):
        os.write(log.fileno(), (json.dumps({
            "archive_phase": phase, "raw_bytes": count, **observed,
            "seconds": time.monotonic() - started,
        }) + "\n").encode())
    try:
        with partial.open("xb") as output:
            encoder = launch(["zstd", "-q", "-T1", "-1", "--long=27", "--stdout"],
                             stdin=subprocess.PIPE, stdout=output, stderr=log,
                             preexec_fn=cap_encoder)
            decoder = launch(["zstd", "-q", "-dc"], stdin=subprocess.PIPE,
                             stdout=subprocess.PIPE, stderr=log)
            def decode_stored_bytes():
                # Read the actual encoded file while its single bounded writer
                # appends. EOF is final only after that writer has exited.
                with partial.open("rb") as stored:
                    while True:
                        data = stored.read(1 << 20)
                        if data:
                            observed["encoded_bytes"] += len(data)
                            encoded_digest.update(data)
                            decoder.stdin.write(data)
                        elif encoder_finished.is_set():
                            break
                        elif expired.is_set() or failures:
                            return
                        else:
                            time.sleep(0.01)
                decoder.stdin.close()
            def hash_decoded_bytes():
                while data := decoder.stdout.read(1 << 20):
                    observed["decoded_bytes"] += len(data)
                    if observed["decoded_bytes"] > decoded_limit:
                        raise ValueError("decoded archive exceeds its original observation bound")
                    decoded.update(data)
                decoder.stdout.close()
            timer.start()
            work(decode_stored_bytes)
            work(hash_decoded_bytes)
            try:
                producer = launch(command, stdout=subprocess.PIPE, stderr=log)
                last_progress = started
                try:
                    while data := producer.stdout.read(1 << 20):
                        if expired.is_set():
                            raise subprocess.TimeoutExpired(command, timeout)
                        count += len(data)
                        if count > decoded_limit:
                            raise ValueError(f"decoded archive stream exceeds its {decoded_limit}-byte observation bound")
                        digest.update(data)
                        encoder.stdin.write(data)
                        if time.monotonic() - last_progress >= 10:
                            progress("streaming-and-verifying")
                            last_progress = time.monotonic()
                    encoder.stdin.close()
                finally:
                    producer.stdout.close()
                if producer.wait(timeout=5) or encoder.wait(timeout=5):
                    raise ValueError("source tar or bounded encoder failed; original volume must remain")
                encoder_finished.set()
                output.flush()
                os.fsync(output.fileno())
                for worker in workers:
                    while worker.is_alive():
                        worker.join(timeout=0.1)
                        if expired.is_set():
                            raise subprocess.TimeoutExpired(command, timeout)
                if failures:
                    raise ValueError("decoded archive differs from its complete original stream") from failures[0]
                if (decoder.wait(timeout=5) or observed["decoded_bytes"] != count or
                        decoded.hexdigest() != digest.hexdigest()):
                    raise ValueError("decoded archive differs from its complete original stream")
            finally:
                encoder_finished.set()
                stop_children()
                for worker in workers:
                    worker.join(timeout=5)
        if partial.stat().st_size >= maximum:
            raise ValueError("encoded archive reached its storage cap; original volume must remain")
        progress("decoded-stream-verified")
        with partial.open("rb") as stream:
            encoded_sha = hashlib.file_digest(stream, "sha256").hexdigest()
        if (observed["encoded_bytes"] != partial.stat().st_size or
                encoded_digest.hexdigest() != encoded_sha):
            raise ValueError("encoded archive changed during stream verification")
        if expired.is_set():
            raise subprocess.TimeoutExpired(command, timeout)
        record = {
            "schema": 1,
            "original": {"file": path.name, "bytes": count, "sha256": digest.hexdigest()},
            "encoded": {"file": encoded.name, "bytes": partial.stat().st_size, "sha256": encoded_sha},
            "codec_parameters": ["-T1", "-1", "--long=27"],
            "decoded_limit_bytes": decoded_limit,
            "whole_decompressed_payload_verified": True,
            "original_stream_hashed": True, "pipelined_disk_verification": True,
            "seconds": time.monotonic() - started,
            "reconstruct": ["zstd", "-d", "--keep", str(encoded), "-o", str(path)],
        }
        os.rename(partial, encoded)
        with receipt.open("x") as output:
            output.write(json.dumps(record, indent=2) + "\n")
            output.flush()
            os.fsync(output.fileno())
        directory = os.open(path.parent, os.O_RDONLY)
        try:
            os.fsync(directory)
        finally:
            os.close(directory)
        if expired.is_set():
            raise subprocess.TimeoutExpired(command, timeout)
        return record
    except Exception as error:
        progress("failed")
        if expired.is_set():
            raise subprocess.TimeoutExpired(command, timeout) from error
        raise
    finally:
        timer.cancel()
        encoder_finished.set()
        stop_children()
        for worker in workers:
            worker.join(timeout=5)
        for child in children:
            child.wait(timeout=5)
            for stream in [child.stdin, child.stdout]:
                if stream is not None and not stream.closed:
                    try:
                        stream.close()
                    except BrokenPipeError:
                        pass
        partial.unlink(missing_ok=True)


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
