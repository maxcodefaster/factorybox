#!/usr/bin/env python3
"""Bound public base imports before their exit metadata exhausts an epoch."""
import argparse
import json
from pathlib import Path
import shutil
import subprocess
import time
import uuid

import build_worker
import consumer_worker_loss
import reference
import upstream_bats


PREPARE = r'''
import hashlib,json,os,pathlib,subprocess,sys,time
status=json.loads(sys.argv[1]);deadline=float(sys.argv[2]);root=status['lifecycle']['root'];limit=2<<20
source=pathlib.Path(status['store'])/'base-admission-source';source.mkdir(mode=0o700)
while len(str(source))+221<3500:
 source=source/('segment-'+'x'*210);source.mkdir(mode=0o700)
payload=b'owned base admission payload\n';(source/'owned.txt').write_bytes(payload)
compact=lambda value:json.dumps(value,separators=(',',':')).encode()
sha=lambda path:hashlib.sha256(pathlib.Path(path).read_bytes()).hexdigest()
def call(name):
 if time.monotonic()>=deadline-70:raise TimeoutError('base admission exhausted reserved stop/replacement budget')
 return subprocess.run(['/opt/factorybox/factorybox','--worker-run-root','/run/factorybox',
  'prepare-base','--source',str(source),name],capture_output=True,text=True,timeout=min(12,deadline-time.monotonic()))
rows=[];used=0;rejected=None
for number in range(1,1025):
 name='base-%04d-'%number+'b'*32;done=call(name)
 if done.returncode:
  if 'new base imports refused' not in done.stderr:raise ValueError('unexpected public import failure: '+done.stderr[-2000:])
  rejected={'id':name,'error':done.stderr[-4000:]};break
 base=json.loads(done.stdout);used+=len(compact(base))+1;rows.append(base)
 if used>limit:
  pathlib.Path('/output/base-admission-counterexample.json').write_text(json.dumps({
   'accepted_bases':len(rows),'canonical_base_encoded_bytes':used,'budget_bytes':limit,
   'last_base':base,'failure':'public base admission accepted more canonical metadata than its declared budget'},indent=2)+'\n')
  raise ValueError('public base admission accepted more canonical metadata than its declared budget')
else:raise ValueError('public base admission never reached its declared bound')
if not rows or limit-used>2*(len(compact(rows[-1]))+1):raise ValueError('refusal did not reach the declared metadata boundary')
if os.path.lexists(root+'/bases/'+rejected['id']):raise ValueError('rejected import created backing or intent')
for base in [rows[0],rows[-1]]:
 replay=call(base['id'])
 if replay.returncode or json.loads(replay.stdout)!=base:raise ValueError('complete import did not replay exactly at the boundary')
mounts={line.split()[4] for line in open('/proc/self/mountinfo')};manifest=[]
for base in rows:
 path=pathlib.Path(base['tree']['saved']);stat=path.stat()
 if str(path) not in mounts or not os.statvfs(path).f_flag&os.ST_RDONLY:raise ValueError('old canonical mount changed')
 if (path/'owned.txt').read_bytes()!=payload:raise ValueError('old payload changed')
 manifest.append({'id':base['id'],'root':str(path),'inode':stat.st_ino,'mode':stat.st_mode,'uid':stat.st_uid,'gid':stat.st_gid,
  'mtime_ns':stat.st_mtime_ns,'ctime_ns':stat.st_ctime_ns,'base_sha256':sha(path.parent/'base.json'),
  'intent_sha256':sha(path.parent/'intent.json'),'payload_sha256':sha(path/'owned.txt')})
if any(os.path.lexists(root+'/'+name) for name in ['worker-exit.json','worker-exit-complete.json']):raise ValueError('admission fenced the current epoch')
print(json.dumps({'passed':True,'base_count':len(rows),'canonical_base_encoded_bytes':used,'budget_bytes':limit,
 'rejected_before_intent':True,'exact_replay_at_boundary':True,'prior_backing_unchanged':True,
 'rejected':rejected,'manifest':manifest}),flush=True)
'''

