"""Own all mounts and helpers for deterministic kernel busy-decision controls."""
import ctypes
import errno
import json
import os
from pathlib import Path
import subprocess
import sys
import time


def mount_id(path):
    found = [int(line.split()[0]) for line in Path('/proc/self/mountinfo').read_text().splitlines()
             if line.split()[4] == path]
    if len(found) != 1:
        raise ValueError('requires one exact freshly owned mount: ' + path)
    return found[0]


if len(sys.argv) == 4 and sys.argv[1] == 'hold':
    mode, target = sys.argv[2:]
    before = {'inner_pid': os.getpid(), 'outer_pid': int(os.readlink('/proc/self')),
              'pid_namespace': os.stat('/proc/self/ns/pid').st_ino,
              'mount_namespace': os.stat('/proc/self/ns/mnt').st_ino,
              'mount_id': mount_id(target)}
    fd = None
    if mode == 'file':
        fd = os.open(target, os.O_PATH | os.O_DIRECTORY | os.O_CLOEXEC)
    elif mode == 'cwd':
        os.chdir(target)
    elif mode == 'root':
        os.chroot(target)
        os.chdir('/')
    else:
        raise ValueError('unknown holder mode')
    print(json.dumps(before), flush=True)
    assert sys.stdin.readline() == 'release\n'
    if fd is not None:
        os.close(fd)
    if mode == 'cwd':
        os.chdir('/')
    print('released', flush=True)
    raise SystemExit(0)


assert len(sys.argv) == 1
libc = ctypes.CDLL(None, use_errno=True)
libc.umount2.argtypes = [ctypes.c_char_p, ctypes.c_int]
libc.umount2.restype = ctypes.c_int


def attempt(target):
    start = time.monotonic_ns()
    result = libc.umount2(target.encode(), 0)
    error = ctypes.get_errno() if result else 0
    end = time.monotonic_ns()
    return {'start_ns': start, 'end_ns': end, 'result': result, 'errno': error}


def mount(target):
    Path(target).mkdir()
    subprocess.run(['mount', '-t', 'tmpfs', '-o', 'size=1m,nosuid,nodev', 'tmpfs', target], check=True)


outer_pidns = os.stat('/proc/self/ns/pid').st_ino
outer_mntns = os.stat('/proc/self/ns/mnt').st_ino
cases = []


def progress():
    Path('/output/control-progress.json').write_text(json.dumps({
        'caller_pid': os.getpid(), 'pid_namespace': outer_pidns,
        'mount_namespace': outer_mntns, 'cases': cases}, indent=2) + '\n')


for mode in ['file', 'cwd', 'root', 'submount', 'peer']:
    target = '/work/' + mode
    mount(target)
    held = target
    peer = None
    if mode == 'submount':
        held = target + '/leaf'
        mount(held)
    elif mode == 'peer':
        Path(target + '/leaf').mkdir()
        subprocess.run(['mount', '--make-shared', target], check=True)
        peer = target + '-peer'
        Path(peer).mkdir()
        subprocess.run(['mount', '--bind', target, peer], check=True)
        subprocess.run(['mount', '-t', 'tmpfs', '-o', 'size=1m,nosuid,nodev', 'tmpfs', target + '/leaf'], check=True)
        held, target = peer + '/leaf', target + '/leaf'
    original_target, original_held = mount_id(target), mount_id(held)
    case = {'mode': mode, 'target_mount_id': original_target, 'held_mount_id': original_held}
    cases.append(case)
    progress()
    holder = subprocess.Popen(['unshare', '--pid', '--fork', '/usr/bin/python3', '/control.py', 'hold',
                               mode if mode in ('file', 'cwd', 'root') else 'file', held],
                              stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
    identity = json.loads(holder.stdout.readline())
    case['helper'] = identity
    progress()
    assert identity['inner_pid'] == 1 and identity['outer_pid'] > 1
    assert identity['pid_namespace'] != outer_pidns and identity['mount_namespace'] == outer_mntns
    assert identity['mount_id'] == original_held
    blocked = attempt(target)
    case['blocked'] = blocked
    progress()
    assert blocked['result'] == -1 and blocked['errno'] == errno.EBUSY, (mode, blocked)
    holder.stdin.write('release\n')
    holder.stdin.flush()
    assert holder.stdout.readline().strip() == 'released'
    assert holder.wait(timeout=8) == 0, holder.stderr.read()
    if mode == 'submount':
        assert attempt(held)['result'] == 0
    success = attempt(target)
    case['success'] = success
    progress()
    assert success['result'] == 0, (mode, success)
    if mode == 'peer':
        assert not any(line.split()[4] == held for line in Path('/proc/self/mountinfo').read_text().splitlines())
        assert attempt(peer)['result'] == 0
        assert attempt('/work/peer')['result'] == 0
proof = {'schema': 1, 'passed': True, 'caller_pid': os.getpid(), 'pid_namespace': outer_pidns,
         'mount_namespace': outer_mntns, 'cases': cases,
         'scope': 'Actual ordinary EBUSY and success after exact release for owned file/cwd/root/submount/propagation-peer controls. '
                  'No retry, lazy unmount, runtime repair or historical failure attribution.'}
Path('/output/controls.json').write_text(json.dumps(proof, indent=2) + '\n')
print(json.dumps(proof), flush=True)
