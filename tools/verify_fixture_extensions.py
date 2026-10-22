#!/usr/bin/env python3
"""Check ordered fixture hashes without replacing their original assertions."""
import argparse
import json
from pathlib import Path
import shutil
import subprocess

import reference


def verify(root, baseline=None):
    root=Path(root)
    extensions=json.loads((root/"tests/pilot-fixture-extensions.json").read_text())
    previous=json.loads((root/extensions["previous_manifest"]).read_text())
    if extensions["schema"]!=1 or not extensions["assertions_preserved"] or extensions["upstream_assertions_modified"]:
        raise ValueError("fixture declaration weakens original assertions")
    for name,row in previous["files"].items():
        final=extensions["files"].get(name,row)
        if name in extensions["files"] and final["before_sha256"]!=row["after_sha256"]:
            raise ValueError("fixture extension does not continue the declared before/after chain: "+name)
        if reference.sha(root/name)!=final["after_sha256"]:
            raise ValueError("fixture differs from its declared final hash: "+name)
    for name,row in extensions["files"].items():
        if reference.sha(root/name)!=row["after_sha256"]:raise ValueError("pilot fixture differs: "+name)
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
            "before_files_verified":baseline is not None,"upstream_assertions_modified":False}


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
