#!/usr/bin/env python3
"""Prove the extracted local lifecycle against real Sysbox in an owned fixture."""
import argparse
import datetime
import difflib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import uuid

import native_base
import criu_candidate
import reference
import reference_lifecycle
import upstream_unit
import build_config

ROOT = reference.ROOT
EXPECTED_TESTS = 7
EXPECTED_CAPTURE_SUBTESTS = 17
EXPECTED_ADAPTER_TESTS = 11
EXPECTED_ADAPTER_SUBTESTS = 61
ADAPTER_TESTS = (
    "TestImmutableBaseNative", "TestFilesystemBaseRecoveryNative",
    "TestPrivateConfigViewPreservesSysboxState", "TestCheckpointWatchSearchAdmission",
    "TestCriuFeatures", "TestFilesystemHookAdmission", "TestFilesystemCopyNative",
    "TestImageOwnershipNative", "TestFilesystemMaterializationNative",
    "TestNetworkRollbackAdmission", "TestSocketRollbackAdmission",
)


def candidate_compile_options(platform, kernel):
    return build_config.candidate_compile_options(platform, kernel)


def prepare_zombie_actor(context, archive):
    """Extend only a fresh native copy of the declared original actor."""
    declaration = json.loads((ROOT / "tests/compatibility/criu-zombie-high-exit-actor.json").read_text())
    if (context.resolve() != archive.resolve() / "context" or
            declaration["file"] != "internal/lifecycle/capture_native_linux_test.go" or
            declaration["patch"] != "tests/compatibility/criu-zombie-high-exit-actor.patch" or
            declaration["assertions_preserved"] is not True or declaration["upstream_assertions_modified"] is not False):
        raise ValueError("high-exit actor requires its exact fresh context and assertion declaration")
    original, target = ROOT / declaration["file"], context / declaration["file"]
    patch = ROOT / declaration["patch"]
    if (target.is_symlink() or reference.sha(original) != declaration["before_sha256"] or
            reference.sha(target) != declaration["before_sha256"] or
            reference.sha(patch) != declaration["patch_sha256"]):
        raise ValueError("high-exit actor original or ordered patch changed")
    shutil.copyfile(target, archive / "zombie-high-exit-actor.before.go")
    saved = archive / "zombie-high-exit-actor.patch"
    shutil.copyfile(patch, saved)
    completed = subprocess.run(["patch", "--batch", "--forward", "--fuzz=0", "-p1", "-i", str(saved.resolve())],
                               cwd=context, capture_output=True, text=True, timeout=15)
    (archive / "zombie-high-exit-actor.log").write_text(completed.stdout + completed.stderr)
    completed.check_returncode()
    if reference.sha(target) != declaration["after_sha256"] or reference.sha(original) != declaration["before_sha256"]:
        raise ValueError("high-exit actor differs from its declared result or changed the original")
    reference_lifecycle.write_json(archive / "zombie-high-exit-actor.json", declaration)
    return declaration


