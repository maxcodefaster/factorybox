#!/usr/bin/env python3
"""Run the retained full lifecycle with rebuilt components in one owned Kubernetes Pod."""
import argparse
import collections
import copy
import datetime
import fcntl
import hashlib
import json
import pathlib
import re
import shutil
import subprocess
import tarfile
import time
import uuid

import reference
from import_lifecycle_fixture import normalized

ROOT = reference.ROOT
FIXTURE = ROOT / "tests/reference/fixture"
KUBECTL = ["kubectl", "--context", "orbstack", "--request-timeout=8s"]
LABEL = "com.factorybox.run"


def write_json(path, value):
    path.write_text(json.dumps(value, indent=2, sort_keys=True) + "\n")


def kube(args, *, input=None, timeout=12):
    return subprocess.run(KUBECTL + args, input=input, capture_output=True,
                          text=True, check=True, timeout=timeout).stdout


def owned(metadata, name, owner, uid):
    if (metadata.get("name") != name or metadata.get("uid") != uid or not uid or
            metadata.get("labels", {}).get(LABEL) != owner):
        raise ValueError("Kubernetes resource identity or ownership changed")


def verify_fixture():
    manifest = json.loads((FIXTURE / "provenance.json").read_text())
    for name, expected in manifest["files_sha256"].items():
        path = reference.REFERENCE / "fixture" / pathlib.PurePosixPath(name)
        if pathlib.PurePosixPath(name).name != name or path.is_symlink():
            raise ValueError("invalid fixture path")
        if reference.sha(path) != expected:
            raise ValueError("retained lifecycle fixture differs: " + name)
    return manifest


def make_pod(template, namespace, owner, image):
    pod = copy.deepcopy(template)
    pod["metadata"] = {"name": "lifecycle", "namespace": namespace,
                       "labels": {LABEL: owner, "app.kubernetes.io/part-of": "factorybox-test"}}
    for container in [*pod["spec"]["initContainers"], pod["spec"]["containers"][0]]:
        container["image"] = image
    # Keep the exact reference resource limits, workload, readiness assertions and policy.
    if (pod["spec"]["activeDeadlineSeconds"] != 420 or
            pod["spec"]["containers"][0]["resources"]["limits"]["cpu"] != "3" or
            pod["spec"]["containers"][0]["resources"]["limits"]["memory"] != "3Gi" or
            pod["spec"]["containers"][1]["resources"]["limits"]["memory"] != "768Mi"):
        raise ValueError("lifecycle fixture resource contract changed")
    # This worker also runs the retained Factory installation. Reserve 2.5 CPUs
    # so the fixture can be admitted without changing unrelated workloads.
    # Its original 3-CPU execution limit and complete workload stay intact.
    pod["spec"]["containers"][0]["resources"]["requests"]["cpu"] = "2500m"
    probe = pod["spec"]["containers"][0]
    terminal = "printf '%s\\n' \"$status\" > /experiment/exit-code\n"
    if probe["args"][0].count(terminal) != 1:
        raise ValueError("reference terminal status writer changed")
    probe["args"][0] = probe["args"][0].replace(
        terminal, terminal + "printf 'FACTORYBOX_EXIT=%s\\n' \"$status\"\n")
    return pod


def verify_records(text, expected):
    records = []
    for line in text.splitlines():
        if line.startswith("{"):
            row = json.loads(line)
            if "status" in row and "name" in row:
                records.append(row)
    failed = [row["name"] for row in records if row["status"] != "passed"]
    if failed:
        raise ValueError("native lifecycle assertion failed: " + ", ".join(failed))
    actual = collections.Counter(normalized(row["name"]) for row in records)
    missing = collections.Counter(expected) - actual
    if missing:
        raise ValueError("native lifecycle assertions missing: " + json.dumps(dict(missing)))
    return records


def terminal_status(text):
    values = [line.rstrip("\n").removeprefix("FACTORYBOX_EXIT=")
              for line in text.splitlines(keepends=True)
              if line.startswith("FACTORYBOX_EXIT=") and line.endswith("\n")]
    if not values:
        return None
    if len(values) != 1 or not values[0].isdigit() or not 0 <= int(values[0]) <= 255:
        raise ValueError("native terminal status is malformed or duplicated")
    return int(values[0])


