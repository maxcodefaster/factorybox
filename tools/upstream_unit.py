#!/usr/bin/env python3
"""Run complete upstream Go unit suites inside an owned Linux container."""
import argparse
import datetime
import json
import pathlib
import re
import shutil
import subprocess
import uuid
import urllib.request

import reference
import reference_lifecycle
import build_config
import native_base

ROOT = reference.ROOT


def compose_offline_unit_dockerfile(prefix, template):
    header = b"ARG BUILDER_IMAGE\nFROM ${BUILDER_IMAGE}\n"
    if (template.count(header) != 1 or template.count(b"FROM ${BUILDER_IMAGE}\n") != 1 or
            prefix.count(b" AS build\n") != 1 or b" AS offline\n" in prefix):
        raise ValueError("offline unit recipe must retain the exact in-file build stage")
    return prefix + b"\n" + template.replace(header, b"FROM build AS offline\n", 1)


def offline_builder_command(options, dockerfile, context, name, archive):
    return [*reference.DOCKER, "build", "--provenance=false", "--progress=plain",
            *build_config.arguments(options), "--target", "offline", "--load",
            "--tag", name + "-offline", "--iidfile", str(archive / "builder-id.txt"),
            "--metadata-file", str(archive / "builder-metadata.json"),
            "-f", str(dockerfile), str(context)]


def context_manifest(directory):
    entries = {}
    for path in directory.rglob("*"):
        name = str(path.relative_to(directory))
        if path.is_symlink():
            entries[name] = {"symlink": str(path.readlink())}
        elif path.is_file():
            entries[name] = {"sha256": reference.sha(path)}
    return entries


