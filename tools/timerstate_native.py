#!/usr/bin/env python3
"""Native owned timer-reader prerequisite, not lifecycle or platform qualification."""
import argparse
import json
import os
from pathlib import Path
import subprocess
import uuid

import build_config
import native_base
import reference
import reference_lifecycle


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--platform", required=True, choices=build_config.PLATFORMS)
    parser.add_argument("--native-base-build", required=True)
    options = parser.parse_args()
    host = json.loads(reference.docker(["info", "--format", "{{json .}}"]))
    native_base.require_native_host(options.platform, host)
    image, base = native_base.load(options.native_base_build, for_build=True,
                                  candidate_platform=options.platform)
    build_config.require_platform(options.platform, base)
    owner = uuid.uuid4().hex
    name = "factorybox-timerstate-native-" + owner
    archive = reference.new_artifact(name)
    archive.mkdir()
    (archive / "runner.py").write_bytes(Path(__file__).read_bytes())
    binary = archive / "timerstate.test"
    result = {"schema": 1, "owner": owner, "platform": options.platform, "passed": False,
              "cleanup": False, "native_lifecycle_qualified": False, "production_qualified": False,
              "scope": "Actual owned Linux relative timer/BPF reader only; no runtime admission"}
    attempted = False
    try:
        compile = ["go", "test", "-c", "-trimpath", "-buildvcs=false", "-o", str(binary), "./internal/timerstate"]
        subprocess.run(compile, cwd=reference.ROOT, check=True,
                       env=dict(os.environ, GOOS="linux", GOARCH=build_config.architecture(options.platform),
                                CGO_ENABLED="0"), timeout=120)
        build_config.require_elf(binary, options.platform)
        result["binary_sha256"] = reference.sha(binary)
        command = [*reference.DOCKER, "run", "--pull=never", "--platform=" + options.platform,
                   "--name", name, "--label", "com.factorybox.run=" + owner,
                   "--privileged", "--network=none", "--read-only", "--cpus=1", "--memory=256m",
                   "--pids-limit=64", "--env", "FACTORYBOX_TIMERSTATE_NATIVE=1",
                   "--mount", f"type=bind,source={binary},target=/timerstate.test,readonly",
                   "--entrypoint", "/timerstate.test", image, "-test.v", "-test.timeout=15s",
                   "-test.run=^TestNativeOwnedRelativeTimerState$"]
        result["command"] = command
        reference_lifecycle.write_json(archive / "intent.json", result)
        attempted = True
        done = subprocess.run(command, capture_output=True, text=True, timeout=120)
        (archive / "stdout.txt").write_text(done.stdout)
        (archive / "stderr.txt").write_text(done.stderr)
        result["exit_code"] = done.returncode
        result["passed"] = (done.returncode == 0 and "--- PASS: TestNativeOwnedRelativeTimerState" in done.stdout
                            and "--- SKIP:" not in done.stdout)
        if not result["passed"]:
            raise ValueError("native timer reader prerequisite failed")
    except Exception as error:
        if isinstance(error, subprocess.TimeoutExpired):
            (archive / "stdout.txt").write_bytes(error.stdout or b"")
            (archive / "stderr.txt").write_bytes(error.stderr or b"")
        result["error"] = str(error)
        raise
    finally:
        if attempted:
            reference.owned_delete("container", name, owner)
        result["cleanup"] = True
        reference_lifecycle.write_json(archive / "result.json", result)
        print(json.dumps({"evidence": str(archive), **result}), flush=True)


if __name__ == "__main__":
    main()
