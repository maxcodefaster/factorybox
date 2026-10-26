#!/usr/bin/env python3
"""Owned native acceptance of Factory's actual image, bootstrap and Connector."""
import argparse
import datetime
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import select
import subprocess
import time
import uuid

import build_worker
import build_config
import consumer_worker_loss
import native_base
import reference
import upstream_bats
import upstream_unit


def require_native_worker(worker, host, candidate):
    native_base.require_native_host(worker["platform"], host)
    actual = worker.get("native_candidate")
    if candidate:
        if actual != build_worker.native_candidate_profile(worker["platform"], host):
            raise ValueError("Factory consumer candidate must match its exact native kernel recipe")
    elif actual is not None:
        raise ValueError("unqualified Factory consumer worker requires explicit --native-candidate")


def load_consumer(name, platform):
    if not re.fullmatch(r'factorybox-build-consumer-[0-9a-f]{32}', name):
        raise ValueError('requires one exact current Factory consumer build')
    root = reference.ROOT / 'artifacts' / name
    if root.resolve() != root or (root / 'result.json').is_symlink():
        raise ValueError('Factory consumer build cannot be redirected')
    consumer = json.loads((root / 'result.json').read_text())
    if (consumer.get('passed') is not True or consumer.get('cleanup') is not True or
            consumer.get('platform') != platform or
            consumer.get('hub_bun') != '/opt/factory/opencode/musl/bun' or
            set(consumer.get('images', {})) != {'runtime', 'hub'} or
            upstream_unit.context_manifest(root / 'context') != consumer.get('source_context')):
        raise ValueError('rebuild the exact current native Factory consumer and Linux Bun inputs')
    for role, key in [('runtime', 'image'), ('hub', 'hub_image')]:
        image = consumer['images'][role]
        if (image.get('platform') != platform or consumer[key] != image.get('image') or
                not re.fullmatch(r'[a-z0-9][a-z0-9./:_-]*@sha256:[0-9a-f]{64}', consumer[key])):
            raise ValueError('Factory consumer image roles or platform differ')
        inspected = json.loads(reference.docker(['image', 'inspect', image['image']]))[0]
        native_base.verify_image(image, inspected)
    return root, consumer


def run_owned_container(run, containers, name, arguments, **options):
    # A timeout is an unknown create outcome, not proof that Docker created
    # nothing. Register the exact cleanup identity before the external call.
    containers.append(name)
    return run(['run', '--name', name, *arguments], **options)


def worker_http_ready(run, worker_id, helper_id, worker_pid, verify):
    if type(worker_pid) is not int or worker_pid <= 1 or helper_id == worker_id:
        raise ValueError('Worker readiness requires a separate owned native network helper')
    verify()
    # Enter only the worker's network namespace. The probe remains in the
    # helper's cgroup, so it cannot race PID 1's initial cgroup delegation.
    probe = ("import urllib.request;"
             "client=urllib.request.build_opener(urllib.request.ProxyHandler({}));"
             "print(client.open('http://127.0.0.1:8081/readyz',timeout=2).status)")
    response = run(['exec', helper_id, 'nsenter', '--net=/proc/'+str(worker_pid)+'/ns/net',
                    '--', 'python3', '-c', probe], check=False, timeout=5)
    return response.strip() == '204'


def preparation_assignment(files, reserve):
    names = ['slot.json'] if reserve else ['assignment.json', 'parent-token', 'child-token']
    return {name: files[name] for name in names}


def preparation_identity(request):
    return {name: value for name, value in request.items() if name != 'assignment'}


def consumer_sandbox_id(slot_id, attempt_id, reserve):
    # The used slot retains its original Sandbox. Assignment changes only
    # Factory's attempt and role, never the immutable native identity.
    execution_id = slot_id if reserve else attempt_id
    return 'runtime-' + execution_id.replace('-', '')


def settle_lane_result(result):
    passed = all(result.get(key) is True for key in
                 ['passed', 'cleanup', 'host_sysctls_unchanged', 'inputs_unchanged'])
    result['passed'] = passed
    if result['diagnostic_only']:
        result['diagnostic_control_passed'] = passed
        result['passed'] = False
    return passed


def require_paused_consumer(view, sandbox, runtime, snapshot):
    record = view.get('record', {})
    pause = record.get('pause', {})
    memory = pause.get('snapshot', {})
    if (record.get('id') != sandbox or record.get('runtime_id') != runtime or
            record.get('phase') != 'paused' or pause.get('snapshot_id') != snapshot or
            not pause.get('completed_at') or pause.get('after', {}).get('anon') != 0 or
            memory.get('file_backed') is not True or memory.get('resident_bytes_after') != 0):
        raise ValueError('Consumer Pause did not commit the original Sandbox and release its RAM')


def retire_resumed_checkpoint(paused, resumed, sandbox, runtime, execute):
    record = paused.get('record', {})
    saved = record.get('pause', {})
    snapshot = saved.get('snapshot_id')
    owner, digest = record.get('owner'), saved.get('checkpoint_sha256')
    if (not isinstance(snapshot, str) or not re.fullmatch(r'[a-f0-9]{32}', snapshot) or
            not isinstance(owner, str) or not re.fullmatch(r'[a-f0-9]{32}', owner) or
            not isinstance(digest, str) or not re.fullmatch(r'[a-f0-9]{64}', digest)):
        raise ValueError('Checkpoint retirement requires its exact completed native identity')
    require_paused_consumer(paused, sandbox, runtime, snapshot)
    after = resumed.get('record', {})
    continuation = after.get('pause', {})
    if (after.get('id') != sandbox or after.get('runtime_id') != runtime or
            after.get('owner') != owner or after.get('phase') != 'running' or
            continuation.get('snapshot_id') != snapshot or
            not continuation.get('resume', {}).get('completed_at') or
            resumed.get('runtime', {}).get('status') != 'running'):
        raise ValueError('Checkpoint retirement requires the original completed Resume')
    request = {'source_id': sandbox, 'snapshot_id': snapshot,
               'source_owner': owner, 'checkpoint_sha256': digest}
    retired = execute(dict(request))
    before_bytes = retired.get('before', {}).get('filesystem_available_bytes')
    after_bytes = retired.get('after', {}).get('filesystem_available_bytes')
    if (retired.get('version') != 1 or retired.get('request') != request or
            not retired.get('completed_at') or type(before_bytes) is not int or
            type(after_bytes) is not int or before_bytes < 0 or after_bytes <= before_bytes):
        raise ValueError('Checkpoint retirement did not confirm exact ownership and returned capacity')
    return retired


