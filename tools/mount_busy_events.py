"""Decode bounded kernel busy-decision observations; never infer ownership."""
import argparse
import json
from pathlib import Path
import struct

EVENT = struct.Struct('<6Q4I3i3IQ16s')
FIELDS = ('ns', 'call', 'pid_tgid', 'root', 'candidate', 'parent', 'local_pid',
          'root_id', 'candidate_id', 'namespace', 'operation', 'result', 'refcnt',
          'flags', 'has_children', 'reserved0', 'reserved1', 'comm')


def decode(data):
    if len(data) % EVENT.size or len(data) > EVENT.size * (1 << 20):
        raise ValueError('incomplete or oversized mount-busy observation')
    rows = []
    for values in EVENT.iter_unpack(data):
        row = dict(zip(FIELDS, values))
        if (row['reserved0'] or row['reserved1'] or row['operation'] not in range(1, 6) or
                not 0 < row['call'] <= row['ns'] or row['local_pid'] <= 0 or
                not row['root'] or not row['candidate'] or row['has_children'] not in (0, 1)):
            raise ValueError('invalid mount-busy event layout')
        row['comm'] = row['comm'].split(b'\0', 1)[0].decode('utf-8', errors='replace')
        rows.append(row)
    return rows


def summarize(rows):
    active, complete = {}, []
    for row in rows:
        key = row['pid_tgid'], row['call']
        operation = row['operation']
        if operation == 1:
            if key in active:
                raise ValueError('duplicate unmount entry')
            active[key] = {'call': row['call'], 'start_ns': row['ns'], 'last_ns': row['ns'], 'root': row['root'],
                           'root_id': row['root_id'], 'namespace': row['namespace'],
                           'local_pid': row['local_pid'], 'flags': row['result'],
                           'decisions': [], 'pending': None}
            continue
        if key not in active:
            raise ValueError('unmatched kernel decision or unmount return')
        call = active[key]
        if row['ns'] < call['last_ns']:
            raise ValueError('kernel events are not ordered within one call')
        call['last_ns'] = row['ns']
        if (row['root'], row['root_id'], row['local_pid']) != (call['root'], call['root_id'], call['local_pid']):
            raise ValueError('unmount identity changed within one call')
        if operation == 3:
            if call['pending'] is not None:
                raise ValueError('nested busy decision')
            call['pending'] = {'start_ns': row['ns'], 'refcnt': row['refcnt'],
                               'root_has_children': bool(row['has_children']), 'counts': []}
        elif operation == 5:
            if call['pending'] is None:
                raise ValueError('reference count outside its kernel decision')
            call['pending']['counts'].append({key: row[key] for key in
                ('ns', 'candidate', 'candidate_id', 'parent', 'namespace', 'result', 'flags', 'has_children')})
        elif operation == 4:
            if call['pending'] is None:
                raise ValueError('busy decision return without entry')
            decision = call['pending']
            decision.update(end_ns=row['ns'], result=row['result'])
            call['decisions'].append(decision)
            call['pending'] = None
        elif operation == 2:
            if call['pending'] is not None:
                raise ValueError('unmount return before kernel decision completion')
            call.update(end_ns=row['ns'], result=row['result'])
            call.pop('pending')
            call.pop('last_ns')
            complete.append(call)
            active.pop(key)
    if active:
        raise ValueError('unfinished unmount observations')
    return {'schema': 1, 'event_bytes': EVENT.size, 'events': len(rows), 'unmounts': complete,
            'scope': 'Actual kernel decisions and count returns for observed owned-namespace unmount calls. '
                     'Counts and candidate mounts identify where to investigate; they do not identify a holder '
                     'or establish the cause of an uncaptured historical failure.'}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('events', type=Path)
    args = parser.parse_args()
    print(json.dumps(summarize(decode(args.events.read_bytes())), indent=2))


if __name__ == '__main__':
    main()