def observations(output):
    suites = {}
    for path in sorted(output.glob("*.exit-code")):
        name = path.name.removesuffix(".exit-code")
        records = []
        for line in (output / (name + ".jsonl")).read_text().splitlines():
            try:
                records.append(json.loads(line))
            except json.JSONDecodeError:
                continue
        failed = [r.get("Test", r.get("Package")) for r in records if r.get("Action") == "fail"]
        suites[name] = {
            "exit_code": int(path.read_text()),
            "passed_tests": sum(r.get("Action") == "pass" and "Test" in r for r in records),
            "passed_top_level_tests": sum(r.get("Action") == "pass" and "Test" in r and
                                         "/" not in r["Test"] for r in records),
            "passed_subtests": sum(r.get("Action") == "pass" and "/" in r.get("Test", "")
                                  for r in records),
            "skipped_tests": [r["Test"] for r in records
                              if r.get("Action") == "skip" and "Test" in r],
            "failed": failed,
        }
    return suites


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--sysbox-build", required=True)
    parser.add_argument("--suite", choices=["all", "runc", "libcontainer", "fs", "ipc", "mgr", "idmap",
                                            "runc-integration"], default="all")
    parser.add_argument("--test", help="One exact upstream integration test name")
    parser.add_argument("--candidate-platform", choices=["linux/amd64"],
                        help="Run native upstream tests on an explicitly unqualified platform")
    parser.add_argument("--network", choices=["default", "host"], default="default",
                        help="Builder dependency network only")
    parser.add_argument("--bounded-build", action="store_true",
                        help="Require a verified 3 CPU / 3 GiB builder")
    parser.add_argument("--builder", help="Explicit existing Buildx builder")
    options = parser.parse_args()
    builder = build_config.verify_builder(options, reference.DOCKER)
    if options.test and (options.suite != "runc-integration" or
                         not re.fullmatch(r"Test[A-Za-z0-9_]+", options.test)):
        parser.error("--test requires --suite runc-integration and one exact Test name")
    build, build_result = reference_lifecycle.component_build(
        options.sysbox_build, "factorybox-build-sysbox-")
    target = options.candidate_platform or "linux/arm64"
    if build_result.get("platform", "linux/arm64") != target:
        raise ValueError("Sysbox build differs from the explicit native target")
    host = json.loads(reference.docker(["info", "--format", "{{json .}}"]))
    native_base.require_native_host(target, host)
    options.platform = target
    owner = uuid.uuid4().hex
    name = "factorybox-upstream-" + owner
    archive = reference.new_artifact(name)
    output = archive / "output"
    output.mkdir(parents=True)
    result = {"schema": 1, "owner": owner, "container_name": name,
              "started_at": datetime.datetime.now(datetime.timezone.utc).isoformat(),
              "passed": False, "cleanup": False, "production_qualified": False,
              "platform": target, "candidate_platform": bool(options.candidate_platform),
              "native_lifecycle_qualified": False,
              "builder": builder,
              "host": {key: host[key] for key in ["OSType", "Architecture", "KernelVersion"]},
              "suite": options.suite,
              "test": options.test,
              "scope": ("Pinned libcontainer unit package only; executes the independently compiled binary."
                        if options.suite == "libcontainer" else
                        f"One unchanged upstream integration test: {options.test}."
                        if options.test else
                        "Complete pinned libcontainer integration test package."
                        if options.suite == "runc-integration" else
                        f"Complete pinned {options.suite} Go module unit suite only."
                        if options.suite != "all" else
                        "Complete pinned runc/fs/mgr/ipc/libs Go unit suites. "
                        "Sysbox umbrella/Bats/systemd and CRIU integration gates are separate.")}
    attempted = False
    volumes_attempted = []
    failure = None
    try:
        dockerfile = ROOT / "build/sysbox.Dockerfile"
        if reference.sha(dockerfile) != build_result["dockerfile_sha256"]:
            raise ValueError("builder recipe changed since its component build")
        expected = json.loads((build / "context-manifest.json").read_text())
        if context_manifest(build / "context") != expected:
            raise ValueError("verified build context changed")
        result["component_build"] = options.sysbox_build
        result["source_lock_sha256"] = build_result["source_lock_sha256"]
        result["context_manifest_sha256"] = reference.sha(build / "context-manifest.json")
        for script in ["upstream-unit.sh", "private-cgroup-delegation.sh"]:
            shutil.copyfile(ROOT / "tests" / script, archive / script)
        result["test_script_sha256"] = reference.sha(archive / "upstream-unit.sh")
        result["delegation_setup_sha256"] = reference.sha(archive / "private-cgroup-delegation.sh")
        busybox = json.loads((ROOT / "upstream/test-inputs.json").read_text())[
            "busybox-" + build_config.architecture(target)]
        rootfs = ROOT / ".cache/downloads" / busybox["archive"]
        if not rootfs.exists():
            with urllib.request.urlopen(busybox["url"], timeout=30) as response:
                rootfs.write_bytes(response.read())
        if reference.sha(rootfs) != busybox["sha256"]:
            raise ValueError("upstream Busybox test rootfs differs")
        result["busybox_rootfs_sha256"] = busybox["sha256"]
        # Keep the original component recipe byte-for-byte as a prefix. The
        # dependency stage uses FROM build instead of a daemon-only image name
        # that an isolated docker-container builder cannot resolve.
        dependency_recipe = ROOT / "build/upstream-unit.Dockerfile"
        shutil.copyfile(dependency_recipe, archive / "upstream-unit.Dockerfile")
        result["dependency_recipe_sha256"] = reference.sha(archive / "upstream-unit.Dockerfile")
        effective = archive / "offline.Dockerfile"
        effective.write_bytes(compose_offline_unit_dockerfile(
            dockerfile.read_bytes(), (archive / "upstream-unit.Dockerfile").read_bytes()))
        result["effective_dockerfile_sha256"] = reference.sha(effective)
        command = offline_builder_command(options, effective, build / "context", name, archive)
        result["builder_command"] = command
        with (archive / "builder.log").open("x") as log:
            # A cold bounded builder must acquire the pinned snapshot packages
            # before compiling. This limit is separate from native lifecycle
            # execution and must allow the observed slow snapshot downloads.
            subprocess.run(command, stdout=log, stderr=subprocess.STDOUT, check=True, timeout=900)
        result["builder_image_config"] = (archive / "builder-id.txt").read_text().strip()
        metadata = json.loads((archive / "builder-metadata.json").read_text())
        image = name + "-offline@" + metadata["containerimage.digest"]
        result["builder_image_manifest"] = image
        inspected = json.loads(reference.docker(["image", "inspect", image]))[0]
        native_base.verify_image({"image": image, "image_config": result["builder_image_config"],
                                  "platform": target}, inspected)
        result["offline_image"] = image
        command = [*reference.DOCKER, "run", "--pull=never", "--platform=" + target, "--name", name,
                   "--label", "com.factorybox.run=" + owner, "--privileged",
                   "--network=none",
                   "--cgroupns=private", "--cpus=2", "--memory=2g", "--pids-limit=512",
                   "--env", "FACTORYBOX_UPSTREAM_CONTAINER=1",
                   "--env", "FACTORYBOX_UPSTREAM_SUITE=" + options.suite,
                   "--env", "FACTORYBOX_UPSTREAM_TEST=" + (options.test or ""),
                   "--env", "GOMAXPROCS=2", "--env", "GOFLAGS=-p=2",
                   "--env", "GOPROXY=off", "--env", "GOTOOLCHAIN=local",
                   "--mount", f"type=bind,source={build / 'context'},target=/input,readonly",
                   "--mount", f"type=bind,source={output},target=/output",
                   "--mount", f"type=bind,source={archive / 'upstream-unit.sh'},target=/run-tests.sh,readonly",
                   "--mount", f"type=bind,source={archive / 'private-cgroup-delegation.sh'},target=/delegate.sh,readonly",
                   "--mount", f"type=bind,source={rootfs},target=/busybox-rootfs.tar.gz,readonly",
                   "--entrypoint", "/bin/bash", image, "/run-tests.sh"]
        if build_result.get("shm_startup_reexec") and options.suite == "all":
            command[command.index("--entrypoint"):command.index("--entrypoint")] = [
                "--env", "FACTORYBOX_NATIVE_SHM_PROBE=1"]
            result["shm_reexec_native_tests_requested"] = True
        if options.suite in ["all", "mgr"]:
            command[command.index("--entrypoint"):command.index("--entrypoint")] = [
                "--mount", f"type=volume,source={name},target=/var/lib/docker"]
            result["private_docker_volume"] = name
        if options.suite in ["all", "runc", "mgr", "idmap", "runc-integration"]:
            scratch_name = name + "-scratch"
            result["private_sysbox_volume"] = scratch_name
            command[command.index("--entrypoint"):command.index("--entrypoint")] = [
                "--mount", f"type=volume,source={scratch_name},target=/var/lib/sysbox"]
        if options.suite == "all":
            alpine = json.loads((ROOT / "upstream/test-inputs.json").read_text())[
                "alpine" if target == "linux/arm64" else "alpine-amd64"]["image"]
            result["private_docker_alpine"] = alpine
            reference.docker(["pull", "--platform=" + target, alpine], timeout=120)
            inspected = json.loads(reference.docker(["image", "inspect", alpine]))[0]
            if (alpine.split("@", 1)[1] not in [digest.split("@", 1)[1]
                    for digest in inspected.get("RepoDigests", [])] or
                    inspected["Os"] != "linux" or
                    inspected["Architecture"] != build_config.architecture(target)):
                raise ValueError("private Docker Alpine image identity/platform differs")
            alias = "factorybox-upstream-alpine:" + owner
            reference.docker(["image", "tag", alpine, alias])
            try:
                with (archive / "alpine.tar").open("xb") as stream:
                    subprocess.run([*reference.DOCKER, "image", "save", alias],
                                   stdout=stream, check=True, timeout=60)
            finally:
                tagged = json.loads(reference.docker(["image", "inspect", alias]))[0]
                if tagged["Id"] != inspected["Id"]:
                    raise ValueError("private Docker transport alias identity changed")
                reference.docker(["image", "rm", alias])
            result["private_docker_alpine_archive_sha256"] = reference.sha(archive / "alpine.tar")
            # Docker save/load retains repository tags, not registry digests.
            # The immutable archive above transports the verified digest's bytes.
            result["private_docker_alpine_loaded_tag"] = alias
            command[command.index("--entrypoint"):command.index("--entrypoint")] = [
                "--env", "FACTORYBOX_TEST_ALPINE_IMAGE=" + result["private_docker_alpine_loaded_tag"],
                "--mount", f"type=bind,source={archive / 'alpine.tar'},target=/alpine.tar,readonly"]
        result["command"] = command
        reference_lifecycle.write_json(archive / "intent.json", result)
        print(json.dumps({"running": str(archive)}), flush=True)
        if options.suite in ["all", "mgr"]:
            volumes_attempted.append(name)
            reference.docker(["volume", "create", "--label", "com.factorybox.run=" + owner, name])
        if options.suite in ["all", "runc", "mgr", "idmap", "runc-integration"]:
            volumes_attempted.append(scratch_name)
            reference.docker(["volume", "create", "--label", "com.factorybox.run=" + owner, scratch_name])
        attempted = True
        with (archive / "stdout.txt").open("x") as log, (archive / "stderr.txt").open("x") as errors:
            done = subprocess.run(command, stdout=log, stderr=errors, timeout=900)
        result["exit_code"] = done.returncode
        result["suites"] = observations(output)
        if done.returncode == 125:
            raise ValueError("upstream test container did not start: " +
                             (archive / "stderr.txt").read_text()[-4096:])
        required = ({"libs-idMap"} if options.suite == "idmap" else
                    {options.suite} if options.suite != "all" else
                    {"runc", "fs", "mgr", "ipc"} | {
                        "libs-" + p.parent.name
                        for p in (build / "context/sysbox-libs").glob("*/go.mod")})
        state = json.loads(reference.docker(["container", "inspect", name]))[0]["State"]
        result["container_state"] = state
        result["passed"] = (done.returncode == 0 and not state["OOMKilled"] and
                            required == result["suites"].keys() and
                            all(v["exit_code"] == 0 and not v["failed"]
                                for v in result["suites"].values()))
        if result.get("shm_reexec_native_tests_requested"):
            records = [json.loads(line) for line in (output / "libs-linuxUtils.jsonl").read_text().splitlines()
                       if line.startswith("{")]
            required_shm = {"TestFactoryboxShmProbeNative", "TestShmProbeRefusesUnmappedProcess",
                            "TestShmProbeRefusesSharedNamespaces", "TestShmProbeRefusesSharedNamespaces/ipc",
                            "TestShmProbeRefusesSharedNamespaces/mount"}
            passed_shm = {row.get("Test") for row in records if row.get("Action") == "pass"}
            result["shm_reexec_native_tests_passed"] = required_shm <= passed_shm
            result["passed"] = result["passed"] and result["shm_reexec_native_tests_passed"]
        if options.test:
            records = [json.loads(line) for line in
                       (output / "runc-integration.jsonl").read_text().splitlines()
                       if line.startswith("{")]
            result["selected_test_passed"] = any(
                r.get("Action") == "pass" and r.get("Test") == options.test for r in records)
            result["passed"] = result["passed"] and result["selected_test_passed"]
        result["inputs_unchanged"] = (
            reference.sha(archive / "upstream-unit.sh") == result["test_script_sha256"] and
            reference.sha(archive / "private-cgroup-delegation.sh") == result["delegation_setup_sha256"] and
            reference.sha(effective) == result["effective_dockerfile_sha256"] and
            reference.sha(archive / "upstream-unit.Dockerfile") == result["dependency_recipe_sha256"])
        result["passed"] = result["passed"] and result["inputs_unchanged"]
        if not result["passed"]:
            raise ValueError("upstream unit suites failed or did not all finish")
    except Exception as error:
        failure = error
        result["error"] = str(error)
        result["suites"] = observations(output)
    finally:
        try:
            if attempted:
                reference.owned_delete("container", name, owner)
            for volume_name in volumes_attempted:
                reference.owned_delete("volume", volume_name, owner)
            result["cleanup"] = True
        except Exception as error:
            result["cleanup_error"] = str(error)
            failure = failure or error
        result["passed"] = bool(result["passed"] and result["cleanup"] and failure is None)
        reference_lifecycle.write_json(archive / "result.json", result)
        print(json.dumps({"evidence": str(archive), **result}), flush=True)
    if failure:
        raise RuntimeError("upstream qualification failed: " + str(archive)) from failure


if __name__ == "__main__":
    main()
