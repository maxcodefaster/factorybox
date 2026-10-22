"""Build an explicitly unselected CRIU recovery candidate from the pinned tree."""
import copy
import argparse
import json
import os
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
parser.add_argument("--exit-hold", action="store_true",
                    help="Freeze the original source at the tracer exit stop, before tracee detach")
parser.add_argument("--exit-hold-socket", action="store_true",
                    help="Keep the observer through socket EOF until the parent's terminal event")
parser.add_argument("--nested-overlay", action="store_true",
                    help="Add the unselected captured-layer mount-v2 diagnostic patch")
parser.add_argument("--nested-overlay-stable-paths", action="store_true",
                    help="Keep original layer names through a private temporary alias root")
parser.add_argument("--nested-overlay-shared-root", action="store_true",
                    help="Bind each captured covering filesystem once so upper/work share a mount")
parser.add_argument("--pid-collision-diagnostics", action="store_true",
                    help="Read bounded process/pidfd metadata only after clone3 fails with EEXIST; no behavioral fix")
options = parser.parse_args()
if options.exit_hold_socket and not options.exit_hold:
    parser.error("--exit-hold-socket requires --exit-hold")
if options.nested_overlay_stable_paths and not options.nested_overlay:
    parser.error("--nested-overlay-stable-paths requires --nested-overlay")
if options.nested_overlay_shared_root and not options.nested_overlay_stable_paths:
    parser.error("--nested-overlay-shared-root requires --nested-overlay-stable-paths")
patch_inputs = [
    (root / "upstream/patches/criu-retained-context-candidate.patch", "retained.patch"),
    (root / "upstream/patches/criu-retained-early-hold-candidate.patch", "early-hold.patch"),
    (root / "upstream/patches/criu-retained-exact-wait-candidate.patch", "exact-wait.patch"),
]
if options.exit_hold:
    patch_inputs.append(
        (root / "upstream/patches/criu-retained-exit-hold-candidate.patch", "exit-hold.patch"))
if options.exit_hold_socket:
    patch_inputs.append(
        (root / "upstream/patches/criu-retained-exit-socket-candidate.patch", "exit-hold-socket.patch"))
if options.nested_overlay:
    patch_inputs.append(
        (root / "upstream/patches/criu-nested-overlay-candidate.patch", "nested-overlay.patch"))
if options.nested_overlay_stable_paths:
    patch_inputs.append(
        (root / "upstream/patches/criu-nested-overlay-stable-path-candidate.patch",
         "nested-overlay-stable-path.patch"))
if options.nested_overlay_shared_root:
    patch_inputs.append(
        (root / "upstream/patches/criu-nested-overlay-shared-root-candidate.patch",
         "nested-overlay-shared-root.patch"))
if options.pid_collision_diagnostics:
    patch_inputs.append(
        (root / "upstream/patches/criu-pid-collision-diagnostics-candidate.patch",
         "pid-collision-diagnostics.patch"))
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
candidate = copy.deepcopy(lock)
component = candidate["components"]["criu"]
overlay_inputs = {}
if options.exit_hold:
    overlay = root / "upstream/candidates/criu-exit-observer.h"
    relative = "criu/include/factorybox-exit-observer.h"
    shutil.copyfile(overlay, archive / "exit-observer.h")
    shutil.copyfile(overlay, context / relative)
    local = str(overlay.relative_to(root))
    component["overlays"][relative] = local
    component["local_inputs_sha256"][local] = reference.sha(overlay)
    component.setdefault("prepared_files_sha256", {})[relative] = reference.sha(overlay)
    overlay_inputs["exit-observer.h"] = reference.sha(overlay)
for patch_input, filename in patch_inputs:
    shutil.copyfile(patch_input, archive / filename)
    subprocess.run(["patch", "--batch", "--fuzz=0", "-p1", "-i", str(archive / filename)],
                   cwd=context, check=True, capture_output=True, timeout=15)
    patch_name = str(patch_input.relative_to(root))
    component["patches"].append(patch_name)
    component["local_inputs_sha256"][patch_name] = reference.sha(archive / filename)
for relative in [
    "criu/cr-dump.c", "criu/parasite-syscall.c", "criu/Makefile.crtools",
    "criu/factorybox-retained.c", "criu/include/factorybox-retained.h",
    "compel/src/lib/infect.c",
] + (["criu/cr-restore.c"] if options.pid_collision_diagnostics else []) + (
        ["criu/mount-v2.c"] if options.nested_overlay else []) + (
        ["criu/include/factorybox-exit-observer.h"] if options.exit_hold else []):
    component.setdefault("prepared_files_sha256", {})[relative] = reference.sha(context / relative)
