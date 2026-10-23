#!/usr/bin/env python3
"""Prove over-limit worker exit refuses before canonical-base unmounts."""
import argparse
import difflib
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import time
import uuid

import build_worker
import consumer_stop_trace
import consumer_worker_loss
import reference
import upstream_bats
import upstream_unit


PREPARE = r'''
import hashlib,json,os,pathlib,subprocess,sys,time
status=json.loads(sys.argv[1]);deadline=float(sys.argv[2]);boundary=sys.argv[3];root=status['lifecycle']['root']
source=pathlib.Path(status['store'])/'ledger-source'
source.mkdir(mode=0o700)
while len(str(source))+221 < 3500:
 source=source/('segment-'+('x'*210));source.mkdir(mode=0o700)
if len(str(source/'owned.txt'))>=4096:raise ValueError('owned source exceeds pathname limit')
(source/'owned.txt').write_bytes(b'owned ledger payload\n')
envelope={'version':1,'root':root,'boot_id':status['boot_id'],
 'runtime_sha256':status['lifecycle']['runtime_sha256'],'criu_sha256':status['lifecycle']['criu_sha256'],
 'manager_root':status['lifecycle']['manager_root'],'requested_at':'','bases':[]}
compact=lambda value:json.dumps(value,separators=(',',':')).encode()
size=len(compact(envelope))+1;rows=[]
def import_base(number,where,extra=''):
 global size
 if time.monotonic()>=deadline-60:raise TimeoutError('real base imports exhausted reserved Stop budget')
 name='ledger-%04d-'%number+'b'*32+extra
 done=subprocess.run(['/opt/factorybox/factorybox','--worker-run-root','/run/factorybox',
  'prepare-base','--source',str(where),name],capture_output=True,text=True,timeout=min(10,deadline-time.monotonic()))
 if done.returncode:raise ValueError('public base import failed before size boundary: '+done.stderr[-2000:])
 base=json.loads(done.stdout);size+=len(compact(base))+(1 if rows else 0)
 rows.append(base)
 return base
for number in range(1,1025):
 base=import_base(number,source)
 if boundary=='oversized' and size>(4<<20)+1024:break
 if boundary=='completion-edge' and (4<<20)-size<=2*len(compact(base)):break
else:raise ValueError('real public inventory did not reach the ledger boundary')
if boundary=='completion-edge':
 target=(4<<20)-30-15
 for tuning in range(2):
  template=rows[-1];next_id='ledger-%04d-'%(len(rows)+1)+'b'*32
  common=len(compact(template))-2*len(template['source'])-3*(len(template['id'])-len(next_id))
  goal=(target-size)//2 if tuning==0 else target-size
  extra=''
  if (goal-common)%2:extra='c';common+=3
  length=(goal-common)//2
  where=pathlib.Path(status['store'])/('ledger-tune-%04d'%(len(rows)+1))
  if not len(str(where))+2<=length<3500:raise ValueError('selected final record cannot fit the real pathname domain')
  where.mkdir(mode=0o700)
  while len(str(where))<length:
   remaining=length-len(str(where));part=min(220,remaining-1)
   if remaining-part-1==1:part-=1
   if part<1:raise ValueError('cannot form exact private source pathname')
   where=where/('x'*part);where.mkdir(mode=0o700)
  (where/'owned.txt').write_bytes(b'owned ledger payload\n')
  import_base(len(rows)+1,where,extra)
 if not (4<<20)-35<=size+30<(4<<20):raise ValueError('actual public imports missed the narrow completion boundary')
mounts={line.split()[4] for line in open('/proc/self/mountinfo')}
sha=lambda p:hashlib.sha256(pathlib.Path(p).read_bytes()).hexdigest()
manifest=[]
for base in rows:
 path=pathlib.Path(base['tree']['saved']);parent=path.parent;s=path.stat()
 if str(path) not in mounts or not os.statvfs(path).f_flag&os.ST_RDONLY:raise ValueError('prepared canonical base is not mounted read-only')
 manifest.append({'id':base['id'],'root':str(path),'inode':s.st_ino,'mode':s.st_mode,'uid':s.st_uid,'gid':s.st_gid,
  'mtime_ns':s.st_mtime_ns,'ctime_ns':s.st_ctime_ns,'base_sha256':sha(parent/'base.json'),
  'intent_sha256':sha(parent/'intent.json'),'payload_sha256':sha(path/'owned.txt')})
if any(os.path.lexists(root+'/'+name) for name in ['worker-exit.json','worker-exit-complete.json']):raise ValueError('fresh owned epoch was already fenced')
print(json.dumps({'base_count':len(rows),'minimum_encoded_ledger_bytes':size,'intent_with_max_timestamp_bytes':size+30,'limit_bytes':4<<20,
 'boundary':boundary,
 'source':str(source),'source_path_bytes':len(str(source)),'manifest':manifest}))
'''

