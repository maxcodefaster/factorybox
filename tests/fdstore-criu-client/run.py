#!/usr/bin/env python3
"""Run the actual patched CRIU descriptor reader against the Go worker store."""
import argparse
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import uuid

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / "tools"))
import criu_candidate
import descriptor_transfer_proof
import native_base
import reference


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--criu-candidate", required=True)
    parser.add_argument("--native-base-build", required=True)
    parser.add_argument("--ordered-patch", type=Path)
    args = parser.parse_args()
    original, previous, _, _ = criu_candidate.load(args.criu_candidate)
    image, native = native_base.load(args.native_base_build, for_build=True)
    platform = native.get("platform", "linux/arm64")
    native_base.require_native_host(platform, json.loads(reference.docker(["info", "--format", "{{json .}}"])))
    owner = uuid.uuid4().hex
    artifact = reference.new_artifact("factorybox-fdstore-criu-" + owner)
    artifact.mkdir(mode=0o700)
    context = artifact / "source"
    context.mkdir()
    go_source = artifact / "go-source"
    for directory in ["internal", "cmd/factorybox"]:
        shutil.copytree(ROOT / directory, go_source / directory)
    for name in ["go.mod", "go.sum"]:
        shutil.copy2(ROOT / name, go_source / name)
    shutil.copy2(__file__, artifact / "runner.py")
    shutil.copy2(Path(__file__).with_name("client.c"), artifact / "client-fixture.c")
    for name in ["criu/files.c", "criu/crtools.c"]:
        target = context / name
        target.parent.mkdir(exist_ok=True)
        shutil.copy2(original / "context" / name, target)
    if args.ordered_patch:
        patch = args.ordered_patch.resolve()
        if patch.parent != ROOT / "upstream/patches":
            raise ValueError("require the explicit repository-owned ordered patch")
        shutil.copy2(patch, artifact / "ordered.patch")
        patched = subprocess.run(["patch", "--batch", "--forward", "--fuzz=0", "-p1", "-i", str(patch)],
                                 cwd=context, capture_output=True, text=True, timeout=30)
        (artifact / "patch.log").write_text(patched.stdout + patched.stderr)
        patched.check_returncode()
    source = (context / "criu/files.c").read_text()
    start = source.index("/* Same-worker resources are received")
    end = source.index("\nbool inherited_fd(", start)
    functions = source[start:end]
    fixture = (artifact / "client-fixture.c").read_text()
    assert fixture.count("/* @CRIU_SHARED_DESCRIPTION_CLIENT@ */") == 1
    (artifact / "client.c").write_text(fixture.replace("/* @CRIU_SHARED_DESCRIPTION_CLIENT@ */", functions))
    result = {
        "schema": 1, "passed": False, "cleanup": False, "nativePlatform": platform,
        "platform": platform,
        "criuCandidate": original.name, "criuResultSHA256": reference.sha(original / "result.json"),
        "criuBinarySHA256": reference.sha(original / "criu"),
        "nativeBaseBuild": args.native_base_build,
        "goSourceContext": descriptor_transfer_proof.go_source_context(go_source),
        "originalContextSHA256": previous["context_sha256"],
        "actualCRIUFunctionsSHA256": reference.sha(context / "criu/files.c"),
        "generatedClientSHA256": reference.sha(artifact / "client.c"),
        "scope": "Native root PID1 Go store and the actual extracted CRIU C reader. No Sysbox Sandbox, checkpoint/restore, retained worker, Source or model mutation. Not production qualification.",
    }
    (artifact / "intent.json").write_text(json.dumps(result, indent=2))
    name = "factorybox-fdstore-criu-" + owner
    attempted = feature_attempted = False
    try:
        compile_name = name + "-build"
        command = [*reference.DOCKER, "run", "--rm", "--pull=never", "--name", compile_name,
                   "--label", "com.factorybox.run=" + owner, "--network=none", "--read-only",
                   "--cpus=1", "--memory=768m", "--pids-limit=64", "--cap-drop=ALL",
                   "--security-opt=no-new-privileges", "--tmpfs", "/tmp:rw,exec,nosuid,nodev,size=64m",
                   "--user", f"{os.getuid()}:{os.getgid()}",
                   "--mount", f"type=bind,source={artifact},target=/output",
                   "--entrypoint", "/bin/sh", image, "-ec",
                   "cc -std=gnu11 -O2 -Wall -Wextra -Werror /output/client.c -o /output/criu-client"]
        try:
            build = subprocess.run(command, capture_output=True, text=True, timeout=60)
            (artifact / "compile.log").write_text(build.stdout + build.stderr)
            build.check_returncode()
        finally:
            reference.owned_delete("container", compile_name, owner)
        for package, binary in [("fdstore", "fdstore.test"), ("sysboxcapture", "features.test")]:
            compile_go = subprocess.run(
                ["go", "test", "-c", "-trimpath", "-buildvcs=false", "-o",
                 str(artifact / binary), "./internal/" + package],
                cwd=go_source,
                env=dict(os.environ, GOOS="linux", GOARCH=platform.split("/")[1],
                         CGO_ENABLED="0", GOPROXY="off", GOSUMDB="off", GOWORK="off"),
                capture_output=True, text=True, timeout=120)
            (artifact / (binary + ".build.log")).write_text(compile_go.stdout + compile_go.stderr)
            compile_go.check_returncode()
        command = [*reference.DOCKER, "create", "--pull=never", "--name", name,
                   "--label", "com.factorybox.run=" + owner, "--network=none", "--read-only",
                   "--cpus=1", "--memory=768m", "--pids-limit=64", "--cap-drop=ALL",
                   "--security-opt=no-new-privileges", "--tmpfs", "/tmp:rw,nosuid,nodev,size=64m,mode=1777",
                   "--mount", f"type=bind,source={artifact},target=/proof,readonly",
                   "--env", "FACTORYBOX_FDSTORE_CRIU_CLIENT=/proof/criu-client",
                   "--entrypoint", "/proof/fdstore.test", image, "-test.v"]
        # The proof container runs as root without CAP_DAC_OVERRIDE, so on a Linux
        # host it needs search permission on the owner-only artifact directory.
        artifact.chmod(0o711)
        attempted = True
        subprocess.run(command, check=True, capture_output=True, timeout=30)
        tested = subprocess.run([*reference.DOCKER, "start", "-a", name], capture_output=True, text=True, timeout=90)
        (artifact / "stdout.log").write_text(tested.stdout)
        (artifact / "stderr.log").write_text(tested.stderr)
        observed = json.loads(reference.docker(["inspect", name]))[0]
        result.update(exitCode=tested.returncode, oomKilled=observed["State"]["OOMKilled"],
                      testsPassed=tested.stdout.count("--- PASS:"), testsSkipped=tested.stdout.count("--- SKIP:"))
        feature_command = list(command)
        feature_command[feature_command.index(name)] = name + "-features"
        feature_command[feature_command.index("/proof/fdstore.test")] = "/proof/features.test"
        # InspectFeatures executes a hash-verified descriptor. Its synthetic
        # shell fixtures need executable scratch, unlike the IPC-only tests.
        temporary = feature_command.index("--tmpfs") + 1
        feature_command[temporary] = "/tmp:rw,exec,nosuid,nodev,size=64m,mode=1777"
        feature_command += ["-test.run=^TestCriuFeatures$"]
        feature_attempted = True
        subprocess.run(feature_command, check=True, capture_output=True, timeout=30)
        features = subprocess.run([*reference.DOCKER, "start", "-a", name + "-features"],
                                  capture_output=True, text=True, timeout=30)
        (artifact / "features.stdout.log").write_text(features.stdout)
        (artifact / "features.stderr.log").write_text(features.stderr)
        feature_state = json.loads(reference.docker(["inspect", name + "-features"]))[0]["State"]
        result.update(featureExitCode=features.returncode, featureOOMKilled=feature_state["OOMKilled"],
                      featureTestsSkipped=features.stdout.count("--- SKIP:"),
                      passedTests=sorted({line.split()[2] for line in
                                          (tested.stdout + features.stdout).splitlines()
                                          if line.startswith("--- PASS: ")}))
        result["inputsSHA256"] = {
            name: reference.sha(artifact / name) for name in descriptor_transfer_proof.INPUTS}
        result["passed"] = (
            tested.returncode == 0 and features.returncode == 0 and
            not result["oomKilled"] and not result["featureOOMKilled"] and
            not result["testsSkipped"] and not result["featureTestsSkipped"] and
            descriptor_transfer_proof.REQUIRED_TESTS.issubset(result["passedTests"]) and
            descriptor_transfer_proof.go_source_context(go_source) == result["goSourceContext"])
        print(tested.stdout)
        print(features.stdout)
        return 0 if result["passed"] else 1
    finally:
        if feature_attempted:
            reference.owned_delete("container", name + "-features", owner)
        if attempted:
            reference.owned_delete("container", name, owner)
        result["cleanup"] = True
        (artifact / "result.json").write_text(json.dumps(result, indent=2))
        summary = {key: value for key, value in result.items() if key != "goSourceContext"}
        print(json.dumps({"evidence": str(artifact), **summary}), flush=True)


if __name__ == "__main__":
    raise SystemExit(main())
