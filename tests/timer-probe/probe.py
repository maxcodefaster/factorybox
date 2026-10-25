#!/usr/bin/env python3
"""Read one frozen owned task's restart block through a temporary BPF iterator.

Diagnostic only. This program writes no task memory, pins no BPF object and
attaches no event hook. The iterator emits only the task with the requested
PID in the caller's own PID namespace. Closing its descriptors removes it.
"""
import ctypes
import hashlib
import json
import os
from pathlib import Path
import struct
import sys
import time
import select


class BTF:
    def __init__(self, data):
        magic, version, flags, header, off, size, strings, strings_size = struct.unpack_from('<HBB5I', data)
        if magic != 0xeb9f or version != 1 or flags or len(data) > 32 << 20:
            raise ValueError('unsupported bounded native BTF')
        self.strings = data[header + strings:header + strings + strings_size]
        self.types = [None]
        cursor, end = header + off, header + off + size
        fixed = {1: 4, 2: 0, 3: 12, 7: 0, 8: 0, 9: 0, 10: 0, 11: 0,
                 12: 0, 14: 4, 16: 0, 17: 4, 18: 0}
        repeated = {4: 12, 5: 12, 6: 8, 13: 8, 15: 12, 19: 12}
        while cursor < end:
            name, info, value = struct.unpack_from('<III', data, cursor)
            cursor += 12
            kind, count = (info >> 24) & 127, info & 0xffffff
            tail = repeated[kind] * count if kind in repeated else fixed[kind]
            if cursor + tail > end:
                raise ValueError('BTF record exceeds its bound')
            members = []
            if kind in (4, 5):
                for index in range(count):
                    member, target, bits = struct.unpack_from('<III', data, cursor + 12 * index)
                    if info >> 31:
                        if bits >> 24:
                            continue
                        bits &= 0xffffff
                    if bits % 8:
                        continue
                    members.append((self.string(member), target, bits // 8))
            record = {'name': self.string(name), 'kind': kind, 'value': value, 'members': members}
            if kind == 3:
                record['element_type'] = struct.unpack_from('<I', data, cursor)[0]
            self.types.append(record)
            cursor += tail
        if cursor != end:
            raise ValueError('incomplete BTF types')

    def string(self, offset):
        end = self.strings.index(b'\0', offset)
        return self.strings[offset:end].decode('ascii')

    def named(self, name, kind):
        matches = [i for i, value in enumerate(self.types) if value and value['name'] == name and value['kind'] == kind]
        if len(matches) != 1:
            raise ValueError('BTF name is absent or ambiguous: ' + name)
        return matches[0]

    def member(self, identifier, name):
        value = self.types[identifier]
        for member, target, offset in value['members']:
            if member == name:
                return target, offset
            if not member:
                try:
                    child, relative = self.member(target, name)
                    return child, offset + relative
                except KeyError:
                    pass
        raise KeyError(name)

    def referenced_aggregate(self, identifier, name):
        seen = set()
        for _ in range(16):
            if not isinstance(identifier, int) or not 0 < identifier < len(self.types) or identifier in seen:
                raise ValueError('invalid or recursive referenced BTF type')
            seen.add(identifier)
            value = self.types[identifier]
            if value['kind'] in (2, 8, 9, 10, 11, 18):
                identifier = value['value']
            elif value['kind'] == 3:
                identifier = value['element_type']
            elif value['kind'] == 4 and value['name'] == name:
                return identifier
            else:
                raise ValueError('referenced BTF aggregate is not ' + name)
        raise ValueError('referenced BTF type exceeds its depth bound')


def bpf_syscall_number(machine):
    try:
        return {'x86_64': 321, 'aarch64': 280}[machine]
    except KeyError:
        raise ValueError('diagnostic requires native Linux AMD64 or ARM64') from None


def wait_program_released(program_id, bpf):
    if not isinstance(program_id, int) or not 0 < program_id < 1 << 32:
        raise ValueError('temporary program lacks an exact release ID')
    # Enumerating immediately before the exact ID acquires no new reference.
    # Reopening its FD can itself defer RCU destruction on the native kernel.
    attr = bytearray(struct.pack('<III', program_id - 1, 0, 0))
    for _ in range(50):
        try:
            bpf(11, attr)  # BPF_PROG_GET_NEXT_ID, never GET_FD_BY_ID.
        except OSError as error:
            if error.errno == 2:
                return
            raise
        next_id = struct.unpack_from('<I', attr, 4)[0]
        if next_id > program_id:
            return
        if next_id != program_id:
            raise ValueError('temporary program release returned an invalid ID')
        time.sleep(.01)
    raise ValueError('exact temporary BPF program remains after descriptor closure')


class Program:
    def __init__(self):
        self.code, self.labels, self.jumps = [], {}, []

    def emit(self, opcode, dest=0, source=0, offset=0, immediate=0):
        self.code.append([opcode, source << 4 | dest, offset, immediate])

    def jump(self, opcode, dest, immediate=0, source=0):
        self.jumps.append(len(self.code))
        self.emit(opcode, dest, source, immediate=immediate)

    def mov(self, dest, source):
        self.emit(0xbf, dest, source)

    def add(self, dest, immediate):
        self.emit(0x07, dest, immediate=immediate)

    def read(self, dest, size, source, offset=0):
        self.mov(1, 10)
        self.add(1, dest)
        self.emit(0xb7, 2, immediate=size)
        self.mov(3, source)
        self.add(3, offset)
        self.emit(0x85, immediate=113)  # bpf_probe_read_kernel
        self.jump(0x55, 0)  # A failed read produces no task record.

    def finish(self):
        end = len(self.code)
        for index in self.jumps:
            self.code[index][2] = end - index - 1
        self.emit(0xb7, 0)
        self.emit(0x95)
        return b''.join(struct.pack('<BBhi', *row) for row in self.code)


def task_layout(btf):
    ctx = btf.named('bpf_iter__task', 4)
    task = btf.referenced_aggregate(btf.member(ctx, 'task')[0], 'task_struct')
    meta = btf.referenced_aggregate(btf.member(ctx, 'meta')[0], 'bpf_iter_meta')
    pid = btf.referenced_aggregate(btf.member(task, 'thread_pid')[0], 'pid')
    upid = btf.referenced_aggregate(btf.member(pid, 'numbers')[0], 'upid')
    block, restart_offset = btf.member(task, 'restart_block')
    block_size = btf.types[block]['value']
    if not 16 <= block_size <= 128 or btf.types[upid]['value'] != 16:
        raise ValueError('unsupported bounded task restart layout')
    return ctx, task, pid, upid, meta, block, restart_offset, block_size


def task_restart(btf, source_pid):
    ctx, task, pid, upid, meta, block, restart_offset, block_size = task_layout(btf)
    p = Program()
    p.mov(6, 1)
    p.emit(0x79, 7, 6, btf.member(ctx, 'task')[1])
    p.jump(0x15, 7)
    p.emit(0x85, immediate=158)  # bpf_get_current_task_btf
    p.read(-16, 8, 0, btf.member(task, 'thread_pid')[1])
    p.emit(0x79, 8, 10, -16)
    p.read(-24, 4, 8, btf.member(pid, 'level')[1])
    p.emit(0x61, 9, 10, -24)
    p.jump(0x25, 9, 32)
    p.emit(0x7b, 10, 9, -40)
    p.emit(0x67, 9, immediate=4)
    p.emit(0x0f, 8, 9)
    p.read(-32, 8, 8, btf.member(pid, 'numbers')[1] + btf.member(upid, 'ns')[1])
    p.read(-16, 8, 7, btf.member(task, 'thread_pid')[1])
    p.emit(0x79, 8, 10, -16)
    p.read(-24, 4, 8, btf.member(pid, 'level')[1])
    p.emit(0x61, 0, 10, -24)
    p.emit(0x79, 9, 10, -40)
    p.jump(0x2d, 9, source=0)
    p.emit(0x67, 9, immediate=4)
    p.emit(0x0f, 8, 9)
    p.add(8, btf.member(pid, 'numbers')[1])
    p.read(-48, 4, 8, btf.member(upid, 'nr')[1])
    p.emit(0x61, 0, 10, -48)
    p.jump(0x55, 0, source_pid)
    p.read(-56, 8, 8, btf.member(upid, 'ns')[1])
    p.emit(0x79, 0, 10, -56)
    p.emit(0x79, 9, 10, -32)
    p.jump(0x5d, 0, source=9)
    p.read(-256, block_size, 7, restart_offset)
    p.read(-256 + block_size, 8, 7, btf.member(task, 'start_boottime')[1])
    p.emit(0x79, 1, 6, btf.member(ctx, 'meta')[1])
    p.emit(0x79, 1, 1, btf.member(meta, 'seq')[1])
    p.mov(2, 10)
    p.add(2, -256)
    p.emit(0xb7, 3, immediate=block_size + 8)
    p.emit(0x85, immediate=127)  # bpf_seq_write
    code = p.finish()
    libc = ctypes.CDLL(None, use_errno=True)
    libc.syscall.restype = ctypes.c_long
    syscall = bpf_syscall_number(os.uname().machine)
    def bpf(command, data):
        attr = ctypes.create_string_buffer(bytes(data))
        result = libc.syscall(syscall, command, attr, len(data))
        if result < 0:
            raise OSError(ctypes.get_errno(), 'diagnostic BPF command ' + str(command))
        if isinstance(data, bytearray):
            data[:] = attr.raw[:len(data)]
        return result
    insns = ctypes.create_string_buffer(code)
    license = ctypes.create_string_buffer(b'GPL\0')
    log = ctypes.create_string_buffer(65536)
    attr = bytearray(144)
    struct.pack_into('<IIQQIIQ', attr, 0, 26, len(code) // 8, ctypes.addressof(insns), ctypes.addressof(license),
                     1, len(log), ctypes.addressof(log))
    struct.pack_into('<I', attr, 68, 28)
    struct.pack_into('<I', attr, 108, btf.named('bpf_iter_task', 12))
    descriptors = []
    program_id = None
    try:
        try:
            descriptors.append(bpf(5, bytes(attr)))
        except OSError as error:
            raise RuntimeError(str(error) + ': ' + log.value.decode('ascii', 'replace')) from error
        link = struct.pack('<IIIIQII', descriptors[0], 0, 28, 0, 0, 0, 0)
        descriptors.append(bpf(28, link))
        descriptors.append(bpf(33, struct.pack('<II', descriptors[-1], 0)))
        info = ctypes.create_string_buffer(128)
        bpf(15, struct.pack('<IIQ', descriptors[0], len(info), ctypes.addressof(info)))
        program_id = struct.unpack_from('<I', info.raw, 4)[0]
        if program_id == 0:
            raise ValueError('temporary program lacks an exact ID')
        data = b''
        while part := os.read(descriptors[-1], 4096):
            data += part
            if len(data) > block_size + 8:
                raise ValueError('PID namespace filtering returned multiple tasks')
        if len(data) != block_size + 8:
            raise ValueError('iterator did not identify one exact task')
        result = block, data[:block_size], struct.unpack_from('<Q', data, block_size)[0], hashlib.sha256(code).hexdigest(), program_id
    finally:
        for descriptor in reversed(descriptors):
            os.close(descriptor)
    # Check the same exact ID after every iterator/link/program FD has closed.
    # No program is reopened, signalled, modified or left pinned.
    wait_program_released(program_id, bpf)
    return result


def main():
    own_init = len(sys.argv) == 3 and sys.argv[1] == '--own-init-control' and os.getpid() == 1
    if (len(sys.argv) != 2 and not own_init) or os.uname().sysname != 'Linux':
        raise ValueError('native Linux diagnostic requires one exact frozen owned task PID')
    bpf_syscall_number(os.uname().machine)
    source_pid = int(sys.argv[-1])
    if source_pid < 1 or source_pid == 1 and not own_init:
        raise ValueError('invalid source task')
    source_pidfd = os.pidfd_open(source_pid)
    try:
        live = select.poll()
        live.register(source_pidfd, select.POLLIN)
        if live.poll(0):
            raise ValueError('pinned source task already exited')
        raw = Path('/sys/kernel/btf/vmlinux').read_bytes()
        btf = BTF(raw)
        block, data, birth, program_sha, program_id = task_restart(btf, source_pid)
        stat = Path('/proc/' + str(source_pid) + '/stat').read_text()
        start_ticks = int(stat[stat.rindex(')') + 1:].split()[19])
        if birth * os.sysconf('SC_CLK_TCK') // 1000000000 != start_ticks:
            raise ValueError('task birth differs from the exact pinned process')
        if live.poll(0):
            raise ValueError('pinned task exited during the read-only probe')
        fn = struct.unpack_from('<Q', data, btf.member(block, 'fn')[1])[0]
        sleep, sleep_offset = btf.member(block, 'nanosleep')
        value = lambda name, encoding: struct.unpack_from(encoding, data, sleep_offset + btf.member(sleep, name)[1])[0]
        names = [line.split()[2] for line in Path('/proc/kallsyms').read_text().splitlines()
                 if int(line.split()[0], 16) == fn and fn != 0]
        print(json.dumps({'schema': 1, 'diagnostic_only': True, 'source_pid': source_pid,
                          'kernel': os.uname().release, 'btf_sha256': hashlib.sha256(raw).hexdigest(),
                          'program_sha256': program_sha, 'restart_function_names': names,
                          'temporary_program_id': program_id, 'program_released': True,
                          'task_birth_ns': birth, 'start_ticks': start_ticks, 'birth_matches': True,
                          'clock_id': value('clockid', '<i'), 'timespec_type': value('type', '<i'),
                          'remaining_user_pointer': hex(value('rmtp', '<Q')),
                          'expires_ns': value('expires', '<q'), 'observed_monotonic_ns': time.monotonic_ns(),
                          'descriptors_closed': True, 'task_memory_modified': False}))
    finally:
        os.close(source_pidfd)


if __name__ == '__main__':
    main()
