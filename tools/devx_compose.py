#!/usr/bin/env python3
"""Run the real example with private Docker/Compose and the pinned Dev Containers CLI."""
import argparse
import datetime
import json
import os
import shutil
import subprocess
import uuid

import devx_inputs
import devx_project_images
import native_base
import reference
import reference_lifecycle
import upstream_bats
import upstream_unit

ROOT = reference.ROOT


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--consumer-inputs", required=True)
    parser.add_argument("--project-images", required=True)
    parser.add_argument("--native-base-build", required=True, help="Exact native archive helper image")
    options = parser.parse_args()
    _, consumers = devx_inputs.load(options.consumer_inputs)
    archive_image, _ = native_base.load(options.native_base_build)
    project_archive, projects = devx_project_images.load(options.project_images)
    owner = uuid.uuid4().hex
    name = "factorybox-devx-compose-" + owner
    archive = ROOT / "artifacts" / name
    output = archive / "output"
    output.mkdir(parents=True)
    for script in ["devx-compose-check.sh", "devx-observation.cjs"]:
        shutil.copyfile(ROOT / "tests" / script, archive / script)
    # Freeze a reviewable copy; the native workload never writes into the repo.
    shutil.copytree(ROOT / "examples/factory", archive / "example")
    reference_lifecycle.write_json(archive / "project-images.json",
                                  [{"image": row["image"], "canonical_image": row["canonical_image"]}
                                   for row in projects["images"]])
    worker = archive / "devx-worker"
    result = {
        "schema": 1, "owner": owner, "container_name": name,
        "started_at": datetime.datetime.now(datetime.timezone.utc).isoformat(),
        "consumer_inputs": options.consumer_inputs, "project_images": options.project_images,
        "runtime_image": consumers["selected_images"]["build-runtime-host"],
        "project_archive_sha256": projects["archive_sha256"],
        "example": upstream_unit.context_manifest(archive / "example"),
        "passed": False, "cleanup": False, "production_qualified": False,
        "scope": "Component conformance using real private Docker, Compose, Dev Containers CLI, "
                 "project hooks and Node app RAM. Not a Sysbox/Factorybox checkpoint/fork/resume, "
                 "Theia or active Connector-control proof.",
    }
    attempted = False
    volumes = []
    failure = None
    try:
        env = dict(os.environ, GOOS="linux", GOARCH="arm64", CGO_ENABLED="0")
        subprocess.run(["go", "build", "-trimpath", "-buildvcs=false", "-o", str(worker),
                        "./tests/devx-worker"], cwd=ROOT, env=env, check=True, timeout=60)
        registry = archive / "devx-registry"
        subprocess.run(["go", "build", "-trimpath", "-buildvcs=false", "-o", str(registry),
                        "./tests/devx-registry"], cwd=ROOT, env=env, check=True, timeout=60)
        result["inputs_sha256"] = {p.name: reference.sha(p) for p in [
            worker, registry, archive / "devx-compose-check.sh", archive / "devx-observation.cjs",
            archive / "project-images.json"]}
        command = [
            *reference.DOCKER, "run", "--pull=never", "--name", name,
            "--label", "com.factorybox.run=" + owner, "--privileged", "--cgroupns=private",
            "--network=none", "--cpus=3", "--memory=3g", "--pids-limit=512",
            "--env", "FACTORYBOX_DEVX_WORKER=1",
            "--mount", f"type=bind,source={worker},target=/devx-worker,readonly",
            "--mount", f"type=bind,source={registry},target=/devx-registry,readonly",
            "--mount", f"type=bind,source={archive / 'devx-observation.cjs'},target=/devx-observation.cjs,readonly",
            "--mount", f"type=bind,source={archive / 'devx-compose-check.sh'},target=/run-proof.sh,readonly",
            "--mount", f"type=bind,source={archive / 'example'},target=/example,readonly",
            "--mount", f"type=bind,source={project_archive / 'images.tar'},target=/project-images.tar,readonly",
            "--mount", f"type=bind,source={archive / 'project-images.json'},target=/project-images.json,readonly",
            "--mount", f"type=bind,source={output},target=/output",
        ]
        for suffix, path in [("docker", "/var/lib/docker"), ("work", "/work")]:
            volume = name + "-" + suffix
            volumes.append(volume)
            reference.docker(["volume", "create", "--label", "com.factorybox.run=" + owner, volume])
            command += ["--mount", f"type=volume,source={volume},target={path}"]
        command += ["--entrypoint", "/devx-worker", result["runtime_image"],
                    "/usr/local/bin/docker-init", "--", "/bin/sh", "/run-proof.sh"]
        result["command"] = command
        reference_lifecycle.write_json(archive / "intent.json", result)
        print(json.dumps({"running": str(archive)}), flush=True)
        attempted = True
        with (archive / "stdout.txt").open("x") as stdout, (archive / "stderr.txt").open("x") as stderr:
            done = subprocess.run(command, stdout=stdout, stderr=stderr, timeout=240)
        result["exit_code"] = done.returncode
        state = json.loads(reference.docker(["container", "inspect", name]))[0]["State"]
        result["container_state"] = state
        if done.returncode or state["OOMKilled"] or not (output / "app-state.json").exists():
            raise ValueError("real Docker/Compose/Dev Container component proof failed")
        result["app_state"] = json.loads((output / "app-state.json").read_text())
        before = json.loads((output / "sysctls-before.json").read_text())
        after = json.loads((output / "sysctls-after.json").read_text())
        result["observed_global_sysctls_unchanged"] = before == after
        result["inputs_unchanged"] = (
            all(reference.sha(archive / path) == expected
                for path, expected in result["inputs_sha256"].items()) and
            upstream_unit.context_manifest(archive / "example") == result["example"] and
            devx_inputs.file_sha(project_archive / "images.tar") == projects["archive_sha256"])
        if not result["observed_global_sysctls_unchanged"] or not result["inputs_unchanged"]:
            raise ValueError("fixture inputs or observed global sentinels changed")
        result["passed"] = True
    except Exception as error:
        failure = error
        result["error"] = str(error)
    finally:
        preserve = False
        if attempted and failure:
            try:
                upstream_bats.archive_failure(archive, name, owner, volumes, archive_image, result)
            except Exception as error:
                preserve = True
                result["archive_error"] = str(error)
                result["retained_volumes"] = volumes
                failure = failure or error
        try:
            if attempted:
                reference.owned_delete("container", name, owner)
            if not preserve:
                for volume in volumes:
                    reference.owned_delete("volume", volume, owner)
            result["cleanup"] = not preserve
        except Exception as error:
            result["cleanup_error"] = str(error)
            failure = failure or error
        result["passed"] = result["passed"] and result["cleanup"]
        reference_lifecycle.write_json(archive / "result.json", result)
        print(json.dumps({"evidence": str(archive), **result}), flush=True)
    if failure:
        raise RuntimeError("developer Compose component proof failed: " + str(archive)) from failure


if __name__ == "__main__":
    main()
