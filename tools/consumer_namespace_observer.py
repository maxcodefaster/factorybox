#!/usr/bin/env python3
"""Read one owned consumer init's namespace access without changing that process."""
import fcntl
import json
import os
from pathlib import Path
import subprocess
import sys


def require_pid(pid):
    if type(pid) is not int or pid <= 1:
        raise ValueError("requires the original owned consumer init PID")


def namespace_access(pid):
    require_pid(pid)
    result = {"observer_userns": os.readlink("/proc/self/ns/user"),
              "observer_uid": os.getresuid(), "observer_gid": os.getresgid()}
    for name in ["user", "net", "pid", "mnt"]:
        path = f"/proc/{pid}/ns/{name}"
        try:
            result[name] = os.readlink(path)
            info = os.stat(path, follow_symlinks=False)
            result[name + "_link_owner"] = [info.st_uid, info.st_gid]
        except OSError as error:
            result[name + "_error"] = str(error)
    try:
        fd = os.open(f"/proc/{pid}/ns/net", os.O_RDONLY | os.O_CLOEXEC)
        try:
            owner = fcntl.ioctl(fd, 0xb701, 0)  # NS_GET_USERNS, read only.
            try:
                result["net_owner_userns"] = os.readlink(f"/proc/self/fd/{owner}")
            finally:
                os.close(owner)
        finally:
            os.close(fd)
    except OSError as error:
        result["net_owner_error"] = str(error)
    return result


def inside_command(pid):
    require_pid(pid)
    # Only this short-lived observer enters the target user namespace. It
    # keeps the worker's /proc and network namespace; the target is untouched.
    return ["nsenter", f"--user=/proc/{pid}/ns/user", "--setgid=0", "--setuid=0",
            "--", sys.executable, str(Path(__file__).resolve()), "--inside", str(pid)]


def observe(pid):
    require_pid(pid)
    result = namespace_access(pid)
    proc = Path(f"/proc/{pid}")
    keys = ("Name:", "Uid:", "Gid:", "CapEff:", "CapPrm:", "CapBnd:",
            "TracerPid:", "NoNewPrivs:", "Seccomp:", "Threads:")
    result["threads"] = {
        task.name: [line for line in (task / "status").read_text().splitlines()
                    if line.startswith(keys)]
        for task in (proc / "task").iterdir()
    }
    info = proc.stat()
    result["proc_owner"] = [info.st_uid, info.st_gid]
    child = subprocess.run(inside_command(pid), capture_output=True, text=True, timeout=5)
    result["inside"] = {"exit": child.returncode, "stdout": child.stdout, "stderr": child.stderr}
    return result


if __name__ == "__main__":
    if len(sys.argv) == 3 and sys.argv[1] == "--inside":
        print(json.dumps(namespace_access(int(sys.argv[2]))))
    else:
        print(json.dumps(observe(int(sys.argv[1]))))
