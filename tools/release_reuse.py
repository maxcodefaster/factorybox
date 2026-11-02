"""Verify exact closed native receipts before explicitly reusing their evidence."""
import hashlib
import json
from pathlib import Path
import re
import tarfile

import reference
import sources
import upstream_bats
import upstream_unit


UPSTREAM_ROLES = {
    ("upstream", "all"): "sysbox-unit",
    ("upstream", "runc-integration"): "sysbox-integration",
    ("bats", "lifecycle"): "bats-lifecycle",
    ("bats", "systemd"): "bats-systemd",
}


def load(name, prefixes):
    if not any(re.fullmatch(re.escape(prefix) + r"-[0-9a-f]{32}", name) for prefix in prefixes):
        raise ValueError("reuse requires one exact native artifact identity")
    path = reference.ROOT / "artifacts" / name
    result = path / "result.json"
    if path.resolve() != path or result.is_symlink() or not result.is_file():
        raise ValueError("native reuse receipt cannot be redirected or missing")
    return path, json.loads(result.read_text())


def require_closed(record):
    if record.get("passed") is not True or record.get("cleanup") is not True:
        raise ValueError("native reuse requires successful execution and confirmed cleanup")


def require_unit_identity(record, expected, suites):
    require_closed(record)
    if (record.get("test") is not None or record.get("candidate_platform") is not False or
            record.get("inputs_unchanged") is not True or
            any(record.get(key) != value for key, value in expected.items()) or
            record.get("suites") != suites or not suites or
            any(row.get("exit_code") != 0 or row.get("failed") for row in suites.values())):
        raise ValueError("upstream reuse requires the complete exact source, harness, host and suite")


def require_bats_identity(record, expected, observed):
    require_closed(record)
    if (record.get("test") is not None or record.get("inputs_unchanged") is not True or
            record.get("components_unchanged") is not True or
            any(record.get(key) != value for key, value in expected.items()) or
            record.get("observed") != observed or observed.get("complete") is not True or
            observed.get("failed") or
            set(observed.get("skipped", [])) -
            (upstream_bats.SYSTEMD_SKIPS if expected.get("systemd_network_profile") else set())):
        raise ValueError("Bats reuse requires every original assertion and its exact native inputs")


def require_bats_source(archive, tree):
    seen = set()
    expected = {"tests"} | {str(path.relative_to(tree)) for path in (tree / "tests").rglob("*")}
    with tarfile.open(archive) as transport:
        for member in transport:
            relative = Path(member.name)
            if relative.is_absolute() or ".." in relative.parts or member.name not in expected or member.name in seen:
                raise ValueError("retained Bats source archive has changed members")
            source = tree / relative
            if member.isdir():
                matches = source.is_dir() and not source.is_symlink()
            elif member.issym():
                matches = source.is_symlink() and str(source.readlink()) == member.linkname
            elif member.isfile():
                with transport.extractfile(member) as content:
                    digest = hashlib.file_digest(content, "sha256").hexdigest()
                matches = not source.is_symlink() and reference.sha(source) == digest
            else:
                matches = False
            if not matches:
                raise ValueError("retained Bats source differs from the current pinned assertions")
            seen.add(member.name)
    if seen != expected:
        raise ValueError("retained Bats source omits current pinned assertions")


