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
import native_base
import reference
import reference_lifecycle
import theia_peer
import upstream_bats
import upstream_unit

ROOT = reference.ROOT


def require_no_retained_worker():
    names = reference.docker([
        "volume", "ls", "--filter", "label=com.factorybox.run", "--format", "{{.Name}}"])
    retained = [name for name in names.splitlines()
                if re.fullmatch(r"factorybox-devx-lifecycle-[0-9a-f]{32}", name)]
    if retained:
        raise ValueError("reconcile the existing developer volume before starting another run: " +
                         ", ".join(sorted(retained)))


def collect_observations(output, result):
    for filename, key in [
        ("devx-observations.json", "observations"),
        ("preparation.json", "preparation"),
        ("theia-observations.json", "theia_observations"),
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


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--consumer-inputs", required=True)
    parser.add_argument("--project-images", required=True)
    parser.add_argument("--native-base-build", required=True)
    parser.add_argument("--sysbox-build", required=True)
    parser.add_argument("--criu-build", required=True)
    parser.add_argument("--theia-image", help="Optional exact cached IDE digest for the separate native RPC peer")
    parser.add_argument("--suite", choices=["lifecycle", "projection"], default="lifecycle")
    options = parser.parse_args()
    if options.suite == "projection" and options.theia_image:
        parser.error("projection is a minimal namespace regression without the IDE server")
    require_no_retained_worker()
    consumer_archive, consumers = devx_inputs.load(options.consumer_inputs, rootfs=True)
    project_archive, projects = devx_project_images.load(options.project_images)
    image, _ = native_base.load(options.native_base_build)
    sysbox_archive, sysbox = reference_lifecycle.component_build(
        options.sysbox_build, "factorybox-build-sysbox-")
    criu_archive, criu = reference_lifecycle.component_build(
        options.criu_build, "factorybox-build-criu-")
    ide_image = devx_inputs.inspect_image(options.theia_image) if options.theia_image else None
    if ide_image:
        reference.verify()
    owner = uuid.uuid4().hex
    name = "factorybox-devx-lifecycle-" + owner
    archive = reference.new_artifact(name)
    output = archive / "output"
    output.mkdir(parents=True)
    scripts = ["devx-lifecycle.sh", "devx-lifecycle-body.sh", "devx-sandbox.sh", "devx-devcontainer.sh",
               "private-cgroup-delegation.sh", "global-sysctl-observation.py", "wait-sysbox.sh"]
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
        "started_at": datetime.datetime.now(datetime.timezone.utc).isoformat(),
        "consumer_inputs": options.consumer_inputs, "project_images": options.project_images,
        "sysbox_build": options.sysbox_build, "criu_build": options.criu_build,
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
        result["scope"] += (" The optional separate Theia peer uses worker-controlled live workspace bind "
                            "views with serialized external writes; native RPC connection continuity "
                            "and Connector app readback are required. This is a private fixture "
                            "composition, not a production workspace-projection API.")
    if options.suite == "projection":
        result["scope"] = ("Bounded native regression for exact IDE namespace/root selection and "
                           "live bind-mounted read/write. No Sysbox workload, IDE server or lifecycle qualification.")
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
        if options.suite == "projection":
            command += ["--env", "FACTORYBOX_DEVX_PROJECTION=1"]
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
        observed = [row["phase"] for row in result.get("observations", [])]
        result["passed"] = (
            done.returncode == 0 and not state["OOMKilled"] and result["inputs_unchanged"] and
            result.get("observed_global_sysctls_unchanged", False) and
            len(observed) == len(expected) and set(observed) == expected and
            "--- PASS: TestNativeDeveloperLifecycle " in text and "--- SKIP:" not in text and
            "--- FAIL:" not in text)
        if options.suite == "projection":
            result["passed"] = (
                done.returncode == 0 and not state["OOMKilled"] and result["inputs_unchanged"] and
                result.get("observed_global_sysctls_unchanged", False) and
                "--- PASS: TestNativeIDEProjectionNamespace " in text and
                "--- SKIP:" not in text and "--- FAIL:" not in text)
        if peer:
            ide_observations = result.get("theia_observations", [])
            result["theia"] = peer.result
            result["passed"] = (
                result["passed"] and peer.result["passed"] and peer.result["cleanup"] and
                len(ide_observations) == len(expected) + 1 and
                {row["phase"] for row in ide_observations} == expected | {"selected-sandbox-edit-readback"} and
                all(row["client_id"] == peer.result["peer"]["client_id"] and
                    row["pid"] == peer.result["peer"]["pid"] for row in ide_observations))
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
        preserve = False
        if peer:
            peer.close(bool(failure))
            result["theia"] = peer.result
        if attempted and failure:
            try:
                upstream_bats.archive_failure(archive, name, owner, [name], image, result,
                                             timeout=600, compressed=True)
            except Exception as error:
                preserve = True
                result["archive_error"] = str(error)
                result["retained_volumes"] = [name]
        try:
            if attempted:
                reference.owned_delete("container", name, owner)
            if volume_attempted and not preserve:
                reference.owned_delete("volume", name, owner)
            result["cleanup"] = not preserve
        except Exception as error:
            result["cleanup_error"] = str(error)
            failure = failure or error
        result["passed"] = result["passed"] and result["cleanup"]
        reference_lifecycle.write_json(archive / "result.json", result)
        print(json.dumps({"evidence": str(archive), **result}), flush=True)
        reference.artifact_storage.compact_closed_run(archive, ROOT)
        if peer:
            reference.artifact_storage.compact_closed_run(peer.archive, ROOT)
    if failure:
        raise RuntimeError("public developer lifecycle failed: " + str(archive)) from failure


if __name__ == "__main__":
    main()
