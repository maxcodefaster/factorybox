#!/usr/bin/env python3
"""Verify bounded-worker base release and refusal with the delivered image."""
import argparse
import hashlib
import json
import shutil
import uuid
from pathlib import Path
import subprocess
import sys
import time

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / 'tools'))
import build_worker
import consumer_worker_loss
import reference
import upstream_bats

def backing_observation(identity, base, controller):
    code = '''import hashlib,json,os,sys
root,controller=sys.argv[1:]
s=os.stat(root)
result={'payload_sha256':hashlib.sha256(open(root+'/owned.txt','rb').read()).hexdigest(),
        'inode':s.st_ino,'mode':s.st_mode,'uid':s.st_uid,'gid':s.st_gid,
        'mtime_ns':s.st_mtime_ns,'ctime_ns':s.st_ctime_ns,
        'base_record_sha256':hashlib.sha256(open(os.path.dirname(root)+'/base.json','rb').read()).hexdigest(),
        'covering_mounts':[line.strip() for line in open('/proc/self/mountinfo') if line.split()[4]==root]}
for key,name in [('intent','worker-exit.json'),('complete','worker-exit-complete.json')]:
 path=controller+'/'+name
 result[key]=json.load(open(path)) if os.path.lexists(path) else None
print(json.dumps(result))'''
    return json.loads(reference.docker(['exec', identity, 'python3', '-c', code,
                                       base['tree']['saved'], controller], timeout=15))

