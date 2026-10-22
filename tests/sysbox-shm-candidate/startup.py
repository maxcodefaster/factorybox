"""Diagnose startup with the actual manager, writable worker and log flags."""
import datetime
import argparse
import json
import os
from pathlib import Path
import shutil
import signal
import subprocess
import sys
import time
import uuid


def inspect_tree(pids, prefix):
    output = Path("/output")
    pending, seen = list(pids), set()
    deadline = time.monotonic() + 5
    tasks = 0
    while pending and len(seen) < 64 and tasks < 256 and time.monotonic() < deadline:
        current = pending.pop()
        if current in seen:
            continue
        seen.add(current)
        for task in sorted(Path(f"/proc/{current}/task").glob("*")):
            tasks += 1
            if tasks > 256 or time.monotonic() >= deadline:
                break
            for field in ["children", "syscall", "stack", "wchan", "status"]:
                try:
                    data = (task / field).read_text()
                except OSError:
                    continue
                (output / f"{prefix}-{current}-{task.name}-{field}.txt").write_text(data[:32768])
                if field == "children":
                    pending.extend(int(value) for value in data.split())
    return sorted(seen)


def stop(process):
    if process.poll() is None:
        process.terminate()
        try:
            process.wait(timeout=3)
        except subprocess.TimeoutExpired:
            process.kill()
            process.wait(timeout=3)


def worker():
    output = Path("/output")
    Path("/out").mkdir()
    for component in ["sysbox-fs", "sysbox-mgr"]:
        shutil.copyfile("/components/" + component, "/out/" + component)
        Path("/out", component).chmod(0o755)
    observations = []
    for index in range(16):
        if index:
            # Nothing has ever registered a sandbox with these daemons.
            for name in ["sysfs.sock", "sysfs-seccomp.sock", "sysfs.pid", "sysmgr.sock", "sysmgr.pid"]:
                Path("/run/sysbox", name).unlink(missing_ok=True)
        processes, streams = [], []
        started = time.monotonic()
        row = {"index": index, "ready": False}
        try:
            for component, args in [
                ("sysbox-mgr", ["--disable-shiftfs", "--disable-inner-image-preload",
                                "--disable-binfmt-misc", "--data-root", "/var/lib/sysbox/manager"]),
                ("sysbox-fs", ["--mountpoint", "/var/lib/sysbox/fs"]),
            ]:
                stream = (output / f"{index:02d}-{component}-stdio.log").open("w")
                streams.append(stream)
                processes.append(subprocess.Popen(
                    ["/out/" + component, *args, "--log", str(output / f"{index:02d}-{component}.log")],
                    stdout=stream, stderr=subprocess.STDOUT,
                ))
            row["pids"] = [process.pid for process in processes]
            while time.monotonic() - started < 60:
                if (Path("/run/sysbox/sysfs-seccomp.sock").is_socket() and
                        Path("/run/sysbox/sysmgr.sock").is_socket()):
                    row["ready"] = True
                    break
                if any(process.poll() is not None for process in processes):
                    row["exits"] = [process.poll() for process in processes]
                    break
                if "soft_observation" not in row and time.monotonic() - started > 3:
                    row["soft_observation"] = inspect_tree(row["pids"], f"{index}-soft")
                time.sleep(0.025)
            row["seconds"] = time.monotonic() - started
            if not row["ready"]:
                row["processes"] = inspect_tree(row["pids"], f"{index}-failed")
                # This logical outcome is already failed. Go's startup handler
                # is not yet installed at the observed hang; ask that exact
                # unreaped child for a final goroutine dump before cleanup.
                filesystem = processes[1]
                if filesystem.poll() is None:
                    filesystem.send_signal(signal.SIGQUIT)
                    try:
                        filesystem.wait(timeout=2)
                    except subprocess.TimeoutExpired:
                        pass
        finally:
            for process in reversed(processes):
                stop(process)
            for stream in streams:
                stream.close()
            observations.append(row)
            (output / "observations.json").write_text(json.dumps(observations, indent=2) + "\n")
            print(json.dumps(row), flush=True)
        if not row["ready"]:
            raise ValueError("manager/filesystem startup exceeded the original readiness budget")


