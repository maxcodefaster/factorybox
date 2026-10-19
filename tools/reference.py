#!/usr/bin/env python3
"""Build and run the retained reference; this is not the production runtime."""
import argparse
import datetime
import hashlib
import json
import os
import pathlib
import subprocess
import uuid

ROOT = pathlib.Path(__file__).resolve().parents[1]
REFERENCE = ROOT / "tests/reference"
OUTPUT = ROOT / "dist/reference/linux-arm64"
BASE = "runtime-fork-criu-probe:raw-eager-base"
BASE_ID = "sha256:041140e968e8efdbc6b2f33651f84bc19c13593c6d3f527e90af648f967dd52f"
DOCKER = ["docker", "--context", "orbstack"]


def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def verify():
    provenance = json.loads((REFERENCE / "provenance.json").read_text())
    for name, expected in provenance["files_sha256"].items():
        path = pathlib.PurePosixPath(name)
        if path.is_absolute() or ".." in path.parts:
            raise ValueError("unsafe provenance path")
        file = ROOT / path
        if file.is_symlink() or sha(file) != expected:
            raise ValueError(f"reference source differs: {name}")
    print(json.dumps({"reference_files_verified": len(provenance["files_sha256"]),
                      "production_qualified": False}), flush=True)


def run_logged(command, cwd, env=None, timeout=120):
    completed = subprocess.run(command, cwd=cwd, env=env, capture_output=True,
                               text=True, timeout=timeout)
    print(json.dumps({"command": command, "cwd": str(cwd.relative_to(ROOT)),
                      "exit_code": completed.returncode,
                      "stdout": completed.stdout, "stderr": completed.stderr}), flush=True)
    completed.check_returncode()


def build():
    verify()
    OUTPUT.mkdir(parents=True, exist_ok=True)
    env = dict(os.environ, GOOS="linux", GOARCH="arm64", CGO_ENABLED="0")
    for module, package, name in [
        ("driver", "./cmd/lab", "lab"), ("driver", "./cmd/ramctl", "ramctl"),
        ("keeper", ".", "keeper")]:
        run_logged(["go", "build", "-trimpath", "-buildvcs=false",
                    "-o", str(OUTPUT / name), package], REFERENCE / module, env)
    for module, package, name in [
        ("driver", "./cmd/lab", "lab.test"), ("driver", "./cmd/ramctl", "ramctl.test"),
        ("keeper", ".", "keeper.test"), ("pageview", ".", "pageview.test")]:
        run_logged(["go", "test", "-c", "-o", str(OUTPUT / name), package],
                   REFERENCE / module, env)
    (OUTPUT / "manifest.json").write_text(json.dumps({
        "platform": "linux/arm64", "go_version": subprocess.check_output(
            ["go", "version"], text=True).strip(),
        "source_manifest_sha256": sha(REFERENCE / "provenance.json"),
        "files_sha256": {p.name: sha(p) for p in OUTPUT.iterdir()
                         if p.is_file() and p.name != "manifest.json"},
        "native_qualification": False,
    }, indent=2) + "\n")


def docker(args, timeout=15):
    return subprocess.run(DOCKER + args, check=True, capture_output=True,
                          text=True, timeout=timeout).stdout.strip()


def owned_delete(kind, name, owner):
    try:
        data = json.loads(docker([kind, "inspect", name]))[0]
    except subprocess.CalledProcessError as error:
        message = (error.stderr or "").lower()
        if f"no such {kind}" in message and name in message:
            return
        raise
    labels = data.get("Labels") if kind == "volume" else data["Config"]["Labels"]
    actual_name = data.get("Name", "").removeprefix("/")
    if actual_name != name or not labels or labels.get("com.factorybox.run") != owner:
        raise ValueError(f"refusing to remove foreign {kind}: {name}")
    # Unlinking a stopped multi-gigabyte developer volume can exceed the
    # ordinary inspection budget. Ownership checks above stay unchanged.
    docker([kind, "rm", *(["--force"] if kind == "container" else []), name],
           timeout=60 if kind == "volume" else 15)


