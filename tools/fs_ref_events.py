"""Decode partial root/pwd observations; snapshots do not establish ownership."""
from collections import Counter
import struct

EVENT = struct.Struct('<QQQQQQIIIii16s4x')


def decode(data):
    if len(data) % EVENT.size:
        raise ValueError('truncated fs-reference event')
    rows = []
    for values in EVENT.iter_unpack(data):
        ns, fs, root, pwd, generation, pid_tgid, pid, root_id, pwd_id, operation, result, comm = values
        if not ns or operation not in range(1, 8) or not root:
            raise ValueError('invalid fs-reference event')
        if operation <= 5 and (not fs or not pwd or not generation):
            raise ValueError('context event lacks its generation or root/pwd snapshot')
        if operation >= 6 and (fs or pwd or generation or pwd_id):
            raise ValueError('unmount event contains a context identity')
        rows.append({'ns': ns, 'fs': fs, 'root': root, 'pwd': pwd, 'generation': generation,
            'host_tgid': pid_tgid >> 32, 'host_tid': pid_tgid & 0xffffffff,
            'caller_namespace_pid': pid, 'root_id': root_id, 'pwd_id': pwd_id,
            'operation': operation, 'result': result,
            'comm': comm.split(b'\0')[0].decode('utf-8', errors='strict')})
    return sorted(rows, key=lambda row: row['ns'])


def summarize(rows):
    active, unmounting, completed = {}, {}, []

    def holds(mount):
        return [dict(row, matching_references=[name for name in ['root', 'pwd'] if row[name] == mount])
                for row in active.values() if mount in (row['root'], row['pwd'])]

    for row in rows:
        key = row['fs'], row['generation']
        if row['operation'] in (1, 2, 3):
            active[key] = row
        elif row['operation'] == 4:
            if key not in active:
                raise ValueError('release entry lacks its observed context generation')
            active[key] = row
        elif row['operation'] == 5:
            if key not in active:
                raise ValueError('release completion lacks its observed context generation')
            active.pop(key)
        elif row['operation'] == 6:
            tid = row['host_tgid'], row['host_tid']
            if tid in unmounting:
                raise ValueError('nested unmount observation')
            unmounting[tid] = dict(row, observed_contexts_at_entry=holds(row['root']))
        else:
            tid = row['host_tgid'], row['host_tid']
            entered = unmounting.pop(tid, None)
            if entered is None or (entered['root'], entered['root_id']) != (row['root'], row['root_id']):
                raise ValueError('unmount return lacks its matching entry')
            completed.append(dict(row, entry_ns=entered['ns'],
                observed_contexts_at_entry=entered['observed_contexts_at_entry'],
                observed_contexts_at_return=holds(row['root'])))
    return {'events': len(rows), 'operations': dict(Counter(row['operation'] for row in rows)),
        'complete_mount_reference_inventory': False, 'unmounts': completed,
        'unfinished_unmount_entries': list(unmounting.values()),
        'interpretation': 'Observed coherent fs_struct copy/root/pwd snapshots and tracked context release only. '
            'A copy records the caller, not the new task. Changes are recorded after return; final release return is an upper bound. '
            'References before coverage, in-progress changes, namespace-only and other mount references are unobserved. '
            'Pointer/legacy-ID reuse is not ownership authority. Empty observations cannot exonerate a runtime.'}