def host():
    root = Path(__file__).resolve().parents[2]
    sys.path.insert(0, str(root / "tools"))
    import native_base
    import reference
    import reference_lifecycle
    import upstream_unit
    import consumer_worker_loss
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--sysbox-build", required=True)
    parser.add_argument("--native-base-build", required=True)
    parser.add_argument("--shm-build", help="Exact explicitly unselected native SHM candidate build")
    parser.add_argument("--candidate-platform", choices=["linux/amd64"], help="Explicit native diagnostic target")
    parser.add_argument("--gc-percent", type=int, choices=[1, 100], help="Process-local Go GC setting only; no host tuning")
    options = parser.parse_args()
    candidate = candidate_result = None
    if options.shm_build:
        import re
        if not re.fullmatch(r"factorybox-build-shm-[0-9a-f]{32}", options.shm_build):
            raise ValueError("requires one exact native SHM candidate build")
        candidate = root / "artifacts" / options.shm_build
        candidate_result = json.loads((candidate / "result.json").read_text())
        if (not candidate_result["passed"] or not candidate_result["cleanup"] or
                candidate_result["variant"] != "candidate" or
                upstream_unit.context_manifest(candidate / "inputs") != candidate_result["source_inputs"] or
                reference.sha(candidate / "output/sysbox-fs") != candidate_result["binary_sha256"]["sysbox-fs"]):
            raise ValueError("SHM candidate identity changed")
    owner = uuid.uuid4().hex
    name = "factorybox-startup-manager-" + owner
    archive = reference.new_artifact(name)
    components, output = archive / "components", archive / "output"
    components.mkdir(parents=True)
    output.mkdir()
    shutil.copyfile(__file__, archive / "probe.py")
    shutil.copyfile(root / "tests/private-cgroup-delegation.sh", archive / "delegate.sh")
    build, inputs = reference_lifecycle.component_build(
        options.sysbox_build, "factorybox-build-sysbox-")
    for component in ["sysbox-fs", "sysbox-mgr"]:
        source = build / "output" / component
        expected = inputs["binary_sha256"][component]
        if component == "sysbox-fs" and candidate:
            source = candidate / "output/sysbox-fs"
            expected = candidate_result["binary_sha256"][component]
        reference.artifact_storage.share_frozen_input(
            source, components / component, root, expected)
    image, base = native_base.load(options.native_base_build, candidate_platform=options.candidate_platform)
    target = options.candidate_platform or "linux/arm64"
    native_base.require_native_host(target, json.loads(reference.docker(["info", "--format", "{{json .}}"])))
    if inputs.get("platform", "linux/arm64") != target or base.get("platform", "linux/arm64") != target:
        raise ValueError("startup components and worker must match the explicit native target")
    command = [
        *reference.DOCKER, "run", "--pull=never", "--name", name,
        "--label", "com.factorybox.run=" + owner, "--privileged", "--cgroupns=private",
        "--cpus=3", "--memory=3g", "--pids-limit=512", "--network=none",
        "--env", "GOMAXPROCS=2", "--env", "GOMEMLIMIT=256MiB",
        "--env", "FACTORYBOX_UPSTREAM_CONTAINER=1",
        "--env", "FACTORYBOX_STARTUP_MANAGER=1",
        "--mount", f"type=volume,source={name},target=/var/lib/sysbox",
        "--mount", f"type=bind,source={components},target=/components,readonly",
        "--mount", f"type=bind,source={output},target=/output",
        "--mount", f"type=bind,source={archive / 'probe.py'},target=/probe.py,readonly",
        "--mount", f"type=bind,source={archive / 'delegate.sh'},target=/delegate.sh,readonly",
        "--entrypoint", "/bin/bash", image, "-ec",
        ". /delegate.sh > /output/cgroup-delegation.txt\nmount --make-rprivate /\n"
        "exec setpriv --bounding-set=-sys_module,-sys_rawio,-sys_boot,-sys_time,-syslog python3 /probe.py",
    ]
    if options.gc_percent is not None:
        # Add before Docker's image argument, never as a shell/user command.
        position = command.index("--entrypoint")
        command[position:position] = ["--env", "GOGC=" + str(options.gc_percent), "--env", "GOTRACEBACK=all"]
    before = consumer_worker_loss.sysctls()
    result = {"owner": owner, "command": command, "passed": False, "cleanup": False,
              "shm_candidate": options.shm_build, "candidate_selected": False,
              "gc_percent": options.gc_percent, "platform": target, "global_sysctls_before": before,
              "sysbox_build": options.sysbox_build, "native_base_build": options.native_base_build,
              "started_at": datetime.datetime.now(datetime.timezone.utc).isoformat(),
              "inputs_sha256": {path.name: reference.sha(path) for path in components.iterdir()},
              "scope": "16 exact manager/filesystem starts with the original 60-second readiness limit, "
                       "writable worker, copied executables and --log flags; no sandbox or rootfs extraction.",
              "diagnostic_only": True}
    (archive / "intent.json").write_text(json.dumps(result, indent=2) + "\n")
    print(json.dumps({"running": str(archive)}), flush=True)
    try:
        reference.docker(["volume", "create", "--label", "com.factorybox.run=" + owner, name])
        with (archive / "stdout.txt").open("x") as stdout, (archive / "stderr.txt").open("x") as stderr:
            done = subprocess.run(command, stdout=stdout, stderr=stderr, timeout=420)
        result["exit_code"] = done.returncode
        result["container_state"] = json.loads(reference.docker(["inspect", name]))[0]["State"]
        result["observations"] = json.loads((output / "observations.json").read_text())
        result["inputs_unchanged"] = all(reference.sha(components / key) == value
                                         for key, value in result["inputs_sha256"].items())
        result["passed"] = (
            done.returncode == 0 and not result["container_state"]["OOMKilled"] and
            result["inputs_unchanged"] and len(result["observations"]) == 16 and
            all(row["ready"] for row in result["observations"]))
    except Exception as error:
        result["error"] = str(error)
    finally:
        reference.owned_delete("container", name, owner)
        reference.owned_delete("volume", name, owner)
        result["global_sysctls_after"] = consumer_worker_loss.sysctls()
        result["global_sysctls_unchanged"] = before == result["global_sysctls_after"]
        result["passed"] = result["passed"] and result["global_sysctls_unchanged"]
        result["cleanup"] = True
        (archive / "result.json").write_text(json.dumps(result, indent=2) + "\n")
        print(json.dumps({"evidence": str(archive), "passed": result["passed"],
                          "cleanup": result["cleanup"]}), flush=True)
    if not result["passed"]:
        raise ValueError("manager/filesystem startup diagnostic failed")


worker() if os.environ.get("FACTORYBOX_STARTUP_MANAGER") == "1" else host()