(archive / "source-lock.json").write_text(json.dumps(candidate, indent=2) + "\n")
recipe = json.loads((root / "upstream/criu-build.json").read_text())
recipe["source_recipe_sha256"] = sources.recipe_sha(component)
(archive / "criu-build.json").write_text(json.dumps(recipe, indent=2) + "\n")
unchanged = sources.check_source(context, root / ".cache/downloads" / original["archive"], component)
context_sha = sources.tree_sha(context)
image, base = native_base.load(options.native_base_build, for_build=True)
command = [
    *reference.DOCKER, "run", "--pull=never", "--name", name,
    "--user", f"{os.getuid()}:{os.getgid()}",
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
    "platform": base.get("platform", "linux/arm64"), "native_lifecycle_qualified": False,
    "base_source_lock_sha256": reference.sha(root / "upstream/lock.json"),
    "source_lock_sha256": reference.sha(archive / "source-lock.json"),
    "patch_sha256": reference.sha(archive / "retained.patch"),
    "candidate_patch_sha256": {filename: reference.sha(archive / filename)
                               for _, filename in patch_inputs},
    "candidate_overlay_sha256": overlay_inputs,
    "context_sha256": context_sha, "build_inputs_sha256": reference.sha(archive / "criu-build.json"),
    "native_base_packages_sha256": base["packages_sha256"],
    "unchanged_upstream_test_files": unchanged,
    "scope": "Unselected process-tree recovery candidate. Immediately holds the pinned original source "
             "cgroup on tracer death before waiting for capture-worker rollback/exit. "
             "Retained active-process C context cures only after "
             "the original tracer and worker exit, with the exact original live guard's canonical "
             "grant or an explicitly unauthorized held-source diagnostic after both controllers exit. "
             "Only the Go guard may authorize source release. Single-task cure waits only for "
             "its exact tracee, so an exited recovery helper cannot satisfy that wait. "
             "The existing multi-thread restore wait remains unchanged. No production qualification.",
    "passed": False, "cleanup": False, "diagnostic_only": True, "production_qualified": False,
}
if options.exit_hold:
    result["exit_hold_candidate"] = True
    result["scope"] += (
        " The retained helper traces only its original parent without EXITKILL. "
        "It freezes the pinned source at PTRACE_EVENT_EXIT before the kernel releases "
        "the source tracees, then retains the same original-guard cure/release checks. "
        "The private test request stops after the requested process page dump and before cure. "
        "Normal completion explicitly detaches the observer."
    )
if options.exit_hold_socket:
    result["exit_hold_socket_candidate"] = True
    result["scope"] += (
        " Ordered socket-EOF follow-up keeps the ptrace relationship through readable EOF/HUP "
        "until the exact original parent's terminal wait event. "
        "Socket EOF alone never authorizes source release."
    )
if options.nested_overlay:
    result["nested_overlay_candidate"] = True
    result["scope"] += (
        " Additional unselected mount-v2 candidate resolves nested overlay layer paths only "
        "beneath their captured covering mounts and pins them with bounded openat2 descriptors. "
        "Nested overlay compatibility, confinement and full developer performance remain unqualified."
    )
if options.nested_overlay_stable_paths:
    result["nested_overlay_stable_paths"] = True
    result["scope"] += (
        " Ordered follow-up retains original layer option names by mounting through pinned layer "
        "aliases inside a bounded temporary tmpfs/chroot, then detaches the owned alias root. "
        "Fork-of-fork and pause/resume remain native gates, not inferred from first restore."
    )
if options.nested_overlay_shared_root:
    result["nested_overlay_shared_root"] = True
    result["scope"] += (
        " Ordered shared-root fix binds each non-root captured covering filesystem once, "
        "preserving the kernel's shared-mount requirement for upper/work. Every layer "
        "first passes bounded beneath-root/no-magiclink/no-cross-mount lookup. "
        "Namespace-root and overlapping covering-mount aliases remain unsupported."
    )
if options.pid_collision_diagnostics:
    result["pid_collision_diagnostics"] = True
    result["scope"] += (
        " Read-only bounded diagnostics only when nested clone3 fails with EEXIST: "
        "requested IDs, current process status and an occupant pidfd's metadata. "
        "No signal, PID-counter modification, retry or change to the original failure errno. "
        "This is not a PID collision fix or a selected performance change."
    )
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
