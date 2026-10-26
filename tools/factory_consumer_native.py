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


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--worker-build', required=True)
    parser.add_argument('--consumer-build', required=True)
    parser.add_argument('--factory-repo', type=Path, required=True)
    parser.add_argument('--project-images', required=True)
    parser.add_argument('--reserve', action='store_true')
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
    sandbox = 'runtime-' + attempt.replace('-', '')
    pod_uid = str(uuid.uuid4())
    name = 'factorybox-factory-consumer-'+owner
    output = reference.new_artifact(name)
    output.mkdir(mode=0o755)
    shutil.copyfile(__file__, output/'runner.py')
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
              'diagnostic_only':args.warmup_control,
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
        script = "import pathlib,json;root=pathlib.Path('/factory/assignment');root.mkdir(parents=True);[(root/name).write_text(body) for name,body in json.loads(__import__('sys').argv[1]).items()];ca=pathlib.Path('/factory/registry-ca');ca.mkdir(parents=True);(ca/'ca.crt').write_bytes(pathlib.Path('/etc/ssl/certs/ca-certificates.crt').read_bytes())"
        run(['exec',worker_id,'python3','-c',script,json.dumps(inputs)])
        if not args.reserve:
            run(['cp',str(project),worker_id+':/factory-source/project'])
            run(['exec',worker_id,'chown','-R','101000:101000','/factory-source/project'])
        request = {'version':2,'sandboxID':sandbox,'storeID':owner,'epoch':status['epoch'],'podUID':pod_uid,
                   'runtimeHostImage':consumer['image'],'sharedSource':not args.reserve,'reserveSeed':args.reserve,
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
            sibling = {**request, 'sandboxID': 'runtime-' + uuid.uuid4().hex}
            result['sibling_preparation']=json.loads(run(
                ['exec','-i',worker_id,'python3','-c',(frozen/'prepare.py').read_text()],
                body=json.dumps(sibling),timeout=180))
            if result['sibling_preparation']['bundle'] == prepared['bundle']:
                raise ValueError('Two Sandbox preparations share mutable input backing')
            result['sibling_release']=json.loads(run(
                ['exec','-i',worker_id,'python3','-c',(frozen/'prepare.py').read_text(),
                 'release','--never-started'],body=json.dumps(sibling),timeout=60))
            retained=json.loads(run([*cli,'state',sandbox]))
            if retained['record']['id']!=sandbox or retained['record']['phase']!='running':
                raise ValueError('Releasing an unused sibling changed the active Sandbox')
            result['sibling_isolation']=True
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
        result['pause_attempted']=True
        result['pause']=json.loads(run([*cli,'pause','--snapshot-id',snapshot,sandbox],timeout=90))
        result['memory_paused']=memory()
        phase('assigned-sandbox-paused')
        if args.reserve:
            # Isolated source is private. Its preservation is checked through app RAM and child identity.
            pass
        else:
            run(['exec',worker_id,'python3','-c',"import os,pathlib;p=pathlib.Path('/factory-source/project/.editor-save');p.write_text('during-pause');os.chown(p,101000,101000);p.replace('/factory-source/project/editor.txt')"])
        endpoint('/resume','POST')
        result['resume']=json.loads(run([*cli,'resume','--snapshot-id',snapshot,sandbox],timeout=90));live_pid=result['resume']['runtime']['pid']
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
        result['release']=json.loads(run(['exec','-i',worker_id,'python3','-c',(frozen/'prepare.py').read_text(),'release'],body=json.dumps(request),timeout=60))
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
                          'diagnostic_only':args.warmup_control,'error':result.get('error')}),flush=True)
        if (result.get('pause_attempted') or result.get('standby_pause_attempted')) and not result['passed']:
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
        result['inputs_unchanged']=all(reference.sha(frozen/file)==value for file,value in result['source_sha256'].items()) and reference.sha(hub)==result['hub_sha256'] and reference.sha(bun)==result['bun_sha256']
        result['passed']=result['passed'] and result['cleanup'] and result['host_sysctls_unchanged'] and result['inputs_unchanged']
        if args.warmup_control:
            result['diagnostic_control_passed']=result['passed']
            result['passed']=False
        (output/'result.json').write_text(json.dumps(result,indent=2)+'\n')
        print(json.dumps({'evidence':str(output),'passed':result['passed'],'cleanup':result['cleanup'],
                          'diagnostic_only':args.warmup_control,
                          'diagnostic_control_passed':result.get('diagnostic_control_passed'),
                          'error':result.get('error')}),flush=True)
    if not (result.get('diagnostic_control_passed') if args.warmup_control else result['passed']):
        raise RuntimeError('Native Factory consumer acceptance failed: '+str(output))


if __name__=='__main__':main()
