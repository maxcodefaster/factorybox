#!/usr/bin/env python3
"""Qualify real developer consumers through the public Factorybox CLI on Sysbox."""
import argparse
import datetime
import json
import os
import re
import shutil
import signal
import subprocess
import time
import uuid

import devx_inputs
import devx_project_images
import criu_candidate
import native_base
import reference
import reference_lifecycle
import theia_admission
import theia_peer
import upstream_bats
import upstream_unit

ROOT = reference.ROOT


def require_no_retained_worker():
    names = reference.docker([
        "volume", "ls", "--filter", "label=com.factorybox.run", "--format", "{{.Name}}"])
    retained = [name for name in names.splitlines()
                if re.fullmatch(r"factorybox-devx-lifecycle-[0-9a-f]{32}(?:-ide-work)?", name)]
    if retained:
        raise ValueError("reconcile the existing developer volume before starting another run: " +
                         ", ".join(sorted(retained)))


def collect_observations(output, result):
    for filename, key in [
        ("devx-observations.json", "observations"),
        ("preparation.json", "preparation"),
        ("theia-observations.json", "theia_observations"),
        ("opencode-observations.json", "opencode_observations"),
        ("devx-criu-interruption.json", "docker_fault"),
    ]:
        path = output / filename
        if path.exists():
            result[key] = json.loads(path.read_text())
    if (output / "sysctls-after.json").exists():
        result["observed_global_sysctls_unchanged"] = (
            json.loads((output / "sysctls-before.json").read_text()) ==
            json.loads((output / "sysctls-after.json").read_text()))


def run_with_theia(command, stdout, stderr, peer, output, *, timeout=420):
    class ExecutionDeadline(BaseException):
        pass

    def expired(*_):
        # Component cleanup catches ordinary command failures. The outer
        # execution deadline must still escape those local error handlers.
        raise ExecutionDeadline()
    previous = signal.signal(signal.SIGALRM, expired)
    signal.setitimer(signal.ITIMER_REAL, timeout)
    try:
        with subprocess.Popen(command, stdout=stdout, stderr=stderr) as process:
            try:
                while process.poll() is None:
                    request = output / "theia-request.json"
                    if not peer.started and request.exists():
                        peer.start(json.loads(request.read_text()))
                    if (output / "theia-operations-complete").exists() and not peer.closed:
                        peer.close(False)
                        released = peer.result.get("passed") and peer.result.get("cleanup")
                        (output / "theia-released").write_text("ok\n" if released else "failed\n")
                    complete = output / "theia-lifecycle-complete"
                    if complete.exists():
                        code = int(complete.read_text().strip())
                        if not peer.closed:
                            peer.close(code != 0)
                        (output / "theia-host-finished").write_text("done\n")
                    time.sleep(0.1)
                return subprocess.CompletedProcess(command, process.returncode)
            finally:
                if process.poll() is None:
                    process.kill()
                    process.wait(timeout=5)
    except ExecutionDeadline:
        raise subprocess.TimeoutExpired(command, timeout) from None
    finally:
        signal.setitimer(signal.ITIMER_REAL, 0)
        signal.signal(signal.SIGALRM, previous)