def select_adapter_fixture(archive, *, include_high_exit=False):
    """Keep all original adapter cases in their original unbounded fixture.

    The added bounded-device case has its own real ext4 worker fixture in
    base_recovery_native.py. Running it here without that backing only skips
    it; pretending that this ordinary directory is ext4 would misstate proof.
    Original test sources and the zero-skip/count requirements stay unchanged.
    """
    path = archive / "lifecycle-native.sh"
    before = path.read_bytes()
    original = b"  /sysboxcapture.test -test.v -test.timeout=30s\n"
    if before.count(original) != 1 or len(set(ADAPTER_TESTS)) != EXPECTED_ADAPTER_TESTS:
        raise ValueError("original adapter fixture call or case inventory changed")
    pattern = "^(" + "|".join(ADAPTER_TESTS) + ")$"
    after = before.replace(original,
                           ("  /sysboxcapture.test -test.v -test.timeout=30s -test.run='" + pattern + "'\n").encode())
    if include_high_exit:
        previous = b"^TestCapture(Namespaces|ChildrenNamespace|NestedZombieNamespace)Native$"
        extended = b"^TestCapture(Namespaces|ChildrenNamespace|NestedZombie(HighExit)?Namespace)Native$"
        if after.count(previous) != 1:
            raise ValueError("original complete namespace-suite selection changed")
        after = after.replace(previous, extended)
    (archive / "lifecycle-native.before.sh").write_bytes(before)
    patch = archive / "adapter-fixture-selection.patch"
    patch.write_text(''.join(difflib.unified_diff(before.decode().splitlines(True), after.decode().splitlines(True),
                     fromfile="a/lifecycle-native.sh", tofile="b/lifecycle-native.sh")))
    path.write_bytes(after)
    declaration = {"schema": 1, "source": "tests/lifecycle-native.sh",
                   "before_sha256": reference.sha(archive / "lifecycle-native.before.sh"),
                   "after_sha256": reference.sha(path), "patch_sha256": reference.sha(patch),
                   "original_adapter_tests": list(ADAPTER_TESTS), "assertions_modified": False,
                   "zero_skip_requirement_unchanged": True,
                   "additional_namespace_tests": ["TestCaptureNestedZombieHighExitNamespaceNative"] if include_high_exit else [],
                   "separate_required_case": "TestFilesystemBaseDeviceRecoveryNative",
                   "separate_required_fixture": "tools/base_recovery_native.py"}
    reference_lifecycle.write_json(archive / "adapter-fixture-selection.json", declaration)
    return declaration


def builder_command(build, components, archive, name, *, component_inputs):
    # Supplied completed component outputs and the native image are checked
    # separately below. Their provenance may describe a per-binary composition,
    # which is not a source context for rebuilding a Sysbox builder image.
    if component_inputs:
        return None
    dockerfile = ROOT / "build/sysbox.Dockerfile"
    if reference.sha(dockerfile) != components["dockerfile_sha256"]:
        raise ValueError("Sysbox builder recipe changed")
    expected = json.loads((build / "context-manifest.json").read_text())
    if upstream_unit.context_manifest(build / "context") != expected:
        raise ValueError("Sysbox context changed")
    return [
        *reference.DOCKER, "build", "--provenance=false", "--progress=plain",
        "--platform=linux/arm64", "--target", "build", "--output",
        "type=image,compression=uncompressed", "--tag", name,
        "--metadata-file", str(archive / "builder-metadata.json"),
        "-f", str(dockerfile), str(build / "context"),
    ]


