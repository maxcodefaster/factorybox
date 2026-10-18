#!/usr/bin/env python3
"""One-time import of the sealed, successful developer lifecycle fixture."""
import argparse
import collections
import hashlib
import json
import pathlib
import re

import import_reference

ROOT = import_reference.ROOT
TARGET = ROOT / "tests/reference/fixture"
ARCHIVE = import_reference.FULL


def normalized(name):
    return re.sub(r"\bbranch-[0-9a-f]{32}\b", "branch-IDENTITY", name)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("experiment", type=pathlib.Path)
    options = parser.parse_args()
    if TARGET.exists():
        raise ValueError("fixture already imported; preserve its original bytes")
    archive = options.experiment / "artifacts" / ARCHIVE
    manifest = json.loads((archive / "manifest.json").read_text())
    imported = {}

    def read(relative):
        data = (archive / relative).read_bytes()
        if hashlib.sha256(data).hexdigest() != manifest[relative]:
            raise ValueError("sealed fixture differs: " + relative)
        imported[relative] = manifest[relative]
        return data

    intent = json.loads(read("artifacts/sysbox-criu-run-intent.json"))
    if intent["exit_code"] != 0 or intent["phase"] != "observed":
        raise ValueError("reference lifecycle did not pass")
    files = {"pod.json": read("artifacts/criu-pod.json")}
    for name in ["theia-client.mjs", "theia-client.cjs", "theia-client-build.json"]:
        files[name] = read("artifacts/descendant-fork-prototype/" + name)
    records = [json.loads(line) for line in read("sysbox-criu-results.jsonl").splitlines()
               if line.startswith(b"{")]
    if not records or any(row["status"] != "passed" for row in records):
        raise ValueError("reference contains failed records")
    files["expected-records.json"] = (json.dumps(dict(sorted(collections.Counter(
        normalized(row["name"]) for row in records).items())), indent=2) + "\n").encode()
    TARGET.mkdir(parents=True)
    for name, data in files.items():
        (TARGET / name).write_bytes(data)
    (TARGET / "provenance.json").write_text(json.dumps({
        "archive": ARCHIVE,
        "archive_manifest_sha256": import_reference.digest(
            (archive / "manifest.json").read_bytes()),
        "source_files_sha256": imported,
        "files_sha256": {name: hashlib.sha256(data).hexdigest()
                         for name, data in files.items()},
        "scope": "Immutable test fixture, not the public lifecycle API. "
                 "Pod identities and rebuilt component images change in the owned runner.",
    }, indent=2, sort_keys=True) + "\n")


if __name__ == "__main__":
    main()
