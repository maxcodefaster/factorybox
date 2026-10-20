"""Verify one explicitly unselected retained-context CRIU build."""
import json
import re

import reference
import sources


def load(name):
    if not re.fullmatch(r"factorybox-build-criu-retained-[0-9a-f]{32}", name):
        raise ValueError("expected one exact retained-context candidate build")
    root = reference.ROOT
    path = root / "artifacts" / name
    result = json.loads((path / "result.json").read_text())
    if not result["passed"] or not result["cleanup"] or not result["diagnostic_only"]:
        raise ValueError("retained-context candidate build did not succeed")
    for file, field in [
        ("criu", "binary_sha256"),
        ("source-lock.json", "source_lock_sha256"),
        ("retained.patch", "patch_sha256"),
    ]:
        if reference.sha(path / file) != result[field]:
            raise ValueError("retained-context candidate input changed: " + file)
    for file, expected in result.get("candidate_patch_sha256", {}).items():
        if file not in {"retained.patch", "early-hold.patch", "nested-overlay.patch",
                        "nested-overlay-stable-path.patch",
                        "nested-overlay-shared-root.patch"} or reference.sha(path / file) != expected:
            raise ValueError("retained-context candidate patch input changed")
    if reference.sha(root / "upstream/lock.json") != result["base_source_lock_sha256"]:
        raise ValueError("retained-context candidate belongs to a different selected base")
    if sources.tree_sha(path / "context") != result["context_sha256"]:
        raise ValueError("retained-context candidate source tree changed")
    component = json.loads((path / "source-lock.json").read_text())["components"]["criu"]
    sources.validate_inputs(component)
    unchanged = sources.check_source(
        path / "context", root / ".cache/downloads" / component["archive"], component)
    return path, result, component, unchanged
