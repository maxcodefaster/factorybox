#!/usr/bin/env python3
"""Read the original consumer cgroup during one owned Resume; never freeze or move tasks."""
import json
import os
from pathlib import Path
import re
import sys
import time


STATUS_FIELDS = {'Name', 'State', 'Tgid', 'Pid', 'PPid', 'TracerPid',
                 'NStgid', 'NSpid', 'SigPnd', 'ShdPnd', 'Threads'}
FLAGS = os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC


def validate_request(request):
    if (not isinstance(request, dict) or
            set(request) != {'runtime_id', 'cgroup', 'observer_id'} or
            not isinstance(request['runtime_id'], str) or
            not re.fullmatch(r'fbx-[a-f0-9]{32}', request['runtime_id']) or
            not isinstance(request['observer_id'], str) or
            not re.fullmatch(r'[a-f0-9]{32}', request['observer_id']) or
            not isinstance(request['cgroup'], dict) or
            set(request['cgroup']) != {'device', 'inode', 'mount_id'} or
            any(type(value) is not int or value <= 0 for value in request['cgroup'].values())):
        raise ValueError('Requires one exact original consumer cgroup identity')
    return request


def identity(fd):
    info = os.fstat(fd)
    fields = dict(line.split(':', 1) for line in Path(f'/proc/self/fdinfo/{fd}').read_text().splitlines()
                  if ':' in line)
    return {'device': info.st_dev, 'inode': info.st_ino, 'mount_id': int(fields['mnt_id'])}


def read(fd, name):
    file = os.open(name, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC, dir_fd=fd)
    with os.fdopen(file) as stream:
        data = stream.read(16385)
    if len(data) > 16384:
        raise ValueError('Cgroup observation exceeds its bounded counter size')
    return data.strip()


def task_status(tid):
    # These fields contain task state and identity, never argv or environment.
    try:
        status = Path(f'/proc/{tid}/status').read_text()
        return dict(line.split(':', 1) for line in status.splitlines()
                    if line.split(':', 1)[0] in STATUS_FIELDS)
    except FileNotFoundError:
        return {'exited_before_read': True}


def subtree(fd, prefix='', depth=0):
    if depth > 8:
        raise ValueError('Consumer cgroup depth exceeds observation bound')
    result = []
    threads = read(fd, 'cgroup.threads').split()
    if len(threads) > 512:
        raise ValueError('Consumer task count exceeds its worker ceiling')
    result.append({'path': prefix, 'identity': identity(fd),
                   'events': read(fd, 'cgroup.events'), 'freeze': read(fd, 'cgroup.freeze'),
                   'procs': read(fd, 'cgroup.procs'),
                   'tasks': {tid: task_status(tid) for tid in threads}})
    for name in sorted(os.listdir(fd)):
        try:
            child = os.open(name, FLAGS, dir_fd=fd)
        except NotADirectoryError:
            continue
        except FileNotFoundError:
            continue
        try:
            result.extend(subtree(child, prefix+'/'+name, depth+1))
        finally:
            os.close(child)
        if len(result) > 64:
            raise ValueError('Consumer cgroup count exceeds observation bound')
    return result


def observe(request):
    validate_request(request)
    runtime = request['runtime_id']
    membership = Path('/proc/self/cgroup').read_text()
    if any(line == '0::/'+runtime or line.startswith('0::/'+runtime+'/')
           for line in membership.splitlines()):
        raise ValueError('Observer must remain outside its consumer cgroup')
    fd = os.open('/sys/fs/cgroup/'+runtime, FLAGS)
    output = Path('/tmp/factorybox-cgroup-'+request['observer_id']+'.json')
    stop = output.with_suffix('.stop')
    result = {'schema': 1, 'request': request, 'observer_membership': membership,
              'read_only': True, 'complete': False, 'samples': []}
    started = time.monotonic()
    previous = None
    detailed_at = -1.0
    try:
        if identity(fd) != request['cgroup']:
            raise ValueError('Original consumer cgroup identity changed')
        result['worker_memory_before'] = Path('/sys/fs/cgroup/memory.events').read_text()
        # The host waits for this exact handshake before issuing Resume.
        print(json.dumps({'ready': True, 'observer_id': request['observer_id']}), flush=True)
        while time.monotonic()-started < 90:
            if identity(fd) != request['cgroup']:
                raise ValueError('Original consumer cgroup identity changed during observation')
            now = time.monotonic()-started
            state = {'events': read(fd, 'cgroup.events'), 'freeze': read(fd, 'cgroup.freeze'),
                     'procs': read(fd, 'cgroup.procs')}
            waiting = state['freeze'] == '1' and 'frozen 0' in state['events'].splitlines()
            if state != previous or waiting and now-detailed_at >= 0.1:
                if len(result['samples']) >= 128:
                    raise ValueError('Cgroup observation sample bound reached')
                sample = {'seconds': now, **state, 'memory_events': read(fd, 'memory.events'),
                          'worker_memory_events': Path('/sys/fs/cgroup/memory.events').read_text()}
                if waiting or state != previous:
                    sample['subtree'] = subtree(fd)
                    detailed_at = now
                result['samples'].append(sample)
                previous = state
            if stop.exists():
                result['complete'] = True
                break
            time.sleep(0.01)
        else:
            result['error'] = 'Observer deadline reached without the exact stop marker'
    except Exception as error:
        result['error'] = str(error)
    finally:
        result['worker_memory_after'] = Path('/sys/fs/cgroup/memory.events').read_text()
        os.close(fd)
        with output.open('x') as stream:
            stream.write(json.dumps(result, indent=2)+'\n')
        print(json.dumps({'saved': str(output), 'complete': result['complete']}), flush=True)
    return result


if __name__ == '__main__':
    encoded = sys.stdin.buffer.readline(65537)
    if len(encoded) > 65536:
        raise ValueError('Observer identity exceeds bound')
    observe(json.loads(encoded))
