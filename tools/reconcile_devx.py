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


def verified_previous_archive(archive, run, owner):
    for path in sorted((archive / "reconciliations").glob("*/result.json")):
        if (not re.fullmatch(r"[0-9a-f]{32}", path.parent.name) or
                path.parent.is_symlink() or path.is_symlink()):
            continue
        saved = json.loads(path.read_text())
        if saved.get("run") != run or saved.get("owner") != owner:
            continue
        archives = saved.get("failed_volume_archives", {})
        if set(archives) != {run}:
            continue
        row = archives[run]
        if (row.get("file") != "failed-volume-0.tar" or
                not re.fullmatch(r"[0-9a-f]{64}", row.get("sha256", "")) or
                type(row.get("bytes")) is not int or row["bytes"] <= 0):
            raise ValueError("previous archive receipt has invalid payload identity")
        payload = path.parent / row["file"]
        if (payload.is_symlink() or not payload.is_file() or
                payload.stat().st_size != row["bytes"] or
                upstream_bats.file_sha(payload) != row["sha256"]):
            raise ValueError("previous archive payload no longer matches its receipt")
        return {
            "reconciliation": path.parent.name, "receipt_sha256": reference.sha(path),
            **row,
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
    options = parser.parse_args()
    if not re.fullmatch(r"factorybox-devx-lifecycle-[0-9a-f]{32}", options.run):
        raise ValueError("requires one exact developer lifecycle run")
    archive = reference.ROOT / "artifacts" / options.run
    original = json.loads((archive / "result.json").read_text())
    owner = original["owner"]
    if (options.run != "factorybox-devx-lifecycle-" + owner or
            original.get("passed") or original.get("cleanup") or
            original.get("retained_volumes") != [options.run]):
        raise ValueError("run has no matching unresolved retained volume")
    # This also refuses a foreign object if the old name was reused.
    reference.owned_delete("container", options.run, owner)
    reference.owned_delete("container", options.run + "-archive-0", owner)
    output = archive / "reconciliations" / uuid.uuid4().hex
    output.mkdir(parents=True)
    result = {
        "started_at": datetime.datetime.now(datetime.timezone.utc).isoformat(),
        "run": options.run, "owner": owner, "cleanup": False, "passed": False,
        "scope": "Independent cleanup observation. Original failed lifecycle result remains unchanged.",
    }
    print(json.dumps({"archiving": str(output)}), flush=True)
    try:
        if volume_present(options.run, owner):
            image, _ = native_base.load(original["native_base_build"])
            upstream_bats.archive_stopped_volumes(output, options.run, owner, [options.run], image,
                                                 result, timeout=600)
            reference.owned_delete("volume", options.run, owner)
        else:
            # Docker may complete removal after its caller times out. Absence
            # alone cannot prove cleanup: require the preceding complete data.
            result["archive_reused_from"] = verified_previous_archive(archive, options.run, owner)
        result["cleanup"], result["passed"] = True, True
    except Exception as error:
        result["error"] = str(error)
        raise
    finally:
        reference_lifecycle.write_json(output / "result.json", result)
        print(json.dumps({"evidence": str(output), **result}), flush=True)


if __name__ == "__main__":
    main()
