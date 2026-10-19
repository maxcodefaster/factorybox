#!/usr/bin/env python3
"""Run pinned Sysbox Bats assertions in an owned private Linux fixture."""
import argparse
import datetime
import hashlib
import json
import pathlib
import re
import shutil
import subprocess
import tarfile
import uuid

import native_base
import reference
import reference_lifecycle
import sources

ROOT = reference.ROOT
SUITES = {
    "lifecycle": ("tests/lifecycle/basic.bats", 7),
    "systemd": ("tests/apps/l1/systemd.bats", 6),
}
SYSTEMD_SKIPS = {"systemd ubuntu bionic", "systemd archlinux"}


def file_sha(path):
    with path.open("rb") as file:
        return hashlib.file_digest(file, "sha256").hexdigest()


def build_fixture(archive, name, base):
    command = [
        *reference.DOCKER, "build", "--provenance=false", "--progress=plain",
        "--platform=linux/arm64", "--tag", name,
        "--build-arg", "NATIVE_BASE_IMAGE=" + base,
        "--output", "type=image,compression=uncompressed",
        "--metadata-file", str(archive / "builder.json"), str(archive / "context"),
    ]
    with (archive / "build.log").open("x") as log:
        subprocess.run(command, stdout=log, stderr=subprocess.STDOUT, check=True, timeout=300)
    metadata = json.loads((archive / "builder.json").read_text())
    return name + "@" + metadata["containerimage.digest"]


def image_archive(name, native_build):
    if not name or not re.fullmatch(r"factorybox-bats-images-[0-9a-f]{32}", name):
        raise ValueError("systemd requires one exact owned upstream image archive")
    archive = ROOT / "artifacts" / name
    result = json.loads((archive / "result.json").read_text())
    pins = ROOT / "upstream/bats-images.json"
    if (not result.get("passed") or not result.get("cleanup") or
            result["native_base_build"] != native_build or
            result["inputs_sha256"]["pins.json"] != reference.sha(pins) or
            result["inputs_sha256"]["context/Dockerfile"] != reference.sha(ROOT / "build/upstream-bats.Dockerfile") or
            reference.sha(archive / "output/images.json") != result["images_json_sha256"] or
            file_sha(archive / "output/images.tar") != result["images_tar_sha256"]):
        raise ValueError("upstream image archive identity, inputs or contents changed")
    rows = json.loads((archive / "output/images.json").read_text())
    if (rows != result["images"] or
            [{"alias": r["alias"], "image": r["image"]} for r in rows] != json.loads(pins.read_text())["images"] or
            any(not re.fullmatch(r"sha256:[0-9a-f]{64}", r["id"]) for r in rows)):
        raise ValueError("upstream image archive does not name its exact expected images")
    return archive / "output", result


def observations(text, expected):
    plans = re.findall(r"^1\.\.(\d+)\s*$", text, re.MULTILINE)
    rows = []
    for match in re.finditer(r"^(not ok|ok)\s+(\d+)\s+(.+)$", text, re.MULTILINE):
        status, number, title = match.groups()
        name, marker, reason = title.partition(" # skip")
        if not marker:
            name, marker, reason = title.partition(" # SKIP")
        rows.append({"number": int(number), "name": name.removeprefix("- ").strip(),
                     "passed": status == "ok", "skipped": bool(marker),
                     "skip_reason": reason.strip()})
    complete = (plans == [str(len(expected))] and len(rows) == len(expected) and
                [r["number"] for r in rows] == list(range(1, len(expected) + 1)) and
                [r["name"] for r in rows] == expected)
    return {"complete": complete, "cases": rows,
            "executed_passes": sum(r["passed"] and not r["skipped"] for r in rows),
            "failed": [r["name"] for r in rows if not r["passed"]],
            "skipped": [r["name"] for r in rows if r["skipped"]]}