VERIFY = r'''
import hashlib,json,os,pathlib,sys
saved=json.loads(sys.stdin.read());root=sys.argv[1]
sha=lambda p:hashlib.sha256(pathlib.Path(p).read_bytes()).hexdigest()
ledgers={}
for name in ['worker-exit.json','worker-exit-complete.json']:
 path=pathlib.Path(root)/name
 ledgers[name]={'exists':os.path.lexists(path)}
 if os.path.lexists(path):
  data=path.read_bytes()
  if len(data)>4<<20:raise ValueError('retained ledger exceeds its reader limit')
  value=json.loads(data)
  ledgers[name].update(bytes=len(data),sha256=hashlib.sha256(data).hexdigest(),base_count=len(value['bases']),completed_at=value.get('completed_at'))
mounts={line.split()[4] for line in open('/proc/self/mountinfo')}
for row in saved['manifest']:
 path=pathlib.Path(row['root']);s=path.stat()
 current={
  'inode':s.st_ino,'mode':s.st_mode,'uid':s.st_uid,'gid':s.st_gid,'mtime_ns':s.st_mtime_ns,'ctime_ns':s.st_ctime_ns,
  'base_sha256':sha(path.parent/'base.json'),'intent_sha256':sha(path.parent/'intent.json'),
  'payload_sha256':sha(path/'owned.txt')}
 if any(row[key]!=value for key,value in current.items()):raise ValueError('persistent base data or provenance changed: '+row['id'])
 if str(path) in mounts:raise ValueError('old canonical mount survived exact worker removal')
absent=not any(row['exists'] for row in ledgers.values())
print(json.dumps({'verified_bases':len(saved['manifest']),'all_backing_unchanged':True,'both_exit_fences_absent':absent,'old_mounts_absent':True,'ledgers':ledgers}),flush=True)
if not absent:raise ValueError('size refusal published an exit fence before failure')
'''


def stop(identity, deadline):
    remaining = deadline-time.monotonic()
    if remaining <= 2:
        raise TimeoutError('exit-ledger proof exhausted its 420-second lifecycle')
    reference.docker(['container', 'stop', '--time=' + str(min(45, int(remaining)-1)), identity], timeout=remaining)


