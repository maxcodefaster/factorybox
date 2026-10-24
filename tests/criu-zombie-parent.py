"""Run real C/R and preserve the original cross-namespace parent's wait status."""
import ctypes
import hashlib
import json
import os
from pathlib import Path
import select
import signal
import struct
import subprocess
import time

OUT = Path('/output')


def image_pids(path):
    data = path.read_bytes()
    offset, pids = 8, []
    def varint(data, at):
        value = shift = 0
        while at < len(data):
            byte = data[at]
            at += 1
            value |= (byte & 127) << shift
            if byte < 128:
                return value, at
            shift += 7
            if shift > 70:
                raise ValueError('overlong protobuf value')
        raise ValueError('truncated protobuf value')
    while offset < len(data):
        size, = struct.unpack_from('<I', data, offset)
        offset += 4
        entry = data[offset:offset + size]
        assert len(entry) == size
        offset += size
        key, pos = varint(entry, 0)
        assert key == 8
        pid, pos = varint(entry, pos)
        pids.append(pid)
    assert offset == len(data)
    return pids


def execute(command, log, timeout=30):
    with (OUT / log).open('xb') as stream:
        return subprocess.run(command, stdout=stream, stderr=subprocess.STDOUT, timeout=timeout).returncode


def main():
    assert os.getpid() == 1
    assert ctypes.CDLL(None).prctl(36, 1, 0, 0, 0) == 0
    outcome = {'schema': 1, 'passed': False, 'production_qualified': False, 'scope': __doc__}
    descriptors = []
    try:
        assert execute(['cc', '-std=c11', '-Wall', '-Wextra', '-Werror', '/fixture.c', '-o', '/tmp/fixture'], 'compile.log') == 0
        with (OUT / 'actor.log').open('xb') as log:
            actor = subprocess.Popen(['/tmp/fixture', str(OUT / 'ready.json'), str(OUT / 'actor-result.json')],
                                     stdout=log, stderr=subprocess.STDOUT)
        for _ in range(1000):
            if (OUT / 'ready.json').exists():
                break
            if actor.poll() is not None:
                raise ValueError('native actor failed before readiness')
            time.sleep(.005)
        ready = json.loads((OUT / 'ready.json').read_text())
        assert ready['parent'] == actor.pid and ready['child_own_pid'] == 2
        assert ready['parent_namespace'] != ready['child_namespace']
        expected = [ready[name] for name in ['parent', 'init', 'zombie']]
        descriptors = [os.pidfd_open(pid) for pid in expected]
        outcome['ready'] = ready
        images = OUT / 'images'
        images.mkdir()
        common = ['--images-dir', str(images), '--shell-job', '-v4']
        started = time.monotonic()
        dumped = execute(['/criu', 'dump', '--tree', str(actor.pid), '--leave-running', '--nested-ns',
                          '--log-file', 'dump.log', *common], 'dump-command.log')
        outcome.update(dump_exit_code=dumped, dump_seconds=time.monotonic() - started)
        if (images / 'pstree.img').exists():
            outcome['saved_pids'] = image_pids(images / 'pstree.img')
        if dumped:
            raise ValueError('native cross-namespace zombie dump failed')
        # Match Factorybox's actual capture-then-stop boundary. PID 1 must
        # reap the outside parent's orphaned zombie while its inner init dies;
        # blocking in CRIU's ordinary kill/wait path would prevent that reap.
        for fd in descriptors:
            try:
                signal.pidfd_send_signal(fd, signal.SIGKILL)
            except ProcessLookupError:
                pass
        actor.wait(timeout=5)
        deadline = time.monotonic() + 5
        while time.monotonic() < deadline:
            try:
                child, status = os.waitpid(-1, os.WNOHANG)
                if not child:
                    time.sleep(.005)
            except ChildProcessError:
                break
        for fd in descriptors:
            poll = select.poll()
            poll.register(fd, select.POLLIN)
            assert poll.poll(5000), 'capture-then-stop did not terminate every original process'
        while True:
            try:
                child, status = os.waitpid(-1, os.WNOHANG)
                if not child:
                    break
            except ChildProcessError:
                break
        outcome['all_original_processes_terminated'] = True
        if set(outcome.get('saved_pids', [])) != set(expected):
            raise ValueError('successful dump published an incomplete process tree')
        restored = execute(['/criu', 'restore', '--restore-detached', '--pidfile', str(OUT / 'restored.pid'),
                            '--log-file', 'restore.log', *common], 'restore-command.log')
        outcome['restore_exit_code'] = restored
        if restored:
            raise ValueError('native cross-namespace zombie restore failed')
        pid = int((OUT / 'restored.pid').read_text())
        assert pid == actor.pid
        fd = os.pidfd_open(pid)
        descriptors.append(fd)
        signal.pidfd_send_signal(fd, signal.SIGTERM)
        poll = select.poll()
        poll.register(fd, select.POLLIN)
        assert poll.poll(15000), 'restored parent failed to complete its original wait assertions'
        waited, status = os.waitpid(pid, 0)
        assert waited == pid and os.waitstatus_to_exitcode(status) == 0
        proof = json.loads((OUT / 'actor-result.json').read_text())
        assert proof == {'passed': True, 'waitable_child_pid': ready['zombie'], 'exit_status': 47}
        outcome.update(passed=True, proof=proof)
    except BaseException as error:
        outcome['error'] = repr(error)
    finally:
        for fd in descriptors:
            try:
                signal.pidfd_send_signal(fd, signal.SIGKILL)
            except ProcessLookupError:
                pass
            os.close(fd)
        (OUT / 'native-result.json').write_text(json.dumps(outcome, indent=2) + '\n')
        uid, gid = int(os.environ['HOST_UID']), int(os.environ['HOST_GID'])
        for root, directories, files in os.walk(OUT, followlinks=False):
            for name in files + directories:
                os.chown(Path(root) / name, uid, gid, follow_symlinks=False)
        os.chown(OUT, uid, gid)
        print(json.dumps(outcome), flush=True)
    raise SystemExit(0 if outcome['passed'] else 1)


if __name__ == '__main__':
    main()
