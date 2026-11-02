"""Exercise the actual CRIU inherited-open branch with real shared mmap."""
import argparse
import hashlib
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
import native_base
import reference


def inherited_branch(text):
    start = text.index("\tif (factorybox_inherited_fd(d, &tmp)) {")
    end = text.index("\n\tif (inherited_fd(d, &tmp))", start)
    if text.count("\tif (factorybox_inherited_fd(d, &tmp)) {") != 1:
        raise ValueError("ambiguous actual CRIU inherited-open branch")
    return text[start:end]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--criu-candidate", required=True)
    parser.add_argument("--native-base-build", required=True)
    parser.add_argument("--ordered-patch", type=Path)
    args = parser.parse_args()
    original, previous, _, _ = criu_candidate.load(args.criu_candidate)
    image, base = native_base.load(args.native_base_build, for_build=True)
    native_base.require_native_host(base["platform"], json.loads(reference.docker(["info", "--format", "{{json .}}"])))
    if previous["platform"] != base["platform"]:
        raise ValueError("CRIU source and native compiler targets differ")
    owner = uuid.uuid4().hex
    name = "factorybox-criu-shared-filemap-" + owner
    artifact = reference.new_artifact(name)
    artifact.mkdir(mode=0o700)
    source = artifact / "source/criu/files-reg.c"
    source.parent.mkdir(parents=True)
    shutil.copyfile(original / "context/criu/files-reg.c", source)
    shutil.copyfile(__file__, artifact / "runner.py")
    shutil.copyfile(Path(__file__).with_name("fixture.c"), artifact / "fixture.c")
    result = {
        "schema": 1, "owner": owner, "passed": False, "cleanup": False,
        "platform": base["platform"], "criu_candidate": original.name,
        "original_source_sha256": reference.sha(source),
        "fixture_sha256": reference.sha(artifact / "fixture.c"),
        "production_qualified": False,
        "scope": "Actual pinned CRIU inherited-open branch, inert directory/fown services and real Linux open descriptions/shared mmap. No Sandbox, retained installation, model call or full lifecycle qualification.",
    }
    attempted = False
    try:
        if args.ordered_patch:
            patch = args.ordered_patch.resolve()
            if patch.parent != ROOT / "upstream/patches":
                raise ValueError("require the explicit repository-owned ordered patch")
            targets = [line.removeprefix("+++ b/") for line in patch.read_text().splitlines()
                       if line.startswith("+++ b/")]
            if targets != ["criu/files-reg.c"]:
                raise ValueError("mapping delta must change only its regular-file adapter")
            shutil.copyfile(patch, artifact / "ordered.patch")
            patched = subprocess.run(["patch", "--batch", "--forward", "--fuzz=0", "-p1",
                                      "-i", str(artifact / "ordered.patch")],
                                     cwd=artifact / "source", capture_output=True, text=True, timeout=30)
            (artifact / "patch.log").write_text(patched.stdout + patched.stderr)
            patched.check_returncode()
            result["ordered_patch_sha256"] = reference.sha(artifact / "ordered.patch")
        branch = inherited_branch(source.read_text())
        fixture = (artifact / "fixture.c").read_text()
        assert fixture.count("/* @CRIU_INHERITED_OPEN_BRANCH@ */") == 1
        generated = artifact / "mapping.c"
        generated.write_text(fixture.replace("/* @CRIU_INHERITED_OPEN_BRANCH@ */", branch))
        result["branch_sha256"] = hashlib.sha256(branch.encode()).hexdigest()
        result["source_sha256"] = reference.sha(source)
        result["generated_sha256"] = reference.sha(generated)
        command = [*reference.DOCKER, "run", "--pull=never", "--name", name,
                   "--label", "com.factorybox.run=" + owner, "--network=none", "--read-only",
                   "--user", f"{os.getuid()}:{os.getgid()}", "--cpus=1", "--memory=256m",
                   "--pids-limit=32", "--cap-drop=ALL", "--security-opt=no-new-privileges",
                   "--tmpfs", "/tmp:rw,exec,nosuid,nodev,size=16m,mode=1777",
                   "--mount", f"type=bind,source={artifact},target=/proof,readonly",
                   "--entrypoint", "/bin/sh", image, "-ec",
                   "cc -std=gnu11 -O2 -Wall -Wextra -Werror /proof/mapping.c -o /tmp/mapping; /tmp/mapping"]
        attempted = True
        done = subprocess.run(command, capture_output=True, text=True, timeout=30)
        (artifact / "stdout.log").write_text(done.stdout)
        (artifact / "stderr.log").write_text(done.stderr)
        result["exit_code"] = done.returncode
        result["inputs_unchanged"] = (
            reference.sha(source) == result["source_sha256"] and
            reference.sha(generated) == result["generated_sha256"] and
            reference.sha(original / "context/criu/files-reg.c") == result["original_source_sha256"])
        result["passed"] = done.returncode == 0 and result["inputs_unchanged"]
        print(done.stdout, end="")
        print(done.stderr, end="")
    except Exception as error:
        result["error"] = str(error)
    finally:
        if attempted:
            reference.owned_delete("container", name, owner)
        result["cleanup"] = True
        (artifact / "result.json").write_text(json.dumps(result, indent=2) + "\n")
        print(json.dumps({"evidence": str(artifact), **result}), flush=True)
    return 0 if result["passed"] else 1


if __name__ == "__main__":
    raise SystemExit(main())
