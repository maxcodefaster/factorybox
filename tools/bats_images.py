#!/usr/bin/env python3
"""Archive pinned upstream images through an owned, isolated Docker daemon."""
import argparse
import datetime
import json
import shutil
import subprocess
import uuid

import native_base
import reference
import reference_lifecycle
import upstream_bats

ROOT = reference.ROOT


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--native-base-build", required=True)
    options = parser.parse_args()
    image, base = native_base.load(options.native_base_build)
    owner = uuid.uuid4().hex
    name = "factorybox-bats-images-" + owner
    archive = reference.new_artifact(name)
    output, context = archive / "output", archive / "context"
    output.mkdir(parents=True)
    context.mkdir()
    shutil.copyfile(ROOT / "build/upstream-bats.Dockerfile", context / "Dockerfile")
    shutil.copyfile(ROOT / "tests/upstream-bats-images.sh", archive / "acquire.sh")
    shutil.copyfile(ROOT / "upstream/bats-images.json", archive / "pins.json")
    result = {
        "schema": 1, "owner": owner, "container_name": name, "volume_name": name,
        "started_at": datetime.datetime.now(datetime.timezone.utc).isoformat(),
        "native_base_build": options.native_base_build, "base": image,
        "base_packages_sha256": base["packages_sha256"],
        "inputs_sha256": {p: reference.sha(archive / p)
                          for p in ["context/Dockerfile", "acquire.sh", "pins.json"]},
        "passed": False, "cleanup": False, "production_qualified": False,
        "scope": "Public pinned ARM64 upstream test images, acquired and aliased only "
                 "inside a new owned Docker daemon. No worker Docker socket or worker image tag writes.",
    }
    attempted = False
    volume_attempted = False
    failure = None
    try:
        fixture = upstream_bats.build_fixture(archive, name, image)
        result["fixture_image"] = fixture
        command = [
            *reference.DOCKER, "run", "--pull=never", "--name", name,
            "--label", "com.factorybox.run=" + owner, "--privileged", "--cgroupns=private",
            "--cpus=3", "--memory=3g", "--pids-limit=256", "--env", "FACTORYBOX_BATS_IMAGES=1",
            "--mount", f"type=volume,source={name},target=/var/lib/docker",
            "--mount", f"type=bind,source={output},target=/output",
            "--mount", f"type=bind,source={archive / 'pins.json'},target=/pins.json,readonly",
            "--mount", f"type=bind,source={archive / 'acquire.sh'},target=/acquire.sh,readonly",
            "--entrypoint", "/bin/bash", fixture, "-ec",
            "mount --make-rprivate /\n"
            "mount --bind /proc/sys /proc/sys\n"
            "mount -o remount,bind,ro /proc/sys\n"
            "exec setpriv --bounding-set=-sys_module,-sys_rawio,-sys_boot,-sys_time,-syslog "
            "tini -- timeout --signal=TERM --kill-after=5s 300s /bin/bash /acquire.sh",
        ]
        result["command"] = command
        reference_lifecycle.write_json(archive / "intent.json", result)
        print(json.dumps({"acquiring": str(archive)}), flush=True)
        volume_attempted = True
        reference.docker(["volume", "create", "--label", "com.factorybox.run=" + owner, name])
        attempted = True
        with (archive / "stdout.txt").open("x") as log, (archive / "stderr.txt").open("x") as errors:
            done = subprocess.run(command, stdout=log, stderr=errors, timeout=330)
        result["exit_code"] = done.returncode
        result["container_state"] = json.loads(reference.docker(["container", "inspect", name]))[0]["State"]
        result["inputs_unchanged"] = all(
            reference.sha(archive / path) == expected for path, expected in result["inputs_sha256"].items())
        if done.returncode or result["container_state"]["OOMKilled"] or not result["inputs_unchanged"]:
            raise ValueError("private upstream image acquisition failed")
        observed = json.loads((output / "images.json").read_text())
        expected = json.loads((archive / "pins.json").read_text())["images"]
        if [{"alias": r["alias"], "image": r["image"]} for r in observed] != expected:
            raise ValueError("acquired image identities differ from their pinned inputs")
        result["images"] = observed
        result["images_json_sha256"] = reference.sha(output / "images.json")
        result["images_tar_sha256"] = upstream_bats.file_sha(output / "images.tar")
        result["passed"] = True
    except Exception as error:
        failure = error
        result["error"] = str(error)
    finally:
        preserve = False
        if attempted and failure:
            try:
                upstream_bats.archive_failure(archive, name, owner, [name], fixture, result)
            except Exception as error:
                preserve = True
                result["archive_error"] = str(error)
                result["retained_volumes"] = [name]
                failure = failure or error
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
    if failure:
        raise RuntimeError("private Bats image archive failed: " + str(archive)) from failure


if __name__ == "__main__":
    main()
