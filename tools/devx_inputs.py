#!/usr/bin/env python3
"""Freeze actual Factory consumer images and export the selected runtime rootfs."""
import argparse
import datetime
import hashlib
import json
import pathlib
import re
import shutil
import subprocess
import uuid

import reference
import reference_lifecycle

ROOT = reference.ROOT
ROLES = ["build-runtime-host", "developer-ide"]
PIN = re.compile(r"^[a-z0-9][a-z0-9.:/_-]*@sha256:[0-9a-f]{64}$")


def file_sha(path):
    with path.open("rb") as file:
        return hashlib.file_digest(file, "sha256").hexdigest()


def canonical_digest(image):
    """Compare Docker's familiar repository spelling without dropping the digest."""
    if not isinstance(image, str) or not PIN.fullmatch(image):
        raise ValueError("consumer image must have an exact digest and no credentials")
    repository, digest = image.split("@")
    prefix, separator, final = repository.rpartition("/")
    final = final.split(":", 1)[0]
    repository = prefix + separator + final
    if repository.startswith("docker.io/"):
        repository = repository[len("docker.io/"):]
    if repository.startswith("library/"):
        repository = repository[len("library/"):]
    return repository + "@" + digest


def inspect_image(image):
    canonical = canonical_digest(image)
    data = json.loads(reference.docker(["image", "inspect", image]))[0]
    if (data["Architecture"] != "arm64" or data["Os"] != "linux" or
            canonical not in [canonical_digest(ref) for ref in data.get("RepoDigests", [])]):
        raise ValueError("consumer image digest or native platform differs")
    config = data["Config"]
    return {
        "image": image, "canonical_image": canonical, "id": data["Id"], "architecture": data["Architecture"],
        "os": data["Os"], "bytes": data["Size"],
        "user": config.get("User"), "volumes": config.get("Volumes") or {},
        "entrypoint": config.get("Entrypoint"), "command": config.get("Cmd"),
        "source": (config.get("Labels") or {}).get("org.opencontainers.image.source"),
    }


