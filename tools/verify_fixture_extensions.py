#!/usr/bin/env python3
"""Check ordered fixture hashes without replacing their original assertions."""
import argparse
import hashlib
import json
from pathlib import Path
import shutil
import subprocess
import tempfile

import reference


def legacy_cleanup_fixture_before(root):
    root = Path(root)
    manifest = root / "tests/compatibility/stopped-capture-legacy-cleanup.json"
    if not manifest.exists():
        return {}
    value = json.loads(manifest.read_text())
    name = "tools/bounded_store_native.py"
    if (value.get("schema") != 1 or value.get("assertions_preserved") is not True or
            value.get("upstream_assertions_modified") is not False or set(value["files"]) != {name} or
            value["previous_manifest"] != "tests/compatibility/stopped-capture-delete.json" or
            reference.sha(root / value["previous_manifest"]) != value["previous_manifest_sha256"] or
            reference.sha(root / value["patch"]) != value["patch_sha256"]):
        raise ValueError("legacy cleanup fixture compatibility declaration differs")
    previous = json.loads((root / value["previous_manifest"]).read_text())
    row = value["files"][name]
    if (reference.sha(root / name) != row["after_sha256"] or
            row["before_sha256"] != previous["files"][name]["after_sha256"]):
        raise ValueError("legacy cleanup fixture changed its ordered input")
    with tempfile.TemporaryDirectory(prefix="fbx-legacy-cleanup-fixture-") as directory:
        target = Path(directory)
        (target / name).parent.mkdir(parents=True)
        shutil.copyfile(root / name, target / name)
        done = subprocess.run(["patch", "--batch", "--reverse", "--fuzz=0", "-p1",
                               "-i", str((root / value["patch"]).resolve())],
                              cwd=target, capture_output=True, text=True, timeout=10)
        if done.returncode or reference.sha(target / name) != row["before_sha256"]:
            raise ValueError("legacy cleanup fixture cannot reconstruct original runner assertions")
        return {name: (target / name).read_bytes()}


def stopped_capture_fixture_before(root):
    root = Path(root)
    prior = legacy_cleanup_fixture_before(root)
    manifest = root / "tests/compatibility/stopped-capture-delete.json"
    if not manifest.exists():
        return {}
    value = json.loads(manifest.read_text())
    if (value.get("schema") != 1 or value.get("assertions_preserved") is not True or
            value.get("upstream_assertions_modified") is not False or
            set(value["files"]) != {"tools/bounded_store_native.py", "tests/lifecycle-native.sh"} or
            value["previous_manifest"] != "tests/compatibility/pidfd-namespaces.json" or
            reference.sha(root / value["previous_manifest"]) != value["previous_manifest_sha256"] or
            value["production_manifest"] != "tests/production-fixture-extensions.json" or
            reference.sha(root / value["production_manifest"]) != value["production_manifest_sha256"] or
            reference.sha(root / value["patch"]) != value["patch_sha256"]):
        raise ValueError("stopped capture fixture compatibility declaration differs")
    previous = json.loads((root / value["previous_manifest"]).read_text())
    production = json.loads((root / value["production_manifest"]).read_text())
    before = {}
    with tempfile.TemporaryDirectory(prefix="fbx-stopped-capture-fixture-") as directory:
        target = Path(directory)
        for name, row in value["files"].items():
            earlier = previous if name == "tools/bounded_store_native.py" else production
            data = prior[name] if name in prior else (root / name).read_bytes()
            if (hashlib.sha256(data).hexdigest() != row["after_sha256"] or
                    earlier["files"][name]["after_sha256"] != row["before_sha256"]):
                raise ValueError("stopped capture fixture changed its ordered input: " + name)
            (target / name).parent.mkdir(parents=True, exist_ok=True)
            (target / name).write_bytes(data)
        done = subprocess.run(["patch", "--batch", "--reverse", "--fuzz=0", "-p1",
                               "-i", str((root / value["patch"]).resolve())],
                              cwd=target, capture_output=True, text=True, timeout=10)
        if done.returncode:
            raise ValueError("stopped capture fixture cannot reconstruct the original runner")
        for name, row in value["files"].items():
            if reference.sha(target / name) != row["before_sha256"]:
                raise ValueError("stopped capture fixture changed original assertions: " + name)
            before[name] = (target / name).read_bytes()
    return before


