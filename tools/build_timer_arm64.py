#!/usr/bin/env python3
"""Assemble exact ARM64 timer templates using one pinned native base."""
import argparse
import json
from pathlib import Path
import shutil
import subprocess
import uuid

import native_base
import reference
import reference_lifecycle


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--native-base-build", required=True)
    options = parser.parse_args()
    host = json.loads(reference.docker(["info", "--format", "{{json .}}"]))
    native_base.require_native_host("linux/arm64", host)
    image, _ = native_base.load(options.native_base_build, for_build=True,
                               candidate_platform="linux/arm64")
    owner = uuid.uuid4().hex
    name = "factorybox-timer-arm64-code-" + owner
    archive = reference.new_artifact(name)
    source, output = archive / "source", archive / "output"
    source.mkdir(parents=True)
    output.mkdir()
    for filename in ["continuation-arm64.S", "kernel-arm64.S"]:
        shutil.copyfile(reference.ROOT / "internal/lifecycle/timer-code" / filename, source / filename)
    (archive / "runner.py").write_bytes(Path(__file__).read_bytes())
    result = {"schema": 1, "owner": owner, "image": image, "platform": "linux/arm64",
              "passed": False, "cleanup": False, "native_lifecycle_qualified": False,
              "source_sha256": {p.name: reference.sha(p) for p in source.iterdir()}}
    command = [*reference.DOCKER, "run", "--pull=never", "--name", name,
               "--label", "com.factorybox.run=" + owner, "--network=none", "--read-only",
               "--cpus=1", "--memory=256m", "--pids-limit=32",
               "--mount", f"type=bind,source={source},target=/source,readonly",
               "--mount", f"type=bind,source={output},target=/output",
               "--entrypoint", "/bin/bash", image, "-euc",
               "for name in continuation kernel; do "
               "as -o /output/$name.o /source/$name-arm64.S; "
               "ld -Ttext=0 -e timer_restart_entry -o /output/$name.elf /output/$name.o; "
               "objcopy -O binary -j .text /output/$name.elf /output/$name.bin; "
               "nm -P /output/$name.elf > /output/$name.symbols; "
               "done; as --version | head -1; sha256sum /usr/bin/as /usr/bin/ld /usr/bin/objcopy"]
    result["command"] = command
    attempted = False
    try:
        attempted = True
        done = subprocess.run(command, capture_output=True, text=True, timeout=120)
        (archive / "stdout.txt").write_text(done.stdout)
        (archive / "stderr.txt").write_text(done.stderr)
        done.check_returncode()
        templates = {}
        for kind in ["continuation", "kernel"]:
            data = (output / f"{kind}.bin").read_bytes()
            if not data or len(data) > 4096 or len(data) % 4:
                raise ValueError("invalid ARM64 template bound")
            symbols = {fields[0]: int(fields[2], 16) for line in
                       (output / f"{kind}.symbols").read_text().splitlines()
                       if len(fields := line.split()) >= 3 and fields[0].startswith("timer_")}
            if symbols["timer_restart_entry"] != 0 or symbols["timer_code_end"] != len(data):
                raise ValueError("ARM64 template entry or complete extent differs")
            offset = symbols["timer_return_branch"]
            if data[offset:offset + 4] != bytes.fromhex("00000014"):
                raise ValueError("ARM64 return branch is not the exact unpatched marker")
            templates[kind] = {"sha256": reference.sha(output / f"{kind}.bin"), "bytes": len(data),
                               "symbols": symbols}
        result["templates"] = templates
        result["inputs_unchanged"] = result["source_sha256"] == {
            p.name: reference.sha(p) for p in source.iterdir()}
        result["passed"] = result["inputs_unchanged"]
    except Exception as error:
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