def load(name, *, rootfs=False):
    if not re.fullmatch(r"factorybox-devx-inputs-[0-9a-f]{32}", name):
        raise ValueError("requires one exact successful developer-input artifact")
    archive = ROOT / "artifacts" / name
    result = json.loads((archive / "result.json").read_text())
    if not result.get("passed") or not result.get("cleanup"):
        raise ValueError("developer inputs did not pass preparation and cleanup")
    if set(result["selected_images"]) != set(ROLES) or set(result["images"]) != set(ROLES):
        raise ValueError("developer inputs do not contain the exact consumer roles")
    for role, image in result["selected_images"].items():
        canonical_digest(image)
        saved = result["images"][role]
        if (saved["image"] != image or saved["architecture"] != "arm64" or saved["os"] != "linux" or
                not re.fullmatch(r"sha256:[0-9a-f]{64}", saved["id"])):
            raise ValueError("recorded consumer image identity or platform differs")
        if not rootfs:
            current = inspect_image(image)
            if current["id"] != saved["id"]:
                raise ValueError("consumer image changed since its preparation")
    if rootfs:
        # This consumer executes the sealed export, not an image in Docker's
        # mutable cache. Require the actual archive, even after image eviction.
        payload = archive / "runtime-rootfs.tar"
        if (payload.is_symlink() or not payload.is_file() or
                payload.stat().st_size != result["rootfs_tar_bytes"] or
                file_sha(payload) != result["rootfs_tar_sha256"]):
            raise ValueError("prepared runtime rootfs archive changed")
    return archive, result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--factory-artifacts", type=pathlib.Path, required=True,
                        help="Read-only JSON containing Factory's published role digest references")
    parser.add_argument("--pull-missing", action="store_true",
                        help="Allow acquiring exactly those pinned references; never choose another image")
    options = parser.parse_args()
    source_input = options.factory_artifacts.read_bytes()
    supplied = json.loads(source_input)
    selected = {role: supplied[role] for role in ROLES}
    if any(not isinstance(image, str) or not PIN.fullmatch(image) for image in selected.values()):
        raise ValueError("selected consumer inputs require exact credential-free image digests")
    owner = uuid.uuid4().hex
    name = "factorybox-devx-inputs-" + owner
    archive = reference.new_artifact(name)
    archive.mkdir()
    shutil.copyfile(ROOT / "tests/devx-versions.sh", archive / "versions.sh")
    result = {
        "schema": 1, "owner": owner, "started_at": datetime.datetime.now(datetime.timezone.utc).isoformat(),
        "passed": False, "cleanup": False, "production_qualified": False,
        "source_artifacts_sha256": hashlib.sha256(source_input).hexdigest(),
        "selected_images": selected, "versions_script_sha256": reference.sha(archive / "versions.sh"),
        "scope": "Exact existing Factory consumer artifacts and runtime rootfs only. "
                 "No Factory installation, authority, source tree or runtime intent is changed. "
                 "Version inspection and image export are not complete developer lifecycle qualification.",
    }
    attempted = []
    failure = None
    try:
        result["images"] = {}
        for role, image in selected.items():
            try:
                inspected = inspect_image(image)
            except subprocess.CalledProcessError:
                if not options.pull_missing:
                    raise
                with (archive / (role + "-pull.log")).open("x") as log:
                    subprocess.run([*reference.DOCKER, "pull", "--platform=linux/arm64", image],
                                   stdout=log, stderr=subprocess.STDOUT, check=True, timeout=180)
                inspected = inspect_image(image)
            result["images"][role] = inspected
        reference_lifecycle.write_json(archive / "intent.json", result)
        print(json.dumps({"preparing": str(archive)}), flush=True)
        runtime = selected["build-runtime-host"]
        volume_paths = list(result["images"]["build-runtime-host"]["volumes"])
        if len(volume_paths) > 8:
            raise ValueError("unexpected consumer volume count")
        for path in volume_paths:
            if (not path.startswith("/") or str(pathlib.PurePosixPath(path)) != path or
                    ".." in pathlib.PurePosixPath(path).parts or path == "/" or
                    any(c in path for c in [",", "\n", "\r"])):
                raise ValueError("unexpected consumer volume path")
        version_name = name + "-versions"
        command = [
            *reference.DOCKER, "run", "--pull=never", "--name", version_name,
            "--label", "com.factorybox.run=" + owner, "--network=none", "--read-only",
            "--cpus=1", "--memory=512m", "--pids-limit=64", "--cap-drop=ALL",
            "--security-opt=no-new-privileges",
            "--mount", f"type=bind,source={archive / 'versions.sh'},target=/versions.sh,readonly",
            "--entrypoint", "/bin/sh", runtime, "/versions.sh",
        ]
        position = command.index("--entrypoint")
        for path in volume_paths:
            command[position:position] = ["--mount", "type=tmpfs,target=" + path]
            position += 2
        result["version_command"] = command
        attempted.append(version_name)
        with (archive / "versions.txt").open("x") as output, (archive / "versions.stderr").open("x") as errors:
            done = subprocess.run(command, stdout=output, stderr=errors, timeout=40)
        result["version_exit_code"] = done.returncode
        if done.returncode:
            raise ValueError("actual Factory consumer version/Connector inspection failed")
        version_state = json.loads(reference.docker(["container", "inspect", version_name]))[0]["State"]
        if version_state["OOMKilled"]:
            raise ValueError("consumer version fixture exceeded its memory bound")
        result["versions"] = (archive / "versions.txt").read_text()
        export_name = name + "-export"
        command = [
            *reference.DOCKER, "create", "--pull=never", "--name", export_name,
            "--label", "com.factorybox.run=" + owner, "--network=none", "--read-only",
            "--cpus=1", "--memory=64m", "--pids-limit=16", "--cap-drop=ALL",
            "--security-opt=no-new-privileges", "--entrypoint", "/bin/true",
        ]
        # Explicit tmpfs mounts suppress automatic anonymous volumes. The
        # container is never started; export retains the image's own rootfs.
        for path in volume_paths:
            command += ["--mount", "type=tmpfs,target=" + path]
        command.append(runtime)
        result["create_export_command"] = command
        attempted.append(export_name)
        reference.docker(command[len(reference.DOCKER):], timeout=30)
        container = json.loads(reference.docker(["container", "inspect", export_name]))[0]
        if (container["State"]["Running"] or container["State"]["Pid"] != 0 or
                any(m["Type"] == "volume" for m in container.get("Mounts", []))):
            raise ValueError("rootfs export unexpectedly started a process or created an anonymous volume")
        rootfs = archive / "runtime-rootfs.tar"
        with (archive / "export.log").open("x") as log:
            subprocess.run([*reference.DOCKER, "container", "export", "--output", str(rootfs), export_name],
                           stdout=log, stderr=subprocess.STDOUT, check=True, timeout=120)
        result["rootfs_tar_sha256"] = file_sha(rootfs)
        result["rootfs_tar_bytes"] = rootfs.stat().st_size
        result["passed"] = True
    except Exception as error:
        failure = error
        result["error"] = str(error)
    finally:
        try:
            for container in reversed(attempted):
                reference.owned_delete("container", container, owner)
            result["cleanup"] = True
        except Exception as error:
            result["cleanup_error"] = str(error)
            failure = failure or error
        result["passed"] = result["passed"] and result["cleanup"]
        reference_lifecycle.write_json(archive / "result.json", result)
        print(json.dumps({"evidence": str(archive), **result}), flush=True)
    if failure:
        raise RuntimeError("developer consumer input preparation failed: " + str(archive)) from failure


if __name__ == "__main__":
    main()
