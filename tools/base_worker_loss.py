#!/usr/bin/env python3
"""Probe persisted files or committed RAM across complete same-host worker loss."""
import argparse
import json
import os
import shutil
import subprocess
import time
import uuid
import worker_readiness

import native_base
import criu_candidate
import reference
import reference_lifecycle
import upstream_unit
import build_worker


def rejected_worker(artifact, command, container, owner, reason, *, foreign=False):
    name = container + ("-foreign-store" if foreign else "-duplicate-store")
    rejected = list(command)
    rejected[rejected.index("--name") + 1] = name
    if foreign:
        index = rejected.index("--entrypoint")
        if "--store-id" in rejected:
            rejected[rejected.index("--store-id")+1] = "0"*32
        else:
            rejected[index:index] = ["--env", "FACTORYBOX_WORKER_EXPECTED_STORE_ID=" + "0"*32]
    identity = reference.docker(rejected)
    try:
        reference.docker(["container", "wait", identity], timeout=15)
        inspected = json.loads(reference.docker(["container", "inspect", identity]))[0]
        logs = subprocess.run([*reference.DOCKER, "logs", identity], capture_output=True, text=True, timeout=5)
        (artifact / (name + ".log")).write_text(logs.stdout + logs.stderr)
        state = inspected["State"]
        if (inspected["Name"] != "/" + name or inspected["Config"].get("Labels", {}).get("com.factorybox.run") != owner or
                state["Running"] or state["Pid"] != 0 or state["ExitCode"] != 1 or state["OOMKilled"] or
                reason not in logs.stderr):
            raise ValueError("worker did not reject the exact foreign/duplicate store: " + logs.stderr)
        return {"id": identity, "state": state, "expected_rejection": reason}
    finally:
        reference.owned_delete("container", name, owner)