def pidfd_fixture_before(root):
    root = Path(root)
    prior = stopped_capture_fixture_before(root)
    manifest = root / "tests/compatibility/pidfd-namespaces.json"
    if not manifest.exists():
        return {}
    value = json.loads(manifest.read_text())
    if (value.get("schema") != 1 or value.get("assertions_preserved") is not True or
            value.get("upstream_assertions_modified") is not False or
            set(value["files"]) != {"tools/bounded_store_native.py",
                                   "internal/lifecycle/timer_fixture_native_linux_test.go"} or
            reference.sha(root / value["patch"]) != value["patch_sha256"]):
        raise ValueError("PIDFD fixture compatibility declaration differs")
    for name, digest in {**value["previous_manifests"], **value["added_files"]}.items():
        if reference.sha(root / name) != digest:
            raise ValueError("PIDFD fixture changed a bound original manifest or added actor")
    before = {}
    with tempfile.TemporaryDirectory(prefix="fbx-pidfd-fixture-") as directory:
        target = Path(directory)
        for name, row in value["files"].items():
            data = prior[name] if name in prior else (root / name).read_bytes()
            if hashlib.sha256(data).hexdigest() != row["after_sha256"]:
                raise ValueError("PIDFD fixture differs from its declared after bytes: " + name)
            (target / name).parent.mkdir(parents=True, exist_ok=True)
            (target / name).write_bytes(data)
        done = subprocess.run(["patch", "--batch", "--reverse", "--fuzz=0", "-p1",
                               "-i", str((root / value["patch"]).resolve())],
                              cwd=target, capture_output=True, text=True, timeout=10)
        if done.returncode:
            raise ValueError("PIDFD fixture cannot reconstruct its unchanged original inputs")
        for name, row in value["files"].items():
            if reference.sha(target / name) != row["before_sha256"]:
                raise ValueError("PIDFD fixture changed its original assertion bytes: " + name)
            before[name] = (target / name).read_bytes()
    return before


def production_extensions(root):
    prior = stopped_capture_fixture_before(root)
    digest = lambda name: hashlib.sha256(prior[name]).hexdigest() if name in prior else reference.sha(root/name)
    path=root/"tests/production-fixture-extensions.json"
    if not path.exists():return {"production_extensions":0,"production_before_files_verified":False}
    value=json.loads(path.read_text())
    if (value["schema"]!=1 or value["previous_manifest"]!="tests/pilot-fixture-extensions.json" or
            reference.sha(root/value["previous_manifest"])!=value["previous_manifest_sha256"] or
            not value["assertions_preserved"] or value["upstream_assertions_modified"]):
        raise ValueError("production fixture declaration changes its original assertions or manifest")
    baseline_name=value["baseline_artifact"]
    if Path(baseline_name).name!=baseline_name:raise ValueError("invalid production fixture baseline")
    baseline=root/"artifacts"/baseline_name/"source-manifest.json"
    originals=None
    if baseline.exists():
        if reference.sha(baseline)!=value["baseline_source_manifest_sha256"]:
            raise ValueError("production baseline manifest differs")
        originals=json.loads(baseline.read_text())
    before_verified=originals is not None
    for name,row in value["files"].items():
        if (digest(name)!=row["after_sha256"] or
                reference.sha(root/row["patch"])!=row["patch_sha256"]):
            raise ValueError("production fixture or ordered patch differs: "+name)
        if originals is not None and originals[name]["sha256"]!=row["before_sha256"]:
            raise ValueError("production fixture before differs from its baseline: "+name)
        before=root/"artifacts"/row["before_file"]
        if before.exists():
            if reference.sha(before)!=row["before_sha256"]:
                raise ValueError("production original fixture bytes differ: "+name)
        else:before_verified=False
    return {"production_extensions":len(value["files"]),"production_before_files_verified":before_verified}