def component_build(relative, expected_prefix):
    path = ROOT / "artifacts" / relative
    if pathlib.PurePath(relative).name != relative or not relative.startswith(expected_prefix):
        raise ValueError("expected a named Factorybox build artifact")
    result = json.loads((path / "result.json").read_text())
    if not result["passed"]:
        raise ValueError("component build failed")
    current_lock = ROOT / "upstream/lock.json"
    if result["source_lock_sha256"] != reference.sha(current_lock):
        retained = path / "source-lock.json"
        if not retained.exists() or reference.sha(retained) != result["source_lock_sha256"]:
            raise ValueError("build source lock is unavailable or changed")
        before = json.loads(retained.read_text())["components"]
        current = json.loads(current_lock.read_text())["components"]
        relevant = (["criu"] if expected_prefix == "factorybox-build-criu-" else
                    ["sysbox-runc", "sysbox-fs", "sysbox-mgr", "sysbox-ipc",
                     "sysbox-libs", "sysbox-fuse"])
        if any(before[name] != current[name] for name in relevant):
            raise ValueError("build belongs to different component sources")
    return path, result


def prepare_image(archive, owner, criu_name, sysbox_name):
    reference.verify()
    verify_fixture()
    build = json.loads((reference.OUTPUT / "manifest.json").read_text())
    if build["source_manifest_sha256"] != reference.sha(reference.REFERENCE / "provenance.json"):
        raise ValueError("driver build belongs to another reference")
    criu_path, criu = component_build(criu_name, "factorybox-build-criu-")
    sysbox_path, sysbox = component_build(sysbox_name, "factorybox-build-sysbox-")
    context = archive / "context"
    context.mkdir()
    for name in ["lab", "ramctl", "keeper"]:
        source = reference.OUTPUT / name
        if reference.sha(source) != build["files_sha256"][name]:
            raise ValueError("driver binary changed: " + name)
        shutil.copyfile(source, context / name)
    if reference.sha(criu_path / "criu") != criu["binary_sha256"]:
        raise ValueError("rebuilt CRIU changed")
    shutil.copyfile(criu_path / "criu", context / "criu")
    for name in ["sysbox-runc", "sysbox-fs", "sysbox-mgr"]:
        source = sysbox_path / "output" / name
        if reference.sha(source) != sysbox["binary_sha256"][name]:
            raise ValueError("rebuilt Sysbox changed: " + name)
        shutil.copyfile(source, context / name)
    shutil.copyfile(FIXTURE / "theia-client.cjs", context / "theia-client.cjs")
    for path in context.iterdir():
        path.chmod(0o444 if path.suffix == ".cjs" else 0o555)
    hashes = {p.name: reference.sha(p) for p in context.iterdir()}
    write_json(archive / "inputs.json", hashes)
    # The worker's image-inspect endpoint can stall even for an existing config
    # ID. Pin a unique build alias directly from that exact local identity.
    # A missing ID fails the tag command; no replacement image is pulled.
    base_alias = "factorybox-base:" + owner
    reference.docker(["tag", reference.BASE_ID, base_alias])
    tag = "factorybox-reference:" + owner
    command = [*reference.DOCKER, "build", "--provenance=false", "--progress=plain",
               "--build-arg", "BASE_IMAGE=" + base_alias,
               "--output", "type=image,compression=uncompressed", "--tag", tag,
               "--iidfile", str(archive / "image-id.txt"),
               "--metadata-file", str(archive / "image-metadata.json"),
               "-f", str(ROOT / "build/reference-lifecycle.Dockerfile"), str(context)]
    with (archive / "image-build.log").open("x") as log:
        subprocess.run(command, stdout=log, stderr=subprocess.STDOUT, check=True, timeout=180)
    metadata = json.loads((archive / "image-metadata.json").read_text())
    digest = metadata["containerimage.digest"]
    if not re.fullmatch(r"sha256:[0-9a-f]{64}", digest):
        raise ValueError("build did not return an immutable image digest")
    return "factorybox-reference@" + digest, hashes


def current_pod(namespace, owner, namespace_uid, pod_uid, requested):
    ns = json.loads(kube(["get", "namespace", namespace, "-o", "json"]))
    owned(ns["metadata"], namespace, owner, namespace_uid)
    pod = json.loads(kube(["get", "pod", "lifecycle", "-n", namespace, "-o", "json"]))
    owned(pod["metadata"], "lifecycle", owner, pod_uid)
    if pod["metadata"]["namespace"] != namespace:
        raise ValueError("observed pod changed namespace")
    for section in ["containers", "initContainers"]:
        actual = {c["name"]: c for c in pod["spec"].get(section, [])}
        for expected in requested["spec"].get(section, []):
            observed = actual[expected["name"]]
            for field in ["image", "env", "command", "args"]:
                if observed.get(field) != expected.get(field):
                    raise ValueError("observed pod workload changed: " + field)
    return pod