VERIFY = r'''
import hashlib,json,os,pathlib,sys
saved=json.load(sys.stdin);root=sys.argv[1]
sha=lambda path:hashlib.sha256(pathlib.Path(path).read_bytes()).hexdigest()
ledgers={}
for name in ['worker-exit.json','worker-exit-complete.json']:
 path=pathlib.Path(root)/name;data=path.read_bytes();value=json.loads(data)
 if len(data)>4<<20 or len(value['bases'])!=saved['base_count']:raise ValueError('exit ledger lost an admitted base or exceeded its reader bound')
 ledgers[name]={'bytes':len(data),'sha256':hashlib.sha256(data).hexdigest(),'base_count':len(value['bases'])}
 if name=='worker-exit-complete.json' and not value.get('completed_at'):raise ValueError('exit completion is absent')
mounts={line.split()[4] for line in open('/proc/self/mountinfo')}
for row in saved['manifest']:
 path=pathlib.Path(row['root']);stat=path.stat()
 actual={'inode':stat.st_ino,'mode':stat.st_mode,'uid':stat.st_uid,'gid':stat.st_gid,'mtime_ns':stat.st_mtime_ns,'ctime_ns':stat.st_ctime_ns,
  'base_sha256':sha(path.parent/'base.json'),'intent_sha256':sha(path.parent/'intent.json'),'payload_sha256':sha(path/'owned.txt')}
 if any(row[key]!=value for key,value in actual.items()):raise ValueError('old base bytes or provenance changed: '+row['id'])
 if str(path) in mounts:raise ValueError('old canonical mount survived worker replacement')
if os.path.lexists(root+'/bases/'+saved['rejected']['id']):raise ValueError('rejected import acquired backing later')
print(json.dumps({'passed':True,'verified_bases':saved['base_count'],'all_backing_unchanged':True,
 'rejected_still_absent':True,'old_mounts_absent':True,'ledgers':ledgers}),flush=True)
'''


