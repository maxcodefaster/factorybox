#!/usr/bin/env python3
"""Observe fixed global kernel sentinels without writing any kernel value."""
import json
import pathlib
import sys

NAMES = [
    "kernel/panic", "kernel/panic_on_oops", "kernel/sysrq", "kernel/modules_disabled",
    "kernel/perf_event_paranoid", "kernel/kptr_restrict", "kernel/dmesg_restrict",
    "vm/overcommit_memory", "vm/swappiness", "vm/max_map_count", "vm/nr_hugepages",
    "fs/protected_hardlinks", "fs/protected_symlinks", "fs/protected_fifos",
    "fs/protected_regular", "user/max_user_namespaces",
]

values = {}
for name in NAMES:
    try:
        values[name] = (pathlib.Path("/proc/sys") / name).read_text().strip()
    except FileNotFoundError:
        values[name] = None
phase = sys.argv[1]
if phase not in {"before", "after"}:
    raise ValueError("requires a before or after observation")
path = pathlib.Path("/output/sysctls-" + phase + ".json")
path.write_text(json.dumps(values, indent=2, sort_keys=True) + "\n")
if phase == "after":
    original = json.loads(pathlib.Path("/output/sysctls-before.json").read_text())
    changed = [name for name in NAMES if values[name] != original[name]]
    if changed:
        raise ValueError("observed global kernel sentinels changed: " + ", ".join(changed))
