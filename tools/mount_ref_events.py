"""Decode bounded diagnostic events; file opens are not a complete mount inventory."""
from collections import Counter
import struct

EVENT = struct.Struct('<QQQQQIIii16s')

def decode(data):
    if len(data) % EVENT.size:
        raise ValueError('truncated mount-reference event')
    rows = []
    for values in EVENT.iter_unpack(data):
        ns,file,mount,pid_tgid,generation,local_pid,mount_id,operation,result,comm = values
        if not ns or not mount or operation not in range(1,6):
            raise ValueError('invalid mount-reference event')
        if operation in (1,2,3) and (not file or not generation):
            raise ValueError('file event lacks its original generation')
        rows.append({'ns':ns,'file':file,'mount':mount,'host_tgid':pid_tgid>>32,'host_tid':pid_tgid&0xffffffff,
                     'generation':generation,'opener_namespace_pid':local_pid,'mount_id':mount_id,
                     'operation':operation,'result':result,'comm':comm.split(b'\0')[0].decode('utf-8',errors='strict')})
    return sorted(rows,key=lambda row:row['ns'])

def summarize(rows, *, open_hook='security_file_open'):
    if open_hook not in ('security_file_open','path_openat'):
        raise ValueError('unknown mount-reference open hook')
    active = {}
    mounts = []
    closes = {}
    for row in rows:
        key = (row['file'],row['generation'])
        if row['operation'] == 1:
            active[key] = row
        elif row['operation'] == 3:
            active.pop(key,None)
            closes[key] = row
        elif row['operation'] == 5:
            mounts.append(dict(row,observed_file_opens_without_final_release=[v for v in active.values() if v['mount']==row['mount']]))
    for mount in mounts:
        for opened in mount['observed_file_opens_without_final_release']:
            closed = closes.get((opened['file'],opened['generation']))
            if closed:
                opened['observed_final_release_ns'] = closed['ns']
                opened['observed_final_release_comm'] = closed['comm']
    interval = ('Observed successful path-open/final-release intervals, including O_PATH.' if open_hook == 'path_openat'
                else 'Observed LSM file-open/final-release intervals only, which can include failed opens and omit O_PATH.')
    return {'events':len(rows),'open_hook':open_hook,'zero_legacy_mount_id_events':sum(row['mount_id']==0 for row in rows),
            'operations':dict(Counter(row['operation'] for row in rows)),
            'complete_mount_reference_inventory':False,
            'interpretation':interval+' Cwd/root/namespace references and opens before tracer coverage are outside this inventory. An empty list cannot exonerate a runtime.',
            'unmounts':mounts}
