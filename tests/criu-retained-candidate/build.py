"""Build an explicitly unselected CRIU recovery candidate from the pinned tree."""
import copy
import argparse
import json
from pathlib import Path
import shutil
import subprocess
import sys
import uuid

sys.path.insert(0, str(Path(__file__).resolve().parents[2] / "tools"))
import native_base
import reference
import sources

root = reference.ROOT
parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument("--native-base-build", required=True)
options = parser.parse_args()
patch_input = root / "upstream/patches/criu-retained-context-candidate.patch"
lock = json.loads((root / "upstream/lock.json").read_text())
original = lock["components"]["criu"]
sources.validate_inputs(original)
prepared = root / ".cache/sources/criu"
sources.check_source(prepared, root / ".cache/downloads" / original["archive"], original)
original_sha = sources.tree_sha(prepared)
owner = uuid.uuid4().hex
name = "factorybox-build-criu-retained-" + owner
archive = reference.new_artifact(name)
archive.mkdir()
context = archive / "context"
shutil.copytree(prepared, context, symlinks=True)
(context / sources.CACHE_RECORD).unlink(missing_ok=True)
shutil.copyfile(patch_input, archive / "retained.patch")
subprocess.run(["patch", "--batch", "--fuzz=0", "-p1", "-i", str(archive / "retained.patch")],
               cwd=context, check=True, capture_output=True, timeout=15)
candidate = copy.deepcopy(lock)
component = candidate["components"]["criu"]
patch_name = str(patch_input.relative_to(root))
component["patches"].append(patch_name)
component["local_inputs_sha256"][patch_name] = reference.sha(archive / "retained.patch")
for relative in [
    "criu/cr-dump.c", "criu/parasite-syscall.c", "criu/Makefile.crtools",
    "criu/factorybox-retained.c", "criu/include/factorybox-retained.h",
]:
    component.setdefault("prepared_files_sha256", {})[relative] = reference.sha(context / relative)
(archive / "source-lock.json").write_text(json.dumps(candidate, indent=2) + "\n")
recipe = json.loads((root / "upstream/criu-build.json").read_text())
recipe["source_recipe_sha256"] = sources.recipe_sha(component)
(archive / "criu-build.json").write_text(json.dumps(recipe, indent=2) + "\n")
unchanged = sources.check_source(context, root / ".cache/downloads" / original["archive"], component)
context_sha = sources.tree_sha(context)
image, base = native_base.load(options.native_base_build)
command = [
    *reference.DOCKER, "run", "--pull=never", "--name", name,
    "--label", "com.factorybox.run=" + owner, "--cpus=2", "--memory=1g",
    "--pids-limit=128", "--network=none", "--read-only", "--cgroupns=private",
    "--cap-drop=ALL", "--security-opt=no-new-privileges",
    "--env", "SOURCE_DATE_EPOCH=" + str(recipe["source_date_epoch"]),
    "--tmpfs", "/tmp:rw,exec,nosuid,nodev,size=384m",
    "--mount", f"type=bind,source={context},target=/source,readonly",
    "--mount", f"type=bind,source={archive},target=/output",
    "--entrypoint", "/bin/sh", image, "-ec",
    "cc --version > /output/toolchain.txt\n"
    "cp -R /source /tmp/criu-source\n"
    "make -C /tmp/criu-source -j2 criu/criu > /output/build.log 2>&1\n"
    "cp /tmp/criu-source/criu/criu /output/criu\n"
    "/output/criu --version > /output/version.txt\n",
]
result = {
    "owner": owner, "command": command, "builder": image,
    "native_base_build": options.native_base_build,
    "base_source_lock_sha256": reference.sha(root / "upstream/lock.json"),
    "source_lock_sha256": reference.sha(archive / "source-lock.json"),
    "patch_sha256": reference.sha(archive / "retained.patch"),
    "context_sha256": context_sha, "build_inputs_sha256": reference.sha(archive / "criu-build.json"),
    "native_base_packages_sha256": base["packages_sha256"],
    "unchanged_upstream_test_files": unchanged,
    "scope": "Unselected single-process recovery candidate. Retained C context cures only after "
             "the original tracer and worker exit, with the exact original live guard's canonical "
             "grant or an explicitly unauthorized held-source diagnostic after both controllers exit. "
             "Only the Go guard may authorize source release. No production qualification.",
    "passed": False, "cleanup": False, "diagnostic_only": True, "production_qualified": False,
}
(archive / "intent.json").write_text(json.dumps(result, indent=2) + "\n")
print(json.dumps({"building": str(archive)}), flush=True)
try:
    with (archive / "stdout.txt").open("x") as out, (archive / "stderr.txt").open("x") as err:
        done = subprocess.run(command, stdout=out, stderr=err, timeout=300)
    result["exit_code"] = done.returncode
    result["container_state"] = json.loads(reference.docker(["container", "inspect", name]))[0]["State"]
    result["inputs_unchanged"] = (
        sources.tree_sha(context) == context_sha and sources.tree_sha(prepared) == original_sha)
    result["passed"] = done.returncode == 0 and not result["container_state"]["OOMKilled"] and result["inputs_unchanged"]
    if (archive / "criu").exists():
        result["binary_sha256"] = reference.sha(archive / "criu")
except Exception as error:
    result["error"] = str(error)
finally:
    reference.owned_delete("container", name, owner)
    result["cleanup"] = True
    (archive / "result.json").write_text(json.dumps(result, indent=2) + "\n")
    print(json.dumps({"evidence": str(archive), "passed": result["passed"],
                      "cleanup": result["cleanup"]}), flush=True)
if not result["passed"]:
    raise RuntimeError("candidate build failed: " + str(archive))
