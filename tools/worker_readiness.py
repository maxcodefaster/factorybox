"""Observe publication without creating an exec task in the cgroup root."""
import io
import json
import subprocess
import tarfile


def published(docker, identity, store):
    # Docker's archive endpoint reads the file without starting a process.
    # The caller must still verify live worker-status after this hint exists.
    result = subprocess.run([*docker, "cp", identity+":/run/factorybox/worker.json", "-"],
                            capture_output=True, timeout=5)
    if result.returncode:
        return False
    if len(result.stdout) > 2 << 20:
        raise ValueError("worker readiness archive exceeds its bound")
    try:
        with tarfile.open(fileobj=io.BytesIO(result.stdout), mode="r:") as archive:
            members = archive.getmembers()
            if len(members) != 1 or not members[0].isfile() or members[0].size > 1 << 20:
                raise ValueError("worker readiness archive is not one bounded regular record")
            value = json.load(archive.extractfile(members[0]))
    except (tarfile.TarError, json.JSONDecodeError):
        return False  # Publication may still be writing its first record.
    if not isinstance(value, dict) or value.get("schema") != 1 or value.get("store_id") != store:
        raise ValueError("published worker readiness has another schema or store")
    return bool(value.get("ready_at"))
