"""Read only the worker's IPC-namespace SHM limits before/after its child probe."""
import json
from pathlib import Path
import sys

phase = sys.argv[1]
if phase not in ["before", "after"]:
    raise ValueError("requires one bounded observation phase")
values = {name: Path("/proc/sys/kernel", name).read_text()
          for name in ["shmmni", "shmmax", "shmall"]}
Path("/output", "shm-" + phase + ".json").write_text(json.dumps(values, sort_keys=True) + "\n")
if phase == "after" and values != json.loads(Path("/output/shm-before.json").read_text()):
    raise ValueError("child probe changed its parent IPC namespace's SHM limits")