def run_supervised_probe(artifact, mode, phase, identity, container, owner, deadline, observation, command, image_probe=False):
    started = time.monotonic()
    ready_deadline = min(started + 65, deadline)
    while True:
        inspected = json.loads(reference.docker(["container", "inspect", identity]))[0]
        if (inspected["Id"] != identity or inspected["Name"] != "/" + container or
                inspected["Config"].get("Labels", {}).get("com.factorybox.run") != owner):
            raise ValueError("supervised worker ownership changed")
        if not inspected["State"]["Running"]:
            logs = subprocess.run([*reference.DOCKER, "logs", identity], capture_output=True, text=True, timeout=5)
            (artifact / (mode + "-" + phase + "-worker.log")).write_text(logs.stdout + logs.stderr)
            raise ValueError("Go worker exited before readiness: " + (logs.stdout + logs.stderr)[-4096:])
        if not worker_readiness.published(reference.DOCKER, identity, owner):
            if time.monotonic() >= ready_deadline:
                raise TimeoutError("Go worker readiness publication deadline exceeded")
            time.sleep(0.1)
            continue
        status = subprocess.run([*reference.DOCKER, "exec", identity, "/factorybox", "worker-status"],
                                capture_output=True, text=True, timeout=5)
        if status.returncode == 0:
            ready = json.loads(status.stdout)
            if ready["schema"] != 1 or ready["store_id"] != owner:
                raise ValueError("worker status differs from its exact persistent store")
            break
        if time.monotonic() >= ready_deadline:
            raise TimeoutError("Go worker readiness deadline exceeded: " + status.stderr)
        time.sleep(0.1)
    observation[phase + "_worker"] = {"status": ready, "startup_seconds": time.monotonic() - started}
    if phase == "prepare":
        observation["duplicate_store"] = rejected_worker(artifact, command, container, owner,
                                                          "persistent store already has a worker")
    probe_start = time.monotonic()
    with (artifact / (mode + "-" + phase + ".log")).open("x") as out, \
            (artifact / (mode + "-" + phase + ".stderr")).open("x") as err:
        command = [*reference.DOCKER, "exec", identity, "/probe.test", "-test.v",
                   "-test.run=^TestWorkerRestartProbeNative$", "-test.timeout=360s"]
        if image_probe:
            command = [*reference.DOCKER, "exec", identity, "/bin/bash", "/worker-image-probe-setup.sh"]
        probe = subprocess.Popen(command, stdout=out, stderr=err)
        try:
            if phase == "prepare" and mode == "abrupt":
                while "FACTORYBOX_PAUSE_COMMITTED=" + owner not in (artifact / (mode + "-" + phase + ".log")).read_text().splitlines():
                    if probe.poll() is not None:
                        raise ValueError("supervised preparation exited before committed pause")
                    if time.monotonic() >= min(probe_start + 120, deadline):
                        raise TimeoutError("supervised pause commit deadline exceeded")
                    time.sleep(0.1)
                inspected = json.loads(reference.docker(["container", "inspect", identity]))[0]
                if (inspected["Name"] != "/" + container or
                        inspected["Config"].get("Labels", {}).get("com.factorybox.run") != owner or
                        not inspected["State"]["Running"]):
                    raise ValueError("worker identity changed before intentional SIGKILL")
                reference.docker(["container", "kill", "--signal=KILL", identity])
                code = probe.wait(timeout=max(1, deadline-time.monotonic()))
                if code != 137:
                    raise ValueError("killed worker exec exit differs: " + str(code))
            else:
                code = probe.wait(timeout=max(1, deadline-time.monotonic()))
                if code != 0:
                    raise ValueError("supervised native probe failed: " + str(code))
                reference.docker(["container", "stop", "--time=15", identity], timeout=20)
            observation[phase + "_worker"]["probe_seconds"] = time.monotonic()-probe_start
            observation[phase + "_worker"]["probe_exit"] = code
        finally:
            if probe.poll() is None:
                reference.owned_delete("container", container, owner)
                probe.wait(timeout=10)
            logs = subprocess.run([*reference.DOCKER, "logs", identity], capture_output=True, text=True, timeout=5)
            (artifact / (mode + "-" + phase + "-worker.log")).write_text(logs.stdout + logs.stderr)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--native-base-build", required=True)
    parser.add_argument("--scope", choices=["base", "ram"], default="base")
    parser.add_argument("--sysbox-build")
    parser.add_argument("--retained-candidate")
    parser.add_argument("--supervisor", action="store_true", help="Use the Go worker as container PID 1")
    parser.add_argument("--worker-build", help="Test the actual delivered CLI and complete worker image")
    options = parser.parse_args()
    options.supervisor = options.supervisor or bool(options.worker_build)
    if (options.scope == "ram") != bool(options.sysbox_build and options.retained_candidate):
        parser.error("RAM prototype requires both exact Sysbox and CRIU builds")
    if options.supervisor and options.scope != "ram":
        parser.error("--supervisor requires --scope ram")
    owner = uuid.uuid4().hex
    name = ("factorybox-worker-restart-probe-" if options.scope == "ram" else "factorybox-base-worker-loss-") + owner
    artifact = reference.new_artifact(name)
    artifact.mkdir()
    shutil.copyfile(__file__, artifact / "runner.py")
    result = {"schema": 1, "owner": owner, "scope": __doc__, "passed": False,
              "cleanup": False, "production_qualified": False, "ram_recovery_qualified": False,
              "modes": [], "commands": [], "probe_scope": options.scope}
    result["go_worker_supervisor"] = options.supervisor
    result["runner_sha256"] = reference.sha(artifact / "runner.py")
    print(json.dumps({"running": str(artifact), "scope": options.scope, "supervisor": options.supervisor}), flush=True)
    containers, volumes = [], []
    try:
        host = json.loads(reference.docker(["info", "--format", "{{json .}}" ]))
        native_base.require_native_host("linux/amd64", host)
        image, _ = native_base.load(options.native_base_build, candidate_platform="linux/amd64")
        worker_path, worker_result = build_worker.load(options.worker_build) if options.worker_build else (None, None)
        if worker_result:
            if (worker_result["native_base_build"] != options.native_base_build or worker_result["sysbox_build"] != options.sysbox_build or
                    worker_result["criu_build"] != options.retained_candidate):
                raise ValueError("delivered worker differs from requested exact component builds")
            image = worker_result["image"]
            result["worker_build"], result["worker_image_id"] = options.worker_build, worker_result["image_id"]
        result["image"] = image
        result["host"] = {k: host[k] for k in ["OSType", "Architecture", "KernelVersion"]}
        context = artifact / "context"
        shutil.copytree(reference.ROOT / "internal", context / "internal")
        if options.scope == "ram":
            shutil.copytree(reference.ROOT / "cmd", context / "cmd")
        for file in ["go.mod", "go.sum"]:
            shutil.copyfile(reference.ROOT / file, context / file)
        result["source_context"] = upstream_unit.context_manifest(context)
        if worker_result:
            # Test fixtures may evolve independently, but every non-test input
            # must match the delivered executable's frozen compilation context.
            compiled = lambda manifest: {key: value for key, value in manifest.items() if not key.endswith("_test.go")}
            if compiled(result["source_context"]) != compiled(worker_result["source_context"]):
                raise ValueError("native probe runtime sources differ from delivered worker")
        binary = artifact / "probe.test"
        package = "./internal/lifecycle" if options.scope == "ram" else "./internal/sysboxcapture"
        subprocess.run(["go", "test", "-c", "-trimpath", "-buildvcs=false", "-tags=factorybox_worker_probe",
                        "-o", str(binary), package], cwd=context, check=True,
                       env=dict(os.environ, GOOS="linux", GOARCH="amd64", CGO_ENABLED="0"), timeout=120)
        result["binary_sha256"] = reference.sha(binary)
        mounts, environment = [], []
        if options.scope == "ram":
            if worker_result:
                reference.artifact_storage.share_frozen_input(worker_path / "image-context/bundle/factorybox", artifact / "factorybox",
                                                            reference.ROOT, worker_result["bundle_sha256"]["factorybox"])
            else:
                subprocess.run(["go", "build", "-trimpath", "-buildvcs=false", "-o", str(artifact / "factorybox"),
                                "./cmd/factorybox"], cwd=context, check=True,
                               env=dict(os.environ, GOOS="linux", GOARCH="amd64", CGO_ENABLED="0"), timeout=120)
            result["cli_sha256"] = reference.sha(artifact / "factorybox")
            components = artifact / "components"
            components.mkdir()
            sysbox_path, sysbox = reference_lifecycle.component_build(options.sysbox_build, "factorybox-build-sysbox-")
            criu_path, criu, _, _ = criu_candidate.load(options.retained_candidate)
            if any(row.get("platform") != "linux/amd64" for row in [sysbox, criu]):
                raise ValueError("cold-worker component platforms differ")
            for file in ["sysbox-runc", "sysbox-mgr", "sysbox-fs"]:
                reference.artifact_storage.share_frozen_input(sysbox_path / "output" / file, components / file,
                                                            reference.ROOT, sysbox["binary_sha256"][file])
            reference.artifact_storage.share_frozen_input(criu_path / "criu", components / "criu",
                                                        reference.ROOT, criu["binary_sha256"])
            result["components_sha256"] = {file.name: reference.sha(file) for file in components.iterdir()}
            if options.supervisor:
                (artifact / "components.json").write_text(json.dumps({"schema": 1, "binary_sha256": result["components_sha256"]}) + "\n")
                shutil.copyfile(reference.ROOT / "tests/worker-native-init.sh", artifact / "worker-native-init.sh")
            for file in ["worker-restart-probe.sh", "private-cgroup-delegation.sh", "wait-sysbox.sh"]:
                shutil.copyfile(reference.ROOT / "tests" / file, artifact / file)
            busybox = json.loads((reference.ROOT / "upstream/test-inputs.json").read_text())["busybox-amd64"]
            rootfs = reference.ROOT / ".cache/downloads" / busybox["archive"]
            if reference.sha(rootfs) != busybox["sha256"]:
                raise ValueError("cold-worker busybox input changed")
            mounts = [(components, "/component-input"), (artifact / "factorybox", "/factorybox"),
                      (rootfs, "/busybox-rootfs.tar.gz"), (artifact / "worker-restart-probe.sh", "/worker-probe.sh"),
                      (artifact / "private-cgroup-delegation.sh", "/delegate.sh"),
                      (artifact / "wait-sysbox.sh", "/wait-sysbox.sh")]
            if options.supervisor:
                mounts += [(artifact / "components.json", "/component-manifest.json"),
                           (artifact / "worker-native-init.sh", "/worker-native-init.sh")]
            if worker_result:
                shutil.copyfile(reference.ROOT / "tests/worker-image-probe-setup.sh", artifact / "worker-image-probe-setup.sh")
                mounts += [(artifact / "worker-image-probe-setup.sh", "/worker-image-probe-setup.sh")]
            environment = ["FACTORYBOX_UPSTREAM_CONTAINER=1", "FACTORYBOX_NATIVE_LIFECYCLE=1", "FACTORYBOX_NATIVE_IMMUTABLE_BASE=1",
                           "FACTORYBOX_RUNTIME_SHA256=" + sysbox["binary_sha256"]["sysbox-runc"],
                           "FACTORYBOX_CRIU_SHA256=" + criu["binary_sha256"]]
            if options.supervisor:
                environment += ["FACTORYBOX_NATIVE_WORKER_SUPERVISOR=1"]
        for mode in ["planned", "abrupt"]:
            volume = name + "-" + mode
            reference.docker(["volume", "create", "--label", "com.factorybox.run=" + owner, volume])
            volumes.append(volume)
            observation = {"mode": mode, "passed": False}
            result["modes"].append(observation)
            lifecycle_deadline = time.monotonic() + 420
            for phase in ["prepare", "recover"]:
                container = volume + "-" + phase
                command = ["run", "--detach", "--pull=never", "--platform=linux/amd64",
                           "--name", container, "--label", "com.factorybox.run=" + owner,
                           "--privileged", "--cgroupns=private", "--cpus=3", "--memory=3g",
                           "--pids-limit=128", "--network=none", "--read-only", "--env", "GOMAXPROCS=2",
                           "--tmpfs", "/tmp:rw,nosuid,nodev,size=32m",
                           "--env", "FACTORYBOX_BASE_WORKER_OWNER=" + owner,
                           "--env", "FACTORYBOX_BASE_WORKER_PHASE=" + phase,
                           "--env", "FACTORYBOX_BASE_WORKER_ABRUPT=" + ("1" if mode == "abrupt" else "0"),
                           "--mount", f"type=volume,source={volume},target=/var/lib/sysbox",
                           "--mount", f"type=bind,source={binary},target=/probe.test,readonly"]
                for value in environment:
                    command += ["--env", value]
                for source, target in mounts:
                    command += ["--mount", f"type=bind,source={source},target={target},readonly"]
                if options.scope == "ram":
                    # Match the qualified Sysbox worker envelope. Its daemons
                    # need ephemeral /out, /run and /busybox; data stays on the
                    # explicit persistent volume. The base-only probe is RO.
                    command.remove("--read-only")
                    output = artifact / (mode + "-" + phase + "-output")
                    output.mkdir()
                    command += ["--mount", f"type=bind,source={output},target=/output"]
                    if worker_result:
                        command += ["--entrypoint", "/opt/factorybox/factorybox", image, "worker",
                                    "--store", "/var/lib/sysbox/store", "--store-id", owner]
                    else:
                        command += ["--entrypoint", "/bin/bash", image,
                                    "/worker-native-init.sh" if options.supervisor else "/worker-probe.sh"]
                else:
                    command += ["--entrypoint", "/probe.test", image, "-test.v",
                                "-test.run=^TestFilesystemBaseWorkerLossNative$", "-test.timeout=30s"]
                result["commands"].append(command)
                containers.append(container)
                if options.supervisor and phase == "recover":
                    observation["foreign_store"] = rejected_worker(artifact, command, container, owner,
                                                                     "invalid persistent store identity", foreign=True)
                identity = reference.docker(command)
                if options.supervisor:
                    run_supervised_probe(artifact, mode, phase, identity, container, owner, lifecycle_deadline, observation, command, bool(worker_result))
                elif phase == "prepare" and mode == "abrupt":
                    deadline = min(time.monotonic() + (120 if options.scope == "ram" else 15), lifecycle_deadline)
                    while True:
                        inspected = json.loads(reference.docker(["container", "inspect", container]))[0]
                        if (inspected["Id"] != identity or inspected["Name"] != "/" + container or
                                inspected["Config"].get("Labels", {}).get("com.factorybox.run") != owner):
                            raise ValueError("original worker identity changed before intentional loss")
                        state = inspected["State"]
                        if not state["Running"]:
                            raise ValueError("preparation exited before intentional worker loss")
                        log = reference.docker(["logs", identity])
                        marker = "FACTORYBOX_PAUSE_COMMITTED=" if options.scope == "ram" else "FACTORYBOX_BASE_COMMITTED="
                        if marker + owner in log.splitlines():
                            break
                        if time.monotonic() >= deadline:
                            raise ValueError("base preparation did not publish its durable proof")
                        time.sleep(0.1)
                    reference.docker(["container", "kill", "--signal=KILL", identity])
                remaining = lifecycle_deadline - time.monotonic()
                if remaining <= 0:
                    raise TimeoutError("cold-worker lifecycle deadline exceeded")
                reference.docker(["container", "wait", container], timeout=remaining)
                inspected = json.loads(reference.docker(["container", "inspect", container]))[0]
                state = inspected["State"]
                logs = subprocess.run([*reference.DOCKER, "logs", identity], capture_output=True, text=True,
                                      check=True, timeout=5)
                if not options.supervisor:
                    (artifact / (mode + "-" + phase + ".log")).write_text(logs.stdout)
                    (artifact / (mode + "-" + phase + ".stderr")).write_text(logs.stderr)
                expected = 137 if phase == "prepare" and mode == "abrupt" else 0
                if state["Running"] or state["Pid"] != 0 or state["ExitCode"] != expected or state["OOMKilled"]:
                    raise ValueError(f"worker {mode}/{phase} exit {state['ExitCode']} != {expected}; running={state['Running']} oom={state['OOMKilled']}")
                if phase == "recover":
                    log = (artifact / (mode + "-" + phase + ".log")).read_text()
                    test_name = "TestWorkerRestartProbeNative" if options.scope == "ram" else "TestFilesystemBaseWorkerLossNative"
                    if "--- PASS: " + test_name + " " not in log or "--- SKIP:" in log:
                        raise ValueError("fresh worker recovery assertion omitted")
                observation[phase] = {"id": inspected["Id"], "state": state}
                reference.owned_delete("container", container, owner)
                containers.remove(container)
                if phase == "prepare":
                    observation["old_worker_removed_before_recovery"] = True
            observation["passed"] = True
        if upstream_unit.context_manifest(context) != result["source_context"]:
            raise ValueError("base worker-loss source changed")
        result["passed"] = True
    except Exception as error:
        result["error"] = str(error)
        raise
    finally:
        try:
            for container in containers:
                logs = subprocess.run([*reference.DOCKER, "logs", container], capture_output=True, text=True, timeout=5)
                (artifact / (container + "-final.log")).write_text(logs.stdout + logs.stderr)
                reference.owned_delete("container", container, owner)
            if result["passed"]:
                for volume in volumes:
                    reference.owned_delete("volume", volume, owner)
                result["cleanup"] = True
            else:
                result["retained_volumes"] = volumes
        finally:
            (artifact / "result.json").write_text(json.dumps(result, indent=2) + "\n")
            print(json.dumps({"evidence": str(artifact), "passed": result["passed"],
                              "cleanup": result["cleanup"], "error": result.get("error")}))


if __name__ == "__main__":
    main()