def archive_capture(archive, name, owner, result, image):
    fixture = json.loads(reference.docker(["container", "inspect", name]))[0]
    if fixture["Config"].get("Labels", {}).get("com.factorybox.run") != owner:
        raise ValueError("capture fixture ownership changed before archive")
    if fixture["State"]["Running"]:
        reference.docker(["container", "stop", "--time=2", name])
        fixture = json.loads(reference.docker(["container", "inspect", name]))[0]
    if fixture["State"]["Running"] or fixture["State"]["Pid"] != 0:
        raise ValueError("capture fixture writers have not stopped")
    volume = json.loads(reference.docker(["volume", "inspect", name]))[0]
    if volume.get("Labels", {}).get("com.factorybox.run") != owner:
        raise ValueError("capture source volume ownership changed")
    helper = name + "-archive"
    command = [
        *reference.DOCKER, "run", "--pull=never", "--name", helper,
        "--label", "com.factorybox.run=" + owner,
        "--cpus=1", "--memory=256m", "--pids-limit=32", "--network=none", "--read-only",
        "--cap-drop=ALL", "--cap-add=DAC_READ_SEARCH", "--security-opt=no-new-privileges",
        "--env", "FACTORYBOX_CAPTURE_ARCHIVE=1",
        "--mount", f"type=volume,source={name},target=/input,readonly",
        "--mount", f"type=bind,source={archive / 'capture-archive.sh'},target=/archive.sh,readonly",
        "--entrypoint", "/bin/bash", image, "/archive.sh",
    ]
    result["archive_command"] = command
    try:
        with (archive / "archive.log").open("x") as log, \
                (archive / "output/capture-source.tar").open("xb") as stream:
            done = subprocess.run(command, stdout=stream, stderr=log, timeout=180)
        state = json.loads(reference.docker(["container", "inspect", helper]))[0]["State"]
        if done.returncode or state["OOMKilled"]:
            raise ValueError("capture source archive failed")
        result["capture_archive_sha256"] = reference.sha(archive / "output/capture-source.tar")
    finally:
        reference.owned_delete("container", helper, owner)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--sysbox-build", required=True)
    parser.add_argument("--suite", choices=["lifecycle", "guard", "capture", "pause", "base", "base-pause", "delegation", "auxiliary", "namespaces", "time", "mountfd", "overlay", "interruption", "admission", "worker-loss"], default="lifecycle")
    inputs = parser.add_mutually_exclusive_group()
    inputs.add_argument("--criu-build", help="Required rebuilt CRIU artifact for checkpoint-based suites")
    inputs.add_argument("--retained-candidate", help="Explicitly unselected retained-context CRIU build")
    parser.add_argument("--criu-base-lock", type=Path,
                        help="Exact frozen selected-base lock for a retained candidate; current CRIU recipe must still match")
    parser.add_argument("--native-base-build", help="Exact successful public-input native base build for checkpoint-based suites")
    parser.add_argument("--test", help="One exact native diagnostic test")
    parser.add_argument("--candidate-platform", choices=build_config.PLATFORMS,
                        help="Use a separate unqualified test build pinned to this Docker host's kernel")
    parser.add_argument("--ordinary-build", action="store_true",
                        help="Use the delivered platform check and ordinary compile flags")
    options = parser.parse_args()
    if options.criu_base_lock and not options.retained_candidate:
        parser.error("--criu-base-lock requires --retained-candidate")
    if options.ordinary_build and not options.candidate_platform:
        parser.error("--ordinary-build requires an explicit target")
    if options.test and not re.fullmatch(r"TestNative[A-Za-z0-9_]+", options.test):
        parser.error("--test must be one exact native test name")
    if options.test and options.suite == "guard":
        parser.error("the guard suite always runs all failure/disarm cases")
    capture_mode = options.suite in ["capture", "pause", "base", "base-pause", "delegation", "auxiliary", "namespaces", "time", "mountfd", "overlay", "interruption", "admission", "worker-loss"]
    if options.suite == "auxiliary" and options.candidate_platform != "linux/amd64":
        parser.error("auxiliary-controller regression requires this explicit AMD64 worker profile")
    if capture_mode and (not (options.criu_build or options.retained_candidate) or options.test):
        parser.error("checkpoint-based suites require a CRIU input and run their complete selected suite")
    if (options.criu_build or options.retained_candidate) and not capture_mode:
        parser.error("CRIU inputs are only used by checkpoint-based suites")
    component_inputs = capture_mode or bool(options.native_base_build)
    build, components = reference_lifecycle.component_build(
        options.sysbox_build, "factorybox-build-sysbox-")
    target = options.candidate_platform or "linux/arm64"
    if components.get("platform", "linux/arm64") != target:
        raise ValueError("Sysbox build differs from the explicit native target")
    if options.candidate_platform and not options.native_base_build:
        parser.error("candidate tests require an exact public native base build")
    host = json.loads(reference.docker(["info", "--format", "{{json .}}"]))
    native_base.require_native_host(target, host)
    compile_options = []
    if options.candidate_platform and not options.ordinary_build:
        compile_options = candidate_compile_options(target, host["KernelVersion"])
    owner = uuid.uuid4().hex
    name = "factorybox-runtime-" + owner
    archive = reference.new_artifact(name)
    output = archive / "output"
    output.mkdir(parents=True)
    result = {
        "schema": 1, "owner": owner, "container_name": name, "volume_name": name,
        "started_at": datetime.datetime.now(datetime.timezone.utc).isoformat(),
        "component_build": options.sysbox_build, "test": options.test, "suite": options.suite,
        "retained_candidate": bool(options.retained_candidate),
        "scope": ("Exact owned empty cgroup subtree retirement, foreign child-mount refusal and "
                  "populated-child preservation through Engine.Delete. No Sysbox daemon or process workload."
                  if options.test == "TestNativeEmptyCgroupRetirement" else
                  "Unselected nested-overlay diagnostic. Plain/direct/relative-symlink cases require "
                  "independent RAM/layers, fork of fork after original deletion, RAM-releasing pause, "
                  "resume and a fork of the resumed source. An escaping layer symlink must refuse "
                  "restore while the original mounted contents/RAM/useful work remain intact. "
                  "No Docker consumer or production storage-driver selection."
                  if options.suite == "overlay" else
                  "Actual capture-worker SIGKILL during filesystem copy after successful CRIU. "
                  "Checkpoint and pause must preserve the original running PID/RAM/useful work and "
                  "independently usable prior checkpoint without publishing incomplete backing. "
                  "The exact stopped GNU cp child must exit with its worker; a live worker and "
                  "forged receipt cannot authorize release. Failed pause publication must reconcile "
                  "with immutable history while refusing changed receipts and source-state bytes. "
                  "Hard CRIU-tracer interruption remains a separate required gate."
                  if options.suite == "worker-loss" else
                  "Public checkpoint/pause admission before process capture. Original hook assertions "
                  "remain unchanged. Held root/child fscontext descriptors must reject without entering "
                  "capture, preserving original identity, RAM, useful work and immutable failure history. "
                  "A real transient mount context must settle before capture and produce an independent fork."
                  if options.suite == "admission" else
                  "Actual pinned CRIU child interrupted through its pidfd after an image reaches "
                  "8 MiB while tracing its verified original 32-MiB RAM source. Failed capture "
                  "must preserve original identity, RAM, private network rules, useful work and "
                  "incomplete evidence without publishing a checkpoint. Its previous committed "
                  "checkpoint must still produce an independently useful recovery fork. No diagnostic thaw."
                  if options.suite == "interruption" else
                  "All nine controllers available on this AMD64 worker, finite 2-MiB/1-GiB hugepage "
                  "limits without page allocation, actual RDMA/misc/dmem limits and independent "
                  "fork, pause/resume and resumed-child limit mutation. Original five-controller "
                  "assertions remain a separate unchanged baseline. No hugepage or GPU/RDMA workload qualification."
                  if options.suite == "auxiliary" else
                  "Minimal parent plus two populated delegated cgroups with the same controller "
                  "set as the real private Docker tree. Public checkpoint/fork, RAM-releasing pause, "
                  "resume into the original delegated cgroup and an independent fork of that resumed "
                  "runtime, preserving controller sets, limits, leaf membership and open controller descriptors, "
                  "including controller loss after empty-tree preparation but before a runtime attempt. "
                  "No Docker, Compose, Connector, IDE or general delegated-runtime qualification."
                  if options.suite == "delegation" else
                  "Extracted source guard's eleven retained native failure/disarm cases and "
                  "foreign-mount rejection. "
                  "Kernel RAM actors only; not complete Sysbox capture qualification."
                  if options.suite == "guard" else
                  "Public checkpoint/pause rejection of an unsupported time namespace "
                  "before CRIU, original RAM/files and useful work preserved, retained failed "
                  "pause intent with recovery of lost publication and refusal of an unsafe receipt, "
                  "then ordinary capture of the same tree sharing its root time namespace. "
                  "A separate nested-PID-namespace init retains an exited child across independent "
                  "fork/recapture/descendant and each real parent reaps its original PID/status. "
                  "This does not qualify nested time namespace capture or the developer lifecycle."
                  if options.suite == "namespaces" else
                  "Positive nested time namespace capture/fork/pause/resume with two actors sharing "
                  "one time namespace, another separate namespace, exact relative monotonic/boottime "
                  "offsets, actor RAM retention and independent fork mutation. No Docker or IDE."
                  if options.suite == "time" else
                  "Detached proc-root O_PATH descriptor capture and independent restore, "
                  "including mount identity, proc PID-namespace semantics and retained actor RAM. "
                  "Minimal reproducer for the developer runc descriptor failure; no Docker or IDE."
                  if options.suite == "mountfd" else
                  "Public pause/resume with complete 32-MiB private RAM, original-cgroup anonymous "
                  "memory release, zero resident file-backed snapshot payload, retained runtime "
                  "identity, repeated cycles, receipt reconciliation and independent fork of a "
                  "resumed source. Full developer workload and complete failure recovery remain unqualified."
                  if options.suite in ["pause", "base-pause"] else
                  "Actual retained-source CRIU capture, virtual FUSE state and independent overlay/"
                  "managed-volume backing with complete content/metadata comparison. "
                  "Public checkpoint, immutable replay and completed-capture publication recovery. "
                  "Public independent fork with unavailable original rootfs, restored-source capture, "
                  "descendant independence, persisted restore completion and exact overlay deletion. "
                  "Incomplete preparation/restore refuses replay. Pause/resume has a separate gate; "
                  "complete interrupted-CRIU recovery remains unqualified."
                  if options.suite in ["capture", "base"] else
                  "Application-independent start/state/delete, saved-outcome recovery, rejection "
                  "before destructive actions and the actual CLI source guard on Sysbox. "
                  "No CRIU capture; not checkpoint/fork/pause/resume qualification."),
        "immutable_base": options.suite in ["base", "base-pause"],
        "passed": False, "cleanup": False, "production_qualified": False,
        "platform": target, "candidate_platform": bool(options.candidate_platform and not options.ordinary_build),
        "ordinary_build": options.ordinary_build,
        "native_lifecycle_qualified": False,
        "host": {key: host[key] for key in ["OSType", "Architecture", "KernelVersion"]},
        "compile_options": compile_options,
    }
    if options.suite == "overlay":
        result.update(diagnostic_only=True, driver_selected=False)
    attempted = False
    volume_attempted = False
    failure = None
    try:
        context = archive / "context"
        for directory in ["internal", "cmd/factorybox"]:
            shutil.copytree(ROOT / directory, context / directory)
        for module_file in ["go.mod", "go.sum"]:
            shutil.copyfile(ROOT / module_file, context / module_file)
        if options.suite == "namespaces":
            result["zombie_actor_extension"] = prepare_zombie_actor(context, archive)
        result["source_context"] = upstream_unit.context_manifest(context)
        env = dict(os.environ, GOOS="linux", GOARCH=build_config.architecture(target), CGO_ENABLED="0")
        binary = archive / "lifecycle.test"
        subprocess.run(["go", "test", "-c", "-trimpath", "-buildvcs=false", *compile_options,
                        "-o", str(binary), "./internal/lifecycle"],
                       cwd=context, env=env, check=True, timeout=120)
        cli = archive / "factorybox"
        subprocess.run(["go", "build", "-trimpath", "-buildvcs=false", *compile_options, "-o", str(cli),
                        "./cmd/factorybox"], cwd=context, env=env, check=True, timeout=120)
        if capture_mode:
            subprocess.run(["go", "test", "-c", "-trimpath", "-buildvcs=false", *compile_options,
                            "-o", str(archive / "sysboxcapture.test"),
                            "./internal/sysboxcapture"], cwd=context, env=env, check=True, timeout=120)
        for script in ["lifecycle-native.sh", "private-cgroup-delegation.sh"]:
            shutil.copyfile(ROOT / "tests" / script, archive / script)
        if capture_mode:
            result["adapter_fixture_selection"] = select_adapter_fixture(archive, include_high_exit=options.suite == "namespaces")
        shutil.copyfile(ROOT / "tests/capture-archive.sh", archive / "capture-archive.sh")
        input_files = [binary, cli, archive / "lifecycle-native.sh",
                       archive / "private-cgroup-delegation.sh"]
        input_files.append(archive / "capture-archive.sh")
        if capture_mode:
            input_files.append(archive / "sysboxcapture.test")
        result["inputs_sha256"] = {p.name: reference.sha(p) for p in input_files}
        result["source_sha256"] = {str(p.relative_to(context)): reference.sha(p)
                                   for directory in ["internal/lifecycle", "internal/sysboxcapture",
                                                     "cmd/factorybox"]
                                   for p in (context / directory).glob("*.go")}
        for module_file in ["go.mod", "go.sum"]:
            result["source_sha256"][module_file] = reference.sha(context / module_file)
        command = builder_command(build, components, archive, name, component_inputs=component_inputs)
        if component_inputs:
            inputs = archive / "components"
            inputs.mkdir()
            for component in ["sysbox-runc", "sysbox-fs", "sysbox-mgr"]:
                source = build / "output" / component
                if reference.sha(source) != components["binary_sha256"][component]:
                    raise ValueError("rebuilt Sysbox executable changed: " + component)
                reference.artifact_storage.share_frozen_input(
                    source, inputs / component, ROOT, components["binary_sha256"][component])
            if capture_mode:
                if options.retained_candidate:
                    base_lock = None
                    if options.criu_base_lock:
                        if options.criu_base_lock.is_symlink():
                            raise ValueError("frozen CRIU base lock must not be a symlink")
                        base_lock = archive / "criu-base-source-lock.json"
                        shutil.copyfile(options.criu_base_lock, base_lock)
                        result["criu_base_source_lock_sha256"] = reference.sha(base_lock)
                    criu_build, criu, _, _ = criu_candidate.load(options.retained_candidate, base_lock=base_lock)
                else:
                    criu_build, criu = reference_lifecycle.component_build(
                        options.criu_build, "factorybox-build-criu-")
                if criu.get("platform", "linux/arm64") != target:
                    raise ValueError("CRIU build differs from the explicit native target")
                if reference.sha(criu_build / "criu") != criu["binary_sha256"]:
                    raise ValueError("rebuilt CRIU executable changed")
                reference.artifact_storage.share_frozen_input(
                    criu_build / "criu", inputs / "criu", ROOT, criu["binary_sha256"])
                result["criu_build"] = options.criu_build or options.retained_candidate
            result["component_sha256"] = {p.name: reference.sha(p) for p in inputs.iterdir()}
            if options.native_base_build:
                image, base_result = native_base.load(options.native_base_build,
                                                     candidate_platform=options.candidate_platform)
                result["native_base_build"] = options.native_base_build
                result["native_base_image_config"] = base_result["image_config"]
                result["native_base_packages_sha256"] = base_result["packages_sha256"]
            else:
                image = reference.BASE_ID
        else:
            with (archive / "builder.log").open("x") as log:
                subprocess.run(command, stdout=log, stderr=subprocess.STDOUT, check=True, timeout=120)
            image = name + "@" + json.loads((archive / "builder-metadata.json").read_text())[
                "containerimage.digest"]
        result["builder_image_manifest"] = image
        busybox = json.loads((ROOT / "upstream/test-inputs.json").read_text())[
            "busybox-" + build_config.architecture(target)]
        rootfs = ROOT / ".cache/downloads" / busybox["archive"]
        if reference.sha(rootfs) != busybox["sha256"]:
            raise ValueError("pinned native rootfs changed")
        result["busybox_rootfs_sha256"] = busybox["sha256"]
        command = [
            *reference.DOCKER, "run", "--pull=never", "--platform=" + target, "--name", name,
            "--label", "com.factorybox.run=" + owner, "--privileged", "--cgroupns=private",
            "--cpus=" + ("1" if options.suite == "guard" else "3"),
            "--memory=" + ("256m" if options.suite == "guard" else "3g"),
            "--pids-limit=512", "--network=none",
            "--env", "FACTORYBOX_NATIVE_LIFECYCLE=1",
            "--env", "FACTORYBOX_UPSTREAM_CONTAINER=1",
            "--env", "FACTORYBOX_NATIVE_CAPTURE_GUARD=" + ("1" if options.suite == "guard" else "0"),
            "--env", "FACTORYBOX_NATIVE_RETAINED_CAPTURE=" + ("1" if capture_mode else "0"),
            "--env", "FACTORYBOX_COMPONENT_INPUTS=" + ("1" if component_inputs else "0"),
            "--env", "FACTORYBOX_NATIVE_PAUSE=" + ("1" if options.suite in ["pause", "base-pause"] else "0"),
            "--env", "FACTORYBOX_NATIVE_IMMUTABLE_BASE=" + ("1" if options.suite in ["base", "base-pause"] else "0"),
            "--env", "FACTORYBOX_NATIVE_NAMESPACES=" + ("1" if options.suite == "namespaces" else "0"),
            "--env", "FACTORYBOX_NATIVE_TIME=" + ("1" if options.suite == "time" else "0"),
            "--env", "FACTORYBOX_NATIVE_DELEGATION=" + ("1" if options.suite == "delegation" else "0"),
            "--env", "FACTORYBOX_NATIVE_AUXILIARY_DELEGATION=" + ("1" if options.suite == "auxiliary" else "0"),
            "--env", "FACTORYBOX_NATIVE_MOUNT_FD=" + ("1" if options.suite == "mountfd" else "0"),
            "--env", "FACTORYBOX_NATIVE_OVERLAY=" + ("1" if options.suite == "overlay" else "0"),
            "--env", "FACTORYBOX_NATIVE_INTERRUPTION=" + ("1" if options.suite == "interruption" else "0"),
            "--env", "FACTORYBOX_NATIVE_WORKER_LOSS=" + ("1" if options.suite == "worker-loss" else "0"),
            "--env", "FACTORYBOX_NATIVE_FILESYSTEM_ADMISSION=" + ("1" if options.suite == "admission" else "0"),
            "--env", "FACTORYBOX_NATIVE_TEST=" + (options.test or ""),
            "--env", "FACTORYBOX_RUNTIME_SHA256=" + components["binary_sha256"]["sysbox-runc"],
            "--env", "GOMAXPROCS=2", "--env", "GOMEMLIMIT=256MiB",
            "--mount", f"type=volume,source={name},target=/var/lib/sysbox",
            "--mount", f"type=bind,source={output},target=/output",
            "--mount", f"type=bind,source={binary},target=/lifecycle.test,readonly",
            "--mount", f"type=bind,source={cli},target=/factorybox,readonly",
            "--mount", f"type=bind,source={rootfs},target=/busybox-rootfs.tar.gz,readonly",
            "--mount", f"type=bind,source={archive / 'lifecycle-native.sh'},target=/native.sh,readonly",
            "--mount", f"type=bind,source={archive / 'private-cgroup-delegation.sh'},target=/delegate.sh,readonly",
            "--entrypoint", "/bin/bash", image, "/native.sh",
        ]
        if component_inputs:
            extra = ["--mount", f"type=bind,source={inputs},target=/component-input,readonly"]
            if capture_mode:
                extra += [
                    "--env", "FACTORYBOX_CRIU_SHA256=" + criu["binary_sha256"],
                    "--mount", f"type=bind,source={archive / 'sysboxcapture.test'},target=/sysboxcapture.test,readonly",
                ]
            command[command.index("--entrypoint"):command.index("--entrypoint")] = extra
        result["command"] = command
        reference_lifecycle.write_json(archive / "intent.json", result)
        print(json.dumps({"running": str(archive)}), flush=True)
        volume_attempted = True
        reference.docker(["volume", "create", "--label", "com.factorybox.run=" + owner, name])
        attempted = True
        with (archive / "stdout.txt").open("x") as log, (archive / "stderr.txt").open("x") as errors:
            done = subprocess.run(command, stdout=log, stderr=errors, timeout=420 if capture_mode else 150)
        result["exit_code"] = done.returncode
        text = (archive / "stdout.txt").read_text()
        result["passed_tests"] = sum(line.startswith("--- PASS: ") for line in text.splitlines())
        result["passed_subtests"] = sum(line.startswith(" ") and line.lstrip().startswith("--- PASS: ")
                                         for line in text.splitlines())
        result["skipped_tests"] = sum(line.lstrip().startswith("--- SKIP: ") for line in text.splitlines())
        if done.returncode == 125:
            raise ValueError("native fixture container did not start: " +
                             (archive / "stderr.txt").read_text()[-4096:])
        state = json.loads(reference.docker(["container", "inspect", name]))[0]["State"]
        result["container_state"] = state
        result["inputs_unchanged"] = all(reference.sha(archive / name) == expected
                                         for name, expected in result["inputs_sha256"].items())
        result["source_context_unchanged"] = (
            upstream_unit.context_manifest(context) == result["source_context"])
        if component_inputs:
            result["components_unchanged"] = all(
                reference.sha(inputs / component) == expected
                for component, expected in result["component_sha256"].items())
        result["passed"] = (
            done.returncode == 0 and not state["OOMKilled"] and result["inputs_unchanged"] and
            result["source_context_unchanged"] and
            result["passed_tests"] == (EXPECTED_ADAPTER_TESTS + (6 if options.suite == "admission" else 4 if options.suite == "namespaces" else 1) if capture_mode else
                                      1 if options.test or options.suite == "guard" else EXPECTED_TESTS) and
            (options.suite != "guard" or result["passed_subtests"] == 12) and
            (options.suite not in ["capture", "base"] or result["passed_subtests"] == EXPECTED_CAPTURE_SUBTESTS + EXPECTED_ADAPTER_SUBTESTS) and
            (options.suite not in ["pause", "base-pause"] or result["passed_subtests"] == 6 + EXPECTED_ADAPTER_SUBTESTS) and
            (options.suite != "interruption" or result["passed_subtests"] == EXPECTED_ADAPTER_SUBTESTS) and
            (options.suite != "admission" or result["passed_subtests"] == 17 + EXPECTED_ADAPTER_SUBTESTS) and
            (options.suite != "worker-loss" or result["passed_subtests"] == 3 + EXPECTED_ADAPTER_SUBTESTS) and
            (options.suite != "namespaces" or result["passed_subtests"] == 1 + EXPECTED_ADAPTER_SUBTESTS) and
            (options.suite not in ["time", "delegation", "auxiliary"] or result["passed_subtests"] == EXPECTED_ADAPTER_SUBTESTS) and
            (options.suite != "mountfd" or result["passed_subtests"] == 2 + EXPECTED_ADAPTER_SUBTESTS) and
            (options.suite != "overlay" or result["passed_subtests"] == 4 + EXPECTED_ADAPTER_SUBTESTS) and
            result.get("components_unchanged", True) and
            result["skipped_tests"] == 0)
        if not result["passed"]:
            raise ValueError("native lifecycle qualification failed")
    except Exception as error:
        failure = error
        result["error"] = str(error)
    finally:
        preserve_volume = False
        try:
            if attempted:
                if capture_mode or failure or not result["passed"]:
                    try:
                        archive_capture(archive, name, owner, result, image)
                    except Exception as error:
                        preserve_volume = True
                        result["native_passed"] = result["passed"]
                        result["passed"] = False
                        result["archive_error"] = str(error)
                        result["retained_source_volume"] = name
                        failure = failure or error
                reference.owned_delete("container", name, owner)
            if volume_attempted and not preserve_volume:
                reference.owned_delete("volume", name, owner)
            result["cleanup"] = not preserve_volume
        except Exception as error:
            result["cleanup_error"] = str(error)
            failure = failure or error
        result["passed"] = bool(result["passed"] and result["cleanup"] and failure is None)
        reference_lifecycle.write_json(archive / "result.json", result)
        print(json.dumps({"evidence": str(archive), **result}), flush=True)
        reference.artifact_storage.compact_closed_run(archive, ROOT)
    if failure:
        raise RuntimeError("native lifecycle gate failed: " + str(archive)) from failure


if __name__ == "__main__":
    main()