def attach(identity, artifact, receipt, program):
    consumer_stop_trace.validate_root(artifact/'stop-trace-rootfs',receipt['files'])
    destination = consumer_stop_trace.DESTINATION
    reference.docker(['exec',identity,'mkdir','-m','0700',destination])
    subprocess.run([*reference.DOCKER,'cp',str(artifact/'stop-trace-rootfs')+'/.',identity+':'+destination],
                   check=True,timeout=10,capture_output=True)
    expected = {name:row['sha256'] for name,row in receipt['files'].items() if 'sha256' in row}
    attached = subprocess.run([*reference.DOCKER,'exec',identity,'python3','-c',program,destination,json.dumps(expected)],
                              timeout=12,capture_output=True,text=True)
    (artifact/'stop-tracer-attach.stdout').write_text(attached.stdout)
    (artifact/'stop-tracer-attach.stderr').write_text(attached.stderr)
    if attached.returncode:
        raise ValueError('ledger tracer did not attach to every supervisor thread')
    observed = json.loads(attached.stdout)
    if observed['executable'] != destination+'/usr/bin/strace' or not observed['attached_after_base_assertions']:
        raise ValueError('ledger diagnostic attachment identity differs')
    (artifact/'stop-tracer.json').write_text(json.dumps(observed,indent=2)+'\n')
    return observed


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--worker-build', required=True)
    parser.add_argument('--trace-inputs', required=True)
    parser.add_argument('--boundary',choices=['oversized','completion-edge'],default='oversized')
    args = parser.parse_args()
    build, worker = build_worker.load(args.worker_build)
    if reference.docker(['ps', '--filter', 'label=com.factorybox.run', '--format', '{{.ID}}']):
        raise ValueError('native cases must close before the exit-ledger proof')
    owner = uuid.uuid4().hex
    name = 'factorybox-exit-ledger-' + owner
    artifact = reference.new_artifact(name)
    artifact.mkdir()
    shutil.copyfile(__file__, artifact/'runner.py')
    source = artifact/'source'
    for directory in ['internal', 'cmd/factorybox']:
        shutil.copytree(reference.ROOT/directory, source/directory)
    for file in ['go.mod', 'go.sum']:
        shutil.copyfile(reference.ROOT/file, source/file)
    manifest = upstream_unit.context_manifest(source)
    core = lambda rows: {k:v for k,v in rows.items() if not k.endswith('_test.go')}
    if core(manifest) != core(worker['source_context']):
        raise ValueError('native ledger source differs from delivered core')
    result = {'schema':1, 'passed':False, 'cleanup':False, 'production_qualified':False,
              'owner':owner, 'worker_build':args.worker_build, 'package_sha256':worker['package_sha256'],
              'worker_result_sha256':reference.sha(build/'result.json'), 'source_context':manifest,
              'lifecycle_cli_sha256':worker['bundle_sha256']['factorybox'],
              'boundary':args.boundary,
              'global_sysctls_before':consumer_worker_loss.sysctls(),
              'scope':'Owned 512-MiB bounded store, public tiny base imports, selected encoded-size boundary, required refusal before any canonical unmount, targeted supervisor umount2 trace, and backing verification after replacement. Diagnostic instrumentation; no latency or production qualification.'}
    attempted = created = preserve = False
    generation = 'source'
    identity = None
    print(json.dumps({'running':str(artifact)}), flush=True)
    try:
        receipt = consumer_stop_trace.freeze(args.trace_inputs, worker, artifact)
        result['trace_inputs'] = receipt
        before = consumer_stop_trace.ATTACH
        needle = "'trace=%file'"
        if before.count(needle) != 1:
            raise ValueError('diagnostic filter anchor changed')
        marker = 'attached_after_consumer_assertions'
        if before.count(marker) != 1:
            raise ValueError('diagnostic scope marker changed')
        after = before.replace(needle, "'trace=umount2'").replace(marker,'attached_after_base_assertions')
        (artifact/'trace-attach-before.py').write_text(before)
        (artifact/'trace-attach-after.py').write_text(after)
        patch = ''.join(difflib.unified_diff(before.splitlines(True), after.splitlines(True),
                        fromfile='a/trace-attach.py', tofile='b/trace-attach.py'))
        (artifact/'trace-attach.patch').write_text(patch)
        result['diagnostic_filter'] = {'before_sha256':hashlib.sha256(before.encode()).hexdigest(),
            'after_sha256':hashlib.sha256(after.encode()).hexdigest(), 'patch_sha256':reference.sha(artifact/'trace-attach.patch'),
            'change':'Copied diagnostic selects umount2 and labels its completed public-base assertions; runtime and original tests remain unchanged.'}
        reference.docker(['volume','create','--label','com.factorybox.run='+owner,name])
        created = True
        command = ['run','--detach','--pull=never','--name',name,'--label','com.factorybox.run='+owner,
                   '--privileged','--cgroupns=private','--cpus=3','--memory=3g','--pids-limit=512','--network=none',
                   '--env','GOMAXPROCS=2','--env','GOMEMLIMIT=256MiB',
                   '--mount','type=volume,source='+name+',target=/var/lib/sysbox',worker['image'],
                   '--store-id',owner,'--bounded-root','/var/lib/sysbox/fixed-store','--bounded-bytes',str(512<<20)]
        result['command'] = command
        attempted = True
        identity = reference.docker(command)
        deadline = time.monotonic()+420
        result['startup'] = consumer_worker_loss.ready(name,owner,identity,deadline)
        status = result['startup']['status']
        with (artifact/'prepare.stdout').open('x') as out, (artifact/'prepare.stderr').open('x') as error:
            done = subprocess.run([*reference.DOCKER,'exec',identity,'python3','-c',PREPARE,json.dumps(status),str(deadline),args.boundary],
                                  stdout=out,stderr=error,timeout=max(1,deadline-time.monotonic()))
        if done.returncode:
            raise ValueError('real ledger inventory preparation failed')
        prepared = json.loads((artifact/'prepare.stdout').read_text())
        result['prepared'] = {key:value for key,value in prepared.items() if key != 'manifest'}
        if (not prepared['manifest'] or args.boundary=='oversized' and prepared['minimum_encoded_ledger_bytes'] <= 4<<20 or
                args.boundary=='completion-edge' and not (4<<20)-35<=prepared['intent_with_max_timestamp_bytes']<4<<20):
            raise ValueError('real inventory did not exceed the original encoded limit')
        consumer_worker_loss.exact_worker(name,owner,identity)
        result['tracer'] = attach(identity,artifact,receipt,after)
        stop(identity,deadline)
        state = consumer_worker_loss.exact_worker(name,owner,identity)['State']
        result['state'] = state
        logs = subprocess.run([*reference.DOCKER,'logs',identity],capture_output=True,text=True,timeout=5)
        (artifact/'source-worker.log').write_text(logs.stdout+logs.stderr)
        result['trace'] = consumer_stop_trace.collect(identity,artifact)
        trace = (artifact/'worker-stop.strace').read_text()
        canonical_calls = [line for line in trace.splitlines() if 'umount2(' in line and status['lifecycle']['root']+'/bases/' in line]
        close_pattern = re.compile(r'umount2\("'+re.escape(status['lifecycle']['bounded_store']['target'])+r'", 0\)\s*= -1 EBUSY')
        close_calls = [line for line in trace.splitlines() if close_pattern.search(line)]
        result['observed_unmounts'] = {'canonical_base_calls':canonical_calls,'later_store_close_ebusy':close_calls}
        refusal_ok = (not state['Running'] and not state['Pid'] and not state['OOMKilled'] and state['ExitCode']==1 and
                'checkpoint record exceeds size limit' in logs.stdout+logs.stderr and not canonical_calls and bool(close_calls))
        result['refusal_before_release_verified'] = refusal_ok
        reference.owned_delete('container',name,owner)
        identity = reference.docker(command)
        generation = 'replacement'
        result['replacement'] = consumer_worker_loss.ready(name,owner,identity,deadline)
        if result['replacement']['status']['epoch'] == status['epoch']:
            raise ValueError('replacement reused the old controller epoch')
        consumer_worker_loss.exact_worker(name,owner,identity)
        verified = subprocess.run([*reference.DOCKER,'exec','--interactive',identity,'python3','-c',VERIFY,status['lifecycle']['root']],
                                  input=json.dumps(prepared),capture_output=True,text=True,timeout=max(1,deadline-time.monotonic()))
        (artifact/'verify.stdout').write_text(verified.stdout)
        (artifact/'verify.stderr').write_text(verified.stderr)
        if verified.stdout.strip():
            result['verification'] = json.loads(verified.stdout)
        if verified.returncode:
            raise ValueError('over-limit refusal changed persistent backing or its fences')
        if not refusal_ok:
            raise ValueError('size refusal did not precede every canonical unmount')
        stop(identity,deadline)
        replacement = consumer_worker_loss.exact_worker(name,owner,identity)['State']
        result['replacement_state'] = replacement
        if replacement['Running'] or replacement['Pid'] or replacement['OOMKilled'] or replacement['ExitCode']:
            raise ValueError('empty replacement failed clean Stop')
        consumer_stop_trace.validate_root(artifact/'stop-trace-rootfs',receipt['files'])
        result['inputs_unchanged'] = (upstream_unit.context_manifest(source) == manifest and
            reference.sha(artifact/'runner.py') == reference.sha(Path(__file__)))
        result['passed'] = result['inputs_unchanged']
    except Exception as error:
        result['error'] = str(error)
        raise
    finally:
        try:
            if attempted:
                logs = subprocess.run([*reference.DOCKER,'logs',name],capture_output=True,text=True,timeout=5)
                (artifact/(generation+'-final-worker.log')).write_text(logs.stdout+logs.stderr)
                reference.owned_delete('container',name,owner)
            if created and not result['passed']:
                try:
                    upstream_bats.archive_stopped_volumes(artifact,name,owner,[name],worker['image'],result,
                        timeout=120,compressed=True,decoded_limit=(512<<20)+(2<<20))
                except Exception as error:
                    preserve = True
                    result.update(archive_error=str(error),retained_volumes=[name])
            if created and not preserve:
                reference.owned_delete('volume',name,owner)
            result['cleanup'] = not preserve
            result['global_sysctls_after'] = consumer_worker_loss.sysctls()
            result['passed'] = result['passed'] and result['cleanup'] and result['global_sysctls_after'] == result['global_sysctls_before']
        finally:
            (artifact/'result.json').write_text(json.dumps(result,indent=2)+'\n')
            print(json.dumps({'evidence':str(artifact),'passed':result['passed'],'cleanup':result['cleanup']}),flush=True)
    if not result['passed']:
        raise SystemExit(1)


if __name__ == '__main__':
    main()
