#!/usr/bin/env python3
"""Run portable checks with retained results and disk-backed temporary fixtures."""
import json
import os
import subprocess
import sys
import time
import uuid

import reference


def main():
    artifact = reference.new_artifact("factorybox-checks-" + uuid.uuid4().hex)
    artifact.mkdir()
    temporary = artifact / "tmp"
    temporary.mkdir(mode=0o700)
    commands = [
        ["go", "test", "./cmd/...", "./internal/...", "./tests/..."],
        ["go", "test", "-tags=factorybox_worker_probe", "-run=^$", "./tests/devx-lifecycle"],
        [sys.executable, "-m", "unittest", "discover", "-s", "tools", "-p", "test_*.py"],
        [sys.executable, "tools/reference.py", "verify"],
        [sys.executable, "tools/verify_fixture_extensions.py"],
        ["git", "diff", "--check"],
    ]
    result = {"schema": 1, "passed": False, "native_qualified": False,
              "production_qualified": False, "checks": [], "temporary_root": str(temporary)}
    try:
        for index, command in enumerate(commands):
            started = time.monotonic()
            with (artifact / f"{index}.stdout").open("wb") as out, (artifact / f"{index}.stderr").open("wb") as err:
                completed = subprocess.run(command, cwd=reference.ROOT, stdout=out, stderr=err,
                                           env=dict(os.environ, TMPDIR=str(temporary)), timeout=120)
            observation = {"command": command, "exit_code": completed.returncode,
                           "seconds": time.monotonic() - started}
            result["checks"].append(observation)
            if completed.returncode:
                return 1
        result["passed"] = True
        return 0
    finally:
        (artifact / "result.json").write_text(json.dumps(result, indent=2) + "\n")
        print(json.dumps({"evidence": str(artifact), **result}), flush=True)


if __name__ == "__main__":
    raise SystemExit(main())
