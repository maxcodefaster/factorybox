#!/usr/bin/env python3
"""Archive and remove only one developer run's explicitly retained stopped volume."""
import argparse
import datetime
import json
import re
import uuid

import native_base
import reference
import reference_lifecycle
import upstream_bats


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
    image, _ = native_base.load(original["native_base_build"])
    output = archive / "reconciliations" / uuid.uuid4().hex
    output.mkdir(parents=True)
    result = {
        "started_at": datetime.datetime.now(datetime.timezone.utc).isoformat(),
        "run": options.run, "owner": owner, "cleanup": False, "passed": False,
        "scope": "Independent cleanup observation. Original failed lifecycle result remains unchanged.",
    }
    print(json.dumps({"archiving": str(output)}), flush=True)
    try:
        upstream_bats.archive_stopped_volumes(output, options.run, owner, [options.run], image,
                                             result, timeout=600)
        reference.owned_delete("volume", options.run, owner)
        result["cleanup"], result["passed"] = True, True
    except Exception as error:
        result["error"] = str(error)
        raise
    finally:
        reference_lifecycle.write_json(output / "result.json", result)
        print(json.dumps({"evidence": str(output), **result}), flush=True)


if __name__ == "__main__":
    main()