def native():
    verify()
    manifest = json.loads((OUTPUT / "manifest.json").read_text())
    if manifest["source_manifest_sha256"] != sha(REFERENCE / "provenance.json"):
        raise ValueError("native build belongs to another source reference")
    for name, expected in manifest["files_sha256"].items():
        if sha(OUTPUT / name) != expected:
            raise ValueError(f"native build differs: {name}")
    owner = uuid.uuid4().hex
    name = "factorybox-native-" + owner
    evidence = ROOT / "artifacts" / name
    evidence.mkdir(parents=True)
    command = [
        *DOCKER, "run", "--pull=never", "--name", name,
        "--label", "com.factorybox.run=" + owner,
        "--cgroupns=private", "--privileged", "--cpus=1", "--memory=256m",
        "--pids-limit=64", "--network=none", "--read-only",
        "--env", "RUNTIME_FORK_GRAPH_NATIVE=1", "--env", "GOMAXPROCS=1",
        "--env", "TMPDIR=/experiment",
        "--env", "RUNTIME_FORK_SNAPSHOT_OWNER_NATIVE=1",
        "--mount", f"type=volume,source={name},target=/experiment",
    ]
    for file in ["lab.test", "ramctl.test"]:
        command += ["--mount", f"type=bind,source={OUTPUT / file},target=/{file},readonly"]
    command += ["--entrypoint", "/bin/sh", BASE_ID, "-ec",
                "uname -r\n"
                "/lab.test -test.run='^Test(DescendantParentPin|DescendantResourceSample|"
                "DescendantCommandFailure|DescendantOutcome|SnapshotHashCoalescing|"
                "InheritedMemory|Oracle|ImageGraph|DiagnosticPayloadRetirement|"
                "RangeSnapshot|StreamOriginal)' -test.v -test.timeout=15s\n"
                "/ramctl.test -test.run='^TestStreamSnapshotOwner' -test.v -test.timeout=15s"]
    result = {"schema": 1, "owner": owner, "command": command,
              "started_at": datetime.datetime.now(datetime.timezone.utc).isoformat(),
              "source_manifest_sha256": sha(REFERENCE / "provenance.json"),
              "production_qualified": False, "passed": False, "cleanup": False}
    volume_created = container_started = False
    try:
        docker(["volume", "create", "--label", "com.factorybox.run=" + owner, name])
        volume_created = True
        container_started = True
        completed = subprocess.run(command, capture_output=True, text=True, timeout=65)
        (evidence / "stdout.txt").write_text(completed.stdout)
        (evidence / "stderr.txt").write_text(completed.stderr)
        result["exit_code"] = completed.returncode
        state = json.loads(docker(["container", "inspect", name]))[0]
        result["container_state"] = state["State"]
        result["passed"] = completed.returncode == 0 and not state["State"]["OOMKilled"]
        result["top_level_tests_passed"] = sum(
            line.startswith("--- PASS: ") for line in completed.stdout.splitlines())
        if not result["passed"] or result["top_level_tests_passed"] < 50:
            raise ValueError("native regression failed or original test coverage missing")
    except Exception as error:
        result["error"] = str(error)
        if isinstance(error, subprocess.TimeoutExpired):
            for field, value in [("stdout", error.stdout), ("stderr", error.stderr)]:
                if value:
                    (evidence / (field + ".txt")).write_text(
                        value.decode(errors="replace") if isinstance(value, bytes) else value)
        raise
    finally:
        try:
            if container_started:
                owned_delete("container", name, owner)
            if volume_created:
                owned_delete("volume", name, owner)
            result["cleanup"] = True
        finally:
            (evidence / "result.json").write_text(json.dumps(result, indent=2) + "\n")
            print(json.dumps({"evidence": str(evidence), **result}), flush=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("operation", choices=["verify", "check", "build", "native"])
    args = parser.parse_args()
    if args.operation == "verify":
        verify()
    elif args.operation == "check":
        verify()
        run_logged(["go", "test", "./cmd/lab"], REFERENCE / "driver")
    elif args.operation == "build":
        build()
    else:
        native()


if __name__ == "__main__":
    main()
