"""Verify native Go/CRIU descriptor transport evidence for exact worker inputs."""
import json
from pathlib import Path
import re

import reference
import upstream_unit


REQUIRED_TESTS = {
    "TestRootPID1CRIUConsumesCompleteLargeGeneration",
    "TestRootPID1CRIURejectsPartialGenerationWithoutLeakingOrPublishing",
    "TestRootPID1RetainsAndReleasesLargeGenerationAtomically",
    "TestDescriptorTransportSplitsDirectoryMetadataWithoutLosingMappings",
    "TestCriuFeatures",
}
INPUTS = {
    "client.c", "client-fixture.c", "runner.py", "fdstore.test", "features.test",
    "source/criu/files.c", "source/criu/crtools.c",
}


def go_source_context(root):
    root = Path(root)
    entries = {}
    for name in ["go.mod", "go.sum", "internal", "cmd/factorybox"]:
        path = root / name
        if path.is_symlink():
            raise ValueError("descriptor interoperability source cannot be redirected")
        if path.is_file():
            entries[name] = {"sha256": reference.sha(path)}
        elif path.is_dir():
            for relative, value in upstream_unit.context_manifest(path).items():
                if "symlink" in value:
                    raise ValueError("descriptor interoperability source cannot be redirected")
                entries[name + "/" + relative] = value
        else:
            raise ValueError("descriptor interoperability source is incomplete")
    return entries


def validate(result, expected):
    tests = result.get("passedTests")
    if (type(result.get("schema")) is not int or result["schema"] != 1 or
            result.get("passed") is not True or result.get("cleanup") is not True or
            result.get("oomKilled") is not False or result.get("featureOOMKilled") is not False or
            any(type(result.get(key)) is not int or result[key] != 0
                for key in ["exitCode", "testsSkipped", "featureExitCode", "featureTestsSkipped"]) or
            not isinstance(tests, list) or not all(isinstance(test, str) for test in tests) or
            len(tests) != len(set(tests)) or not REQUIRED_TESTS.issubset(tests) or
            any(result.get(key) != value for key, value in expected.items())):
        raise ValueError("worker requires exact successful native descriptor interoperability")


def load(name, *, platform, criu_name, native_base_build, source_root):
    if not re.fullmatch(r"factorybox-fdstore-criu-[0-9a-f]{32}", name):
        raise ValueError("descriptor interoperability requires one exact completed artifact")
    artifact = reference.ROOT / "artifacts" / name
    result_path = artifact / "result.json"
    if artifact.is_symlink() or result_path.is_symlink():
        raise ValueError("descriptor interoperability receipt cannot be redirected")
    result = json.loads(result_path.read_text())
    if not re.fullmatch(r"factorybox-build-criu-retained-[0-9a-f]{32}", criu_name):
        raise ValueError("descriptor interoperability requires one exact CRIU build")
    criu = reference.ROOT / "artifacts" / criu_name
    validate(result, {
        "platform": platform,
        "nativePlatform": platform,
        "criuCandidate": criu_name,
        "criuResultSHA256": reference.sha(criu / "result.json"),
        "criuBinarySHA256": reference.sha(criu / "criu"),
        "nativeBaseBuild": native_base_build,
        "goSourceContext": go_source_context(source_root),
    })
    if go_source_context(artifact / "go-source") != result["goSourceContext"]:
        raise ValueError("descriptor interoperability frozen Go source changed")
    if set(result.get("inputsSHA256", {})) != INPUTS:
        raise ValueError("descriptor interoperability input inventory differs")
    for name, digest in result["inputsSHA256"].items():
        path = artifact / name
        if path.is_symlink() or reference.sha(path) != digest:
            raise ValueError("descriptor interoperability frozen input changed: " + name)
    for name in ["criu/files.c", "criu/crtools.c"]:
        if reference.sha(artifact / "source" / name) != reference.sha(criu / "context" / name):
            raise ValueError("descriptor interoperability exercised a different CRIU source")
    for frozen, current in [
            ("runner.py", "run.py"), ("client-fixture.c", "client.c")]:
        if reference.sha(artifact / frozen) != reference.sha(
                reference.ROOT / "tests/fdstore-criu-client" / current):
            raise ValueError("descriptor interoperability test fixture changed")
    return artifact, result