def upstream_receipt(name, worker, component, base, host, builder, bats_images):
    path, record = load(name, ["factorybox-upstream", "factorybox-bats"])
    kind = "upstream" if path.name.startswith("factorybox-upstream-") else "bats"
    role = UPSTREAM_ROLES.get((kind, record.get("suite")))
    if not role:
        raise ValueError("only complete upstream unit, integration and Bats gates may be reused")
    if kind == "upstream":
        prefix = (reference.ROOT / "build/sysbox.Dockerfile").read_bytes()
        template = (reference.ROOT / "build/upstream-unit.Dockerfile").read_bytes()
        effective = upstream_unit.compose_offline_unit_dockerfile(prefix, template)
        expected = {
            "component_build": worker["sysbox_build"], "platform": worker["platform"],
            "host": {key: host[key] for key in ["OSType", "Architecture", "KernelVersion"]},
            "builder": builder, "source_lock_sha256": component["source_lock_sha256"],
            "context_manifest_sha256": reference.sha(
                reference.ROOT / "artifacts" / worker["sysbox_build"] / "context-manifest.json"),
            "test_script_sha256": reference.sha(reference.ROOT / "tests/upstream-unit.sh"),
            "delegation_setup_sha256": reference.sha(reference.ROOT / "tests/private-cgroup-delegation.sh"),
            "dependency_recipe_sha256": hashlib.sha256(template).hexdigest(),
            "effective_dockerfile_sha256": hashlib.sha256(effective).hexdigest(),
        }
        for file, key in [
                ("upstream-unit.sh", "test_script_sha256"),
                ("private-cgroup-delegation.sh", "delegation_setup_sha256"),
                ("upstream-unit.Dockerfile", "dependency_recipe_sha256"),
                ("offline.Dockerfile", "effective_dockerfile_sha256")]:
            if reference.sha(path / file) != expected[key]:
                raise ValueError("retained upstream test inputs changed")
        observed = upstream_unit.observations(path / "output")
        required = ({"runc-integration"} if role == "sysbox-integration" else
                    {"runc", "fs", "mgr", "ipc"} | {
                        "libs-" + p.parent.name for p in
                        (reference.ROOT / "artifacts" / worker["sysbox_build"] / "context/sysbox-libs").glob("*/go.mod")})
        if set(observed) != required:
            raise ValueError("upstream reuse cannot substitute a narrower suite")
        require_unit_identity(record, expected, observed)
        if role == "sysbox-unit" and component.get("shm_startup_reexec"):
            required_shm = {
                "TestFactoryboxShmProbeNative", "TestShmProbeRefusesUnmappedProcess",
                "TestShmProbeRefusesSharedNamespaces", "TestShmProbeRefusesSharedNamespaces/ipc",
                "TestShmProbeRefusesSharedNamespaces/mount",
            }
            rows = [json.loads(line) for line in (path / "output/libs-linuxUtils.jsonl").read_text().splitlines()
                    if line.startswith("{")]
            if (record.get("shm_reexec_native_tests_requested") is not True or
                    record.get("shm_reexec_native_tests_passed") is not True or
                    not required_shm <= {row.get("Test") for row in rows if row.get("Action") == "pass"}):
                raise ValueError("upstream reuse lacks its required native shared-memory regressions")
    else:
        for file, digest in record.get("inputs_sha256", {}).items():
            candidate = path / file
            if candidate.resolve() != candidate or not candidate.is_relative_to(path) or reference.sha(candidate) != digest:
                raise ValueError("retained Bats test inputs changed")
        scripts = ["upstream-bats.sh", "private-cgroup-delegation.sh", "global-sysctl-observation.py"]
        if role == "bats-systemd":
            scripts += ["systemd-docker-cli.sh", "systemd-docker-profile.json"]
        for script in scripts:
            if reference.sha(path / script) != reference.sha(reference.ROOT / "tests" / script):
                raise ValueError("current Bats harness differs from retained execution")
        if reference.sha(path / "context/Dockerfile") != reference.sha(reference.ROOT / "build/upstream-bats.Dockerfile"):
            raise ValueError("Bats dependency recipe changed")
        lock = reference.ROOT / "upstream/lock.json"
        declaration = json.loads(lock.read_text())["components"]["sysbox-tests"]
        tree = reference.ROOT / ".cache/sources/sysbox-tests"
        sources.validate_inputs(declaration)
        unchanged = sources.check_source(tree, reference.ROOT / ".cache/downloads" / declaration["archive"], declaration)
        require_bats_source(path / "source.tar", tree)
        selected, count = upstream_bats.SUITES[record["suite"]]
        titles = re.findall(r'^@test "([^"]+)" \{', (tree / selected).read_text(), re.MULTILINE)
        if len(titles) != count:
            raise ValueError("Bats upstream assertion count changed")
        expected = {
            "sysbox_build": worker["sysbox_build"], "component_sha256": component["binary_sha256"],
            "native_base_build": worker["native_base_build"], "base_image": base["image"],
            "base_packages_sha256": base["packages_sha256"],
            "target_platform": worker["platform"], "source_lock_sha256": reference.sha(lock),
            "selected": selected, "expected_titles": titles,
            "systemd_network_profile": role == "bats-systemd", **sources.test_counts(declaration, unchanged),
        }
        native = record.get("platform", {})
        if native.get("kernel") != host["KernelVersion"] or native.get("machine") != host["Architecture"]:
            raise ValueError("Bats native host changed")
        output = path / "output"
        if (json.loads((output / "platform.json").read_text()) != native or
                reference.sha(output / "packages.tsv") != record.get("fixture_packages_sha256") or
                int((output / "tests.exit-code").read_text()) != 0 or
                json.loads((output / "sysctls-before.json").read_text()) !=
                json.loads((output / "sysctls-after.json").read_text()) or
                record.get("observed_global_sysctls_unchanged") is not True):
            raise ValueError("retained Bats native observations changed")
        observed = upstream_bats.observations((output / "tests.tap").read_text(), titles)
        require_bats_identity(record, expected, observed)
        if role == "bats-systemd":
            _, acquisition = upstream_bats.image_archive(bats_images, worker["native_base_build"], platform=worker["platform"])
            if (record.get("images_archive") != bats_images or
                    record.get("images_tar_sha256") != acquisition["images_tar_sha256"] or
                    record.get("images") != acquisition["images"]):
                raise ValueError("Bats systemd image inputs changed")
    return role, {"role": role, "passed": True, "evidence": name,
                  "result_sha256": reference.sha(path / "result.json"), "reused": True,
                  "reuse_scope": "Exact unchanged component, native host and complete upstream assertions; not a new execution"}


def continuation(name, worker_name, worker, commands):
    path, record = load(name, ["factorybox-release-native", "factorybox-release-offline-continuation"])
    if (record.get("cleanup") is not True or record.get("worker_build") != worker_name or
            record.get("image_id") != worker["image_id"]):
        raise ValueError("continuation requires the exact same worker and settled original qualification")
    accepted = {}
    for row in record.get("stages", []):
        if row.get("passed") is not True:
            continue
        role = row.get("role")
        if role not in commands or row.get("command") != commands[role]:
            raise ValueError("continued gate command or required role changed")
        if role in UPSTREAM_ROLES.values():
            # Component proofs require upstream_receipt's complete current
            # host/source/assertion checks, not same-worker continuation.
            continue
        evidence, result = load(row["evidence"], ["factorybox-" + suffix for suffix in [
            "criu-shared-filemap", "capture-budget-native",
            "storefs-native", "storage-exit-native", "shared-source", "criu-core",
            "checkpoint-retirement", "bounded-store", "bounded-loss", "bounded-store-loss",
            "factory-consumer",
        ]])
        require_closed(result)
        if "worker_build" in result and result["worker_build"] != worker_name:
            raise ValueError("continued native evidence belongs to another worker")
        if reference.sha(evidence / "result.json") != row.get("result_sha256"):
            raise ValueError("continued native result changed")
        if role in accepted:
            raise ValueError("duplicate continued native role")
        accepted[role] = {**row, "reused": True, "continued_from": name,
                          "continuation_result_sha256": reference.sha(path / "result.json")}
    return accepted