def remove_namespace(namespace, owner, uid, archive):
    observed = json.loads(kube(["get", "namespace", namespace, "-o", "json"]))
    owned(observed["metadata"], namespace, owner, uid)
    options = {"apiVersion": "v1", "kind": "DeleteOptions",
               "preconditions": {"uid": uid}, "propagationPolicy": "Foreground"}
    write_json(archive / "delete-options.json", options)
    kube(["delete", "--raw", "/api/v1/namespaces/" + namespace, "-f", "-"],
         input=json.dumps(options))
    deadline = time.monotonic() + 45
    while time.monotonic() < deadline:
        text = kube(["get", "namespace", namespace, "--ignore-not-found", "-o", "json"])
        if not text.strip():
            return
        owned(json.loads(text)["metadata"], namespace, owner, uid)
        time.sleep(1)
    raise ValueError("owned namespace deletion is pending; preserve this run and reconcile its UID")


def harvest(namespace, archive):
    # Preserve complete native JSON/log/metadata evidence, excluding the large page
    # payloads. Do not follow symlinks or change any snapshot contents.
    script = """cd /experiment && find . -type f \\( -name '*.json' -o -name '*.log' \
-o -name 'stats-*' -o -name 'pagemap-*.img' -o -name 'mm-*.img' \
-o -name 'inventory.img' -o -name 'pstree.img' -o -name 'exit-code' \\) \
-print0 | tar --null -T - -cf -"""
    with (archive / "native-evidence.tar").open("xb") as output, \
            (archive / "harvest-stderr.txt").open("xb") as errors:
        subprocess.run(KUBECTL[:-1] + ["--request-timeout=35s",
                                 "exec", "-n", namespace, "lifecycle", "-c", "probe", "--",
                                 "sh", "-ec", script], stdout=output, stderr=errors,
                       check=True, timeout=40)
    with tarfile.open(archive / "native-evidence.tar") as bundle:
        for member in bundle:
            path = pathlib.PurePosixPath(member.name)
            if not member.isfile() or path.is_absolute() or ".." in path.parts:
                raise ValueError("unsafe native evidence member")
        member = bundle.getmember("./pipe-store/result.json")
        proof = json.load(bundle.extractfile(member))
    readers = proof["direct_criu_readers"]
    if (proof["status"] != "passed" or proof["registered_pipe_bytes"] <= 0 or
            not readers["required"] or readers["calls"] <= 0 or readers["private_pipe_bytes"] <= 0 or
            readers["active_client_leases"] or readers["active_client_descriptors"] or readers["failures"]):
        raise ValueError("native page-owner teardown or descriptor release failed")
    write_json(archive / "page-owner-result.json", proof)


