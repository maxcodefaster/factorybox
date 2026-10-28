"""Verify one explicitly unselected retained-context CRIU build."""
import json
from pathlib import Path
import re

import reference
import sources


PSTREE_CORRECTION = reference.ROOT / "upstream/patches/criu-pstree-error-propagation-candidate.patch"
RECOVERY_CORRECTIONS = {
    name + ".patch": reference.ROOT / ("upstream/patches/criu-" + name + "-candidate.patch")
    for name in ["pid-collision-diagnostics", "pid-collision-references",
                 "nested-pid-counter", "nested-zombie-peer-pidns"]
}


def require_base_failure_correction(component):
    base_path = str(PSTREE_CORRECTION.relative_to(reference.ROOT))
    if component.get("local_inputs_sha256", {}).get(base_path) != reference.sha(PSTREE_CORRECTION):
        raise ValueError("worker requires the exact base process-tree failure correction")


def require_recovery_corrections(result, component):
    """Keep exact selected recovery fixes in ordinary builds and reused inputs."""
    require_base_failure_correction(component)
    patches = result.get("candidate_patch_sha256", {})
    if any(patches.get(name) != reference.sha(path) for name, path in RECOVERY_CORRECTIONS.items()):
        raise ValueError("worker requires the exact complete CRIU recovery corrections")


def supports_exit_hold(result):
    """Read a verified build's page-hold capability without changing its receipt.

    Base-lock rebuilds retain the verified patch chain but can omit the legacy
    convenience flag. Explicit flags keep their original meaning.
    """
    if 'exit_hold_candidate' in result:
        flag = result['exit_hold_candidate']
        if type(flag) is not bool:
            raise ValueError('exit-hold capability flag must be boolean')
        return flag
    patches = result.get('candidate_patch_sha256', {})
    return isinstance(patches, dict) and all(
        isinstance(patches.get(name), str) and re.fullmatch(r'[0-9a-f]{64}', patches[name])
        for name in ['exit-hold.patch', 'exit-hold-socket.patch'])


def load(name, *, base_lock=None):
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
        if file not in {"retained.patch", "early-hold.patch", "exact-wait.patch", "exit-hold.patch",
                        "exit-hold-socket.patch", "nested-overlay.patch",
                        "nested-overlay-stable-path.patch",
                        "nested-overlay-shared-root.patch", "pid-collision-diagnostics.patch",
                        "pid-collision-references.patch", "nested-pid-counter.patch",
                        "pstree-error-propagation.patch", "nested-zombie-peer-pidns.patch", "shared-fds.patch",
                        "namespace-access.patch"} or reference.sha(path / file) != expected:
            raise ValueError("retained-context candidate patch input changed")
    for file, expected in result.get("candidate_overlay_sha256", {}).items():
        if file != "exit-observer.h" or reference.sha(path / file) != expected:
            raise ValueError("retained-context candidate overlay input changed")
    current_lock = root / "upstream/lock.json"
    selected_lock = Path(base_lock) if base_lock is not None else current_lock
    if selected_lock.is_symlink() or reference.sha(selected_lock) != result["base_source_lock_sha256"]:
        raise ValueError("retained-context candidate belongs to a different selected base")
    if base_lock is not None:
        selected_component = json.loads(selected_lock.read_text())["components"]["criu"]
        current_component = json.loads(current_lock.read_text())["components"]["criu"]
        if selected_component != current_component:
            raise ValueError("current CRIU recipe differs from the exact frozen selected base")
    if sources.tree_sha(path / "context") != result["context_sha256"]:
        raise ValueError("retained-context candidate source tree changed")
    component = json.loads((path / "source-lock.json").read_text())["components"]["criu"]
    sources.validate_inputs(component)
    unchanged = sources.check_source(
        path / "context", root / ".cache/downloads" / component["archive"], component)
    return path, result, component, unchanged
