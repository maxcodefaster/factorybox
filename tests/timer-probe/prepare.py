#!/usr/bin/env python3
"""Single-thread, zero-time-offset native experiment, never a production hook.

Runs only in the native test binary's _capture child, with an already validated
sealed request and its original cgroup directory FD. The delivered CLI does
not import this file. The original required tests continue using that CLI.
"""
import ctypes
import hashlib
import json
import os
from pathlib import Path
import select
import signal
import struct
import subprocess
import sys
import time


class Registers(ctypes.Structure):
    _fields_ = [(name, ctypes.c_ulonglong) for name in (
        'r15 r14 r13 r12 rbp rbx r11 r10 r9 r8 rax rcx rdx rsi rdi '
        'orig_rax rip cs eflags rsp ss fs_base gs_base ds es fs gs').split()]


libc = ctypes.CDLL(None, use_errno=True)
libc.ptrace.restype = ctypes.c_long


def ptrace(request, pid, address=0, data=0):
    ctypes.set_errno(0)
    result = libc.ptrace(request, pid, ctypes.c_void_p(address), data if not isinstance(data, int) else ctypes.c_void_p(data))
    if result == -1 and ctypes.get_errno():
        raise OSError(ctypes.get_errno(), 'owned timer experiment ptrace ' + str(request))
    return result


def regs(pid):
    value = Registers()
    ptrace(12, pid, 0, ctypes.byref(value))
    return value


def setregs(pid, value):
    ptrace(13, pid, 0, ctypes.byref(value))


def wait_stop(pid):
    for _ in range(2000):
        actual, status = os.waitpid(pid, os.WNOHANG | 0x40000000)
        if actual == pid:
            if not os.WIFSTOPPED(status):
                raise ValueError('original timer task exited while traced')
            return status
        time.sleep(.001)
    raise ValueError('original timer task did not enter its bounded trace stop')


def memory_read(pid, address, length):
    fd = os.open('/proc/' + str(pid) + '/mem', os.O_RDONLY | os.O_CLOEXEC)
    try:
        data = os.pread(fd, length, address)
        if len(data) != length:
            raise ValueError('short exact source memory read')
        return data
    finally:
        os.close(fd)


def memory_write(pid, address, data):
    for offset in range(0, len(data), 8):
        word = data[offset:offset+8]
        if len(word) != 8:
            word += memory_read(pid, address+offset+len(word), 8-len(word))
        ptrace(5, pid, address+offset, int.from_bytes(word, 'little'))
    if memory_read(pid, address, len(data)) != data:
        raise ValueError('source memory write verification failed')