def archive_failure(archive, name, owner, volumes, image, result):
    state = json.loads(reference.docker(["container", "inspect", name]))[0]
    if state["Config"].get("Labels", {}).get("com.factorybox.run") != owner:
        raise ValueError("failed Bats fixture ownership changed")
    if state["State"]["Running"]:
        reference.docker(["container", "stop", "--time=2", name])
        state = json.loads(reference.docker(["container", "inspect", name]))[0]
    if state["State"]["Running"] or state["State"]["Pid"] != 0:
        raise ValueError("failed fixture writers have not stopped")
    result["failed_volume_archives"] = {}
    for index, volume in enumerate(volumes):
        saved = json.loads(reference.docker(["volume", "inspect", volume]))[0]
        if saved.get("Labels", {}).get("com.factorybox.run") != owner:
            raise ValueError("failed fixture volume ownership changed")
        helper = name + "-archive-" + str(index)
        filename = "failed-volume-%d.tar" % index
        command = [
            *reference.DOCKER, "run", "--pull=never", "--name", helper,
            "--label", "com.factorybox.run=" + owner, "--network=none",
            "--cpus=1", "--memory=256m", "--pids-limit=16", "--read-only",
            "--cap-drop=ALL", "--cap-add=DAC_READ_SEARCH", "--security-opt=no-new-privileges",
            "--mount", f"type=volume,source={volume},target=/input,readonly",
            "--mount", f"type=bind,source={archive},target=/output",
            "--entrypoint", "/bin/tar", image, "--numeric-owner", "--sparse",
            "-C", "/input", "-cpf", "/output/" + filename, ".",
        ]
        try:
            with (archive / (filename + ".log")).open("x") as log:
                subprocess.run(command, stdout=log, stderr=subprocess.STDOUT, check=True, timeout=60)
            result["failed_volume_archives"][volume] = {
                "file": filename, "sha256": file_sha(archive / filename)}
        finally:
            reference.owned_delete("container", helper, owner)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--native-base-build", required=True)
    parser.add_argument("--sysbox-build", required=True)
    parser.add_argument("--suite", choices=SUITES, default="lifecycle")
    parser.add_argument("--test", help="One exact original Bats test title for diagnosis")
    parser.add_argument("--images", help="Exact successful owned upstream image archive for systemd")
    options = parser.parse_args()
    if options.suite != "systemd" and options.images:
        parser.error("--images is only used by the systemd suite")
    saved_images, acquisition = (image_archive(options.images, options.native_base_build)
                                 if options.suite == "systemd" else (None, None))
    image, base = native_base.load(options.native_base_build)
    component_build, components = reference_lifecycle.component_build(
        options.sysbox_build, "factorybox-build-sysbox-")
    for name, expected in components["binary_sha256"].items():
        if reference.sha(component_build / "output" / name) != expected:
            raise ValueError("Sysbox component executable changed")
    lock = ROOT / "upstream/lock.json"
    component = json.loads(lock.read_text())["components"]["sysbox-tests"]
    tree = ROOT / ".cache/sources/sysbox-tests"
    sources.validate_inputs(component)
    unchanged = sources.check_source(tree, ROOT / ".cache/downloads" / component["archive"], component)
    selected, count = SUITES[options.suite]
    titles = re.findall(r'^@test "([^"]+)" \{', (tree / selected).read_text(), re.MULTILINE)
    if len(titles) != count:
        raise ValueError("original selected test count changed")
    if options.test:
        if options.test not in titles:
            parser.error("--test must be one exact original Bats title")
        titles = [options.test]
    owner = uuid.uuid4().hex
    name = "factorybox-bats-" + owner
    archive = ROOT / "artifacts" / name
    output = archive / "output"
    context = archive / "context"
    output.mkdir(parents=True)
    context.mkdir()
    shutil.copyfile(ROOT / "build/upstream-bats.Dockerfile", context / "Dockerfile")
    shutil.copyfile(lock, archive / "source-lock.json")
    for script in ["upstream-bats.sh", "private-cgroup-delegation.sh", "global-sysctl-observation.py"]:
        shutil.copyfile(ROOT / "tests" / script, archive / script)
    with tarfile.open(archive / "source.tar", "w", dereference=False) as transport:
        transport.add(tree / "tests", arcname="tests")
    result = {
        "schema": 1, "owner": owner, "container_name": name, "suite": options.suite,
        "test": options.test, "passed": False, "cleanup": False, "production_qualified": False,
        "started_at": datetime.datetime.now(datetime.timezone.utc).isoformat(),
        "sysbox_build": options.sysbox_build, "component_sha256": components["binary_sha256"],
        "native_base_build": options.native_base_build, "base_image": image,
        "base_packages_sha256": base["packages_sha256"],
        "source_lock_sha256": reference.sha(lock), **sources.test_counts(component, unchanged),
        "selected": selected, "expected_titles": titles,
        "inputs_sha256": {p: reference.sha(archive / p) for p in [
            "source.tar", "upstream-bats.sh", "private-cgroup-delegation.sh",
            "global-sysctl-observation.py", "context/Dockerfile"]},
        "scope": "Selected pinned Sysbox Bats file with original assertions in a bounded private "
                 "PID/net/cgroup fixture. A declared missing-helper import repairs Systemd's original "
                 "cgroup admission; lifecycle helpers wait for real listener readiness after the "
                 "original delay. No test body, PID-file assertion or skip condition is changed. "
                 "Module/rawio/boot/time/syslog capabilities removed. "
                 "Selected global kernel sentinels compared before and after. "
                 "Not the complete upstream umbrella matrix or Factorybox lifecycle qualification.",
        "parent_sysctls_readonly": options.suite == "lifecycle",
    }
    if acquisition:
        result["images_archive"] = options.images
        result["images_tar_sha256"] = acquisition["images_tar_sha256"]
        result["images"] = acquisition["images"]
    attempted = False
    volumes = []
    failure = None
    try:
        fixture = build_fixture(archive, name, image)
        result["fixture_image"] = fixture
        command = [
            *reference.DOCKER, "run", "--pull=never", "--name", name,
            "--label", "com.factorybox.run=" + owner, "--privileged",
            "--cgroupns=private", "--network=none", "--cpus=3", "--memory=3g", "--pids-limit=512",
            "--env", "FACTORYBOX_UPSTREAM_CONTAINER=1", "--env", "FACTORYBOX_UPSTREAM_BATS=1",
            "--env", "FACTORYBOX_BATS_SUITE=" + options.suite,
            "--env", "FACTORYBOX_BATS_SOURCE_SHA256=" + result["inputs_sha256"]["source.tar"],
            "--env", "FACTORYBOX_BATS_TEST=" + ("^" + re.escape(options.test) + "$" if options.test else ""),
            "--mount", f"type=bind,source={archive / 'source.tar'},target=/source.tar,readonly",
            "--mount", f"type=bind,source={component_build / 'output'},target=/components,readonly",
            "--mount", f"type=bind,source={archive / 'upstream-bats.sh'},target=/run-tests.sh,readonly",
            "--mount", f"type=bind,source={archive / 'private-cgroup-delegation.sh'},target=/delegate.sh,readonly",
            "--mount", f"type=bind,source={archive / 'global-sysctl-observation.py'},target=/observe-sysctls.py,readonly",
            "--mount", f"type=bind,source={output},target=/output",
        ]
        for suffix, destination in [("data", "/var/lib"), ("scratch", "/mnt/scratch")]:
            volume = name + "-" + suffix
            volumes.append(volume)
            reference.docker(["volume", "create", "--label", "com.factorybox.run=" + owner, volume])
            command += ["--mount", f"type=volume,source={volume},target={destination}"]
        if saved_images:
            command += [
                "--mount", f"type=bind,source={saved_images / 'images.tar'},target=/images.tar,readonly",
                "--mount", f"type=bind,source={saved_images / 'images.json'},target=/expected-images.json,readonly",
            ]
        setup = "mount --make-rprivate /\n"
        if options.suite == "lifecycle":
            setup += "mount --bind /proc/sys /proc/sys\nmount -o remount,bind,ro /proc/sys\n"
        setup += (
            ". /delegate.sh > /output/cgroup-delegation.txt\n"
            "exec setpriv --bounding-set=-sys_module,-sys_rawio,-sys_boot,-sys_time,-syslog "
            "tini -- /bin/bash /run-tests.sh")
        command += ["--entrypoint", "/bin/bash", fixture, "-ec", setup]
        result["command"] = command
        reference_lifecycle.write_json(archive / "intent.json", result)
        print(json.dumps({"running": str(archive), "suite": options.suite}), flush=True)
        attempted = True
        with (archive / "stdout.txt").open("x") as stdout, (archive / "stderr.txt").open("x") as stderr:
            done = subprocess.run(command, stdout=stdout, stderr=stderr, timeout=370)
        result["exit_code"] = done.returncode
        result["container_state"] = json.loads(reference.docker(["container", "inspect", name]))[0]["State"]
        result["inputs_unchanged"] = all(
            reference.sha(archive / path) == expected for path, expected in result["inputs_sha256"].items())
        result["components_unchanged"] = all(
            reference.sha(component_build / "output" / component) == expected
            for component, expected in result["component_sha256"].items())
        if (output / "platform.json").exists():
            result["platform"] = json.loads((output / "platform.json").read_text())
        if (output / "packages.tsv").exists():
            result["fixture_packages_sha256"] = reference.sha(output / "packages.tsv")
            result["bats_version"] = (output / "bats-version.txt").read_text().strip()
        if (output / "sysctls-after.json").exists():
            before = json.loads((output / "sysctls-before.json").read_text())
            after = json.loads((output / "sysctls-after.json").read_text())
            result["observed_global_sysctls_unchanged"] = before == after
        if not (output / "tests.tap").exists():
            raise ValueError("fixture stopped before running Bats: " + (archive / "stderr.txt").read_text()[-4096:])
        observed = observations((output / "tests.tap").read_text(), titles)
        result["observed"] = observed
        result["passed"] = (
            done.returncode == 0 and not result["container_state"]["OOMKilled"] and
            result["inputs_unchanged"] and result["components_unchanged"] and
            result.get("observed_global_sysctls_unchanged", False) and
            observed["complete"] and observed["executed_passes"] > 0 and not observed["failed"] and
            set(observed["skipped"]) <= (SYSTEMD_SKIPS if options.suite == "systemd" else set()))
        if not result["passed"]:
            raise ValueError("original selected Bats cases failed or did not all finish")
    except Exception as error:
        failure = error
        result["error"] = str(error)
    finally:
        preserve = False
        if attempted and failure:
            try:
                archive_failure(archive, name, owner, volumes, fixture, result)
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
        raise RuntimeError("upstream Bats gate failed; evidence: " + str(archive)) from failure


if __name__ == "__main__":
    main()