def stop(name, owner, identity, deadline):
    consumer_worker_loss.exact_worker(name, owner, identity)
    remaining = deadline-time.monotonic()
    if remaining <= 2:
        raise TimeoutError('base admission exhausted its 420-second lifecycle')
    reference.docker(['container', 'stop', '--time=' + str(min(45, int(remaining)-1)), identity], timeout=remaining)
    state = consumer_worker_loss.exact_worker(name, owner, identity)['State']
    if state['Running'] or state['Pid'] or state['OOMKilled'] or state['ExitCode']:
        raise ValueError('admitted inventory did not stop cleanly')
    return state


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--worker-build', required=True)
    args = parser.parse_args()
    build, worker = build_worker.load(args.worker_build)
    if reference.docker(['ps', '--filter', 'label=com.factorybox.run', '--format', '{{.ID}}']):
        raise ValueError('other owned native runs must finish first')
    owner = uuid.uuid4().hex
    name = 'factorybox-base-admission-' + owner
    artifact = reference.new_artifact(name)
    artifact.mkdir()
    shutil.copyfile(__file__, artifact/'runner.py')
    (artifact/'prepare.py').write_text(PREPARE)
    (artifact/'verify.py').write_text(VERIFY)
    result = {'schema': 1, 'passed': False, 'cleanup': False, 'production_qualified': False,
              'owner': owner, 'worker_build': args.worker_build,
              'worker_result_sha256': reference.sha(build/'result.json'),
              'package_sha256': worker['package_sha256'],
              'lifecycle_cli_sha256': worker['bundle_sha256']['factorybox'],
              'inputs_sha256': {path.name: reference.sha(path) for path in artifact.iterdir()},
              'global_sysctls_before': consumer_worker_loss.sysctls(),
              'scope': 'Public real tiny-base imports up to the 2-MiB canonical exit-metadata budget, refusal before intent, exact replay and unchanged prior backing at that boundary, clean stop, independent replacement and retained backing/complete exit ledger verification. Does not qualify other new-work admission or epoch retirement.'}
    volume = attempted = preserve = False
    identity = None
    print(json.dumps({'running': str(artifact)}), flush=True)
    try:
        reference.docker(['volume', 'create', '--label', 'com.factorybox.run='+owner, name])
        volume = True
        command = ['run', '--detach', '--pull=never', '--name', name, '--label', 'com.factorybox.run='+owner,
                   '--privileged', '--cgroupns=private', '--cpus=3', '--memory=3g', '--pids-limit=512', '--network=none',
                   '--env', 'GOMAXPROCS=2', '--env', 'GOMEMLIMIT=256MiB',
                   '--mount', 'type=volume,source='+name+',target=/var/lib/sysbox',
                   '--mount', 'type=bind,source='+str(artifact)+',target=/output', worker['image'],
                   '--store-id', owner, '--bounded-root', '/var/lib/sysbox/fixed-store', '--bounded-bytes', str(512<<20)]
        result['command'] = command
        attempted = True
        identity = reference.docker(command)
        deadline = time.monotonic()+420
        result['startup'] = consumer_worker_loss.ready(name, owner, identity, deadline)
        status = result['startup']['status']
        prepared = subprocess.run([*reference.DOCKER, 'exec', identity, 'python3', '/output/prepare.py', json.dumps(status), str(deadline)],
                                  capture_output=True, text=True, timeout=max(1, deadline-time.monotonic()))
        (artifact/'prepare.stdout').write_text(prepared.stdout)
        (artifact/'prepare.stderr').write_text(prepared.stderr)
        if prepared.returncode:
            raise ValueError('public base admission boundary failed')
        inventory = json.loads(prepared.stdout)
        result['prepared'] = {key: value for key, value in inventory.items() if key != 'manifest'}
        result['source_state'] = stop(name, owner, identity, deadline)
        logs = reference.docker(['logs', identity])
        (artifact/'source-worker.log').write_text(logs)
        reference.owned_delete('container', name, owner)
        identity = reference.docker(command)
        result['replacement'] = consumer_worker_loss.ready(name, owner, identity, deadline)
        if result['replacement']['status']['epoch'] == status['epoch']:
            raise ValueError('replacement reused its original controller epoch')
        verified = subprocess.run([*reference.DOCKER, 'exec', '--interactive', identity, 'python3', '/output/verify.py', status['lifecycle']['root']],
                                  input=json.dumps(inventory), capture_output=True, text=True, timeout=max(1, deadline-time.monotonic()))
        (artifact/'verify.stdout').write_text(verified.stdout)
        (artifact/'verify.stderr').write_text(verified.stderr)
        if verified.returncode:
            raise ValueError('admitted backing or exit completion changed after replacement')
        result['verification'] = json.loads(verified.stdout)
        result['replacement_state'] = stop(name, owner, identity, deadline)
        result['passed'] = True
    except Exception as error:
        result['error'] = str(error)
    finally:
        try:
            if attempted:
                if identity:
                    consumer_worker_loss.exact_worker(name, owner, identity)
                logs = subprocess.run([*reference.DOCKER, 'logs', name], capture_output=True, text=True, timeout=5)
                (artifact/'final-worker.log').write_text(logs.stdout+logs.stderr)
                reference.owned_delete('container', name, owner)
            if volume and not result['passed']:
                try:
                    upstream_bats.archive_stopped_volumes(artifact, name, owner, [name], worker['image'], result,
                        timeout=120, compressed=True, decoded_limit=(512<<20)+(2<<20))
                except Exception as error:
                    preserve = True
                    result.update(archive_error=str(error), retained_volumes=[name])
            if volume and not preserve:
                reference.owned_delete('volume', name, owner)
            result['cleanup'] = not preserve
        finally:
            result['inputs_unchanged'] = all(reference.sha(artifact/name) == expected for name, expected in result['inputs_sha256'].items())
            result['global_sysctls_after'] = consumer_worker_loss.sysctls()
            result['passed'] = (result['passed'] and result['cleanup'] and result['inputs_unchanged'] and
                                result['global_sysctls_before'] == result['global_sysctls_after'])
            (artifact/'result.json').write_text(json.dumps(result, indent=2)+'\n')
            print(json.dumps({'evidence': str(artifact), 'passed': result['passed'], 'cleanup': result['cleanup'],
                              'error': result.get('error')}), flush=True)
    if not result['passed']:
        raise SystemExit(1)


if __name__ == '__main__':
    main()