def main():
    request = json.load(sys.stdin)
    pid = request['pid']
    if not isinstance(pid, int) or pid <= 1 or os.uname().machine != 'x86_64':
        raise ValueError('experiment requires exact native AMD64 PID')
    pidfd = os.pidfd_open(pid)
    alive = select.poll()
    alive.register(pidfd, select.POLLIN)
    group = '/proc/self/fd/3/'
    identity = os.fstat(3)
    membership = Path('/proc/' + str(pid) + '/cgroup').read_text()
    traced = False
    changed = False
    complete = False
    original = instructions = None

    def verify():
        current = os.fstat(3)
        named = os.stat('/sys/fs/cgroup/' + request['runtime_id'])
        if alive.poll(0) or (current.st_dev, current.st_ino) != (identity.st_dev, identity.st_ino) or not os.path.samestat(current, named):
            raise ValueError('pinned task or cgroup changed')
        stat = Path('/proc/' + str(pid) + '/stat').read_text()
        if stat[stat.rindex(')')+1:].split()[19] != request['start']:
            raise ValueError('task birth differs from sealed capture request')
        if Path('/proc/' + str(pid) + '/cgroup').read_text() != membership:
            raise ValueError('task left original owned cgroup')
        members = []
        for directory, children, _ in os.walk(group):
            if len(Path(directory).parts) > 40 or any(Path(directory, child).is_symlink() for child in children):
                raise ValueError('unexpected source cgroup tree')
            members += Path(directory, 'cgroup.threads').read_text().split()
        if members != [str(pid)]:
            raise ValueError('prototype supports one exact owned thread only')
        if len(list(Path('/proc/' + str(pid) + '/task').iterdir())) != 1:
            raise ValueError('prototype requires a single-thread task')

    def freeze(value):
        verify()
        fd = os.open('cgroup.freeze', os.O_WRONLY | os.O_NOFOLLOW | os.O_CLOEXEC, dir_fd=3)
        try:
            os.write(fd, b'1' if value else b'0')
        finally:
            os.close(fd)
        for _ in range(2000):
            if ('frozen ' + ('1' if value else '0')) in Path(group + 'cgroup.events').read_text().splitlines():
                return
            time.sleep(.001)
        raise ValueError('original source freezer transition exceeded bound')

    try:
        verify()
        if 'frozen 1' not in Path(group + 'cgroup.events').read_text().splitlines():
            raise ValueError('experiment requires the already frozen admitted source')
        offsets = Path('/proc/' + str(pid) + '/timens_offsets').read_text().splitlines()
        if any(row.split()[1:] != ['0', '0'] for row in offsets):
            raise ValueError('prototype does not support nonzero time namespace offsets')
        probe = json.loads(subprocess.check_output(['python3', '/timer-probe.py', str(pid)], timeout=5))
        if (probe['restart_function_names'] != ['hrtimer_nanosleep_restart'] or probe['clock_id'] != 1 or
                probe['timespec_type'] not in (0, 1) or not probe['birth_matches'] or not probe['program_released']):
            raise ValueError('exact original relative timer is absent')
        code = Path('/output/timer-stub.bin').read_bytes()
        if not 1 <= len(code) <= 4096 or hashlib.sha256(code).hexdigest() != request['stub_sha256']:
            raise ValueError('native experiment code differs from its compiled input')
        ptrace(0x4206, pid)  # SEIZE only the pinned, single-thread source.
        traced = True
        ptrace(0x4207, pid)  # Queue interrupt before releasing the freezer.
        freeze(False)
        status = wait_stop(pid)
        if os.WSTOPSIG(status) != signal.SIGTRAP:
            raise ValueError('unexpected initial source trace signal')
        original = regs(pid)
        if (original.orig_rax not in (230, 219) or ctypes.c_longlong(original.rax).value != -516 or
                original.rdi != 1 or original.rsi != 0 or original.r10 != int(probe['remaining_user_pointer'], 16) or
                original.cs != 0x33):
            raise ValueError('prototype supports only exact native relative clock_nanosleep/restart_syscall state: ' +
                             json.dumps({name: hex(getattr(original, name)) for name, _ in original._fields_}))
        read_only_request = memory_read(pid, original.rdx, 16)
        redzone = memory_read(pid, original.rsp-8, 8)
        instructions = memory_read(pid, original.rip, 8)
        memory_write(pid, original.rip, b'\x0f\x05\xcc' + instructions[3:])
        changed = True

        def remote(number, args):
            state = Registers.from_buffer_copy(original)
            state.rax, state.orig_rax = number, (1 << 64)-1
            for name, value in zip(('rdi', 'rsi', 'rdx', 'r10', 'r8', 'r9'), args):
                setattr(state, name, value & ((1 << 64)-1))
            setregs(pid, state)
            ptrace(7, pid)
            stopped = wait_stop(pid)
            state = regs(pid)
            if os.WSTOPSIG(stopped) != signal.SIGTRAP or state.rip != original.rip+3:
                raise ValueError('owned remote syscall did not reach its exact trap')
            result = ctypes.c_longlong(state.rax).value
            if -4095 <= result < 0:
                raise OSError(-result, 'owned remote syscall ' + str(number))
            return result

        address = remote(9, (0, 8192, 3, 0x22, -1, 0))  # private RW, never RWX
        frame = bytearray(4096)
        frame[:8] = b'FBXTMR01'
        struct.pack_into('<QqQQ', frame, 8, 1, probe['expires_ns'], 1, original.r10)
        frame[80:88] = redzone
        # A NULL remainder can use an absolute wait. Later captures then use
        # CRIU's existing ERESTARTNOHAND path and retain the same absolute time.
        # Keep the relative kernel path for a non-NULL remainder: real EINTR
        # must still let the kernel write that application's remainder safely.
        struct.pack_into('<Q', frame, 96, 1 if probe['timespec_type'] == 0 else 0)
        frame[128:128+ctypes.sizeof(original)] = bytes(original)
        memory_write(pid, address, code + bytes(4096-len(code)) + frame)
        remote(10, (address, 4096, 5))  # code RX, state remains RW
        memory_write(pid, original.rip, instructions)
        if memory_read(pid, original.rdx, 16) != read_only_request:
            raise ValueError('original application request changed')
        state = Registers.from_buffer_copy(original)
        state.rip, state.rax, state.orig_rax = address, 0, (1 << 64)-1
        setregs(pid, state)
        freeze(True)
        ptrace(17, pid)
        traced = False
        verify()
        complete = True
        print(json.dumps({'schema': 1, 'diagnostic_only': True, 'production_qualified': False,
                          'source_pid': pid, 'source_start': request['start'], 'probe': probe,
                          'original_cgroup_membership': membership.strip(),
                          'continuation_address': hex(address), 'private_bytes': 8192,
                          'code_sha256': request['stub_sha256'], 'original_registers_sha256': hashlib.sha256(bytes(original)).hexdigest(),
                          'original_registers': {name: hex(getattr(original, name)) for name, _ in original._fields_},
                          'request_unchanged': True, 'original_instructions_restored': True,
                          'source_frozen': True, 'tracer_detached': True}))
    finally:
        # Failure after a remote syscall is not claimed to be rollback. Keep
        # the original cgroup frozen and let the unsafe capture guard hold it.
        if traced:
            try:
                if changed and instructions is not None:
                    memory_write(pid, original.rip, instructions)
                if not changed and original is not None:
                    setregs(pid, original)
                freeze(True)
            finally:
                ptrace(17, pid)
        os.close(pidfd)
        if changed and not complete:
            print('timer preparation failed after source mutation; original source held frozen', file=sys.stderr)


if __name__ == '__main__':
    main()