def verify(root, baseline=None):
    root=Path(root)
    prior = stopped_capture_fixture_before(root)
    digest = lambda name: hashlib.sha256(prior[name]).hexdigest() if name in prior else reference.sha(root/name)
    extensions=json.loads((root/"tests/pilot-fixture-extensions.json").read_text())
    previous=json.loads((root/extensions["previous_manifest"]).read_text())
    if extensions["schema"]!=1 or not extensions["assertions_preserved"] or extensions["upstream_assertions_modified"]:
        raise ValueError("fixture declaration weakens original assertions")
    production_path=root/"tests/production-fixture-extensions.json"
    production=json.loads(production_path.read_text())["files"] if production_path.exists() else {}
    def final_row(name, row):
        current=production.get(name)
        if current is None:return row
        if current["before_sha256"]!=row["after_sha256"]:
            raise ValueError("production fixture does not continue its previous after hash: "+name)
        return current
    for name,row in previous["files"].items():
        final=extensions["files"].get(name,row)
        if name in extensions["files"] and final["before_sha256"]!=row["after_sha256"]:
            raise ValueError("fixture extension does not continue the declared before/after chain: "+name)
        final=final_row(name,final)
        if digest(name)!=final["after_sha256"]:
            raise ValueError("fixture differs from its declared final hash: "+name)
    for name,row in extensions["files"].items():
        if digest(name)!=final_row(name,row)["after_sha256"]:raise ValueError("pilot fixture differs: "+name)
    for row in extensions.get("context_patches", []):
        if (reference.sha(root / row["patch"]) != row["patch_sha256"] or
                reference.sha(root / row["file"]) != row["before_sha256"]):
            raise ValueError("ordered native-context fixture patch or original input differs")
    if baseline:
        baseline=Path(baseline)
        if reference.sha(baseline/"source-manifest.json")!=extensions["baseline_source_manifest_sha256"]:
            raise ValueError("frozen baseline source manifest differs")
        if reference.sha(baseline/"context"/extensions["previous_manifest"])!=reference.sha(root/extensions["previous_manifest"]):
            raise ValueError("original fixture compatibility declaration changed")
        for name,row in extensions["files"].items():
            if reference.sha(baseline/"context"/name)!=row["before_sha256"]:
                raise ValueError("fixture before hash differs from the frozen v0.1 input: "+name)
        for row in extensions.get("context_patches", []):
            if reference.sha(baseline/"context"/row["file"]) != row["before_sha256"]:
                raise ValueError("native-context fixture before differs from frozen v0.1 input")
    return {"previous_fixtures":len(previous["files"]),"ordered_extensions":len(extensions["files"]),
            "before_files_verified":baseline is not None,"upstream_assertions_modified":False,
            "pidfd_fixture_extensions":len(pidfd_fixture_before(root)),
            "stopped_capture_fixture_extensions":len(stopped_capture_fixture_before(root)),
            "legacy_cleanup_fixture_extensions":len(legacy_cleanup_fixture_before(root)),
            **production_extensions(root)}


def apply_context_patches(root, context, artifact):
    """Patch only a new test copy; archived/source-tree originals stay intact."""
    root, context, artifact = Path(root), Path(context), Path(artifact)
    verify(root)
    rows = json.loads((root / "tests/pilot-fixture-extensions.json").read_text()).get("context_patches", [])
    for index, row in enumerate(rows):
        target = context / row["file"]
        if (not row["file"].endswith("_test.go") or target.is_symlink()
                or target.resolve() != context.resolve() / row["file"]
                or not target.resolve().is_relative_to(context.resolve())
                or reference.sha(target) != row["before_sha256"]):
            raise ValueError("native-context fixture patch cannot change its declared original test")
        patch = artifact / (f"fixture-{index}.patch")
        if patch.exists():
            raise ValueError("native fixture patch archive already exists")
        shutil.copyfile(root / row["patch"], patch)
        with (artifact / f"fixture-{index}.log").open("x") as log:
            subprocess.run(["patch", "--batch", "--fuzz=0", "-p1", "-i", str(patch.resolve())],
                           cwd=context, stdout=log, stderr=subprocess.STDOUT, check=True, timeout=10)
        if reference.sha(target) != row["after_sha256"]:
            raise ValueError("native-context fixture patch differs from its declared result")
    (artifact / "fixture-context-patches.json").write_text(json.dumps(rows, indent=2) + "\n")
    return rows


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--baseline",type=Path)
    args=parser.parse_args()
    print(json.dumps(verify(reference.ROOT,args.baseline)))


if __name__=="__main__":main()
