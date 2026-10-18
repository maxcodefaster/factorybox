#!/usr/bin/env python3
"""Build the Linux native-test/CRIU toolchain from public pinned inputs."""
import datetime
import json
import shutil
import subprocess
import uuid

import native_base
import reference


def main():
    owner = uuid.uuid4().hex
    name = "factorybox-build-native-base-" + owner
    archive = reference.ROOT / "artifacts" / name
    context = archive / "context"
    context.mkdir(parents=True)
    recipe = reference.ROOT / "build/native-base.Dockerfile"
    shutil.copyfile(recipe, context / "Dockerfile")
    result = {
        "schema": 1, "owner": owner,
        "started_at": datetime.datetime.now(datetime.timezone.utc).isoformat(),
        "dockerfile_sha256": reference.sha(recipe),
        "base": "ghcr.io/coder/envbox@sha256:13e4477ed65912a72e52bb7115db63d4da934cf8a55962c8fdf7c470901dceff",
        "apt_snapshot": "20261018T000000Z", "platform": "linux/arm64",
        "passed": False, "cleanup": False, "production_qualified": False,
        "scope": "Public-input native-test and CRIU build dependencies. "
                 "Does not install a node runtime or qualify production.",
    }
    helper = name + "-inspect"
    attempted = False
    failure = None
    try:
        command = [
            *reference.DOCKER, "build", "--provenance=false", "--progress=plain",
            "--platform=linux/arm64", "--tag", name,
            "--output", "type=image,compression=uncompressed",
            "--metadata-file", str(archive / "metadata.json"), str(context),
        ]
        result["command"] = command
        (archive / "intent.json").write_text(json.dumps(result, indent=2) + "\n")
        print(json.dumps({"building": str(archive)}), flush=True)
        with (archive / "build.log").open("x") as log:
            done = subprocess.run(command, stdout=log, stderr=subprocess.STDOUT, timeout=900)
        if done.returncode:
            raise ValueError("native base image build failed")
        metadata = json.loads((archive / "metadata.json").read_text())
        result["image"] = name + "@" + metadata["containerimage.digest"]
        result["image_config"] = metadata["containerimage.config.digest"]
        inspected = json.loads(reference.docker(["image", "inspect", result["image"]]))[0]
        native_base.verify_image(result, inspected)
        result["docker_reported_image_id"] = inspected["Id"]
        inspect_command = [
            *reference.DOCKER, "run", "--pull=never", "--name", helper,
            "--label", "com.factorybox.run=" + owner, "--cpus=1", "--memory=128m",
            "--pids-limit=32", "--network=none", "--read-only", "--cap-drop=ALL",
            "--security-opt=no-new-privileges", "--entrypoint", "/bin/cat",
            result["image"], "/opt/factorybox-native-packages.tsv",
        ]
        result["inspection_command"] = inspect_command
        attempted = True
        done = subprocess.run(inspect_command, capture_output=True, timeout=30)
        (archive / "inspect.stderr").write_bytes(done.stderr)
        if done.returncode:
            raise ValueError("native base package inspection failed")
        (archive / "packages.tsv").write_bytes(done.stdout)
        result["packages_sha256"] = reference.sha(archive / "packages.tsv")
        result["passed"] = True
    except Exception as error:
        failure = error
        result["error"] = str(error)
    finally:
        try:
            if attempted:
                reference.owned_delete("container", helper, owner)
            result["cleanup"] = True
        except Exception as error:
            result["cleanup_error"] = str(error)
            failure = failure or error
        (archive / "result.json").write_text(json.dumps(result, indent=2) + "\n")
        print(json.dumps({"evidence": str(archive), **result}), flush=True)
    if failure:
        raise RuntimeError("native base build failed: " + str(archive)) from failure


if __name__ == "__main__":
    main()
