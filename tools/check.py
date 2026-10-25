#!/usr/bin/env python3
"""Run portable checks with retained results and disk-backed temporary fixtures."""
import json
import os
import subprocess
import sys
import tempfile
import time
import uuid

import reference


def go_commands(packages):
    commands = [
        ["go", "test", *[package for package in packages if package != "factorybox/internal/fdstore"]],
    ]
    if "factorybox/internal/fdstore" in packages:
        commands.append(["go", "test", "./internal/fdstore"])
    if "factorybox/tests/devx-lifecycle" in packages:
        commands.append(["go", "test", "-tags=factorybox_worker_probe", "-run=^$", "./tests/devx-lifecycle"])
    return commands


def main():
    artifact = reference.new_artifact("factorybox-checks-" + uuid.uuid4().hex)
    artifact.mkdir()
    temporary = artifact / "tmp"
    temporary.mkdir(mode=0o700)
    # Only the tiny fdstore IPC fixtures need a short socket pathname. Keep
    # every larger fixture disk-backed in the artifact. A /proc alias cannot
    # stand in for canonical paths in the source-identity regression tests.
    socket_temporary = tempfile.TemporaryDirectory(prefix="fbx-", dir="/tmp")
    packages = subprocess.check_output(
        ["go", "list", "./cmd/...", "./internal/...", "./tests/..."],
        cwd=reference.ROOT, text=True, timeout=30).split()
    commands = [
        *go_commands(packages),
        [sys.executable, "-m", "unittest", "discover", "-s", "tools", "-p", "test_*.py"],
        [sys.executable, "tools/reference.py", "verify"],
        [sys.executable, "tools/verify_fixture_extensions.py"],
        ["git", "diff", "--check"],
    ]
    result = {"schema": 1, "passed": False, "native_qualified": False,
              "production_qualified": False, "checks": [], "temporary_root": str(temporary),
              "unavailable_linux_packages": [name for name in
                  ["factorybox/internal/fdstore", "factorybox/tests/devx-lifecycle",
                   "factorybox/tests/shared-source-actor"] if name not in packages]}
    try:
        for index, command in enumerate(commands):
            started = time.monotonic()
            with (artifact / f"{index}.stdout").open("wb") as out, (artifact / f"{index}.stderr").open("wb") as err:
                completed = subprocess.run(command, cwd=reference.ROOT, stdout=out, stderr=err,
                                           env=dict(os.environ, TMPDIR=socket_temporary.name if command == ["go", "test", "./internal/fdstore"] else str(temporary)), timeout=120)
            observation = {"command": command, "exit_code": completed.returncode,
                           "seconds": time.monotonic() - started}
            result["checks"].append(observation)
            if completed.returncode:
                return 1
        result["passed"] = True
        return 0
    finally:
        socket_temporary.cleanup()
        (artifact / "result.json").write_text(json.dumps(result, indent=2) + "\n")
        print(json.dumps({"evidence": str(artifact), **result}), flush=True)


if __name__ == "__main__":
    raise SystemExit(main())
