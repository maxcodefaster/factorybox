#!/usr/bin/env python3
"""Archive and remove only one developer run's explicitly retained stopped volume."""
import argparse
import datetime
import json
import re
import subprocess
import uuid

import native_base
import reference
import reference_lifecycle
import upstream_bats


def verified_previous_archive(archive, run, owner, volume=None):
    volume = volume or run
    for path in sorted((archive / "reconciliations").glob("*/result.json")):
        if (not re.fullmatch(r"[0-9a-f]{32}", path.parent.name) or
                path.parent.is_symlink() or path.is_symlink()):
            continue
        saved = json.loads(path.read_text())
        if saved.get("run") != run or saved.get("owner") != owner:
            continue
        archives = saved.get("failed_volume_archives", {})
        if set(archives) != {volume}:
            continue
        row = archives[volume]
        if (row.get("file") != "failed-volume-0.tar" or
                not re.fullmatch(r"[0-9a-f]{64}", row.get("sha256", "")) or
                type(row.get("bytes")) is not int or row["bytes"] <= 0):
            raise ValueError("previous archive receipt has invalid payload identity")
        payload = path.parent / row["file"]
        encoded = None
        try:
            if not payload.exists() and not payload.is_symlink():
                encoded = reference.artifact_storage.verify_compacted(payload, row)
            elif (payload.is_symlink() or not payload.is_file() or
                  payload.stat().st_size != row["bytes"] or
                  upstream_bats.file_sha(payload) != row["sha256"]):
                raise ValueError("changed original archive")
        except (ValueError, OSError, KeyError) as error:
            raise ValueError("previous archive payload no longer matches its receipt") from error
        if payload.is_symlink():
            raise ValueError("previous archive payload no longer matches its receipt")
        return {
            "reconciliation": path.parent.name, "receipt_sha256": reference.sha(path),
            **row, **({"encoding": encoded} if encoded else {}),
        }
    raise ValueError("missing volume has no verified completed archive")


def volume_present(run, owner):
    try:
        saved = json.loads(reference.docker(["volume", "inspect", run]))[0]
    except subprocess.CalledProcessError as error:
        message = (error.stderr or "").lower()
        if "no such volume" in message and run in message:
            return False
        raise
    if saved.get("Name") != run or (saved.get("Labels") or {}).get("com.factorybox.run") != owner:
        raise ValueError("retained volume ownership changed")
    return True


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("run")
    parser.add_argument("--ide", action="store_true",
                        help="Reconcile only this failed run's owned Theia workspace")
    options = parser.parse_args()
    if not re.fullmatch(r"factorybox-devx-lifecycle-[0-9a-f]{32}", options.run):
        raise ValueError("requires one exact developer lifecycle run")
    archive = reference.ROOT / "artifacts" / options.run
    original = json.loads((archive / "result.json").read_text())
    owner = original["owner"]
    if options.run != "factorybox-devx-lifecycle-" + owner or original.get("passed"):
        raise ValueError("run has no matching unresolved retained volume")
    resource_name, volume, saved_root = options.run, options.run, archive
    if options.ide:
        peer = original.get("theia", {})
        resource_name, volume = options.run + "-ide", options.run + "-ide-work"
        if (peer.get("owner") != owner or peer.get("cleanup") is not False or
                peer.get("container_name") != resource_name or peer.get("volume_name") != volume or
                peer.get("setup_name") != options.run + "-ide-setup"):
            raise ValueError("run has no matching unresolved IDE resources")
        saved_root = archive / "theia"
        reference.owned_delete("container", options.run + "-ide-setup", owner)
    elif (original.get("cleanup") or original.get("retained_volumes") != [options.run]):
        raise ValueError("run has no matching unresolved retained volume")
    # This also refuses a foreign object if the old name was reused.
    reference.owned_delete("container", resource_name, owner)
    reference.owned_delete("container", resource_name + "-archive-0", owner)
    output = saved_root / "reconciliations" / uuid.uuid4().hex
    output.mkdir(parents=True)
    result = {
        "started_at": datetime.datetime.now(datetime.timezone.utc).isoformat(),
        "run": options.run, "owner": owner, "cleanup": False, "passed": False,
        "resource": "ide" if options.ide else "worker", "volume_name": volume,
        "original_result_sha256": reference.sha(archive / "result.json"),
        "scope": "Independent cleanup observation. Original failed lifecycle result remains unchanged.",
    }
    print(json.dumps({"archiving": str(output)}), flush=True)
    try:
        if volume_present(volume, owner):
            image, _ = native_base.load(original["native_base_build"])
            upstream_bats.archive_stopped_volumes(output, resource_name, owner, [volume], image,
                                                 result, timeout=600, compressed=True)
            reference.owned_delete("volume", volume, owner)
        else:
            # Docker may complete removal after its caller times out. Absence
            # alone cannot prove cleanup: require the preceding complete data.
            result["archive_reused_from"] = verified_previous_archive(
                saved_root, options.run, owner, volume)
        result["cleanup"], result["passed"] = True, True
    except Exception as error:
        result["error"] = str(error)
        raise
    finally:
        reference_lifecycle.write_json(output / "result.json", result)
        print(json.dumps({"evidence": str(output), **result}), flush=True)
        reference.artifact_storage.compact_closed_run(output, reference.ROOT)


if __name__ == "__main__":
    main()
