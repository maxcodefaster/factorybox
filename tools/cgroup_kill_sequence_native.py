#!/usr/bin/env python3
"""Diagnose clone3/cgroup.kill on the native host without Sysbox or CRIU."""
import argparse
import json
import shutil
import subprocess
import uuid

import build_worker
import native_base
import reference


PROBE = r'''
import ctypes, json, os, signal, sys
owner = sys.argv[1]
assert len(owner) == 32 and all(c in '0123456789abcdef' for c in owner)
assert os.getpid() == 1
libc = ctypes.CDLL(None, use_errno=True)
libc.syscall.restype = ctypes.c_long
class CloneArgs(ctypes.Structure):
    _fields_ = [(name, ctypes.c_uint64) for name in (
        'flags', 'pidfd', 'child_tid', 'parent_tid', 'exit_signal', 'stack',
        'stack_size', 'tls', 'set_tid', 'set_tid_size', 'cgroup')]
groups, children = [], set()
def group(suffix):
    path = '/sys/fs/cgroup/kill-sequence-' + owner + '-' + suffix
    os.mkdir(path, 0o700)
    fd = os.open(path, os.O_RDONLY | os.O_DIRECTORY | os.O_CLOEXEC)
    groups.append((path, fd))
    return path, fd
def clone(fd, hold=False):
    args = CloneArgs(flags=0x200000000, exit_signal=signal.SIGCHLD, cgroup=fd)
    pid = libc.syscall(435, ctypes.byref(args), ctypes.sizeof(args))
    if pid < 0:
        raise OSError(ctypes.get_errno(), 'native clone3(CLONE_INTO_CGROUP)')
    if pid == 0:
        if hold:
            while True:
                signal.pause()
        os._exit(0)
    children.add(pid)
    return pid
def reap(pid):
    actual, status = os.waitpid(pid, 0)
    assert actual == pid
    children.remove(pid)
    return {'exited': os.WIFEXITED(status),
            'exit': os.WEXITSTATUS(status) if os.WIFEXITED(status) else None,
            'signal': os.WTERMSIG(status) if os.WIFSIGNALED(status) else None}
def require_success(observed):
    assert observed == {'exited': True, 'exit': 0, 'signal': None}, observed
try:
    target, fd = group('killed')
    before = reap(clone(fd))
    require_success(before)
    with open(target + '/cgroup.kill', 'w') as stream:
        stream.write('1')
    after = reap(clone(fd))
    _, fresh = group('fresh')
    sibling = reap(clone(fresh))
    require_success(sibling)
    _, targeted = group('pidfd')
    pid = clone(targeted, hold=True)
    pidfd = os.pidfd_open(pid)
    try:
        signal.pidfd_send_signal(pidfd, signal.SIGKILL)
        killed = reap(pid)
    finally:
        os.close(pidfd)
    assert killed['signal'] == signal.SIGKILL, killed
    after_pidfd = reap(clone(targeted))
    require_success(after_pidfd)
    affected = after == {'exited': False, 'exit': None, 'signal': signal.SIGKILL}
    if not affected:
        require_success(after)
    print(json.dumps({'diagnostic_completed': True, 'kernel_affected': affected,
        'before': before, 'after_cgroup_kill': after, 'fresh_sibling': sibling,
        'after_pidfd_kill': after_pidfd, 'syscall': 435,
        'production_qualified': False}))
finally:
    for pid in list(children):
        pidfd = os.pidfd_open(pid)
        try:
            signal.pidfd_send_signal(pidfd, signal.SIGKILL)
            reap(pid)
        finally:
            os.close(pidfd)
    for path, fd in reversed(groups):
        assert os.stat(path).st_ino == os.fstat(fd).st_ino
        os.close(fd)
        os.rmdir(path)
'''


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--worker-build", required=True)
    args = parser.parse_args()
    _, worker = build_worker.load(args.worker_build)
    host = json.loads(reference.docker(["info", "--format", "{{json .}}"]))
    native_base.require_native_host(worker["platform"], host)
    owner = uuid.uuid4().hex
    name = "factorybox-cgroup-kill-sequence-" + owner
    artifact = reference.new_artifact(name)
    artifact.mkdir()
    shutil.copyfile(__file__, artifact / "runner.py")
    result = {"owner": owner, "passed": False, "cleanup": False,
              "platform": worker["platform"], "image": worker["native_base_image"],
              "host_kernel": host["KernelVersion"], "production_qualified": False,
              "scope": "Owned native kernel diagnosis only. No Sysbox, CRIU, Factory, "
                       "host installation, host sysctl change or kernel update. "
                       "A completed diagnosis is not platform qualification."}
    command = ["run", "--pull=never", "--name", name,
               "--label", "com.factorybox.run=" + owner, "--privileged",
               "--cgroupns=private", "--cpus=1", "--memory=256m", "--pids-limit=32",
               "--network=none", "--entrypoint", "python3",
               worker["native_base_image"], "-c", PROBE, owner]
    result["command"] = command
    print(json.dumps({"running": str(artifact)}), flush=True)
    try:
        done = subprocess.run([*reference.DOCKER, *command], capture_output=True,
                              text=True, timeout=45)
        (artifact / "stdout.txt").write_text(done.stdout)
        (artifact / "stderr.txt").write_text(done.stderr)
        result["exit"] = done.returncode
        done.check_returncode()
        result["observation"] = json.loads(done.stdout)
        if result["observation"].get("diagnostic_completed") is not True:
            raise ValueError("native kernel control did not complete")
        result["passed"] = True
    except Exception as error:
        result["error"] = str(error)
    finally:
        reference.owned_delete("container", name, owner)
        result["cleanup"] = True
        (artifact / "result.json").write_text(json.dumps(result, indent=2) + "\n")
    print(json.dumps({"evidence": str(artifact), "passed": result["passed"],
                      "cleanup": result["cleanup"],
                      "observation": result.get("observation"),
                      "error": result.get("error")}), flush=True)
    return 0 if result["passed"] else 1


if __name__ == "__main__":
    raise SystemExit(main())
