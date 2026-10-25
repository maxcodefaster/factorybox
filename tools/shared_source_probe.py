#!/usr/bin/env python3
"""Owned native shared-source proof, including edits between Pause and Resume."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import tarfile
import time
import uuid

import build_worker
import consumer_worker_loss
import criu_candidate
import devx_inputs
import native_base
import reference
import upstream_unit


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--worker-build', required=True)
    parser.add_argument('--diagnostic-runtime', action='store_true', help='Compile frozen current Go sources and mount that CLI read-only over the completed image; never qualifies a delivered package')
    parser.add_argument('--diagnostic-criu-build', help='Install an exact completed CRIU candidate only into this owned diagnostic container layer before PID 1 delegates its cgroup')
    parser.add_argument('--case', choices=['repeat', 'closed-file-edit', 'open-file-atomic-replacement'], default='repeat')
    parser.add_argument('--alias-mount', action='store_true', help='Exercise a private nested mount namespace at the Dev Container project path, not a Docker/Factory consumer qualification')
    parser.add_argument('--rename-directory', action='store_true', help='Keep cwd and an open directory FD in a subdirectory, then rename that directory while paused')
    parser.add_argument('--outbound-network', action='store_true', help='Verify owned IPv4 egress to a separate bounded HTTP container through every Pause/Resume')
    parser.add_argument('--consumer-inputs', help='Use the retained complete consumer rootfs instead of the small contract fixture')
    parser.add_argument('--docker-child', action='store_true', help='Run the workload in a real private Docker bridge network using the complete consumer rootfs')
    parser.add_argument('--factory-preparation-repo', type=Path, help='Use the frozen Factory consumer preparer with its real rootfs; the workload remains this contract fixture')
    args = parser.parse_args()
    sandbox_id = 'runtime' if args.factory_preparation_repo else 'source'
    if args.diagnostic_criu_build and not args.diagnostic_runtime:
        raise ValueError('a CRIU override requires the explicitly unqualified diagnostic runtime')
    if args.alias_mount and args.consumer_inputs:
        raise ValueError('the alias fixture requires the small explicitly owned rootfs')
    if args.docker_child and (not args.consumer_inputs or not args.outbound_network):
        raise ValueError('private Docker proof requires complete consumer inputs and the external network fixture')
    if args.factory_preparation_repo and not args.docker_child:
        raise ValueError('Factory preparation proof requires the complete private Docker contract fixture')
    worker_root, worker = build_worker.load(args.worker_build)
    native_base.require_native_host('linux/amd64', json.loads(reference.docker(['info', '--format', '{{json .}}'])))
    current = {directory + '/' + key: value for directory in ['internal', 'cmd/factorybox']
               for key, value in upstream_unit.context_manifest(reference.ROOT / directory).items()}
    current.update({file: {'sha256': reference.sha(reference.ROOT / file)} for file in ['go.mod', 'go.sum']})
    if not args.diagnostic_runtime and current != worker['source_context']:
        raise ValueError('shared-source probe requires the exact current implementation')
    inputs, consumer = devx_inputs.load(args.consumer_inputs, rootfs=True, platform='linux/amd64') if args.consumer_inputs else (None, None)
    owner = uuid.uuid4().hex
    name = 'factorybox-shared-source-' + owner
    artifact = reference.new_artifact(name)
    artifact.mkdir(mode=0o700)
    shutil.copyfile(__file__, artifact / 'runner.py')
    source = artifact / 'source'
    shutil.copytree(reference.ROOT / 'tests/shared-source-actor', source / 'tests/shared-source-actor')
    for file in ['go.mod', 'go.sum']:
        shutil.copyfile(reference.ROOT / file, source / file)
    if args.diagnostic_runtime:
        for directory in ['internal', 'cmd/factorybox']:
            shutil.copytree(reference.ROOT / directory, source / directory)
    result = {'schema': 1, 'owner': owner, 'worker_build': args.worker_build, 'image_id': worker['image_id'],
              'consumer_inputs': args.consumer_inputs, 'passed': False, 'cleanup': False,
              'kubernetes_qualified': False, 'production_qualified': False, 'commands': [], 'phases': []}
    result['exact_packaged_cli'] = not args.diagnostic_runtime
    result['diagnostic_runtime'] = args.diagnostic_runtime
    result['case'] = args.case
    result['alias_mount'] = args.alias_mount
    result['rename_directory'] = args.rename_directory
    result['outbound_network'] = args.outbound_network
    result['docker_child'] = args.docker_child
    factory_preparer = None
    if args.factory_preparation_repo:
        factory_preparer = artifact / 'factory-prepare.py'
        original = args.factory_preparation_repo.resolve() / 'packages/runtime-kubernetes/src/factorybox/prepare.py'
        shutil.copyfile(original, factory_preparer)
        result['factory_preparer_sha256'] = reference.sha(factory_preparer)
    result['runtime_source_context'] = current
    result['actor_source_context'] = upstream_unit.context_manifest(source / 'tests/shared-source-actor')
    created = []
    attempted = False
    identity = None
    server_identity = None
    server_attempted = False
    live_pid = None
    deadline = time.monotonic() + 420
    before = consumer_worker_loss.sysctls()
    before['net/ipv4/ip_forward'] = Path('/proc/sys/net/ipv4/ip_forward').read_text().strip()
    result['global_sysctls_before'] = before

    def run(arguments, *, timeout=30, failure=False):
        completed = subprocess.run([*reference.DOCKER, *arguments], capture_output=True, text=True,
                                   timeout=min(timeout, max(.1, deadline - time.monotonic())))
        result['commands'].append({'arguments': arguments, 'exit': completed.returncode,
                                   'stdout': completed.stdout, 'stderr': completed.stderr})
        if bool(completed.returncode) != failure:
            raise ValueError('native shared-source outcome differed: ' + completed.stderr[-4096:])
        return completed.stdout

    def actor(method='GET'):
        script = '''import http.client,json,socket
class Local(http.client.HTTPConnection):
 def connect(self):
  self.sock=socket.socket(socket.AF_UNIX);self.sock.settimeout(5);self.sock.connect(socket_path)
connection=Local('localhost',timeout=5)
connection.request(method,'/')
response=connection.getresponse();body=response.read()
assert response.status==200,(response.status,body)
print(body.decode())
'''
        script = 'socket_path=' + repr('/proc/' + str(live_pid) + '/root/workspace/.shared-source.sock') + '\nmethod=' + repr(method) + '\n' + script
        return json.loads(run(['exec', identity, 'python3', '-c', script]))

    def exact():
        saved = consumer_worker_loss.exact_worker(name, owner, identity)
        mounts = {entry['Destination']: entry.get('Name') for entry in saved['Mounts']}
        limits = saved['HostConfig']
        if (saved['Image'] != worker['image_id'] or mounts.get('/var/lib/sysbox') != name or
                mounts.get('/factory-source') != name + '-source' or limits['Memory'] != 3 << 30 or
                limits['NanoCpus'] != 3_000_000_000 or limits['CgroupnsMode'] != 'private' or
                limits['NetworkMode'] != 'none' or not limits['Privileged']):
            raise ValueError('owned shared-source worker differs from its immutable request')
        return saved

    try:
        runtime_mount = []
        runtime_entrypoint = []
        runtime_command = []
        if args.diagnostic_runtime:
            runtime_binary = artifact / 'factorybox'
            reused = False
            if current == worker['source_context']:
                reference.artifact_storage.share_frozen_input(worker_root / 'image-context/bundle/factorybox', runtime_binary, reference.ROOT, worker['bundle_sha256']['factorybox'])
                result['runtime_binary_reused_from'] = {'build': args.worker_build, 'package_sha256': worker['package_sha256']}
                reused = True
            for previous_path in sorted((reference.ROOT / 'artifacts').glob('factorybox-shared-source-*/result.json')):
                if reused:
                    break
                previous = json.loads(previous_path.read_text())
                binary = previous_path.parent / 'factorybox'
                if previous.get('cleanup') is not True or previous.get('runtime_source_context') != current or not binary.is_file() or binary.is_symlink():
                    continue
                if reference.sha(binary) != previous.get('runtime_binary_sha256'):
                    raise ValueError('closed diagnostic runtime binary changed')
                frozen = previous_path.parent / 'source'
                actual = {directory + '/' + key: value for directory in ['internal', 'cmd/factorybox']
                          for key, value in upstream_unit.context_manifest(frozen / directory).items()}
                actual.update({file: {'sha256': reference.sha(frozen / file)} for file in ['go.mod', 'go.sum']})
                if actual != current:
                    raise ValueError('closed diagnostic runtime source changed')
                os.link(binary, runtime_binary)
                result['runtime_binary_reused_from'] = {'file': str(binary), 'result_sha256': reference.sha(previous_path)}
                reused = True
                break
            if not reused:
                subprocess.run(['go', 'build', '-trimpath', '-buildvcs=false', '-o', str(runtime_binary), './cmd/factorybox'],
                               cwd=source, env=dict(os.environ, CGO_ENABLED='0', GOPROXY='off', GOSUMDB='off'), check=True, timeout=120)
                runtime_binary.chmod(0o555)
            elif runtime_binary.stat().st_mode & 0o111 == 0:
                raise ValueError('retained diagnostic runtime is not executable')
            result['runtime_binary_sha256'] = reference.sha(runtime_binary)
            runtime_mount = ['--mount', f'type=bind,source={runtime_binary},target=/opt/factorybox/factorybox,readonly']
        if args.diagnostic_criu_build:
            criu_root, criu, _, _ = criu_candidate.load(args.diagnostic_criu_build)
            criu_binary = artifact / 'criu'
            reference.artifact_storage.share_frozen_input(criu_root / 'criu', criu_binary, reference.ROOT, criu['binary_sha256'])
            manifest = {'schema': 1, 'binary_sha256': {name: worker['bundle_sha256']['components/' + name]
                        for name in ['criu', 'sysbox-runc', 'sysbox-fs', 'sysbox-mgr']}}
            manifest['binary_sha256']['criu'] = criu['binary_sha256']
            manifest_path = artifact / 'components.json'
            manifest_path.write_text(json.dumps(manifest) + '\n')
            runtime_mount += ['--mount', f'type=bind,source={criu_binary},target=/tmp/diagnostic-criu,readonly',
                              '--mount', f'type=bind,source={manifest_path},target=/tmp/diagnostic-components.json,readonly']
            runtime_entrypoint = ['--entrypoint', '/bin/sh']
            runtime_command = ['-ec', 'cp /tmp/diagnostic-criu /opt/factorybox/components/criu; chmod 0755 /opt/factorybox/components/criu; cp /tmp/diagnostic-components.json /opt/factorybox/components.json; chmod 0600 /opt/factorybox/components.json; exec /opt/factorybox/factorybox worker "$@"', 'diagnostic']
            result['criu_override'] = {'build': args.diagnostic_criu_build, 'binary_sha256': criu['binary_sha256'],
                                       'result_sha256': reference.sha(criu_root / 'result.json')}
        actor_binary = artifact / 'shared-source-actor'
        for previous_path in sorted((reference.ROOT / 'artifacts').glob('factorybox-shared-source-*/result.json')):
            previous = json.loads(previous_path.read_text())
            binary, frozen = previous_path.parent / 'shared-source-actor', previous_path.parent / 'source'
            if previous.get('cleanup') is not True or not binary.is_file() or binary.is_symlink():
                continue
            if upstream_unit.context_manifest(frozen / 'tests/shared-source-actor') != result['actor_source_context'] or any(reference.sha(frozen / name) != reference.sha(source / name) for name in ['go.mod','go.sum']):
                continue
            if reference.sha(binary) != previous.get('actor_sha256'):
                raise ValueError('closed diagnostic actor changed')
            os.link(binary, actor_binary)
            result['actor_reused_from'] = {'file': str(binary), 'result_sha256': reference.sha(previous_path)}
            break
        else:
            subprocess.run(['go', 'build', '-trimpath', '-buildvcs=false', '-o', str(actor_binary), './tests/shared-source-actor'],
                           cwd=source, env=dict(os.environ, CGO_ENABLED='0'), check=True, timeout=120)
        result['actor_sha256'] = reference.sha(actor_binary)
        if args.outbound_network:
            server_script = artifact / 'network-server.py'
            shutil.copyfile(reference.ROOT / 'tests/shared-source-network.py', server_script)
            result['network_server_sha256'] = reference.sha(server_script)
            server_attempted = True
            server_identity = run(['run','--detach','--pull=never','--name',name+'-http','--label','com.factorybox.run='+owner,
                '--cpus=.25','--memory=64m','--pids-limit=16','--network=none','--read-only','--cap-drop=ALL','--user','65534:65534',
                '--mount',f'type=bind,source={server_script},target=/fixture.py,readonly','--entrypoint','python3',worker['native_base_image'],'/fixture.py',owner]).strip()
            server = consumer_worker_loss.exact_worker(name+'-http',owner,server_identity)
            result['network_server_id'] = server_identity
            result['network_server_ip'] = '192.0.2.2'
        for volume in [name, name + '-source']:
            run(['volume', 'create', '--label', 'com.factorybox.run=' + owner, volume])
            created.append(volume)
        attempted = True
        identity = run(['run', '--detach', '--pull=never', '--name', name, '--label', 'com.factorybox.run=' + owner,
                        '--privileged', '--cgroupns=private', '--cpus=3', '--memory=3g', '--pids-limit=512',
                        '--network=none', '--stop-timeout=45', '--env', 'GOMAXPROCS=2', '--env', 'GOMEMLIMIT=256MiB',
                        '--mount', f'type=volume,source={name},target=/var/lib/sysbox',
                        '--mount', f'type=volume,source={name}-source,target=/factory-source', *runtime_mount, *runtime_entrypoint, worker['image'], *runtime_command,
                        '--store-id', owner, '--bounded-root', '/var/lib/sysbox/fixed-store',
                        '--bounded-bytes', str(8 << 30), '--health-listen', '127.0.0.1:8081']).strip()
        result['container_id'] = identity
        started = time.monotonic()
        while True:
            if not exact()['State']['Running']:
                raise ValueError('shared-source worker exited before readiness')
            try:
                status = json.loads(reference.docker(['logs', '--tail=5', identity]))
            except json.JSONDecodeError:
                status = None
            if isinstance(status, dict) and status.get('ready_at') and status.get('store_id') == owner:
                break
            if time.monotonic() - started > 120:
                raise TimeoutError('shared-source worker readiness timed out')
            time.sleep(.1)
        if args.outbound_network:
            host_network = os.stat('/proc/self/ns/net')
            result['worker_network_namespace'] = json.loads(run(['exec',identity,'python3','-c',"import json,os;s=os.stat('/proc/self/ns/net');print(json.dumps({'device':s.st_dev,'inode':s.st_ino}))"]))
            if result['worker_network_namespace'] == {'device':host_network.st_dev,'inode':host_network.st_ino}:
                raise ValueError('network proof must not use the host namespace')
            server = consumer_worker_loss.exact_worker(name+'-http',owner,server_identity)
            if not server['State']['Running'] or server['HostConfig']['NetworkMode']!='none' or server['HostConfig']['Memory']!=64<<20:
                raise ValueError('HTTP network endpoint differs from its private bounded request')
            server_pid = server['State']['Pid']
            worker_pid = exact()['State']['Pid']
            server_ns = json.loads(run(['exec',server_identity,'python3','-c',"import json,os;s=os.stat('/proc/self/ns/net');print(json.dumps({'device':s.st_dev,'inode':s.st_ino}))"]))
            result['server_network_namespace'] = server_ns
            if (server_ns['device'],server_ns['inode']) in [(host_network.st_dev,host_network.st_ino),(result['worker_network_namespace']['device'],result['worker_network_namespace']['inode'])]:
                raise ValueError('external endpoint does not have a separate private namespace')
            def fixture_ip(pid,arguments):
                current_server = consumer_worker_loss.exact_worker(name+'-http',owner,server_identity)
                if current_server['State']['Pid']!=server_pid or exact()['State']['Pid']!=worker_pid:
                    raise ValueError('owned network fixture process changed')
                command = ['sudo','-n','nsenter','--net=/proc/'+str(pid)+'/ns/net','--','ip',*arguments]
                done = subprocess.run(command,capture_output=True,text=True,timeout=10)
                result['commands'].append({'arguments':command,'exit':done.returncode,'stdout':done.stdout,'stderr':done.stderr})
                done.check_returncode()
            fixture_ip(worker_pid,['link','add','name','proof-out','alias','fixture:'+owner,'type','veth','peer','name','proof-in','netns',str(server_pid)])
            for pid,interface,address in [(worker_pid,'proof-out','192.0.2.1/30'),(server_pid,'proof-in','192.0.2.2/30')]:
                fixture_ip(pid,['addr','add',address,'dev',interface])
                fixture_ip(pid,['link','set',interface,'up'])
            fixture_ip(worker_pid,['route','add','default','via','192.0.2.2','dev','proof-out'])
            result['external_fixture_private_namespaces_verified'] = True
        result['status'] = json.loads(run(['exec', identity, '/opt/factorybox/factorybox', 'worker-status']))
        setup = artifact / 'setup.sh'
        if args.consumer_inputs:
            declaration = reference.ROOT / 'tests/compatibility/worker-example-bounded-paths.json'
            fixture = json.loads(declaration.read_text())
            original = reference.ROOT / fixture['file']
            if reference.sha(original) != fixture['before_sha256'] or not fixture['assertions_preserved']:
                raise ValueError('immutable bundle setup no longer matches its path-only declaration')
            setup.write_bytes(original.read_bytes().replace(fixture['old_path'].encode(), fixture['new_path'].encode()))
            if reference.sha(setup) != fixture['after_sha256']:
                raise ValueError('bounded bundle setup compatibility hash differs')
            shutil.copyfile(original, artifact / 'setup.before.sh')
            shutil.copyfile(declaration, artifact / declaration.name)
            run(['cp', str(inputs / 'runtime-rootfs.tar'), identity + ':/var/lib/sysbox/native-rootfs.tar'], timeout=90)
        else:
            shutil.copyfile(reference.ROOT / 'tests/shared-source-setup.sh', setup)
        if args.alias_mount:
            alias_source = artifact / 'shared-source-alias.c'
            shutil.copyfile(reference.ROOT / 'tests/shared-source-alias.c', alias_source)
            result['alias_source_sha256'] = reference.sha(alias_source)
            run(['cp', str(alias_source), identity + ':/var/lib/sysbox/native-alias.c'])
            result['alias_compiler'] = run(['exec', identity, 'cc', '--version'])
        for path, target in [(actor_binary, 'native-minimal-actor'), (setup, 'native-setup.sh')]:
            run(['cp', str(path), identity + ':/var/lib/sysbox/' + target], timeout=90)
        if factory_preparer:
            image = consumer['selected_images']['build-runtime-host']
            seed = r'''import json,os,pathlib
seed=pathlib.Path('/var/lib/sysbox/seed');seed.mkdir()
os.rename('/var/lib/sysbox/native-rootfs.tar',seed/'rootfs.tar')
(seed/'intent').write_text(pod+'\n'+owner+'\n'+image+'\n')
(seed/'rootfs.sha256').write_text(rootfs_sha+'  rootfs.tar\n')
project=pathlib.Path('/factory-source/project');project.mkdir();os.chown(project,101000,101000)
assignment=pathlib.Path('/factory/assignment');assignment.mkdir(parents=True)
for name in ['assignment.json','parent-token','child-token']:(assignment/name).write_text('owned contract fixture')
ca=pathlib.Path('/factory/registry-ca');ca.mkdir(parents=True);(ca/'ca.crt').write_text('owned contract fixture')
'''
            run(['exec',identity,'python3','-c','pod='+repr(identity)+'\nowner='+repr(owner)+'\nimage='+repr(image)+'\nrootfs_sha='+repr(consumer['rootfs_tar_sha256'])+'\n'+seed])
            status = result['status']
            bootstrap = 'export FACTORYBOX_NETWORK_URL=http://'+result['network_server_ip']+':18181/\n'
            if args.rename_directory:
                bootstrap += 'export FACTORYBOX_SHARED_SOURCE_SUBDIRECTORY=1\n'
            bootstrap += 'exec /opt/factorybox-example-actor --docker-parent\n'
            request = {'version':1,'storeID':owner,'epoch':status['epoch'],'podUID':identity,
                       'runtimeHostImage':image,'sharedSource':True,'bootstrap':bootstrap}
            code = factory_preparer.read_text()+'\n'
            code = code[:code.index("if __name__ == '__main__':")]+ '\nprint(json.dumps(prepare(json.loads(sys.argv[1]))))\n'
            result['factory_preparation'] = json.loads(run(['exec','--env','FACTORY_POD_UID='+identity,'--env','FACTORY_STORE_ID='+owner,
                                                           identity,'python3','-c',code,json.dumps(request)],timeout=180))
            bundle = result['factory_preparation']['bundle']
            run(['cp',str(actor_binary),identity+':/var/lib/sysbox/native-minimal-actor'])
            run(['exec',identity,'cp','/var/lib/sysbox/native-minimal-actor',bundle+'/rootfs/opt/factorybox-example-actor'])
            run(['exec',identity,'chmod','0755',bundle+'/rootfs/opt/factorybox-example-actor'])
        else:
            run(['exec', '--env', 'FACTORYBOX_WORKER_EXAMPLE_NATIVE=1', identity, '/bin/bash', '/var/lib/sysbox/native-setup.sh',
                 consumer['rootfs_tar_sha256'] if consumer else '', result['actor_sha256'], '1' if args.alias_mount else ''], timeout=180)
        if args.alias_mount:
            alias_binary = artifact / 'shared-source-alias'
            run(['cp', identity + ':/var/lib/sysbox/native-alias-built', str(alias_binary)])
            result['alias_binary_sha256'] = reference.sha(alias_binary)
        if not factory_preparer:
            bundle = '/var/lib/sysbox/fixed-store/data/store/input/bundle'
        mount_helper = artifact / 'input-mounts.py'
        shutil.copyfile(reference.ROOT / 'examples/worker/bundle_mounts.py', mount_helper)
        mount_script = mount_helper.read_text()
        result['input_mounts'] = json.loads(run(['exec', identity, 'python3', '-c', mount_script, 'inspect', bundle]))
        configure = '''import json,os,pathlib
bundle=pathlib.Path(bundle_path)
project=pathlib.Path('/factory-source/project');project.mkdir(exist_ok=True)
for name,content in [('current.txt','before'),('held.txt','original-held---'*256)]:
 path=project/name;path.write_text(content);os.chown(path,101000,101000)
os.chown(project,101000,101000)
for path in [bundle/'rootfs/home/coder/project',bundle/'rootfs/workspace']:
 path.mkdir(parents=True,exist_ok=True);os.chown(path,1000,1000)
path=bundle/'config.json';spec=json.loads(path.read_text())
spec['process']['user']={'uid':1000,'gid':1000}
spec['process']['args']=['/opt/factorybox-example-actor']
spec['process']['terminal']=False
spec['process']['cwd']='/home/coder/project'
spec['process']['env']=['HOME=/home/coder','PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin']
for key,values in spec['process']['capabilities'].items():
 spec['process']['capabilities'][key]=[value for value in values if value not in ['CAP_SYS_MODULE','CAP_SYS_RAWIO','CAP_SYS_BOOT','CAP_SYS_TIME','CAP_SYSLOG']]
spec['root']['readonly']=False
spec['linux']['resources']={'pids':{'limit':128},'devices':[{'allow':True,'access':'rwm'}]}
spec['linux']['uidMappings']=[{'containerID':0,'hostID':100000,'size':65536}]
spec['linux']['gidMappings']=[{'containerID':0,'hostID':100000,'size':65536}]
for mount in spec['mounts']:
 if mount['destination']=='/sys/fs/cgroup':mount['options']=['nosuid','noexec','nodev','rw']
if not any(mount['destination']=='/home/coder/project' for mount in spec['mounts']):
 spec['mounts'].append({'type':'bind','source':str(project),'destination':'/home/coder/project','options':['rbind','rw','rprivate','nosuid','nodev']})
spec.setdefault('annotations',{}).setdefault('io.factorybox.base','example-base')
spec['annotations']['io.factorybox.shared-source']=json.dumps({'source':str(project),'destination':'/home/coder/project'})
spec['annotations']['io.factorybox.checkpoint.irmap-scan-paths']=json.dumps(['/home/coder/project'])
path.write_text(json.dumps(spec))
'''
        if args.alias_mount:
            configure += '''
spec['process']['user']={'uid':0,'gid':0}
spec['process']['args']=['/opt/factorybox-alias-init']
for name in ['bounding','effective','permitted']:
 if 'CAP_SYS_ADMIN' not in spec['process']['capabilities'][name]:spec['process']['capabilities'][name].append('CAP_SYS_ADMIN')
path.write_text(json.dumps(spec))
'''
        if args.rename_directory:
            configure += '''
(project/'folder').mkdir();os.chown(project/'folder',101000,101000)
spec['process']['env'].append('FACTORYBOX_SHARED_SOURCE_SUBDIRECTORY=1')
path.write_text(json.dumps(spec))
'''
        if args.outbound_network:
            configure += "\nspec['annotations']['io.factorybox.outbound-ipv4']='true'\nspec['process']['env'].append("+repr('FACTORYBOX_NETWORK_URL=http://'+result['network_server_ip']+':18181/')+")\npath.write_text(json.dumps(spec))\n"
        if args.docker_child:
            configure += '''
spec['process']['user']={'uid':0,'gid':0}
spec['process']['args']=['/opt/factorybox-example-actor','--docker-parent']
for name in ['bounding','effective','permitted']:
 for capability in ['CAP_SYS_ADMIN','CAP_NET_ADMIN']:
  if capability not in spec['process']['capabilities'][name]:spec['process']['capabilities'][name].append(capability)
path.write_text(json.dumps(spec))
'''
        if factory_preparer:
            configure = '''import os,pathlib
project=pathlib.Path('/factory-source/project')
for name,content in [('current.txt','before'),('held.txt','original-held---'*256)]:
 path=project/name;path.write_text(content);os.chown(path,101000,101000)
'''
            if args.rename_directory:
                configure += "(project/'folder').mkdir();os.chown(project/'folder',101000,101000)\n"
        run(['exec', identity, 'python3', '-c', 'bundle_path=' + repr(bundle) + '\n' + configure])
        ctl = ['exec', identity, '/opt/factorybox/factorybox', '--worker-run-root', '/run/factorybox',
               '--worker-store-id', owner, '--worker-epoch', status['epoch']]
        result['start'] = json.loads(run([*ctl, 'start', '--bundle', bundle, sandbox_id], timeout=90))
        live_pid = result['start']['runtime']['pid']
        def docker_observation():
            prefix = ['exec', identity, '/opt/factorybox/components/sysbox-runc', '--root',
                      status['lifecycle']['root']+'/runtime', 'exec', result['start']['record']['runtime_id'],
                      '/usr/local/bin/docker']
            info = json.loads(run([*prefix, 'info', '--format', '{{json .}}']))
            child = json.loads(run([*prefix, 'inspect', 'source-probe']))[0]
            if not child['State']['Running'] or child['RestartCount'] != 0:
                raise ValueError('private Docker child stopped or restarted')
            return {'storage_driver': info['Driver'], 'server_version': info['ServerVersion'],
                    'child_id': child['Id'], 'child_running': True, 'child_restart_count': child['RestartCount']}
        useful_deadline = time.monotonic() + (60 if args.docker_child else 15)
        while True:
            try:
                result['first_use'] = actor('POST')
                if args.outbound_network and result['first_use']['network'] != owner:
                    raise ValueError('first useful work did not reach its owned external endpoint')
                break
            except ValueError:
                if args.docker_child:
                    state = json.loads(run([*ctl,'state',sandbox_id]))
                    if state.get('runtime',{}).get('status')=='stopped':
                        raise ValueError('private Docker fixture stopped before first useful work; see its preserved private logs')
                if time.monotonic() > useful_deadline:
                    raise
                time.sleep(.1)
        if args.docker_child:
            result['docker_first_use'] = docker_observation()
        ownership = json.loads(run(['exec', identity, 'python3', '-c', "import json,os;stat=os.stat('/factory-source/project/runtime-write');print(json.dumps({'uid':stat.st_uid,'gid':stat.st_gid}))"]))
        result['runtime_source_owner'] = ownership
        if ownership != {'uid':101000,'gid':101000}:
            raise ValueError('runtime source writes differ from the Factory Source service identity')
        cases = ['closed-file-edit', 'open-file-atomic-replacement', 'closed-file-edit'] if args.case == 'repeat' else [args.case]
        for kind in cases:
            phase = {'kind': kind, 'before': actor(), 'snapshot_id': uuid.uuid4().hex}
            result['phases'].append(phase)
            phase['pause'] = json.loads(run([*ctl, 'pause', '--snapshot-id', phase['snapshot_id'], sandbox_id], timeout=90))
            edit = '''import os,pathlib
root=pathlib.Path('/factory-source/project')
temporary=root/'.editor-save';temporary.write_text(content);os.chown(temporary,101000,101000)
os.replace(temporary,root/filename)
'''
            filename = 'current.txt' if kind == 'closed-file-edit' else 'held.txt'
            content = 'accepted editor change with a different size' if kind == 'closed-file-edit' else 'replacement-held'*512
            run(['exec', identity, 'python3', '-c', 'filename=' + repr(filename) + '\ncontent=' + repr(content) + '\n' + edit])
            if args.rename_directory and kind == 'open-file-atomic-replacement':
                run(['exec', identity, 'python3', '-c', "import os;os.rename('/factory-source/project/folder','/factory-source/project/renamed')"])
                phase['expected_cwd'] = phase['before']['cwd'].removesuffix('/folder') + '/renamed'
            else:
                phase['expected_cwd'] = phase['before']['cwd']
            phase['retained_watch_before_resume'] = json.loads(run(['exec', identity, 'python3', '-c', '''import array,ctypes,fcntl,json,os,pathlib,termios
result=[]
pidfd=os.pidfd_open(1);libc=ctypes.CDLL(None,use_errno=True)
for path in pathlib.Path('/proc/1/fd').iterdir():
 try:
  if os.readlink(path)!='anon_inode:inotify':continue
  fd=libc.syscall(438,pidfd,int(path.name),0)
  if fd<0:raise OSError(ctypes.get_errno(),'duplicate retained watch')
  pending=array.array('i',[0])
  fcntl.ioctl(fd,termios.FIONREAD,pending);os.close(fd)
  result.append({'fd':path.name,'info':pathlib.Path('/proc/1/fdinfo',path.name).read_text(),'pending_bytes':pending[0]})
 except OSError as error:result.append({'fd':path.name,'error':str(error)})
print(json.dumps(result))
''']))
            if args.outbound_network and len(result['phases'])==1:
                peer = result['start']['record']['network']['peer']
                run(['exec',identity,'ip','link','add',peer,'type','dummy'])
                run(['exec',identity,'ip','link','set',peer,'alias','fixture:'+owner])
                run([*ctl,'resume','--snapshot-id',phase['snapshot_id'],sandbox_id],failure=True)
                refused = json.loads(run([*ctl,'state',sandbox_id]))
                link = json.loads(run(['exec',identity,'ip','-j','-d','link','show','dev',peer]))
                if (refused['record']['phase']!='paused' or refused['record']['pause'].get('resume') is not None or
                        len(link)!=1 or link[0].get('ifalias')!='fixture:'+owner or link[0].get('linkinfo',{}).get('info_kind')!='dummy'):
                    raise ValueError('foreign network peer refusal changed the pause or unrelated interface')
                run(['exec',identity,'ip','link','delete',peer])
                phase['foreign_peer_refused_before_resume_transition'] = True
            phase['resume'] = json.loads(run([*ctl, 'resume', '--snapshot-id', phase['snapshot_id'], sandbox_id], timeout=90))
            live_pid = phase['resume']['runtime']['pid']
            phase['after'] = actor()
            if args.docker_child:
                phase['docker_after'] = docker_observation()
                if phase['docker_after'] != result['docker_first_use']:
                    raise ValueError('Resume changed the private Docker daemon or child identity')
            if args.outbound_network and phase['after']['network'] != owner:
                raise ValueError('restored sandbox lost its owned outbound network')
            if any(phase['after'][key] != phase['before'][key] for key in ['counter', 'ram_sha256', 'held', 'mapped']) or phase['after']['cwd'] != phase['expected_cwd']:
                raise ValueError('Resume lost private RAM or the original open file/mapping')
            if phase['after']['current'] != (content if filename == 'current.txt' else phase['before']['current']):
                raise ValueError('Resume replaced an accepted editor change')
            phase['queued_watch_preserved'] = phase['after']['watch_bytes'] > 0
            if kind == 'open-file-atomic-replacement' and run(['exec', identity, 'cat', '/factory-source/project/held.txt']) != content:
                raise ValueError('Resume rewrote the independently owned canonical source')
            run(['exec', identity, 'python3', '-c', "from pathlib import Path;Path('/factory-source/project/watch-after-resume').write_text('changed')"])
            phase['watch_after_resume'] = actor()
            if phase['watch_after_resume']['watch_bytes'] <= 0:
                raise ValueError('restored source directory watcher did not observe new edits')
        if not all(phase['queued_watch_preserved'] for phase in result['phases']):
            raise ValueError('Resume lost the source watch events queued during the pause')
        result['delete'] = json.loads(run([*ctl, 'delete', sandbox_id], timeout=60))
        if args.outbound_network:
            rules = run(['exec',identity,'iptables-save'])
            links = json.loads(run(['exec',identity,'ip','-j','link','show']))
            peer = result['start']['record']['network']['peer']
            if 'factorybox:'+result['start']['record']['owner'] in rules or any(link['ifname']==peer for link in links):
                raise ValueError('Delete left an owned network rule or peer')
            result['network_cleanup_verified'] = True
        if factory_preparer:
            code = factory_preparer.read_text()
            code = code[:code.index("if __name__ == '__main__':")]+ '\nprint(json.dumps(release(json.loads(sys.argv[1]),False)))\n'
            result['factory_input_release'] = json.loads(run(['exec','--env','FACTORY_POD_UID='+identity,'--env','FACTORY_STORE_ID='+owner,
                                                            identity,'python3','-c',code,json.dumps(request)],timeout=60))
        else:
            result['input_release'] = json.loads(run(['exec', identity, 'python3', '-c', mount_script, 'release', bundle, json.dumps(result['input_mounts'])]))
        run(['stop', '--timeout', '45', identity])
        result['final_state'] = exact()['State']
        result['worker_log'] = run(['logs', identity])
        if result['final_state']['Running'] or result['final_state']['ExitCode'] != 0 or result['final_state']['OOMKilled']:
            raise ValueError('shared-source worker did not drain cleanly')
        result['passed'] = True
    except Exception as error:
        result['error'] = str(error)
        if identity:
            try:
                exact()
                if args.outbound_network:
                    result['failed_worker_network'] = {'links':json.loads(run(['exec',identity,'ip','-j','-d','link','show'])),
                        'routes':json.loads(run(['exec',identity,'ip','-j','route','show']))}
                diagnostic = '''import json,os,pathlib,subprocess
status=json.loads(subprocess.check_output(['/opt/factorybox/factorybox','worker-status']))
root=status['lifecycle']['root']
arguments=['tar','--exclude=*/filesystem/lower','--exclude=*/materialized','-czf','/var/lib/sysbox/shared-source-diagnostic.tar.gz','-C',root]
arguments.extend(name for name in ['objects','runtime','forks'] if pathlib.Path(root,name).exists())
bundle=pathlib.Path('/var/lib/sysbox/fixed-store/data/store/input/bundle')
if (bundle/'upper').exists():arguments.extend(['-C',str(bundle),'upper'])
subprocess.run(arguments,check=True,timeout=45)
'''
                run(['exec', identity, 'python3', '-c', diagnostic], timeout=50)
                archive = artifact / 'diagnostic.tar.gz'
                run(['cp', identity + ':/var/lib/sysbox/shared-source-diagnostic.tar.gz', str(archive)], timeout=60)
                decoded = hashlib.sha256()
                import gzip
                with gzip.open(archive, 'rb') as stream:
                    while chunk := stream.read(1 << 20):
                        decoded.update(chunk)
                with tarfile.open(archive) as tar:
                    result['diagnostic_members'] = len(tar.getmembers())
                result['diagnostic'] = {'sha256': reference.sha(archive), 'decoded_sha256': decoded.hexdigest(), 'whole_decoding_verified': True}
            except Exception as diagnostic_error:
                result['diagnostic_error'] = str(diagnostic_error)
        raise
    finally:
        try:
            if attempted:
                reference.owned_delete('container', name, owner)
            if server_attempted:
                if server_identity:
                    consumer_worker_loss.exact_worker(name+'-http',owner,server_identity)
                reference.owned_delete('container',name+'-http',owner)
            for volume in reversed(created):
                reference.owned_delete('volume', volume, owner)
            result['cleanup'] = True
            result['global_sysctls_after'] = consumer_worker_loss.sysctls()
            result['global_sysctls_after']['net/ipv4/ip_forward'] = Path('/proc/sys/net/ipv4/ip_forward').read_text().strip()
            if result['global_sysctls_after'] != before:
                result['passed'] = False
                raise ValueError('global sysctls changed during shared-source proof')
        finally:
            result['inputs_unchanged'] = (reference.sha(actor_binary) == result.get('actor_sha256') and
                upstream_unit.context_manifest(source / 'tests/shared-source-actor') == result['actor_source_context']) if 'actor_sha256' in result else False
            if args.diagnostic_runtime and 'runtime_binary_sha256' in result:
                actual = {directory + '/' + key: value for directory in ['internal', 'cmd/factorybox']
                          for key, value in upstream_unit.context_manifest(source / directory).items()}
                actual.update({file: {'sha256': reference.sha(source / file)} for file in ['go.mod', 'go.sum']})
                result['inputs_unchanged'] = result['inputs_unchanged'] and actual == current and reference.sha(runtime_binary) == result['runtime_binary_sha256']
            if args.outbound_network and 'network_server_sha256' in result:
                result['inputs_unchanged'] = result['inputs_unchanged'] and reference.sha(server_script)==result['network_server_sha256']
            if args.alias_mount and 'alias_binary_sha256' in result:
                result['inputs_unchanged'] = result['inputs_unchanged'] and reference.sha(alias_binary)==result['alias_binary_sha256']
            if factory_preparer is not None:
                result['inputs_unchanged'] = result['inputs_unchanged'] and reference.sha(factory_preparer)==result['factory_preparer_sha256']
            result['passed'] = result['passed'] and result['cleanup']
            result['passed'] = result['passed'] and result['inputs_unchanged']
            (artifact / 'result.json').write_text(json.dumps(result, indent=2) + '\n')
            print(json.dumps({'evidence': str(artifact), 'passed': result['passed'], 'cleanup': result['cleanup']}), flush=True)


if __name__ == '__main__':
    main()