def retire_obsolete_resume_images(latest, previous, sandbox, runtime, execute):
    before = previous.get('record', {})
    pause = before.get('pause', {})
    snapshot, owner, digest = pause.get('snapshot_id'), before.get('owner'), pause.get('checkpoint_sha256')
    if (before.get('id') != sandbox or before.get('runtime_id') != runtime or
            before.get('phase') != 'running' or previous.get('runtime', {}).get('status') != 'running' or
            not pause.get('completed_at') or not pause.get('resume', {}).get('completed_at') or
            not isinstance(snapshot, str) or not re.fullmatch('[a-f0-9]{32}', snapshot) or
            not isinstance(owner, str) or not re.fullmatch('[a-f0-9]{32}', owner) or
            not isinstance(digest, str) or not re.fullmatch('[a-f0-9]{64}', digest)):
        raise ValueError('Resume-image retirement requires the exact original completed Resume')
    after = latest.get('record', {})
    if (after.get('id') != sandbox or after.get('runtime_id') != runtime or after.get('owner') != owner):
        raise ValueError('Resume-image retirement has a different latest Sandbox identity')
    if after.get('phase') == 'deleted':
        if not after.get('delete', {}).get('completed_at') or latest.get('runtime'):
            raise ValueError('Resume-image retirement requires confirmed Delete')
    else:
        current = after.get('pause', {}).get('snapshot_id')
        current_digest = after.get('pause', {}).get('checkpoint_sha256')
        if (not isinstance(current, str) or not re.fullmatch('[a-f0-9]{32}', current) or
                not isinstance(current_digest, str) or not re.fullmatch('[a-f0-9]{64}', current_digest)):
            raise ValueError('Resume-image retirement requires a complete later checkpoint identity')
        if current == snapshot:
            raise ValueError('The active Resume images remain protected')
        require_paused_consumer(latest, sandbox, runtime, current)
    request = {'source_id': sandbox, 'snapshot_id': snapshot,
               'source_owner': owner, 'checkpoint_sha256': digest}
    retired = execute(dict(request))
    before_bytes = retired.get('before', {}).get('filesystem_available_bytes')
    after_bytes = retired.get('after', {}).get('filesystem_available_bytes')
    if (retired.get('version') != 1 or retired.get('payload_kind') != 'resume-images' or
            retired.get('request') != request or not retired.get('completed_at') or
            type(before_bytes) is not int or type(after_bytes) is not int or
            before_bytes < 0 or after_bytes <= before_bytes):
        raise ValueError('Resume-image retirement did not return exact owned capacity')
    return retired


