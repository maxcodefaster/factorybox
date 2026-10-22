#!/usr/bin/env python3
"""Archive pinned upstream images through an owned, isolated Docker daemon."""
import argparse
import datetime
import hashlib
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
    parser.add_argument("--candidate-platform", choices=["linux/amd64"])
    parser.add_argument("--network", choices=["default", "host"], default="default")
    parser.add_argument("--bounded-build", action="store_true")
    parser.add_argument("--acquisition-network", choices=["bridge", "host"], default="bridge",
                        help="Image acquisition only; host avoids requiring global IP forwarding")
    parser.add_argument("--from-worker-cache", action="store_true",
                        help="Save exact already cached digest images for offline acquisition")
    options = parser.parse_args()
    target = options.candidate_platform or "linux/arm64"
    host = json.loads(reference.docker(["info", "--format", "{{json .}}" ]))
    native_base.require_native_host(target, host)
    image, base = native_base.load(options.native_base_build, candidate_platform=options.candidate_platform)
    owner = uuid.uuid4().hex
    name = "factorybox-bats-images-" + owner
    archive = reference.new_artifact(name)
    output, context = archive / "output", archive / "context"
    output.mkdir(parents=True)
    context.mkdir()
    shutil.copyfile(ROOT / "build/upstream-bats.Dockerfile", context / "Dockerfile")
    shutil.copyfile(ROOT / "tests/upstream-bats-images.sh", archive / "acquire.sh")
    shutil.copyfile(upstream_bats.image_pins(target), archive / "pins.json")
    result = {
        "schema": 1, "owner": owner, "container_name": name, "volume_name": name,
        "started_at": datetime.datetime.now(datetime.timezone.utc).isoformat(),
        "native_base_build": options.native_base_build, "base": image,
        "target_platform": target, "native_lifecycle_qualified": False,
        "acquisition_network": "none" if options.from_worker_cache else options.acquisition_network,
        "base_packages_sha256": base["packages_sha256"],
        "inputs_sha256": {p: reference.sha(archive / p)
                          for p in ["context/Dockerfile", "acquire.sh", "pins.json"]},
        "passed": False, "cleanup": False, "production_qualified": False,
        "scope": "Public pinned native upstream test images, acquired and aliased only "
                 "inside a new owned Docker daemon. No worker Docker socket or worker image tag writes.",
    }
    attempted = False
    volume_attempted = False
    failure = None
    try:
        if options.from_worker_cache:
            pins = json.loads((archive / "pins.json").read_text())
            identities = []
            for row in pins["images"]:
                facts = json.loads(reference.docker(["image", "inspect", row["image"]]))[0]
                if (row["image"] not in facts.get("RepoDigests", []) or
                        facts["Architecture"] != target.split("/")[1] or facts["Os"] != "linux"):
                    raise ValueError("cached upstream image digest/platform differs")
                manifest = reference.docker(["buildx", "imagetools", "inspect", "--raw", row["image"]])
                if "sha256:" + hashlib.sha256(manifest.encode()).hexdigest() != row["image"].split("@")[1]:
                    raise ValueError("cached upstream manifest differs from its exact digest")
                identities.append({"image": row["image"], "manifest": manifest, "facts": facts})
            reference_lifecycle.write_json(archive / "cached-images.json", identities)
            result["cached_identities_sha256"] = reference.sha(archive / "cached-images.json")
            save = [*reference.DOCKER, "image", "save", *[r["image"] for r in pins["images"]]]
            with (archive / "cached-images.tar").open("xb") as payload, \
                    (archive / "cache-save.log").open("x") as log:
                subprocess.run(save, stdout=payload, stderr=log, check=True, timeout=120)
            result["cached_images_sha256"] = reference.sha(archive / "cached-images.tar")
            result["cached_image_command"] = save
        fixture = upstream_bats.build_fixture(archive, name, image, platform=target,
                                             network=options.network, bounded_build=options.bounded_build)
        result["fixture_image"] = fixture
        command = [
            *reference.DOCKER, "run", "--pull=never", "--platform=" + target, "--name", name,
            "--label", "com.factorybox.run=" + owner, "--privileged", "--cgroupns=private",
            "--network=" + result["acquisition_network"],
            "--cpus=3", "--memory=3g", "--pids-limit=256", "--env", "FACTORYBOX_BATS_IMAGES=1",
            "--env", "FACTORYBOX_NATIVE_PLATFORM=" + target,
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
        if options.from_worker_cache:
            location = command.index("--entrypoint")
            command[location:location] = [
                "--env", "FACTORYBOX_BATS_IMAGE_CACHE=1",
                "--mount", f"type=bind,source={archive / 'cached-images.tar'},target=/cached-images.tar,readonly",
                "--mount", f"type=bind,source={archive / 'cached-images.json'},target=/cached-images.json,readonly"]
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
        if options.from_worker_cache:
            result["inputs_unchanged"] = result["inputs_unchanged"] and (
                reference.sha(archive / "cached-images.tar") == result["cached_images_sha256"] and
                reference.sha(archive / "cached-images.json") == result["cached_identities_sha256"])
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