def cleanup_resources(archive, name, owner, image, result, peer,
                      attempted, volume_attempted, failure):
    preserve = False
    if peer and attempted and failure:
        # Killing the Docker client does not stop its container. End the
        # workload before a slow peer close can consume more execution time.
        try:
            upstream_bats.stop_failed_fixture(name, owner)
            result["worker_stopped_before_peer"] = True
        except Exception as error:
            result["worker_stopped_before_peer"] = False
            result["worker_stop_error"] = str(error)
    if peer:
        try:
            peer.close(bool(failure))
        except Exception as error:
            result["peer_cleanup_error"] = str(error)
            failure = failure or error
        result["theia"] = peer.result
    if attempted and failure:
        try:
            upstream_bats.archive_failure(archive, name, owner, [name], image, result,
                                         timeout=600, compressed=True)
        except Exception as error:
            preserve = True
            result["archive_error"] = str(error)
            result["retained_volumes"] = [name]
    result["worker_cleanup"] = False
    try:
        if attempted:
            reference.owned_delete("container", name, owner)
        if volume_attempted and not preserve:
            reference.owned_delete("volume", name, owner)
        result["worker_cleanup"] = not preserve
    except Exception as error:
        result["cleanup_error"] = str(error)
        failure = failure or error
    result["cleanup"] = result["worker_cleanup"] and (
        peer is None or peer.result.get("cleanup") is True)
    if not result["cleanup"]:
        failure = failure or ValueError("developer resource cleanup remains incomplete")
    result["passed"] = result["passed"] and result["cleanup"] and failure is None
    return failure


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--consumer-inputs", required=True)
    parser.add_argument("--project-images", required=True)
    parser.add_argument("--native-base-build", required=True)
    parser.add_argument("--sysbox-build", required=True)
    inputs = parser.add_mutually_exclusive_group(required=True)
    inputs.add_argument("--criu-build")
    inputs.add_argument("--retained-candidate", help="Explicitly unselected retained-context CRIU build")
    parser.add_argument("--theia-image", help="Optional exact cached IDE digest for the separate native RPC peer")
    parser.add_argument("--opencode", action="store_true", help="Optional supplied OpenCode 2 consumer; no inference")
    parser.add_argument("--suite", choices=["lifecycle", "projection", "docker-interruption"],
                        default="lifecycle")
    parser.add_argument("--docker-fault-process", choices=["root", "dockerd"], default="root",
                        help="Exact process whose own page dump must be interrupted")
    parser.add_argument("--docker-fault-operation", choices=["checkpoint", "pause"], default="checkpoint",
                        help="Lifecycle operation whose actual page dump must be interrupted")
    parser.add_argument("--docker-fault-component", choices=["tracer", "helper", "controller"], default="tracer",
                        help="Exact component to kill; controller loss follows tracer/worker rollback")
    options = parser.parse_args()
    if options.docker_fault_process != "root" and options.suite != "docker-interruption":
        parser.error("--docker-fault-process requires --suite docker-interruption")
    if options.docker_fault_operation != "checkpoint" and options.suite != "docker-interruption":
        parser.error("--docker-fault-operation requires --suite docker-interruption")
    if options.docker_fault_component != "tracer" and options.suite != "docker-interruption":
        parser.error("--docker-fault-component requires --suite docker-interruption")
    if options.docker_fault_component == "helper" and options.docker_fault_operation != "checkpoint":
        parser.error("the helper-loss proof currently requires checkpoint")
    if options.suite == "projection" and (options.theia_image or options.opencode):
        parser.error("projection is a minimal namespace regression without application consumers")
    if options.suite == "docker-interruption" and not options.retained_candidate:
        parser.error("Docker interruption requires the explicitly unselected retained-context candidate")
    if options.suite == "docker-interruption" and (options.theia_image or options.opencode):
        parser.error("Docker interruption exercises core recovery without optional IDE/harness consumers")
    require_no_retained_worker()
    ide_image = devx_inputs.inspect_image(options.theia_image) if options.theia_image else None
    ide_admission = theia_admission.qualify(ide_image) if ide_image else None
    consumer_archive, consumers = devx_inputs.load(options.consumer_inputs, rootfs=True)
    project_archive, projects = devx_project_images.load(options.project_images)
    image, _ = native_base.load(options.native_base_build)
    sysbox_archive, sysbox = reference_lifecycle.component_build(
        options.sysbox_build, "factorybox-build-sysbox-")
    if options.retained_candidate:
        criu_archive, criu, _, _ = criu_candidate.load(options.retained_candidate)
    else:
        criu_archive, criu = reference_lifecycle.component_build(
            options.criu_build, "factorybox-build-criu-")
    owner = uuid.uuid4().hex
    name = "factorybox-devx-lifecycle-" + owner
    archive = reference.new_artifact(name)
    output = archive / "output"
    output.mkdir(parents=True)
    scripts = ["devx-lifecycle.sh", "devx-lifecycle-body.sh", "devx-sandbox.sh", "devx-devcontainer.sh",
               "private-cgroup-delegation.sh", "global-sysctl-observation.py", "wait-sysbox.sh"]
    if options.opencode:
        scripts.append("opencode-probe.cjs")
    for script in scripts:
        shutil.copyfile(ROOT / "tests" / script, archive / script)
    shutil.copyfile(ROOT / "tools/devx_lifecycle.py", archive / "runner.py")
    if ide_image:
        shutil.copyfile(ROOT / "tools/theia_peer.py", archive / "theia-peer.py")
        shutil.copyfile(ROOT / "tests/reference/fixture/theia-client.cjs", archive / "theia-client.cjs")
    shutil.copytree(ROOT / "examples/factory", archive / "example")
    reference_lifecycle.write_json(archive / "project-images.json", [
        {"image": row["image"], "canonical_image": row["canonical_image"]}
        for row in projects["images"]])
    components = archive / "components"
    components.mkdir()
    for component in ["sysbox-runc", "sysbox-fs", "sysbox-mgr"]:
        source = sysbox_archive / "output" / component
        if reference.sha(source) != sysbox["binary_sha256"][component]:
            raise ValueError("selected Sysbox executable changed")
        reference.artifact_storage.share_frozen_input(
            source, components / component, ROOT, sysbox["binary_sha256"][component])
    if reference.sha(criu_archive / "criu") != criu["binary_sha256"]:
        raise ValueError("selected CRIU executable changed")
    reference.artifact_storage.share_frozen_input(
        criu_archive / "criu", components / "criu", ROOT, criu["binary_sha256"])
    result = {
        "schema": 1, "owner": owner, "container_name": name, "volume_name": name, "suite": options.suite,
        "opencode": options.opencode,
        "started_at": datetime.datetime.now(datetime.timezone.utc).isoformat(),
        "consumer_inputs": options.consumer_inputs, "project_images": options.project_images,
        "sysbox_build": options.sysbox_build, "criu_build": options.criu_build or options.retained_candidate,
        "retained_candidate": bool(options.retained_candidate),
        "exit_hold_candidate": bool(criu.get("exit_hold_candidate")),
        "native_base_build": options.native_base_build, "native_image": image,
        "runtime_rootfs_sha256": consumers["rootfs_tar_sha256"],
        "project_images_sha256": projects["archive_sha256"],
        "example": upstream_unit.context_manifest(archive / "example"),
        "passed": False, "cleanup": False, "production_qualified": False,
        "scope": "Public Factorybox CLI and real Sysbox/CRIU with private Docker vfs, "
                 "Compose, Dev Containers CLI, the real Factory Connector parent/child and app "
                 "RAM/filesystem independence across forks, descendants and pause/resume. "
                 "A closed test authority is captured inside each isolated network. This does "
                 "not qualify real external Factory authority handoff, browser, "
                 "OpenCode inference, another platform or any latency distribution.",
    }
    attempted, volume_attempted = False, False
    peer = theia_peer.Peer(name, owner, ide_image, archive, output, image) if ide_image else None
    if peer:
        result["theia_image"] = ide_image
        result["theia_admission"] = ide_admission
        result["scope"] += (" The optional separate Theia peer uses worker-controlled live workspace bind "
                            "views with serialized external writes; native RPC connection continuity "
                            "and Connector app readback are required. This is a private fixture "
                            "composition, not a production workspace-projection API.")
    if options.suite == "projection":
        result["scope"] = ("Bounded native regression for exact IDE namespace/root selection and "
                           "live bind-mounted read/write. No Sysbox workload, IDE server or lifecycle qualification.")
    if options.suite == "docker-interruption":
        result["docker_fault_process"] = options.docker_fault_process
        result["docker_fault_operation"] = options.docker_fault_operation
        result["docker_fault_component"] = options.docker_fault_component
        target = ("root process" if options.docker_fault_process == "root" else "dockerd child")
        result["scope"] = ("Real private Docker vfs, Compose, Dev Containers CLI and Factory Connector. "
                           f"Kills only the pinned CRIU {options.docker_fault_component} during {options.docker_fault_operation}, "
                           f"after the {target}'s own page image begins. "
                           "Requires authorized cure, original stable process identities, unchanged running "
                           "containers/app state, a new Docker exec, mutation/capture/fork of the cured source, "
                           "RAM/filesystem independence and a usable prior immutable checkpoint. "
                           "The separate ordinary lifecycle suite covers pause/resume, descendants, IDE and harness. "
                           "No other process target, helper/guard loss, browser, inference or latency distribution qualification.")
        if options.docker_fault_component == "helper":
            result["scope"] = (
                f"Real private Docker/Compose/Dev Container tree; pinned retained helper death during {target} page dump. "
                "The healthy tracer must publish a verified complete checkpoint and leave the original source useful. "
                "That checkpoint must fork independently, then source mutation, fresh capture/fork and the prior "
                "checkpoint fork must still work. No tracer-plus-helper double loss, guard loss, pause, IDE, "
                "inference or latency distribution qualification.")
        elif options.docker_fault_component == "controller":
            result["scope"] = (
                "Real Docker/Compose/Dev Container tree. Kill the pinned tracer at the selected "
                "process page dump, then the original CLI after worker exit and checked network "
                "rollback, before the same original guard grants cure. Require unchanged recovery "
                "identities, public transaction replay, original useful Docker/app state and all "
                "fresh/current/prior independent fork assertions. No guard death, earlier loss "
                "window, optional consumers or latency-distribution qualification.")
    failure = None
    try:
        context = archive / "context"
        source_directories = ["cmd/factorybox", "internal", "tests/devx-actor", "tests/devx-lifecycle",
                              "tests/devx-registry", "tests/ide-filesystem"]
        for directory in source_directories:
            shutil.copytree(ROOT / directory, context / directory)
        for file in ["go.mod", "go.sum"]:
            shutil.copyfile(ROOT / file, context / file)
        result["source_context"] = upstream_unit.context_manifest(context)
        env = dict(os.environ, GOOS="linux", GOARCH="arm64", CGO_ENABLED="0")
        for target, package, test in [
            ("factorybox", "./cmd/factorybox", False),
            ("devx-actor", "./tests/devx-actor", False),
            ("devx-registry", "./tests/devx-registry", False),
            ("devx-lifecycle.test", "./tests/devx-lifecycle", True),
            *([("ide-filesystem", "./tests/ide-filesystem", False)]
              if peer or options.suite == "projection" else []),
        ]:
            command = ["go", "test", "-c"] if test else ["go", "build"]
            command += ["-trimpath", "-buildvcs=false"]
            subprocess.run([*command, "-o", str(archive / target), package],
                           cwd=context, env=env, check=True, timeout=90)
        result["inputs_sha256"] = {
            path.name: reference.sha(path) for path in archive.iterdir() if path.is_file()}
        result["component_sha256"] = {p.name: reference.sha(p) for p in components.iterdir()}
        command = [
            *reference.DOCKER, "run", "--pull=never", "--name", name,
            "--label", "com.factorybox.run=" + owner, "--privileged", "--cgroupns=private",
            "--cpus=3", "--memory=3g", "--pids-limit=512", "--network=none",
            "--env", "FACTORYBOX_UPSTREAM_CONTAINER=1", "--env", "FACTORYBOX_DEVX_LIFECYCLE=1",
            "--env", "FACTORYBOX_RUNTIME_SHA256=" + sysbox["binary_sha256"]["sysbox-runc"],
            "--env", "FACTORYBOX_CRIU_SHA256=" + criu["binary_sha256"],
            "--env", "GOMAXPROCS=2", "--env", "GOMEMLIMIT=256MiB",
            "--mount", f"type=volume,source={name},target=/var/lib/sysbox",
        ]
        if peer:
            command += ["--env", "FACTORYBOX_DEVX_THEIA=1", "--env", "FACTORYBOX_DEVX_RUN=" + owner]
        if options.opencode:
            command += ["--env", "FACTORYBOX_DEVX_OPENCODE=1"]
        if options.suite == "projection":
            command += ["--env", "FACTORYBOX_DEVX_PROJECTION=1"]
        if options.suite == "docker-interruption":
            command += ["--env", "FACTORYBOX_DEVX_CRIU_FAULT=" + options.docker_fault_process,
                        "--env", "FACTORYBOX_DEVX_CRIU_FAULT_OPERATION=" + options.docker_fault_operation,
                        "--env", "FACTORYBOX_DEVX_CRIU_FAULT_COMPONENT=" + options.docker_fault_component]
            if criu.get("exit_hold_candidate"):
                command += ["--env", "FACTORYBOX_CRIU_EXIT_HOLD=1"]
        mounts = [
            (output, "/output", False), (components, "/component-input", True),
            (archive / "example", "/example", True),
            (consumer_archive / "runtime-rootfs.tar", "/runtime-rootfs.tar", True),
            (project_archive / "images.tar", "/project-images.tar", True),
            (archive / "private-cgroup-delegation.sh", "/delegate.sh", True),
            (archive / "global-sysctl-observation.py", "/observe-sysctls.py", True),
        ]
        mounts += [(archive / file, "/" + file, True) for file in [
            "factorybox", "devx-actor", "devx-registry", "devx-lifecycle.test",
            "devx-lifecycle.sh", "devx-lifecycle-body.sh", "devx-sandbox.sh", "devx-devcontainer.sh",
            "project-images.json", "wait-sysbox.sh"]]
        if peer or options.suite == "projection":
            mounts.append((archive / "ide-filesystem", "/ide-filesystem", True))
        if options.opencode:
            mounts.append((archive / "opencode-probe.cjs", "/opencode-probe.cjs", True))
        for source, destination, readonly in mounts:
            command += ["--mount", f"type=bind,source={source},target={destination}" +
                        (",readonly" if readonly else "")]
        command += ["--entrypoint", "/bin/bash", image, "/devx-lifecycle.sh"]
        result["command"] = command
        reference_lifecycle.write_json(archive / "intent.json", result)
        print(json.dumps({"running": str(archive)}), flush=True)
        volume_attempted = True
        reference.docker(["volume", "create", "--label", "com.factorybox.run=" + owner, name])
        attempted = True
        with (archive / "stdout.txt").open("x") as stdout, (archive / "stderr.txt").open("x") as stderr:
            execution_start = time.monotonic()
            try:
                done = (run_with_theia(command, stdout, stderr, peer, output) if peer else
                        subprocess.run(command, stdout=stdout, stderr=stderr, timeout=420))
            finally:
                result["execution_seconds"] = time.monotonic() - execution_start
        result["exit_code"] = done.returncode
        state = json.loads(reference.docker(["container", "inspect", name]))[0]["State"]
        result["container_state"] = state
        result["inputs_unchanged"] = (
            all(reference.sha(archive / path) == expected
                for path, expected in result["inputs_sha256"].items()) and
            all(reference.sha(components / path) == expected
                for path, expected in result["component_sha256"].items()) and
            upstream_unit.context_manifest(archive / "example") == result["example"] and
            upstream_unit.context_manifest(context) == result["source_context"])
        text = (archive / "stdout.txt").read_text()
        collect_observations(output, result)
        expected = {
            "prepared-start-first-use", "fork-first-use", "source-advance", "branch-advance", "source-independent",
            "fork-of-fork-first-use", "resume-first-use", "fork-of-resumed-first-use"}
        test_name = "TestNativeDeveloperLifecycle"
        if options.suite == "docker-interruption":
            expected = {"prepared-start-first-use", "recovered-source-advance", "fork-after-interruption-first-use",
                        "recovered-branch-advance", "recovered-source-independent",
                        "fork-before-interruption-first-use"}
            if options.docker_fault_component == "helper":
                expected |= {"fork-helper-loss-first-use", "helper-loss-branch-advance",
                             "helper-loss-source-independent"}
            test_name = "TestNativeDockerCaptureInterruption"
        observed = [row["phase"] for row in result.get("observations", [])]
        result["passed"] = (
            done.returncode == 0 and not state["OOMKilled"] and result["inputs_unchanged"] and
            result.get("observed_global_sysctls_unchanged", False) and
            len(observed) == len(expected) and set(observed) == expected and
            f"--- PASS: {test_name} " in text and "--- SKIP:" not in text and
            "--- FAIL:" not in text)
        if options.suite == "projection":
            result["passed"] = (
                done.returncode == 0 and not state["OOMKilled"] and result["inputs_unchanged"] and
                result.get("observed_global_sysctls_unchanged", False) and
                "--- PASS: TestNativeIDEProjectionNamespace " in text and
                "--- SKIP:" not in text and "--- FAIL:" not in text)
        if options.suite == "docker-interruption":
            fault = result.get("docker_fault", {})
            expected_fault = ("root-process-page-dump" if options.docker_fault_process == "root"
                              else "docker-daemon-child-page-dump")
            if options.docker_fault_component == "helper":
                expected_fault = "retained-helper-loss-during-" + options.docker_fault_process + "-page-dump"
            elif options.docker_fault_component == "controller":
                expected_fault = "controller-loss-after-" + options.docker_fault_process + "-page-dump"
            result["passed"] = (result["passed"] and fault.get("passed") is True and
                                fault.get("fault") == expected_fault and
                                fault.get("operation") == options.docker_fault_operation and
                                fault.get("component") == options.docker_fault_component)
            if options.docker_fault_component == "helper":
                result["passed"] = (result["passed"] and fault.get("retained_helper_killed") is True and
                                    fault.get("checkpoint_published_after_helper_loss") is True)
            elif options.docker_fault_component == "controller":
                result["passed"] = (result["passed"] and fault.get("controller_killed") is True and
                                    fault.get("same_original_guard") is True)
            if options.docker_fault_operation == "pause":
                result["passed"] = (result["passed"] and
                                    fault.get("failed_pause_history_retained") is True and
                                    fault.get("failed_identity_replay_refused") is True)
        if peer:
            ide_observations = result.get("theia_observations", [])
            result["theia"] = peer.result
            result["passed"] = (
                result["passed"] and peer.result["passed"] and peer.result["cleanup"] and
                len(ide_observations) == len(expected) + 1 and
                {row["phase"] for row in ide_observations} == expected | {"selected-sandbox-edit-readback"} and
                all(row["client_id"] == peer.result["peer"]["client_id"] and
                    row["pid"] == peer.result["peer"]["pid"] for row in ide_observations))
        if options.opencode:
            harness = result.get("opencode_observations", [])
            result["passed"] = (
                result["passed"] and len(harness) == len(expected) and
                {row["phase"] for row in harness} == expected and
                all(row["server_version"] == "2.0.27" and row["unauthorized_status"] == 401 and
                    row["info_status"] == 200 and row["session_status"] == 200 for row in harness))
        if not result["passed"]:
            raise ValueError("public developer lifecycle failed or did not execute every required assertion")
    except Exception as error:
        failure = error
        result["error"] = str(error)
        try:
            collect_observations(output, result)
        except Exception as observation_error:
            result["observation_error"] = str(observation_error)
    finally:
        failure = cleanup_resources(archive, name, owner, image, result, peer,
                                    attempted, volume_attempted, failure)
        reference_lifecycle.write_json(archive / "result.json", result)
        print(json.dumps({"evidence": str(archive), **result}), flush=True)
        reference.artifact_storage.compact_closed_run(archive, ROOT)
        if peer:
            reference.artifact_storage.compact_closed_run(peer.archive, ROOT)
    if failure:
        raise RuntimeError("public developer lifecycle failed: " + str(archive)) from failure


if __name__ == "__main__":
    main()
