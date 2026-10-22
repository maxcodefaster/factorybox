#!/usr/bin/env python3
"""Apply one exact optional consumer patch to a fresh, owned unpacked rootfs."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import stat


def transform(data, patch):
    if patch["schema"] != 1 or patch["order"] != 1 or not patch["assertions_preserved"] or patch["upstream_assertions_modified"]:
        raise ValueError("invalid ordered consumer patch")
    if hashlib.sha256(data).hexdigest() != patch["before_sha256"]:
        raise ValueError("Dev Containers input differs from the exact unpatched consumer")
    old, new = patch["find"].encode(), patch["replace"].encode()
    if data.count(old) != 1:
        raise ValueError("consumer patch anchor is not unique")
    changed = data.replace(old, new)
    if hashlib.sha256(changed).hexdigest() != patch["after_sha256"]:
        raise ValueError("consumer patch result differs from its declared bytes")
    return changed


def apply(root, patch):
    root = Path(root)
    path = root / patch["file"]
    if not root.is_absolute() or root.resolve() != root or path.resolve() != path or not path.is_relative_to(root):
        raise ValueError("consumer patch needs an exact unpacked tree without path aliases")
    info = path.lstat()
    if not stat.S_ISREG(info.st_mode) or info.st_nlink != 1:
        raise ValueError("consumer patch refuses an aliased input")
    changed = transform(path.read_bytes(), patch)
    temporary = path.with_name(path.name + ".factorybox-next")
    with temporary.open("xb") as output:
        output.write(changed)
        output.flush()
        os.fsync(output.fileno())
    os.chmod(temporary, stat.S_IMODE(info.st_mode))
    os.chown(temporary, info.st_uid, info.st_gid)
    os.replace(temporary, path)
    return {"schema": 1, "file": patch["file"], "before_sha256": patch["before_sha256"],
            "after_sha256": patch["after_sha256"], "assertions_preserved": True,
            "upstream_assertions_modified": False, "selected": False}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", type=Path, required=True)
    parser.add_argument("--patch", type=Path, required=True)
    parser.add_argument("--receipt", type=Path, required=True)
    options = parser.parse_args()
    receipt = apply(options.root, json.loads(options.patch.read_text()))
    with options.receipt.open("x") as output:
        json.dump(receipt, output, indent=2)
        output.write("\n")


if __name__ == "__main__":
    main()
