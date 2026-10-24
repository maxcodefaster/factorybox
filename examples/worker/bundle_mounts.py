#!/usr/bin/env python3
"""Release only this example's previously recorded administrator-owned mounts."""
import json
import os
from pathlib import Path
import re
import subprocess
import sys


def mount_rows(paths):
    rows = {}
    for line in Path("/proc/self/mountinfo").read_text().splitlines():
        fields = line.split()
        target = re.sub(r"\\([0-7]{3})", lambda m: chr(int(m[1], 8)), fields[4])
        if any(target == path or target.startswith(path + "/") for path in paths):
            if target not in paths or target in rows:
                raise ValueError("unexpected nested or stacked input mount")
            info = os.stat(target, follow_symlinks=False)
            rows[target] = {"mountinfo": line, "device": info.st_dev,
                            "inode": info.st_ino, "mode": info.st_mode}
    return rows


def inventory(bundle):
    root = Path(bundle)
    if str(root.resolve()) != bundle or not root.is_absolute():
        raise ValueError("bundle must retain its canonical private path")
    paths = [str(root / name) for name in ("rootfs", "lower")]
    rows = mount_rows(paths)
    if set(rows) != set(paths):
        raise ValueError("both original input mounts must remain present")
    overlay = rows[paths[0]]["mountinfo"].split(" - ", 1)[1].split()
    options = set(overlay[2].split(","))
    if overlay[0] != "overlay" or not {
            "lowerdir=" + paths[1], "upperdir=" + str(root / "upper"),
            "workdir=" + str(root / "work"), "index=off"} <= options:
        raise ValueError("input overlay recipe differs")
    if "ro" not in rows[paths[1]]["mountinfo"].split()[5].split(","):
        raise ValueError("input base alias is not read-only")
    return {"schema": 1, "bundle": bundle, "mounts": rows}


def main():
    if len(sys.argv) not in (3, 4) or sys.argv[1] not in ("inspect", "release"):
        raise ValueError("expected inspect BUNDLE or release BUNDLE EXACT_IDENTITY_JSON")
    current = inventory(sys.argv[2])
    if sys.argv[1] == "release":
        if len(sys.argv) != 4 or current != json.loads(sys.argv[3]):
            raise ValueError("input mount ownership changed; retain all backing")
        # Recheck the complete pair immediately before ordinary unmount. The
        # example is the sole administrator of its private prepared bundle.
        if inventory(sys.argv[2]) != current:
            raise ValueError("input mounts changed during verification")
        for name in ("rootfs", "lower"):
            target = str(Path(sys.argv[2]) / name)
            if mount_rows([target]) != {target: current["mounts"][target]}:
                raise ValueError("input mount changed before release")
            subprocess.run(["umount", "--", target], check=True)
        current["released"] = True
    print(json.dumps(current))


if __name__ == "__main__":
    main()