def execute(options):
    owner = uuid.uuid4().hex
    namespace = "factorybox-test-" + owner
    archive = ROOT / "artifacts" / ("factorybox-lifecycle-" + owner)
    archive.mkdir(parents=True)
    result = {"schema": 1, "owner": owner, "namespace": namespace,
              "started_at": datetime.datetime.now(datetime.timezone.utc).isoformat(),
              "passed": False, "cleanup": False, "production_qualified": False,
              "base_image_config": reference.BASE_ID,
              "probe_cpu_request_milli": 2500,
              "performance_baseline": False,
              "scope": "Retained developer lifecycle with rebuilt components; "
                       "not the extracted public API or complete upstream qualification."}
    for relative in ["tools/reference_lifecycle.py", "build/reference-lifecycle.Dockerfile"]:
        target = archive / pathlib.Path(relative).name
        shutil.copyfile(ROOT / relative, target)
    result["runner_sha256"] = reference.sha(archive / "reference_lifecycle.py")
    result["fixture_provenance_sha256"] = reference.sha(FIXTURE / "provenance.json")
    namespace_uid = pod_uid = None
    create_attempted = False
    failure = None
    log_process = None
    log_stream = log_errors = None
    try:
        print(json.dumps({"preparing": str(archive)}), flush=True)
        image, hashes = prepare_image(archive, owner, options.criu_build, options.sysbox_build)
        result.update(image=image, binaries_sha256=hashes)
        # Never overlap the retained experiment or another Factorybox lifecycle.
        old = kube(["get", "namespace", "runtime-fork-experiment", "--ignore-not-found", "-o", "name"])
        other = json.loads(kube(["get", "pods", "-A", "-l",
                                "app.kubernetes.io/part-of=factorybox-test", "-o", "json"]))
        if old.strip() or other["items"]:
            raise ValueError("another native lifecycle owns resources; leave it untouched")
        pod = make_pod(json.loads((FIXTURE / "pod.json").read_text()), namespace, owner, image)
        write_json(archive / "requested-pod.json", pod)
        write_json(archive / "intent.json", result)
        ns = {"apiVersion": "v1", "kind": "Namespace", "metadata": {
            "name": namespace, "labels": {LABEL: owner}}}
        create_attempted = True
        created = json.loads(kube(["create", "-f", "-", "-o", "json"], input=json.dumps(ns)))
        namespace_uid = result["namespace_uid"] = created["metadata"]["uid"]
        created = json.loads(kube(["create", "-f", "-", "-o", "json"], input=json.dumps(pod)))
        pod_uid = result["pod_uid"] = created["metadata"]["uid"]
        write_json(archive / "intent.json", result)
        deadline = time.monotonic() + 420
        print(json.dumps({"running": str(archive), "namespace": namespace}), flush=True)
        finished = False
        while time.monotonic() < deadline:
            try:
                observed = current_pod(namespace, owner, namespace_uid, pod_uid, pod)
            except (subprocess.TimeoutExpired, subprocess.CalledProcessError) as error:
                result.setdefault("observation_errors", []).append(str(error))
                time.sleep(1)
                continue
            write_json(archive / "observed-pod.json", observed)
            for condition in observed.get("status", {}).get("conditions", []):
                if condition.get("reason") == "Unschedulable":
                    raise ValueError("shared worker cannot admit this fixture: " +
                                     condition.get("message", "Unschedulable"))
            states = {c["name"]: c for c in observed.get("status", {}).get("containerStatuses", [])}
            running = "running" in states.get("probe", {}).get("state", {})
            if running and log_process is None:
                log_stream = (archive / "records.jsonl").open("x")
                log_errors = (archive / "log-stderr.txt").open("x")
                log_process = subprocess.Popen(KUBECTL[:-1] + ["--request-timeout=0",
                    "logs", "--follow", "-n", namespace, "lifecycle", "-c", "probe"],
                    stdout=log_stream, stderr=log_errors)
            if running:
                status = terminal_status((archive / "records.jsonl").read_text())
                if status is not None:
                    result["native_exit_code"] = status
                    finished = True
                    break
            if "terminated" in states.get("probe", {}).get("state", {}):
                raise ValueError("native probe terminated before its terminal result")
            if observed.get("status", {}).get("phase") in ["Failed", "Succeeded"]:
                raise ValueError("native pod terminated before evidence was harvested")
            time.sleep(1)
        if not finished:
            raise ValueError("native lifecycle exceeded its 420-second deadline")
        current_pod(namespace, owner, namespace_uid, pod_uid, pod)
        logs = (archive / "records.jsonl").read_text()
        (archive / "complete-records.jsonl").write_text(logs)
        (archive / "theia.log").write_text(kube([
            "--request-timeout=25s", "logs", "-n", namespace, "lifecycle", "-c", "ide"], timeout=30))
        if result["native_exit_code"] != 0:
            raise ValueError("native lifecycle returned nonzero; preserve failed assertions")
        expected = json.loads((FIXTURE / "expected-records.json").read_text())
        records = verify_records(logs, expected)
        result["passed_records"] = len(records)
        harvest(namespace, archive)
        result["passed"] = True
    except (Exception, KeyboardInterrupt) as error:
        result["error"] = str(error) or type(error).__name__
        failure = error
        if pod_uid:
            try:
                current_pod(namespace, owner, namespace_uid, pod_uid, pod)
                (archive / "failed-records.jsonl").write_text(kube([
                    "logs", "-n", namespace, "lifecycle", "-c", "probe"]))
                if not (archive / "native-evidence.tar").exists():
                    harvest(namespace, archive)
            except Exception as diagnostic:
                result["diagnostic_error"] = str(diagnostic)
    finally:
        if log_process is not None:
            log_process.terminate()
            try:
                log_process.wait(timeout=5)
            except subprocess.TimeoutExpired:
                log_process.kill()
                log_process.wait(timeout=5)
            log_stream.close()
            log_errors.close()
        try:
            # If create timed out before returning a UID, do not adopt or remove
            # a same-named namespace based on its name alone.
            if namespace_uid:
                remove_namespace(namespace, owner, namespace_uid, archive)
                result["cleanup"] = True
            elif not create_attempted:
                result["cleanup"] = True
        except Exception as error:
            result["cleanup_error"] = str(error)
            failure = failure or error
        write_json(archive / "result.json", result)
        write_json(archive / "manifest.json", {
            str(p.relative_to(archive)): reference.sha(p) for p in archive.rglob("*")
            if p.is_file() and p.name != "manifest.json"})
        print(json.dumps({"evidence": str(archive), **result}), flush=True)
    if failure:
        raise RuntimeError("native lifecycle remains unqualified: " + str(archive)) from failure


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--criu-build", required=True)
    parser.add_argument("--sysbox-build", required=True)
    options = parser.parse_args()
    (ROOT / ".cache").mkdir(exist_ok=True)
    with (ROOT / ".cache/lifecycle.lock").open("a") as lock:
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        execute(options)


if __name__ == "__main__":
    main()
