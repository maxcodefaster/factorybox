"""Verify clean drain and real recovery after aggregate metadata refusal."""
import json
import subprocess
import time

import consumer_worker_loss
import reference


INVENTORY = r'''
import hashlib,json,os,pathlib,stat,sys
request=json.load(sys.stdin);root=pathlib.Path(request['status']['lifecycle']['root'])
proof=request['proof'];before=request.get('before');mode=sys.argv[1]
if str(root)!=proof['controller_root']:raise ValueError('metadata proof belongs to another controller')
ids=['source','ledger-paused-a','ledger-paused-b']+(['ledger-paused-c'] if proof['minimal_input'] else [])
def sha(path):
 h=hashlib.sha256()
 fd=os.open(path,os.O_RDONLY|os.O_NOFOLLOW|os.O_NOATIME)
 with os.fdopen(fd,'rb') as stream:
  for data in iter(lambda:stream.read(1<<20),b''):h.update(data)
 return h.hexdigest()
def file(path):
 info=path.lstat()
 if not stat.S_ISREG(info.st_mode):raise ValueError('nonregular protected file: '+str(path))
 return {'bytes':info.st_size,'sha256':sha(path)}
def tree(path):
 digest=hashlib.sha256();count=0
 def visit(current,relative):
  nonlocal count
  info=current.lstat();row=[relative,info.st_mode,info.st_uid,info.st_gid,info.st_mtime_ns,info.st_ino,info.st_nlink]
  row.append({name:os.getxattr(current,name,follow_symlinks=False).hex()
   for name in sorted(os.listxattr(current,follow_symlinks=False))})
  if stat.S_ISREG(info.st_mode):row.extend([info.st_size,sha(current)])
  elif stat.S_ISLNK(info.st_mode):row.append(os.readlink(current))
  elif not stat.S_ISDIR(info.st_mode):row.append(info.st_rdev)
  digest.update(json.dumps(row,separators=(',',':'),ensure_ascii=True).encode()+b'\n');count+=1
  if stat.S_ISDIR(info.st_mode):
   for child in sorted(current.iterdir(),key=lambda p:p.name):visit(child,relative+'/'+child.name)
 visit(path,'.')
 return {'entries':count,'sha256':digest.hexdigest()}
if sorted(p.name for p in (root/'objects').iterdir())!=sorted(ids):raise ValueError('unexpected paused inventory')
records={};values={}
for name in ids:
 directory=root/'objects'/name;record=json.loads((directory/'record.json').read_bytes());pause=record['pause']
 if record['id']!=name or record['phase']!='paused' or not pause.get('completed_at') or pause.get('resume'):raise ValueError('source is not a committed unresumed pause')
 capture=directory/'captures'/pause['snapshot_id'];data=(capture/'checkpoint.json').read_bytes()
 if not data.endswith(b'\n') or hashlib.sha256(data[:-1]).hexdigest()!=pause['checkpoint_sha256']:raise ValueError('pause checkpoint identity changed')
 checkpoint=json.loads(data);files={}
 for item in checkpoint['files']:
  relative=pathlib.PurePosixPath(item['name'])
  if relative.is_absolute() or '..' in relative.parts:raise ValueError('invalid checkpoint payload path')
  found=file(capture/relative)
  if found!={'bytes':item['bytes'],'sha256':item['sha256']}:raise ValueError('protected checkpoint payload changed')
  files[item['name']]=found
 trees={item['name']:tree(pathlib.Path(item['saved'])) for item in checkpoint['filesystem']['roots']}
 records[name]={'record':file(directory/'record.json'),'config':file(directory/'config.json'),
  'checkpoint':file(capture/'checkpoint.json'),'files':files,'trees':trees}
 values[name]=record
bases={};base_values={}
for directory in sorted((root/'bases').iterdir()):
 base=json.loads((directory/'base.json').read_bytes())
 if directory.name!=base['id'] or not base.get('prepared_at'):raise ValueError('unconfirmed base inventory')
 bases[base['id']]={'base':file(directory/'base.json'),'intent':file(directory/'intent.json'),
  'tree':tree(pathlib.Path(base['tree']['saved']))}
 base_values[base['id']]=base
if len(bases)!=proof['actual_public_base_imports'] or len(records)!=proof['actual_committed_pauses']:raise ValueError('inventory count differs from admission proof')
inventory={'records':records,'bases':bases}
if before is not None and inventory!=before:raise ValueError('drain or recovery changed protected source data')
ledgers={}
for name in ['worker-exit.json','worker-exit-complete.json']:
 path=root/name
 if mode=='before':
  if os.path.lexists(path):raise ValueError('refusal unexpectedly fenced its worker')
  continue
 data=path.read_bytes();value=json.loads(data)
 if len(data)>4<<20:raise ValueError('actual exit ledger exceeded reader bound')
 expected={'version':1,'root':str(root),'boot_id':request['status']['boot_id'],
  'runtime_sha256':request['status']['lifecycle']['runtime_sha256'],
  'criu_sha256':request['status']['lifecycle']['criu_sha256'],
  'manager_root':request['status']['lifecycle']['manager_root']}
 if any(value.get(key)!=item for key,item in expected.items()) or not value.get('requested_at'):raise ValueError('exit ledger recipe changed')
 if bool(value.get('completed_at'))!=(name=='worker-exit-complete.json'):raise ValueError('exit ledger completion is inconsistent')
 if {item['id']:item for item in value.get('paused',[])}!=values or len(value.get('paused',[]))!=len(values):raise ValueError('exit ledger lost a committed pause')
 if {item['id']:item for item in value['bases']}!=base_values or len(value['bases'])!=len(base_values):raise ValueError('exit ledger lost a canonical base')
 ledgers[name]={'bytes':len(data),'sha256':hashlib.sha256(data).hexdigest(),
  'paused_count':len(values),'base_count':len(base_values)}
if mode!='before':
 mounts={line.split()[4] for line in open('/proc/self/mountinfo')}
 if any(path.startswith(str(root)+'/') for path in mounts):raise ValueError('old controller retained a mount after drain')
print(json.dumps({'passed':True,'inventory':inventory,'ledgers':ledgers}),flush=True)
'''