def repeated_consumer_cycles(count, result, previous, pause, resume, useful, inspect, phase, retire=None):
    if type(count) is not int or count < 1:
        raise ValueError('Repeated consumer cycles require a positive count')
    sandbox = result['sandboxID']
    runtime = result['start']['record']['runtime_id']
    for cycle in range(2, count+1):
        entry = {'cycle': cycle, 'snapshot_id': uuid.uuid4().hex, 'pause_attempted': True}
        result.setdefault('repeat_cycles', []).append(entry)
        entry['pause'] = pause(entry)
        require_paused_consumer(entry['pause'], sandbox, runtime, entry['snapshot_id'])
        entry['resume_attempted'] = True
        entry['resume'] = resume(entry)
        record = entry['resume'].get('record', {})
        if (record.get('id') != sandbox or record.get('runtime_id') != runtime or
                record.get('phase') != 'running' or
                record.get('pause', {}).get('snapshot_id') != entry['snapshot_id'] or
                not record.get('pause', {}).get('resume', {}).get('completed_at')):
            raise ValueError('Repeated Resume replaced its original Sandbox or lacks exact completion')
        entry['after'] = useful()
        after = entry['after']
        if (after['nonce'] != previous['nonce'] or after['pid'] != previous['pid'] or
                after['count'] <= previous['count'] or after['editor'] != previous['editor']):
            raise ValueError('Repeated Resume restarted app memory or changed canonical Source')
        entry['child_after'] = inspect()
        child = entry['child_after']
        if not child['State']['Running'] or child['RestartCount'] != 0:
            raise ValueError('Repeated Resume restarted the original Docker child')
        if retire is not None:
            entry['retirement_attempted'] = True
            entry['retirement'] = retire(entry)
        entry['passed'] = True
        previous = after
        phase('assigned-sandbox-resume-cycle-'+str(cycle))
    return previous


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--worker-build', required=True)
    parser.add_argument('--consumer-build', required=True)
    parser.add_argument('--factory-repo', type=Path, required=True)
    parser.add_argument('--project-images', required=True)
    parser.add_argument('--reserve', action='store_true')
    parser.add_argument('--observe-resume-cgroup', action='store_true',
                        help='Read the original cgroup during assigned Resume; diagnostic only, no freeze or task changes')
    parser.add_argument('--resume-cycles', type=int, default=1,
                        help='Pause/Resume the same assigned stack this many times; multiple cycles are diagnostic only')
    parser.add_argument('--retire-resumed-checkpoints', action='store_true',
                        help='Diagnostic repeat only: explicitly retire each verified resumed checkpoint and require returned capacity')
    parser.add_argument('--retire-resume-images', action='store_true',
                        help='With diagnostic checkpoint retirement: retire superseded private Resume images and verify only the current generation remains')
    parser.add_argument('--retire-deleted-backing', action='store_true',
                        help='After confirmed Delete and exact private mount release, retire private Sandbox filesystem backing')
    parser.add_argument('--warmup-control', action='store_true',
                        help='Diagnostic only: exercise and remove a stock container before standby Pause; cannot qualify a release')
    parser.add_argument('--terminal', action='store_true',
                        help='Require actual idle-PTY state across native Pause/Resume and mapped-user Git on shared Source')
    parser.add_argument('--git', action='store_true',
                        help='Require mapped-user Git without adding a terminal to the native restore')
    parser.add_argument('--native-candidate', action='store_true')
    args = parser.parse_args()
    if args.warmup_control and not args.reserve:
        parser.error('--warmup-control requires --reserve')
    if args.resume_cycles < 1 or args.warmup_control and args.resume_cycles != 1:
        parser.error('--resume-cycles must be positive and warmup-control requires one cycle')
    if args.retire_resumed_checkpoints and args.resume_cycles < 2:
        parser.error('--retire-resumed-checkpoints requires a diagnostic repeat')
    if args.retire_resume_images and not args.retire_resumed_checkpoints:
        parser.error('--retire-resume-images requires --retire-resumed-checkpoints')
    print(json.dumps({'stage':'verify-existing-inputs','worker_build':args.worker_build,
                      'consumer_build':args.consumer_build}),flush=True)
    worker_root, worker = build_worker.load(args.worker_build)
    host = json.loads(reference.docker(['info', '--format', '{{json .}}']))
    require_native_worker(worker, host, args.native_candidate)
    platform = worker['platform']
    architecture = build_config.architecture(platform)
    consumer_root, consumer = load_consumer(args.consumer_build, platform)
    repo = args.factory_repo.resolve()
    for name, digest in consumer['source_context'].items():
        if (repo / name).is_symlink() or reference.sha(repo / name) != digest['sha256']:
            raise ValueError('current Factory consumer source differs from its exact image build')
    if not re.fullmatch(r'factorybox-devx-project-images-[0-9a-f]{32}', args.project_images):
        raise ValueError('requires one exact completed native project-image archive')
    projects = reference.ROOT/'artifacts'/args.project_images
    images = json.loads((projects/'result.json').read_text())
    if (not images['passed'] or reference.sha(projects/'images.tar') != images['archive_sha256'] or
            any(row.get('architecture') != architecture or row.get('os') != 'linux' for row in images['images'])):
        raise ValueError('Project image archive changed')
    node = images['images'][0]['image']
    if not node.startswith('docker.io/library/node:') or len(images['images']) != 1:
        raise ValueError('Requires the retained exact Node project image')
    owner = uuid.uuid4().hex
    attempt = str(uuid.uuid4())
    sandbox = consumer_sandbox_id(owner, attempt, args.reserve)
    pod_uid = str(uuid.uuid4())
    name = 'factorybox-factory-consumer-'+owner
    output = reference.new_artifact(name)
    output.mkdir(mode=0o755)
    shutil.copyfile(__file__, output/'runner.py')
    namespace_observer = output/'namespace-observer.py'
    shutil.copyfile(reference.ROOT/'tools/consumer_namespace_observer.py', namespace_observer)
    cgroup_observer = output/'cgroup-observer.py'
    shutil.copyfile(reference.ROOT/'tools/consumer_cgroup_observer.py', cgroup_observer)
    frozen = output/'factory';frozen.mkdir()
    for file in ['prepare.py','prepare_test.py','export-rootfs.sh','bootstrap.sh','reserve-bootstrap.sh']:
        shutil.copyfile(repo/'packages/runtime-kubernetes/src/factorybox'/file,frozen/file)
    hub = output/'hub.js'
    subprocess.run(['mise','exec','--','bun','build','scripts/runtime/factorybox-consumer-hub.ts',
                    '--target=bun','--outfile',str(hub)],cwd=repo,check=True,capture_output=True,timeout=30)
    bun = Path(subprocess.check_output(['mise','exec','--','which','bun'],cwd=repo,text=True).strip())
    mirror_source=output/'registry-source';shutil.copytree(reference.ROOT/'tests/devx-registry',mirror_source/'tests/devx-registry')
    for file in ['go.mod','go.sum']:shutil.copyfile(reference.ROOT/file,mirror_source/file)
    mirror=output/'native-registry'
    source_hashes={str(p.relative_to(mirror_source)):reference.sha(p) for p in mirror_source.rglob('*') if p.is_file()}
    reused_mirror=None
    for previous in sorted((reference.ROOT/'artifacts').glob('factorybox-factory-consumer-*/result.json')):
        saved=json.loads(previous.read_text());binary=previous.parent/'native-registry';source=previous.parent/'registry-source'
        if saved.get('platform') != platform or saved.get('cleanup') is not True or not binary.is_file() or binary.is_symlink() or not source.is_dir():continue
        if source_hashes!={str(p.relative_to(source)):reference.sha(p) for p in source.rglob('*') if p.is_file()}:continue
        if reference.sha(binary)!=saved.get('registry_binary_sha256'):raise ValueError('Closed fixture binary changed')
        os.link(binary,mirror);reused_mirror=str(previous);break
    if not reused_mirror:
        subprocess.run(['go','build','-trimpath','-buildvcs=false','-o',str(mirror),'./tests/devx-registry'],
                       cwd=mirror_source,env=dict(os.environ,GOOS='linux',GOARCH=architecture,CGO_ENABLED='0'),check=True,capture_output=True,timeout=90)
    build_config.require_elf(mirror, platform)
    pins=output/'project-images.json';pins.write_text(json.dumps(images['images']))
    result = {'schema':1,'owner':owner,'attemptID':attempt,'sandboxID':sandbox,'podUID':pod_uid,'passed':False,'cleanup':False,
              'platform':platform,'native_candidate':worker.get('native_candidate'),
              'worker_build':args.worker_build,'consumer_build':args.consumer_build,'consumer_image':consumer['image'],
              'reserve':args.reserve,'reserve_images':[node] if args.reserve else None,
              'terminal_proof':args.terminal,
              'diagnostic_only':args.warmup_control or args.observe_resume_cgroup or args.resume_cycles != 1,
              'resume_cycles':args.resume_cycles,
              'retire_resumed_checkpoints':args.retire_resumed_checkpoints,
              'retire_resume_images':args.retire_resume_images,
              'retire_deleted_backing':args.retire_deleted_backing,
              'namespace_observer_sha256':reference.sha(namespace_observer),
              'cgroup_observer_sha256':reference.sha(cgroup_observer),
              'observe_resume_cgroup':args.observe_resume_cgroup,
              'git_proof':args.git or args.terminal,
              'fixture_registry_mirror':True,'registry_binary_sha256':reference.sha(mirror),
              'registry_source_sha256':source_hashes,'registry_reused_from':reused_mirror,
              'native_profile_only':True,'kubernetes_qualified':False,'production_qualified':False,
              'source_sha256':{p.name:reference.sha(p) for p in frozen.iterdir()},
              'hub_sha256':reference.sha(hub),'bun_sha256':reference.sha(bun),
              'hub_image':consumer['hub_image'],'hub_bun':consumer['hub_bun'],'commands':[]}
    phase_started=time.monotonic()
    def phase(stage):
        observation={'stage':stage,'elapsed_seconds':round(time.monotonic()-phase_started,3)}
        result.setdefault('stages',[]).append(observation)
        print(json.dumps({'evidence':str(output),**observation}),flush=True)
    phase('prepare-owned-fixture')
    before = consumer_worker_loss.sysctls(docker_image=worker['native_base_image'],
        owner=owner, artifact=output, phase='before', factory_network=True)
    result['host_sysctls_before'] = before
    deadline = time.monotonic()+420
    containers = []
    volumes = []
    worker_id = server_id = None
    live_pid = None

    def run(arguments, *, body=None, timeout=30, check=True):
        started = time.monotonic()
        try:
            done = subprocess.run([*reference.DOCKER,*arguments],input=body,capture_output=True,
                                  text=True,timeout=max(.1,min(timeout,deadline-started)))
        except subprocess.TimeoutExpired as error:
            result['commands'].append({'arguments':arguments,'timeout':error.timeout,
                                       'seconds':time.monotonic()-started,'outcome':'unconfirmed'})
            raise
        result['commands'].append({'arguments':arguments,'exit':done.returncode,'stdout':done.stdout,'stderr':done.stderr})
        if check and done.returncode:
            raise ValueError('Native Factory consumer command failed: '+done.stderr[-2500:])
        return done.stdout

    def own(label, identity):
        return consumer_worker_loss.exact_worker(label,owner,identity)

    def wait(read, accepts, message, seconds=45):
        until = min(deadline,time.monotonic()+seconds)
        last = None
        while time.monotonic()<until:
            try:
                last=read()
                if accepts(last):return last
            except (ValueError,json.JSONDecodeError):pass
            time.sleep(.2)
        raise TimeoutError(message+': '+str(last)[-3000:])

    def endpoint(path='/', method='GET'):
        script = ("const response=await fetch(" + json.dumps('http://127.0.0.1:18181'+path) +
                  ",{method:" + json.dumps(method) + ",headers:{'x-fixture-owner':" +
                  json.dumps(owner) + "},signal:AbortSignal.timeout(8000)});"
                  "if(!response.ok)throw new Error('Owned native endpoint HTTP '+response.status);"
                  "console.log(await response.text());")
        return json.loads(run(['exec',server_id,consumer['hub_bun'],'-e',script]))

    def memory():
        own(name,worker_id)
        script="import json,pathlib;root=pathlib.Path('/sys/fs/cgroup');print(json.dumps({'current':int((root/'memory.current').read_text()),'stat':dict((key,int(value)) for key,value in (line.split() for line in (root/'memory.stat').read_text().splitlines()))}))"
        return json.loads(run(['exec',worker_id,'python3','-c',script]))

    def namespaces(stage):
        own(name,worker_id)
        observed=json.loads(run(['exec',worker_id,'python3','/tmp/factorybox-namespace-observer.py',str(live_pid)]))
        result.setdefault('namespace_observations',{})[stage]=observed

    def resume_assigned(paused, snapshot, evidence, label):
        observer = None
        if args.observe_resume_cgroup:
            observer_id = uuid.uuid4().hex
            observation_path = '/tmp/factorybox-cgroup-'+observer_id+'.json'
            observed_request = {'runtime_id':paused['record']['runtime_id'],
                                'cgroup':paused['record']['cgroup'],'observer_id':observer_id}
            evidence['cgroup_observer_file'] = label+'.json'
            own(name, worker_id)
            observer = subprocess.Popen([*reference.DOCKER,'exec','-i',worker_id,'python3',
                                         '/tmp/factorybox-cgroup-observer.py'],
                                        stdin=subprocess.PIPE,stdout=subprocess.PIPE,stderr=subprocess.PIPE,text=True)
            observer.stdin.write(json.dumps(observed_request)+'\n');observer.stdin.close();observer.stdin=None
            readable, _, _ = select.select([observer.stdout], [], [], 10)
            if not readable or json.loads(observer.stdout.readline()) != {'ready':True,'observer_id':observer_id}:
                observer.kill();observer.communicate(timeout=5)
                raise ValueError('Original cgroup observer did not confirm its exact read-only identity')
        try:
            resumed = json.loads(run([*cli,'resume','--snapshot-id',snapshot,sandbox],timeout=90))
        except Exception as error:
            evidence['resume_error'] = str(error)
            raise
        finally:
            if observer is not None:
                try:
                    own(name, worker_id)
                    run(['exec',worker_id,'touch',observation_path.removesuffix('.json')+'.stop'],timeout=5)
                    observer_out, observer_err = observer.communicate(timeout=10)
                    evidence['cgroup_observer_exit'] = observer.returncode
                    evidence['cgroup_observer_stderr'] = observer_err
                    evidence['cgroup_observer_completion'] = observer_out
                    run(['cp',worker_id+':'+observation_path,str(output/(label+'.json'))],timeout=10)
                    observed = json.loads((output/(label+'.json')).read_text())
                    if (observer.returncode != 0 or observed.get('complete') is not True or
                            observed.get('read_only') is not True or observed.get('request') != observed_request):
                        raise ValueError('Original cgroup observation is incomplete or has a foreign identity')
                except Exception as diagnostic:
                    evidence['cgroup_observer_error'] = str(diagnostic)
                    if observer.poll() is None:
                        observer.kill();observer.communicate(timeout=5)
        if evidence.get('cgroup_observer_error'):
            raise ValueError(evidence['cgroup_observer_error'])
        return resumed

    try:
        for volume in [name,name+'-source']:
            volumes.append(volume)
            run(['volume','create','--label','com.factorybox.run='+owner,volume])
        # The image exporter has only the empty seed directory, no Source or assignment.
        helper = name+'-directories'
        run_owned_container(run,containers,helper,['--label','com.factorybox.run='+owner,'--network=none',
             '--cpus=.25','--memory=64m','--mount',f'type=volume,source={name},target=/volume',
             '--entrypoint','mkdir',worker['native_base_image'],'-p','/volume/seed'],timeout=65)
        exporter = name+'-export'
        started = time.monotonic()
        run_owned_container(run,containers,exporter,['--label','com.factorybox.run='+owner,'--network=none',
             '--cpus=1','--memory=256m','--cap-drop=ALL','--security-opt=no-new-privileges',
             '--tmpfs','/var/lib/docker:rw,size=16m',
             '--mount',f'type=volume,source={name},target=/factory-seed,volume-subpath=seed',
             '--env','FACTORY_POD_UID='+pod_uid,'--env','FACTORY_STORE_ID='+owner,
             '--env','FACTORY_RUNTIME_IMAGE='+consumer['image'],'--entrypoint','/bin/sh',consumer['image'],
             '-ec',(frozen/'export-rootfs.sh').read_text()],timeout=90)
        result['export_seconds'] = time.monotonic()-started
        phase('consumer-image-exported')
        for label in [exporter,helper]:
            reference.owned_delete('container',label,owner);containers.remove(label)
        server_name = name+'-hub'
        server_id = run_owned_container(run,containers,server_name,['--detach','--label','com.factorybox.run='+owner,
            '--network=none','--cpus=.5','--memory=192m','--pids-limit=32','--cap-drop=ALL',
            '--user','65534:65534','--read-only','--mount',f'type=bind,source={hub},target=/hub.js,readonly',
            '--entrypoint',consumer['hub_bun'],
            consumer['hub_image'],'/hub.js',owner,attempt],timeout=65).strip()
        worker_id = run_owned_container(run,containers,name,['--detach','--label','com.factorybox.run='+owner,
             '--privileged','--cgroupns=private','--cpus=3','--memory=3g','--pids-limit=512','--network=none',
             '--stop-timeout=45','--env','GOMAXPROCS=2','--env','GOMEMLIMIT=256MiB',
             '--env','FACTORY_POD_UID='+pod_uid,'--env','FACTORY_STORE_ID='+owner,
             '--mount',f'type=volume,source={name},target=/var/lib/sysbox',
             '--mount',f'type=volume,source={name}-source,target=/factory-source',worker['image'],
             '--store-id',owner,'--bounded-root','/var/lib/sysbox/fixed-store','--bounded-bytes',str(8<<30),
             '--health-listen','127.0.0.1:8081'],timeout=65).strip()
        w = own(name,worker_id);s = own(server_name,server_id)
        if w['HostConfig']['Memory']!=3<<30 or w['HostConfig']['NanoCpus']!=3_000_000_000 or w['HostConfig']['NetworkMode']!='none' or s['HostConfig']['NetworkMode']!='none':
            raise ValueError('Native fixture resource bounds changed')
        wp,sp = w['State']['Pid'],s['State']['Pid']
        network_name = name+'-network'
        network_id = run_owned_container(run,containers,network_name,['--detach','--label','com.factorybox.run='+owner,
            '--privileged','--pid=host','--userns=host','--network=none','--read-only',
            '--cpus=.25','--memory=64m','--pids-limit=32',
            '--entrypoint','/bin/sleep',worker['native_base_image'],'420'],timeout=65).strip()
        network = own(network_name,network_id)
        if network['HostConfig']['PidMode']!='host' or network['HostConfig']['NetworkMode']!='none':
            raise ValueError('Owned native network helper changed its namespace bounds')
        def verify_health():
            current = own(name,worker_id)
            if not current['State']['Running'] or current['State']['Pid']!=wp:
                raise RuntimeError('Original worker exited or changed before HTTP readiness')
            own(network_name,network_id)
        wait(lambda:worker_http_ready(run,worker_id,network_id,wp,verify_health),
             lambda ready:ready is True,'Worker HTTP readiness',120)
        result['worker_http_readiness']=True
        status=json.loads(run(['exec',worker_id,'/opt/factorybox/factorybox','worker-status']))
        if not status.get('ready_at') or status.get('store_id')!=owner:
            raise ValueError('HTTP-ready worker has a different admitted identity')
        result['worker_identity'] = status
        phase('worker-http-ready')
        run(['cp',str(namespace_observer),worker_id+':/tmp/factorybox-namespace-observer.py'])
        if args.observe_resume_cgroup:
            run(['cp',str(cgroup_observer),worker_id+':/tmp/factorybox-cgroup-observer.py'])
        probe = '/var/lib/sysbox/native-fixture-prepare.py'
        run(['cp',str(frozen/'prepare.py'),worker_id+':'+probe])
        run(['exec',worker_id,'python3','-c',(frozen/'prepare_test.py').read_text(),probe])
        result['preparation_ownership_controls_passed'] = True
        def ip(pid, arguments):
            if own(name,worker_id)['State']['Pid']!=wp or own(server_name,server_id)['State']['Pid']!=sp:
                raise ValueError('Owned fixture process changed')
            if pid not in {wp,sp}:raise ValueError('Refusing a foreign fixture network namespace')
            own(network_name,network_id)
            run(['exec',network_id,'nsenter','--net=/proc/'+str(pid)+'/ns/net','--','ip',*arguments],timeout=10)
        ip(wp,['link','add','name','proof-out','alias','fixture:'+owner,'type','veth','peer','name','proof-in','netns',str(sp)])
        for pid,interface,address in [(wp,'proof-out','192.0.2.1/30'),(sp,'proof-in','192.0.2.2/30')]:
            ip(pid,['addr','add',address,'dev',interface]);ip(pid,['link','set',interface,'up'])
        ip(wp,['route','add','default','via','192.0.2.2','dev','proof-out'])
        endpoint()
        project = output/'project';(project/'.devcontainer').mkdir(parents=True)
        (project/'.devcontainer/devcontainer.json').write_text(json.dumps({
            'image':node,'workspaceFolder':'/workspace/project',
            'workspaceMount':'source=${localWorkspaceFolder},target=/workspace/project,type=bind',
            'remoteUser':'node','updateRemoteUserUID':False,'overrideCommand':True,
            'runArgs':['--cap-drop=ALL','--security-opt=no-new-privileges']}))
        (project/'editor.txt').write_text('before')
        (project/'app.cjs').write_text("const fs=require('fs'),http=require('http'),crypto=require('crypto');const nonce=crypto.randomUUID();let count=0;http.createServer((req,res)=>{fs.writeFileSync('/workspace/project/runtime-write',nonce);res.setHeader('content-type','application/json');res.end(JSON.stringify({nonce,pid:process.pid,count:++count,editor:fs.readFileSync('/workspace/project/editor.txt','utf8')}))}).listen(3000,'0.0.0.0');\n")
        subprocess.run(['git','init','--initial-branch=main',str(project)],check=True,capture_output=True,timeout=10)
        for command in [['config','user.name','Native fixture'],['config','user.email','native@factory.invalid'],
                        ['config','factory.repositoryIdentity',owner],['remote','add','origin','file:///factory/native-repo'],
                        ['add','.'],['commit','-m','Owned native consumer fixture']]:
            subprocess.run(['git','-C',str(project),*command],check=True,capture_output=True,timeout=10)
        revision = subprocess.check_output(['git','-C',str(project),'rev-parse','HEAD'],text=True).strip()
        assignment = {'version':1,'runtimeAttemptId':attempt,'generation':1,
            'workspaceFolder':'/home/coder/project','remoteWorkspaceFolder':'/workspace/project',
            'repositoryUrl':'file:///factory/native-repo','repositoryIdentity':owner,'preserveWorkspace':not args.reserve,
            'revision':revision,'devcontainerPath':'.devcontainer/devcontainer.json','composeProjectName':'native_consumer',
            'controlUrl':'ws://192.0.2.2:18181/internal/runtime-connect','connectorStateDir':'/home/coder/connector-state',
            'channels':[{'name':'app','kind':'app','target':'http://127.0.0.1:3000','healthPath':'/'}],
            'processes':[{'name':'app','command':['node','/workspace/project/app.cjs']}]}
        if args.terminal:
            assignment['channels'].append({'name':'ide','kind':'ide'})
        inputs = {'assignment.json':json.dumps(assignment),'parent-token':owner+'-parent','child-token':owner+'-child',
                  'slot.json':json.dumps({'version':1,'id':owner,'role':'staging','images':[node],
                    'expiresAt':(datetime.datetime.now(datetime.timezone.utc)+datetime.timedelta(minutes=15)).isoformat()})}
        script = "import pathlib;ca=pathlib.Path('/factory/registry-ca');ca.mkdir(parents=True);(ca/'ca.crt').write_bytes(pathlib.Path('/etc/ssl/certs/ca-certificates.crt').read_bytes())"
        run(['exec',worker_id,'python3','-c',script])
        run(['exec',worker_id,'test','!','-e','/factory/assignment'])
        result['no_ambient_assignment']=True
        source_project='/factory-source/workspaces/'+owner+'/home/project'
        if not args.reserve:
            run(['exec',worker_id,'mkdir','-p',str(Path(source_project).parent)])
            run(['cp',str(project),worker_id+':'+source_project])
            run(['exec',worker_id,'chown','-R','101000:101000',source_project])
        request = {'version':3,'sandboxID':sandbox,'storeID':owner,'epoch':status['epoch'],'podUID':pod_uid,
                   'runtimeHostImage':consumer['image'],'sharedSource':not args.reserve,'reserveSeed':args.reserve,
                   'sourceWorkspaceID':owner if not args.reserve else None,
                   'assignment':preparation_assignment(inputs,args.reserve),
                   'bootstrap':'/factory/native-registry --archive=/factory/project-images.tar --pins=/factory/project-images.json >/var/log/native-registry.log 2>&1 &\nexec /bin/sh /factory/native-bootstrap.sh\n'}
        result['request'] = request
        started=time.monotonic()
        prepared = json.loads(run(['exec','-i',worker_id,'python3','-c',(frozen/'prepare.py').read_text()],body=json.dumps(request),timeout=180))
        result['preparation'] = prepared;result['preparation_seconds']=time.monotonic()-started
        phase('sandbox-input-prepared')
        for file,target in [(mirror,'native-registry'),(projects/'images.tar','project-images.tar'),
                            (pins,'project-images.json'),(frozen/('reserve-bootstrap.sh' if args.reserve else 'bootstrap.sh'),'native-bootstrap.sh')]:
            staged='/var/lib/sysbox/native-fixture-'+target
            run(['cp',str(file),worker_id+':'+staged])
            run(['exec',worker_id,'cp',staged,prepared['bundle']+'/rootfs/factory/'+target])
        mirror_config="import pathlib;p=pathlib.Path(root)/'etc/docker';p.mkdir(parents=True,exist_ok=True);(p/'daemon.json').write_text('{\"debug\":true,\"registry-mirrors\":[\"http://127.0.0.1:5000\"]}')"
        run(['exec',worker_id,'python3','-c','root='+repr(prepared['bundle']+'/rootfs')+'\n'+mirror_config])
        if args.reserve:
            run(['cp',str(project),worker_id+':/var/lib/sysbox/native-project'])
            run(['exec',worker_id,'cp','-a','/var/lib/sysbox/native-project',prepared['bundle']+'/rootfs/factory/native-repo'])
        cli=['exec',worker_id,'/opt/factorybox/factorybox','--worker-run-root','/run/factorybox',
             '--worker-store-id',owner,'--worker-epoch',status['epoch']]
        def retire_request(action, request):
            return json.loads(run([
                *cli, action, '--snapshot-id', request['snapshot_id'],
                '--source-owner', request['source_owner'],
                '--checkpoint-sha256', request['checkpoint_sha256'], request['source_id'],
            ], timeout=90))

        def retire_checkpoint(paused, resumed):
            return retire_resumed_checkpoint(
                paused, resumed, sandbox, result['start']['record']['runtime_id'],
                lambda request:retire_request('retire-checkpoint', request))

        def retire_resume(latest, previous):
            return retire_obsolete_resume_images(
                latest, previous, sandbox, result['start']['record']['runtime_id'],
                lambda request:retire_request('retire-resume', request))

        def inspect_rolling_backing(snapshot):
            script = """import json,pathlib,re,sys
request=json.load(sys.stdin)
root=pathlib.Path(request['root'])/'objects'/request['sandbox']
def generations(kind):
 parent=root/kind
 if not parent.exists(): return []
 result=[]
 for directory in parent.iterdir():
  if directory.is_symlink() or not directory.is_dir() or not re.fullmatch('[a-f0-9]{32}',directory.name):
   raise ValueError('Unexpected private generation')
  images=directory/'images'
  if images.is_symlink(): raise ValueError('Redirected private images')
  if images.exists():
   if not images.is_dir(): raise ValueError('Invalid private images')
   result.append(directory.name)
 return sorted(result)
print(json.dumps({'resumes':generations('resumes'),'captures':generations('captures')}))
"""
            inventory = json.loads(run(['exec', '-i', worker_id, 'python3', '-c', script],
                body=json.dumps({'root':status['lifecycle']['root'], 'sandbox':sandbox}), timeout=15))
            if inventory != {'resumes':[snapshot], 'captures':[]}:
                raise ValueError('Obsolete private RAM generations remain after rolling retirement')
            return {'snapshot_id':snapshot, 'inventory':inventory,
                    'capacity':json.loads(run([*cli, 'capacity'], timeout=15))}

        started=time.monotonic()
        native=json.loads(run([*cli,'start','--bundle',prepared['bundle'],sandbox],timeout=90))
        result['start']=native;live_pid=native['runtime']['pid']
        phase('sandbox-started')
        runc=['exec',worker_id,'/opt/factorybox/components/sysbox-runc','--root',status['lifecycle']['root']+'/runtime','exec',native['record']['runtime_id']]
        wait(lambda:run([*runc,'docker','info','--format','{{json .}}'],check=False),lambda v:v.startswith('{'),'Private Docker startup')
        if before.get('net/mptcp/enabled') is not None:
            result['guest_mptcp_enabled']=run([*runc,'cat','/proc/sys/net/mptcp/enabled']).strip()
            if result['guest_mptcp_enabled']!='0':raise ValueError('Factory guest did not select checkpointable ordinary TCP')
        endpoint('/prepared','POST')
        if args.reserve:
            ready=wait(lambda:run([*runc,'cat','/home/coder/.factory-reserve/ready'],check=False),lambda v:v==owner,'Actual reserve readiness')
            result['reserve_ready']=ready
            if args.warmup_control:
                control=run([*runc,'docker','run','--detach','--pull=never',
                             '--name','owned-before-pause-'+owner,node,'sleep','300']).strip()
                if not re.fullmatch('[a-f0-9]{64}',control):
                    raise ValueError('Before-Pause Docker control did not return its owned container')
                output_before=run([*runc,'docker','exec',control,'/bin/sh','-c',
                                   'printf factorybox-before-pause'],check=False)
                result['diagnostic_before_exec']=result['commands'][-1].copy()
                if result['diagnostic_before_exec']['exit'] or output_before!='factorybox-before-pause':
                    raise ValueError('The same private Docker exec already fails before Pause')
                run([*runc,'docker','rm','--force',control])
                phase('diagnostic-before-pause-exec-passed')
            cgroup_probe = ("readlink /proc/1/ns/cgroup; readlink /proc/self/ns/cgroup; "
                            "cat /proc/1/cgroup /proc/self/cgroup; "
                            "grep cgroup /proc/self/mountinfo; stat -f /sys/fs/cgroup; "
                            "ls -lai /sys/fs/cgroup; ls -lai /sys/fs/cgroup/docker")
            result['standby_cgroups_before']=run([*runc,'/bin/sh','-c',cgroup_probe],check=False)
            namespaces('unused-before-pause')
            run([*runc,'/bin/sh','-ec',
                 'test ! -e /home/coder/.factory-reserve/activation.json; test ! -e /factory/assignment/parent-token; test ! -e /factory/assignment/child-token'])
            result['standby_pause_attempted']=True
            result['standby_pause']=json.loads(run([*cli,'pause','--snapshot-id',owner,sandbox],timeout=90))
            pause=result['standby_pause']['record']['pause']
            if (result['standby_pause']['record']['phase']!='paused' or
                    pause['snapshot_id']!=owner or not pause.get('completed_at') or
                    pause.get('after',{}).get('anon')!=0 or
                    pause.get('snapshot',{}).get('resident_bytes_after')!=0):
                raise ValueError('Unused standby did not release its anonymous RAM and resident snapshot pages')
            result['standby_memory_paused']=memory()
            phase('unused-standby-paused')
            resumed_at=time.monotonic()
            result['standby_resume']=json.loads(run([*cli,'resume','--snapshot-id',owner,sandbox],timeout=90))
            live_pid=result['standby_resume']['runtime']['pid']
            result['standby_resume_seconds']=time.monotonic()-resumed_at
            phase('unused-standby-resumed')
            if (result['standby_resume']['record']['runtime_id']!=native['record']['runtime_id'] or
                    not result['standby_resume']['record']['pause'].get('resume',{}).get('completed_at')):
                raise ValueError('Standby activation did not resume its original native Sandbox')
            result['standby_cgroups_after']=run([*runc,'/bin/sh','-c',cgroup_probe],check=False)
            namespaces('unused-after-resume')
            # A resumed empty daemon must be able to create and exec new
            # containers, not merely answer Docker info or retain old apps.
            probe_name='owned-standby-exec-'+owner
            probe_container=run([*runc,'docker','run','--detach','--pull=never',
                                 '--name',probe_name,node,'sleep','300']).strip()
            if not re.fullmatch('[a-f0-9]{64}',probe_container):
                raise ValueError('Standby probe did not create its exact private Docker container')
            probe_output=run([*runc,'docker','exec',probe_container,'/bin/sh','-c',
                              'printf factorybox-standby-exec'],check=False)
            result['standby_docker_exec']=result['commands'][-1].copy()
            if result['standby_docker_exec']['exit'] or probe_output!='factorybox-standby-exec':
                result['standby_cgroups_after_failure']=run([*runc,'/bin/sh','-c',cgroup_probe],check=False)
                result['standby_probe_container']=run([*runc,'docker','inspect',probe_container],check=False)
                result['standby_direct_runc']=run(
                    [*runc,'runc','--root','/run/docker/runtime-runc/moby','--log',
                     '/tmp/factorybox-standby-runc.log','--debug','exec',probe_container,'/bin/true'],
                    check=False)
                result['standby_direct_runc_command']=result['commands'][-1].copy()
                result['standby_direct_runc_log']=run(
                    [*runc,'cat','/tmp/factorybox-standby-runc.log'],check=False)
                raise ValueError('Resumed standby cannot exec a new private Docker container')
            run([*runc,'runc','--root','/run/docker/runtime-runc/moby','--log',
                 '/tmp/factorybox-standby-sealing.log','--debug','exec',probe_container,'/bin/true'])
            result['standby_sealing_log']=run([*runc,'cat','/tmp/factorybox-standby-sealing.log'])
            run([*runc,'docker','rm','--force',probe_container])
            result['standby_docker_exec_passed']=True
            namespaces('unused-after-nested-exec')
            phase('resumed-standby-new-container-exec')
            activation={'slotId':owner,'podUID':pod_uid,'assignment':assignment,'parentToken':owner+'-parent','childToken':owner+'-child'}
            activation_script="set -eu; umask 077; cat > /home/coder/.factory-reserve/activation.pending; chmod 0400 /home/coder/.factory-reserve/activation.pending; mv /home/coder/.factory-reserve/activation.pending /home/coder/.factory-reserve/activation.json"
            # No second native Start: the original reserve process becomes the Parent.
            run(['exec','-i',worker_id,*runc[2:],'/bin/sh','-ec',activation_script],body=json.dumps(activation))
        wait(endpoint,lambda v:v['connected'],'Actual Parent and Child Connector readiness',90)
        first=wait(lambda:endpoint('/app'),lambda v:bool(v.get('nonce')),'First app response')
        result['first_useful_seconds']=time.monotonic()-started;result['before']=first
        result['memory_running']=memory()
        phase('first-app-response')
        result['hub_before']=endpoint()
        if not args.reserve:
            sibling_attempt=str(uuid.uuid4())
            sibling_source=uuid.uuid4().hex
            sibling_project='/factory-source/workspaces/'+sibling_source+'/home/project'
            run(['exec',worker_id,'mkdir','-p',sibling_project])
            run(['exec',worker_id,'python3','-c',
                 "import pathlib,sys;p=pathlib.Path(sys.argv[1]);(p/'sibling-only').write_text('before-sibling-pause')",
                 sibling_project])
            run(['exec',worker_id,'chown','-R','101000:101000',sibling_project])
            sibling = {**request, 'sandboxID': 'runtime-' + sibling_attempt.replace('-',''),
                'sourceWorkspaceID':sibling_source,'bootstrap':'exec /bin/sleep 300\n',
                'assignment':{'assignment.json':json.dumps({'version':1,'runtimeAttemptId':sibling_attempt,'generation':1}),
                              'parent-token':sibling_source+'-parent','child-token':sibling_source+'-child'}}
            result['sibling_preparation']=json.loads(run(
                ['exec','-i',worker_id,'python3','-c',(frozen/'prepare.py').read_text()],
                body=json.dumps(sibling),timeout=180))
            if result['sibling_preparation']['bundle'] == prepared['bundle']:
                raise ValueError('Two Sandbox preparations share mutable input backing')
            # This tiny sibling is an administrative source/assignment control,
            # not a second Docker/Connector qualification lane.
            result['sibling_start']=json.loads(run([*cli,'start','--bundle',
                result['sibling_preparation']['bundle'],sibling['sandboxID']],timeout=60))
            sibling_runc=[*runc[:-1],result['sibling_start']['record']['runtime_id']]
            source_check="test ! -e /home/coder/project/editor.txt; cat /home/coder/project/sibling-only; printf '\\n'; cat /factory/assignment/parent-token"
            expected_sibling='before-sibling-pause\n'+sibling_source+'-parent'
            if run([*sibling_runc,'/bin/sh','-ec',source_check]).strip()!=expected_sibling:
                raise ValueError('Sibling acquired another Workspace source or assignment')
            sibling_snapshot=uuid.uuid4().hex
            result['sibling_pause_attempted']=True
            result['sibling_pause']=json.loads(run([*cli,'pause','--snapshot-id',
                sibling_snapshot,sibling['sandboxID']],timeout=90))
            paused=result['sibling_pause']['record']['pause']
            if (not paused.get('completed_at') or paused.get('after',{}).get('anon')!=0 or
                    paused.get('snapshot',{}).get('resident_bytes_after')!=0):
                raise ValueError('Sibling Pause did not release its own memory')
            while_sibling_paused=endpoint('/app')
            if (while_sibling_paused['nonce']!=first['nonce'] or
                    while_sibling_paused['count']<=first['count'] or while_sibling_paused['editor']!='before'):
                raise ValueError('Sibling Pause changed or froze the original app')
            run(['exec',worker_id,'python3','-c',
                 "import pathlib,os,sys;p=pathlib.Path(sys.argv[1]);f=p/'.edit';f.write_text('during-sibling-pause');os.chown(f,101000,101000);f.replace(p/'sibling-only')",
                 sibling_project])
            result['sibling_resume']=json.loads(run([*cli,'resume','--snapshot-id',
                sibling_snapshot,sibling['sandboxID']],timeout=90))
            if run([*sibling_runc,'/bin/sh','-ec',source_check]).strip()!='during-sibling-pause\n'+sibling_source+'-parent':
                raise ValueError('Sibling Resume replaced current canonical source or assignment')
            if run([*runc,'cat','/factory/assignment/parent-token']).strip()!=owner+'-parent':
                raise ValueError('Sibling preparation replaced the original assignment')
            result['sibling_delete']=json.loads(run([*cli,'delete',sibling['sandboxID']],timeout=60))
            result['sibling_release']=json.loads(run(
                ['exec','-i',worker_id,'python3','-c',(frozen/'prepare.py').read_text(),
                 'release'],body=json.dumps(preparation_identity(sibling)),timeout=60))
            run(['exec',worker_id,'test','-f',sibling_project+'/sibling-only'])
            retained=json.loads(run([*cli,'state',sandbox]))
            if retained['record']['id']!=sandbox or retained['record']['phase']!='running':
                raise ValueError('Releasing a sibling changed the active Sandbox')
            result['sibling_isolation']=True
            result['sibling_source_isolation']=True
            phase('two-sandbox-source-and-assignment-isolation')
        if args.terminal:
            result['terminal_before']=endpoint('/terminal/before','POST')
            result['idle_prompt']=wait(lambda:endpoint('/idle'),lambda v:v.get('eligible') is False,
                                      'Factory must block automatic Pause while a PTY remains open',15)
        if (args.git or args.terminal) and not args.reserve:
            result['git_before']=endpoint('/git-status')
            if result['git_before']['head']!=revision or result['git_before']['branch']!='main':
                raise ValueError('Actual personal Git did not read its mapped canonical Source')
        result['docker_before']=json.loads(run([*runc,'docker','info','--format','{{json .}}']))
        children=json.loads(run([*runc,'docker','ps','--format','json']).splitlines()[0]);result['child_before']=children
        child_id=children['ID']
        snapshot=uuid.uuid4().hex
        endpoint('/pause','POST')
        namespaces('assigned-before-pause')
        result['pause_attempted']=True
        result['pause']=json.loads(run([*cli,'pause','--snapshot-id',snapshot,sandbox],timeout=90))
        require_paused_consumer(result['pause'], sandbox, result['start']['record']['runtime_id'], snapshot)
        result['memory_paused']=memory()
        phase('assigned-sandbox-paused')
        if args.reserve and args.retire_resumed_checkpoints:
            result['standby_checkpoint_retirement'] = retire_checkpoint(
                result['standby_pause'], result['standby_resume'])
            if args.retire_resume_images:
                result['standby_resume_retirement'] = retire_resume(
                    result['pause'], result['standby_resume'])
        if args.reserve:
            # Isolated source is private. Its preservation is checked through app RAM and child identity.
            pass
        else:
            run(['exec',worker_id,'python3','-c',
                 "import os,pathlib,sys;root=pathlib.Path(sys.argv[1]);p=root/'.editor-save';p.write_text('during-pause');os.chown(p,101000,101000);p.replace(root/'editor.txt')",
                 source_project])
        endpoint('/resume','POST')
        result['resume']=resume_assigned(result['pause'],snapshot,result,'cgroup-observation')
        live_pid=result['resume']['runtime']['pid']
        wait(endpoint,lambda v:v['connected'],'Same authenticated Connectors after Resume',45)
        after=wait(lambda:endpoint('/app'),lambda v:bool(v.get('nonce')),'App after Resume')
        result['after']=after;result['hub_after']=endpoint()
        phase('assigned-sandbox-resumed')
        result['memory_resumed']=memory()
        if after['nonce']!=first['nonce'] or after['pid']!=first['pid'] or after['count']<=first['count'] or after['editor']!=('before' if args.reserve else 'during-pause'):
            raise ValueError('Resume restarted application memory or lost canonical source edits')
        child=json.loads(run([*runc,'docker','inspect',child_id]))[0];result['child_after']=child
        if not child['State']['Running'] or child['RestartCount']!=0:
            raise ValueError('Dev Container restarted during Pause/Resume')
        result['docker_after']=json.loads(run([*runc,'docker','info','--format','{{json .}}']))
        if args.resume_cycles > 1:
            if args.retire_resumed_checkpoints:
                result['initial_checkpoint_retirement_attempted'] = True
                result['initial_checkpoint_retirement'] = retire_checkpoint(result['pause'], result['resume'])
                if args.retire_resume_images:
                    result['rolling_backing'] = [inspect_rolling_backing(snapshot)]

            previous_resumed = result['resume']

            def pause_again(entry):
                endpoint('/pause','POST')
                paused = json.loads(run([*cli,'pause','--snapshot-id',entry['snapshot_id'],sandbox],timeout=90))
                if args.retire_resume_images:
                    entry['resume_image_retirement'] = retire_resume(paused, previous_resumed)
                return paused

            def resume_again(entry):
                nonlocal live_pid, previous_resumed
                endpoint('/resume','POST')
                resumed = resume_assigned(entry['pause'],entry['snapshot_id'],entry,
                                          'cgroup-observation-cycle-'+str(entry['cycle']))
                live_pid=resumed['runtime']['pid']
                previous_resumed=resumed
                return resumed

            def retire_cycle(entry):
                receipt = retire_checkpoint(entry['pause'],entry['resume'])
                if args.retire_resume_images:
                    result['rolling_backing'].append(inspect_rolling_backing(entry['snapshot_id']))
                return receipt

            def useful_again():
                wait(endpoint,lambda value:value['connected'],'Same Connectors after repeated Resume',45)
                return wait(lambda:endpoint('/app'),lambda value:bool(value.get('nonce')),
                            'Original App after repeated Resume')

            result['repeat_after'] = repeated_consumer_cycles(
                args.resume_cycles, result, after, pause_again, resume_again, useful_again,
                lambda:json.loads(run([*runc,'docker','inspect',child_id]))[0], phase,
                retire=retire_cycle
                if args.retire_resumed_checkpoints else None)
        if args.terminal:
            result['terminal_after']=endpoint('/terminal/after','POST')
            if result['terminal_after']!=result['terminal_before']:
                raise ValueError('Native Resume replaced the existing terminal shell or its private RAM')
            result['idle_prompt_after']=wait(lambda:endpoint('/idle'),lambda v:v.get('eligible') is False,
                                            'The resumed open terminal must still block automatic Pause',15)
        if (args.git or args.terminal) and not args.reserve:
            result['git_after']=endpoint('/git-commit','POST')
            if result['git_after']['head']==revision or result['git_after']['files']:
                raise ValueError('Actual mapped-user personal Git commit did not retain its exact outcome')
        result['delete']=json.loads(run([*cli,'delete',sandbox],timeout=60));live_pid=None
        if args.retire_resume_images:
            result['final_resume_retirement'] = retire_resume(result['delete'], previous_resumed)
        result['release']=json.loads(run(['exec','-i',worker_id,'python3','-c',(frozen/'prepare.py').read_text(),'release'],body=json.dumps(preparation_identity(request)),timeout=60))
        if args.retire_deleted_backing:
            deleted = result['delete']['record']
            result['backing_retirement'] = json.loads(run([
                *cli, 'retire-sandbox', '--sandbox-owner', deleted['owner'],
                '--input-sha256', deleted['input_sha256'], sandbox], timeout=90))
            receipt = result['backing_retirement']
            if (receipt.get('payload_kind') != 'sandbox-backing' or
                    receipt.get('request') != {'sandbox_id':sandbox, 'sandbox_owner':deleted['owner'],
                                               'input_sha256':deleted['input_sha256']} or
                    not receipt.get('completed_at') or
                    receipt['after']['filesystem_available_bytes'] <= receipt['before']['filesystem_available_bytes']):
                raise ValueError('Deleted private filesystem backing did not return real capacity')
        run(['stop','--timeout','45',worker_id],timeout=50)
        result['final_worker_state']=own(name,worker_id)['State']
        if result['final_worker_state']['ExitCode']!=0:raise ValueError('Worker did not shut down cleanly')
        result['passed']=True
    except Exception as error:
        result['error']=str(error)
        if worker_id:
            try:
                state=own(name,worker_id)
                if state['State']['Running']:
                    result['failure_status']=run(['exec',worker_id,'/opt/factorybox/factorybox','worker-status'],check=False)
                    if live_pid:
                        result['failure_private_logs']=run(['exec',worker_id,'/bin/sh','-c',f'cat /proc/{int(live_pid)}/root/var/log/factory-connector.log /proc/{int(live_pid)}/root/var/log/factory-dockerd.log /proc/{int(live_pid)}/root/var/log/native-registry.log'],check=False)
            except Exception as diagnostic:result['diagnostic_error']=str(diagnostic)
    finally:
        # Cleanup has its own bounded window and verifies every immutable owner.
        deadline=time.monotonic()+120
        (output/'before-cleanup.json').write_text(json.dumps(result,indent=2)+'\n')
        print(json.dumps({'evidence':str(output),'workload_passed':result['passed'],
                          'diagnostic_only':result['diagnostic_only'],'error':result.get('error')}),flush=True)
        if (result.get('pause_attempted') or result.get('standby_pause_attempted') or result.get('sibling_pause_attempted')) and not result['passed']:
            try:
                upstream_bats.archive_failure(output,name,owner,[name,name+'-source'],worker['native_base_image'],result,timeout=180,compressed=True)
            except Exception as error:
                result['failure_archive_error']=str(error)
                result['retained_volumes']=volumes[:]
        for label in reversed(containers):
            try:
                # owned_delete also accepts an absent exact name. A failed
                # create may not have reached Docker at all.
                if not run(['container','ls','--all','--quiet','--filter','name=^/'+label+'$']).strip():
                    reference.owned_delete('container',label,owner)
                    continue
                facts=json.loads(reference.docker(['inspect',label]))[0]
                if facts['Config']['Labels'].get('com.factorybox.run')!=owner:raise ValueError('Native consumer cleanup owner changed')
                if label==name:(output/'worker.log').write_text(reference.docker(['logs',label]))
                if label==name+'-hub':(output/'hub.log').write_text(reference.docker(['logs',label]))
                reference.owned_delete('container',label,owner)
            except Exception as error:result.setdefault('cleanup_errors',[]).append(str(error))
        for volume in reversed(volumes):
            if volume in result.get('retained_volumes',[]):continue
            try:reference.owned_delete('volume',volume,owner)
            except Exception as error:result.setdefault('cleanup_errors',[]).append(str(error))
        result['cleanup']=not result.get('cleanup_errors') and not result.get('retained_volumes')
        after=consumer_worker_loss.sysctls(docker_image=worker['native_base_image'],
            owner=owner, artifact=output, phase='after', factory_network=True)
        result['host_sysctls_unchanged']=after==before
        result['inputs_unchanged']=all(reference.sha(frozen/file)==value for file,value in result['source_sha256'].items()) and reference.sha(hub)==result['hub_sha256'] and reference.sha(bun)==result['bun_sha256'] and reference.sha(namespace_observer)==result['namespace_observer_sha256'] and reference.sha(cgroup_observer)==result['cgroup_observer_sha256']
        lane_passed = settle_lane_result(result)
        (output/'result.json').write_text(json.dumps(result,indent=2)+'\n')
        print(json.dumps({'evidence':str(output),'passed':result['passed'],'cleanup':result['cleanup'],
                          'diagnostic_only':result['diagnostic_only'],
                          'diagnostic_control_passed':result.get('diagnostic_control_passed'),
                          'error':result.get('error')}),flush=True)
    if not lane_passed:
        raise RuntimeError('Native Factory consumer acceptance failed: '+str(output))


if __name__=='__main__':main()