def run(HERE, WORKER, OWNER):
    if (HERE / 'result.json').exists():
        raise ValueError('closed native reproduction cannot be rerun')
    if reference.docker(['ps', '--filter', 'label=com.factorybox.run', '--format', '{{.ID}}']):
        raise ValueError('native workers must be serialized')
    _, worker = build_worker.load(WORKER)
    before = consumer_worker_loss.sysctls()
    result = {'schema': 1, 'passed': False, 'production_qualified': False, 'cleanup': False, 'owner': OWNER,
              'worker_build': WORKER, 'worker_image_id': worker['image_id'], 'runner_sha256': reference.sha(Path(__file__)),
              'cases': [], 'global_sysctls_before': before,
              'scope': 'Seven serialized 3 CPU/3 GiB cases with 512-MiB stores: empty, canonical base, changed identity, owned nested mount, incomplete base/object and held operation lock. Exact Stop outcomes and fresh-worker backing/record preservation. No sandbox, manual unmount, force/lazy cleanup or host setting change.'}
    deadline = time.monotonic() + 420
    clean = True
    try:
        for mode in ['empty', 'base', 'changed-identity', 'nested-mount', 'incomplete-base', 'incomplete-object', 'held-operation']:
            if time.monotonic() >= deadline:
                raise TimeoutError('bounded worker-stop cases exceeded 420 seconds')
            name = HERE.name + '-' + mode
            output = HERE / mode
            output.mkdir()
            row = {'case': mode, 'name': name, 'passed': False, 'cleanup': False}
            result['cases'].append(row)
            created = attempted = False
            try:
                reference.docker(['volume', 'create', '--label', 'com.factorybox.run=' + OWNER, name])
                created = True
                command = ['run', '--detach', '--pull=never', '--name', name, '--label', 'com.factorybox.run=' + OWNER,
                           '--privileged', '--cgroupns=private', '--cpus=3', '--memory=3g', '--pids-limit=512', '--network=none',
                           '--env', 'GOMAXPROCS=2', '--env', 'GOMEMLIMIT=256MiB',
                           '--mount', 'type=volume,source=' + name + ',target=/var/lib/sysbox',
                           worker['image'], '--store-id', OWNER, '--bounded-root', '/var/lib/sysbox/fixed-store', '--bounded-bytes', str(512 << 20)]
                row['command'] = command
                attempted = True
                identity = reference.docker(command)
                row['identity'] = identity
                row['startup'] = consumer_worker_loss.ready(name, OWNER, identity, deadline)
                status = row['startup']['status']
                if mode != 'empty':
                    source = status['store'] + '/base-source'
                    code = 'import os,sys; os.mkdir(sys.argv[1],0o700); os.mkdir(sys.argv[1]+"/child",0o700); open(sys.argv[1]+"/owned.txt","xb").write(b"owned immutable base test\\n")'
                    consumer_worker_loss.exact_worker(name, OWNER, identity)
                    reference.docker(['exec', identity, 'python3', '-c', code, source])
                    base = json.loads(reference.docker(['exec', identity, '/opt/factorybox/factorybox', '--worker-run-root', '/run/factorybox',
                                                       'prepare-base', '--source', source, 'stop-base']))
                    (output / 'base.json').write_text(json.dumps(base, indent=2) + '\n')
                    row['prepared_base'] = base
                    if mode == 'changed-identity':
                        code = 'import json,sys; p=sys.argv[1]; v=json.load(open(p)); v["tree"]["immutable"]["mount_id"]+=1; open(p,"w").write(json.dumps(v))'
                        reference.docker(['exec', identity, 'python3', '-c', code,
                                          status['lifecycle']['root'] + '/bases/stop-base/base.json'])
                    elif mode == 'nested-mount':
                        reference.docker(['exec', identity, 'mount', '-t', 'tmpfs', '-o', 'size=1m',
                                          'factorybox-owned-stop-fault', base['tree']['saved'] + '/child'])
                    elif mode == 'incomplete-base':
                        code = 'import os,sys; os.mkdir(sys.argv[1],0o700)'
                        reference.docker(['exec', identity, 'python3', '-c', code,
                                          status['lifecycle']['root'] + '/bases/partial-base'])
                    elif mode == 'incomplete-object':
                        code = 'import os,sys; os.mkdir(sys.argv[1],0o700)'
                        reference.docker(['exec', identity, 'python3', '-c', code,
                                          status['lifecycle']['root'] + '/objects/partial-object'])
                    elif mode == 'held-operation':
                        code = 'import fcntl,json,os,sys,time; f=open(sys.argv[1],"r+b"); fcntl.flock(f,fcntl.LOCK_EX|fcntl.LOCK_NB); open(sys.argv[2],"x").write(json.dumps({"pid":os.getpid(),"lock_inode":os.fstat(f.fileno()).st_ino})); time.sleep(300)'
                        marker = status['store'] + '/held-operation-ready.json'
                        reference.docker(['exec', '--detach', identity, 'python3', '-c', code,
                                          status['lifecycle']['root'] + '/.lock', marker])
                        code = 'import os,sys,time; p=sys.argv[1]; end=time.monotonic()+5\nwhile not os.path.exists(p):\n if time.monotonic()>end:raise TimeoutError("operation lock was not acquired")\n time.sleep(.01)\nprint(open(p).read())'
                        row['held_operation'] = json.loads(reference.docker(['exec', identity, 'python3', '-c', code, marker]))
                    row['backing_before'] = backing_observation(identity, base, status['lifecycle']['root'])
                    if row['backing_before']['intent'] is not None or row['backing_before']['complete'] is not None:
                        raise ValueError('fresh epoch already has a shutdown receipt')
                code = '''import json,os,sys
target=sys.argv[1]
mounts=[]
for line in open('/proc/self/mountinfo'):
 parts=line.split(); path=parts[4]
 if path==target or path.startswith(target+'/'):mounts.append(line.strip())
processes=[]
for entry in os.listdir('/proc'):
 if not entry.isdigit():continue
 try:
  root='/proc/'+entry
  processes.append({'pid':int(entry),'status':open(root+'/status').read(),'cmdline':open(root+'/cmdline','rb').read().replace(b'\\x00',b' ').decode(errors='replace')})
 except (FileNotFoundError,ProcessLookupError):pass
print(json.dumps({'mounts':mounts,'processes':processes}))'''
                observation = reference.docker(['exec', identity, 'python3', '-c', code, status['lifecycle']['bounded_store']['target']])
                (output / 'before-stop.json').write_text(json.dumps(json.loads(observation), indent=2) + '\n')
                reference.docker(['container', 'stop', '--time=15', identity], timeout=20)
                state = consumer_worker_loss.exact_worker(name, OWNER, identity)['State']
                row['state'] = state
                expected_exit = 0 if mode in ['empty', 'base'] else 1
                if state['Running'] or state['Pid'] or state['OOMKilled'] or state['ExitCode'] != expected_exit:
                    raise ValueError('worker Stop did not produce its required exact exit')
                logs = subprocess.run([*reference.DOCKER, 'logs', name], capture_output=True, text=True, check=True, timeout=5)
                worker_log = logs.stdout + logs.stderr
                (output / 'original-worker.log').write_text(worker_log)
                expected_error = {'changed-identity': 'immutable base identity or read-only mount changed',
                                  'nested-mount': 'immutable base contains a descendant mount',
                                  'incomplete-base': 'worker exit refuses an unconfirmed base',
                                  'incomplete-object': 'worker exit refuses an unconfirmed sandbox',
                                  'held-operation': 'resource temporarily unavailable'}.get(mode)
                if expected_error and expected_error not in worker_log:
                    raise ValueError('worker Stop failed without its exact required refusal')
                if mode != 'empty':
                    reference.owned_delete('container', name, OWNER)
                    identity = reference.docker(command)
                    replacement = consumer_worker_loss.ready(name, OWNER, identity, deadline)
                    row['replacement'] = {'identity': identity, **replacement}
                    if replacement['status']['epoch'] == status['epoch']:
                        raise ValueError('replacement reused its old epoch')
                    after = backing_observation(identity, base, status['lifecycle']['root'])
                    row['backing_after'] = after
                    before_backing = row['backing_before']
                    if after['covering_mounts']:
                        raise ValueError('old canonical base mount survived worker replacement')
                    keys = ['payload_sha256', 'inode', 'mode', 'uid', 'gid', 'mtime_ns', 'ctime_ns', 'base_record_sha256']
                    if any(after[key] != before_backing[key] for key in keys):
                        raise ValueError('worker Stop changed persistent base data or provenance')
                    if mode == 'base':
                        if (not after['intent'] or not after['complete'] or not after['complete']['completed_at'] or
                                after['intent']['bases'] != [base] or after['complete']['bases'] != [base]):
                            raise ValueError('canonical release lacks its exact durable intent and completion')
                        cli = ['/opt/factorybox/factorybox', '--root', status['lifecycle']['root'],
                               '--runtime', status['lifecycle']['runtime'], '--runtime-sha256', status['lifecycle']['runtime_sha256'],
                               'state', 'unused']
                        replay = subprocess.run([*reference.DOCKER, 'exec', identity, *cli], capture_output=True, text=True, timeout=10)
                        row['sealed_epoch_probe'] = {'exit': replay.returncode, 'stdout': replay.stdout, 'stderr': replay.stderr}
                        if replay.returncode != 1 or 'controller epoch is closed for worker exit' not in replay.stderr:
                            raise ValueError('old controller epoch accepted an operation after release')
                    elif after['intent'] is not None or after['complete'] is not None:
                        raise ValueError('refused shutdown mutated the canonical release ledger')
                    reference.docker(['container', 'stop', '--time=15', identity], timeout=20)
                    stopped = consumer_worker_loss.exact_worker(name, OWNER, identity)['State']
                    row['replacement_state'] = stopped
                    if stopped['Running'] or stopped['Pid'] or stopped['OOMKilled'] or stopped['ExitCode']:
                        raise ValueError('fresh empty replacement failed to stop cleanly')
                row['passed'] = True
            except Exception as error:
                row['passed'] = False
                row['error'] = str(error)
            finally:
                preserve = False
                try:
                    if attempted:
                        logs = subprocess.run([*reference.DOCKER, 'logs', name], capture_output=True, text=True, timeout=5)
                        (output / 'worker.log').write_text(logs.stdout + logs.stderr)
                        reference.owned_delete('container', name, OWNER)
                    if created and not row['passed']:
                        try:
                            upstream_bats.archive_stopped_volumes(output, name, OWNER, [name], worker['image'], row, timeout=120, compressed=True)
                        except Exception as error:
                            row['archive_error'] = str(error)
                            preserve = True
                    if created and not preserve:
                        reference.owned_delete('volume', name, OWNER)
                    row['cleanup'] = not preserve
                finally:
                    clean = clean and row['cleanup']
                    (output / 'result.json').write_text(json.dumps(row, indent=2) + '\n')
                    print(json.dumps({'case': mode, 'passed': row['passed'], 'cleanup': row['cleanup'], 'state': row.get('state')}), flush=True)
        result['passed'] = len(result['cases']) == 7 and all(r['passed'] for r in result['cases'])
    finally:
        result['cleanup'] = clean
        result['global_sysctls_after'] = consumer_worker_loss.sysctls()
        result['passed'] = result['passed'] and clean and before == result['global_sysctls_after']
        (HERE / 'result.json').write_text(json.dumps(result, indent=2) + '\n')
        print(json.dumps({'evidence': str(HERE), 'passed': result['passed'], 'cleanup': result['cleanup']}), flush=True)
    if not result['passed']:
        raise SystemExit(1)

def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--worker-build', required=True)
    options = parser.parse_args()
    artifact = reference.new_artifact('factorybox-bounded-base-stop-' + uuid.uuid4().hex)
    artifact.mkdir()
    shutil.copyfile(__file__, artifact / 'runner.py')
    run(artifact, options.worker_build, artifact.name.rsplit('-', 1)[1])

if __name__ == '__main__':
    main()