def remaining(deadline):
    seconds = deadline - time.monotonic()
    if seconds <= 0:
        raise TimeoutError('worker metadata exhausted its 420-second lifecycle')
    return seconds


def inventory(identity, artifact, label, request, deadline):
    done = subprocess.run([*reference.DOCKER, 'exec', '--interactive', identity,
                           'python3', '-c', INVENTORY, label], input=json.dumps(request),
                          capture_output=True, text=True, timeout=remaining(deadline))
    (artifact / (label + '.stdout')).write_text(done.stdout)
    (artifact / (label + '.stderr')).write_text(done.stderr)
    if done.returncode:
        raise ValueError('worker metadata protected inventory failed: ' + label)
    return json.loads(done.stdout)


def stop(name, owner, identity, deadline):
    consumer_worker_loss.exact_worker(name, owner, identity)
    grace = min(45, int(remaining(deadline)) - 1)
    if grace <= 0:
        raise TimeoutError('worker metadata has no graceful stop budget')
    reference.docker(['container', 'stop', '--time=' + str(grace), identity], timeout=remaining(deadline))
    state = consumer_worker_loss.exact_worker(name, owner, identity)['State']
    if state['Running'] or state['Pid'] or state['OOMKilled'] or state['ExitCode']:
        raise ValueError('worker metadata inventory did not stop cleanly')
    return state


def drain(artifact, result, command, name, owner, identity, deadline, proof):
    record = result['worker_metadata_drain'] = {'passed': False, 'source_removed': False}
    request = {'proof': proof, 'status': result['startup']['status']}
    consumer_worker_loss.exact_worker(name, owner, identity)
    before = inventory(identity, artifact, 'before', request, deadline)
    request['before'] = before['inventory']
    record['before_inventory_sha256'] = reference.sha(artifact / 'before.stdout')
    record['source_state'] = stop(name, owner, identity, deadline)
    (artifact / 'source-worker.log').write_text(reference.docker(['logs', identity]))
    reference.owned_delete('container', name, owner)
    record['source_removed'] = True
    replacement_name = name + '-replacement'
    replacement_command = list(command)
    replacement_command[replacement_command.index('--name') + 1] = replacement_name
    selection = 'FACTORYBOX_RETIREMENT_TEST=^TestNativeWorkerMetadataBaseAdmission$'
    replacement_command[replacement_command.index(selection)] = 'FACTORYBOX_RETIREMENT_TEST=^TestNativeBoundedLossRecover$'
    record['command'] = replacement_command
    replacement = None
    attempted = False
    try:
        attempted = True
        replacement = reference.docker(replacement_command)
        record['container_id'] = replacement
        record['startup'] = consumer_worker_loss.ready(replacement_name, owner, replacement, deadline)
        status, old = record['startup']['status'], request['status']
        if (status['epoch'] == old['epoch'] or status['lifecycle']['root'] == old['lifecycle']['root'] or
                status['boot_id'] != old['boot_id'] or status['store_id'] != old['store_id'] or
                status['lifecycle']['bounded_store']['image'] != old['lifecycle']['bounded_store']['image']):
            raise ValueError('replacement did not preserve the store with a fresh epoch')
        after = inventory(replacement, artifact, 'after-drain', request, deadline)
        record['ledgers'] = after['ledgers']
        with (artifact / 'recovery.stdout').open('x') as stdout, (artifact / 'recovery.stderr').open('x') as stderr:
            done = subprocess.run([*reference.DOCKER, 'exec', replacement, '/bin/bash', '/setup.sh'],
                                  stdout=stdout, stderr=stderr, timeout=remaining(deadline))
        log = (artifact / 'recovery.stdout').read_text()
        record['recovery_exit_code'] = done.returncode
        if done.returncode or sum(line.startswith('--- PASS: TestNativeBoundedLossRecover ') for line in log.splitlines()) != 1 or '--- SKIP:' in log:
            raise ValueError('required real post-drain recovery assertions failed or skipped')
        recovered = json.loads((artifact / 'output/bounded-loss-recovered.json').read_text())
        required = ['passed', 'same_exact_image', 'new_epoch', 'wrong_hash_refused', 'exact_replay',
                    'private_files_preserved', 'independent_fork', 'fork_of_resumed_descendant',
                    'useful_after_source_delete', 'exact_sandbox_cleanup']
        if any(recovered.get(key) is not True for key in required) or recovered.get('restored_private_ram_bytes') != 32 << 20:
            raise ValueError('real post-drain recovery proof incomplete')
        record['recovery_proof_sha256'] = reference.sha(artifact / 'output/bounded-loss-recovered.json')
        after = inventory(replacement, artifact, 'after-recovery', request, deadline)
        if after['ledgers'] != record['ledgers']:
            raise ValueError('recovery changed the old exit ledgers')
        record['protected_source_data_unchanged'] = True
        record['replacement_state'] = stop(replacement_name, owner, replacement, deadline)
        record['passed'] = True
    finally:
        if attempted:
            if replacement:
                consumer_worker_loss.exact_worker(replacement_name, owner, replacement)
            logs = subprocess.run([*reference.DOCKER, 'logs', replacement_name], capture_output=True, text=True, timeout=5)
            (artifact / 'replacement-worker.log').write_text(logs.stdout + logs.stderr)
            reference.owned_delete('container', replacement_name, owner)
