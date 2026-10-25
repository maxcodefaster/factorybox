#!/usr/bin/env python3
"""Native HTTP readiness and epoch-bound administrative CLI proof, not Kubernetes qualification."""
import argparse
import json
import os
from pathlib import Path
import shutil
import subprocess
import time
import uuid

import build_worker
import consumer_worker_loss
import native_base
import devx_inputs
import reference
import upstream_unit


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--worker-build', required=True)
    parser.add_argument('--factory-repo', type=Path, help='Run the real Factory TypeScript adapter over the owned native CLI transport')
    parser.add_argument('--consumer-inputs', help='Exact retained consumer rootfs; required with --factory-repo')
    args = parser.parse_args()
    if bool(args.factory_repo) != bool(args.consumer_inputs):
        parser.error('--factory-repo and --consumer-inputs must be supplied together')
    _, worker = build_worker.load(args.worker_build)
    native_base.require_native_host('linux/amd64', json.loads(reference.docker(['info', '--format', '{{json .}}'])))
    current = {directory + '/' + key: value for directory in ['internal', 'cmd/factorybox']
               for key, value in upstream_unit.context_manifest(reference.ROOT / directory).items()}
    current.update({file: {'sha256': reference.sha(reference.ROOT / file)} for file in ['go.mod', 'go.sum']})
    if current != worker['source_context']:
        raise ValueError('native probe requires the exact current implementation')
    owner = uuid.uuid4().hex
    name = 'factorybox-kubernetes-worker-' + owner
    artifact = reference.new_artifact(name)
    artifact.mkdir(mode=0o700)
    shutil.copyfile(__file__, artifact / 'runner.py')
    result = {'schema': 1, 'owner': owner, 'worker_build': args.worker_build,
              'image_id': worker['image_id'], 'passed': False, 'cleanup': False,
              'kubernetes_qualified': False, 'production_qualified': False,
              'scope': __doc__, 'commands': []}
    attempted = volume = False
    deadline = time.monotonic() + 420
    before = consumer_worker_loss.sysctls()
    result['global_sysctls_before'] = before

    def run(arguments, *, failure=False, timeout=30):
        completed = subprocess.run([*reference.DOCKER, *arguments], capture_output=True, text=True,
                                   timeout=min(timeout, max(.1, deadline - time.monotonic())))
        result['commands'].append({'arguments': arguments, 'exit': completed.returncode,
                                   'stdout': completed.stdout, 'stderr': completed.stderr})
        if bool(completed.returncode) != failure:
            raise ValueError('native administrative command outcome differed: ' + completed.stderr[-2048:])
        return completed.stdout

    def exact(identity):
        saved = consumer_worker_loss.exact_worker(name, owner, identity)
        limits = saved['HostConfig']
        mounts = [m for m in saved['Mounts'] if m['Destination'] == '/var/lib/sysbox']
        if (saved['Image'] != worker['image_id'] or limits['Memory'] != 3 << 30 or
                limits['NanoCpus'] != 3_000_000_000 or limits['PidsLimit'] != 512 or
                limits['CgroupnsMode'] != 'private' or limits['NetworkMode'] != 'none' or
                not limits['Privileged'] or len(mounts) != 1 or mounts[0].get('Name') != name):
            raise ValueError('native worker ownership, private store or resource limits differ')
        return saved

    try:
        reference.docker(['volume', 'create', '--label', 'com.factorybox.run=' + owner, name])
        volume = True
        command = ['run', '--detach', '--pull=never', '--name', name, '--label', 'com.factorybox.run=' + owner,
                   '--privileged', '--cgroupns=private', '--cpus=3', '--memory=3g', '--pids-limit=512',
                   '--network=none', '--stop-timeout=45', '--env', 'GOMAXPROCS=2', '--env', 'GOMEMLIMIT=256MiB',
                   '--mount', f'type=volume,source={name},target=/var/lib/sysbox', worker['image'],
                   '--store-id', owner, '--bounded-root', '/var/lib/sysbox/fixed-store',
                   '--bounded-bytes', str(8 << 30), '--health-listen', '127.0.0.1:8081']
        attempted = True
        identity = run(command).strip()
        result['container_id'] = identity
        started = time.monotonic()
        # Docker logs creates no exec task during cgroup delegation.
        while True:
            if not exact(identity)['State']['Running']:
                raise ValueError('worker exited before HTTP readiness publication')
            logs = reference.docker(['logs', '--tail=5', identity])
            try:
                status = json.loads(logs)
            except json.JSONDecodeError:
                status = None
            if isinstance(status, dict) and status.get('store_id') == owner and status.get('ready_at'):
                break
            if time.monotonic() - started > 120:
                raise TimeoutError('worker did not publish live readiness')
            time.sleep(.1)
        result['startup_seconds'] = time.monotonic() - started
        result['status'] = json.loads(run(['exec', identity, '/opt/factorybox/factorybox', 'worker-status']))
        probe = '''import json,urllib.request,urllib.error
results=[]
for method,path,expected in [('GET','/readyz',204),('POST','/readyz',405),('GET','/readyz?details=1',404)]:
 try: response=urllib.request.urlopen(urllib.request.Request('http://127.0.0.1:8081'+path,method=method),timeout=5)
 except urllib.error.HTTPError as error: response=error
 body=response.read()
 assert response.code==expected,(method,path,response.code,body)
 if path=='/readyz': assert body==b'',body
 results.append({'method':method,'path':path,'status':response.code})
print(json.dumps(results))
'''
        result['http'] = json.loads(run(['exec', identity, 'python3', '-c', probe]))
        ctl = ['exec', identity, '/opt/factorybox/factorybox', '--worker-run-root', '/run/factorybox']
        result['capacity'] = json.loads(run([*ctl, '--worker-store-id', owner, '--worker-epoch', status['epoch'], 'capacity']))
        for wrong_store, wrong_epoch in [('0' * 32, status['epoch']), (owner, '0' * 32)]:
            output = run([*ctl, '--worker-store-id', wrong_store, '--worker-epoch', wrong_epoch, 'capacity'], failure=True)
            if output or 'differs from the requested worker identity' not in result['commands'][-1]['stderr']:
                raise ValueError('changed worker identity was not refused before the operation')
        if args.factory_repo:
            inputs, consumer = devx_inputs.load(args.consumer_inputs, rootfs=True, platform='linux/amd64')
            result['consumer_inputs'] = args.consumer_inputs
            factory = args.factory_repo.resolve()
            source_names = ['packages/runtime-kubernetes/src/' + file for file in
                            ['factorybox-worker.ts', 'command.ts', 'config.ts']]
            source_names += ['scripts/runtime/factorybox-worker-native.ts', 'package.json', 'bun.lock', 'mise.toml']
            # Execute an immutable copy below the consumer's generated evidence
            # directory, where its locked node_modules remain resolvable.
            # Concurrent edits in the live checkout cannot change this run.
            frozen_factory = factory / '.factory/test-results' / name / 'source'
            frozen_factory.mkdir(mode=0o700, parents=True)
            hashes = {}
            for source_name in source_names:
                source = factory / source_name
                hashes[source_name] = reference.sha(source)
                frozen = artifact / 'factory-source' / source_name
                frozen.parent.mkdir(parents=True, exist_ok=True)
                shutil.copyfile(source, frozen)
                runnable = frozen_factory / source_name
                runnable.parent.mkdir(parents=True, exist_ok=True)
                shutil.copyfile(source, runnable)
            result['factory_source_sha256'] = hashes
            result['factory_frozen_source'] = str(frozen_factory)
            setup = artifact / 'setup.sh'
            declaration = reference.ROOT / 'tests/compatibility/worker-example-bounded-paths.json'
            fixture = json.loads(declaration.read_text())
            original = reference.ROOT / fixture['file']
            if reference.sha(original) != fixture['before_sha256'] or not fixture['assertions_preserved']:
                raise ValueError('bounded setup source no longer matches its immutable declaration')
            setup.write_bytes(original.read_bytes().replace(fixture['old_path'].encode(), fixture['new_path'].encode()))
            if reference.sha(setup) != fixture['after_sha256']:
                raise ValueError('bounded setup differs from its path-only compatibility declaration')
            shutil.copyfile(original, artifact / 'setup.before.sh')
            shutil.copyfile(declaration, artifact / declaration.name)
            result['bounded_fixture'] = fixture
            source = artifact / 'source'
            shutil.copytree(reference.ROOT / 'tests/minimal-actor', source / 'tests/minimal-actor')
            for file in ['go.mod', 'go.sum']:
                shutil.copyfile(reference.ROOT / file, source / file)
            actor = artifact / 'minimal-actor'
            subprocess.run(['go', 'build', '-trimpath', '-buildvcs=false', '-o', str(actor), './tests/minimal-actor'],
                           cwd=source, env=dict(os.environ, CGO_ENABLED='0'), check=True, timeout=120)
            for path, target in [(inputs / 'runtime-rootfs.tar', 'native-rootfs.tar'), (actor, 'native-minimal-actor'), (setup, 'native-setup.sh')]:
                run(['cp', str(path), identity + ':/var/lib/sysbox/' + target], timeout=90)
            run(['exec', '--env', 'FACTORYBOX_WORKER_EXAMPLE_NATIVE=1', identity, '/bin/bash', '/var/lib/sysbox/native-setup.sh',
                 consumer['rootfs_tar_sha256'], reference.sha(actor)], timeout=180)
            bundle = '/var/lib/sysbox/fixed-store/data/store/input/bundle'
            helper = (reference.ROOT / 'examples/worker/bundle_mounts.py').read_text()
            mounts = json.loads(run(['exec', identity, 'python3', '-c', helper, 'inspect', bundle]))
            request = {'version': 1, 'namespace': 'factorybox-native-' + owner[:12], 'podName': 'worker',
                       'storeID': owner, 'trustDomainID': owner, 'image': worker['image'],
                       'executableSHA256': worker['bundle_sha256']['factorybox'], 'nodeName': 'native-owned-node',
                       'kernel': os.uname().release, 'boundedBytes': 8 << 30, 'claimGiB': 32}
            descriptor = artifact / 'factory-native-input.json'
            descriptor.write_text(json.dumps({'request': request, 'docker': reference.DOCKER, 'containerID': identity,
                                              'bootID': status['boot_id']}))
            native = subprocess.run(['mise', 'exec', '--', 'bun', str(frozen_factory / 'scripts/runtime/factorybox-worker-native.ts'), str(descriptor)],
                                    cwd=factory, capture_output=True, text=True, timeout=max(.1, deadline-time.monotonic()))
            (artifact / 'factory-native.stdout').write_text(native.stdout)
            (artifact / 'factory-native.stderr').write_text(native.stderr)
            native.check_returncode()
            proof = json.loads(native.stdout)
            if proof.get('passed') is not True or proof.get('kubernetesQualified') is not False:
                raise ValueError('Factory native adapter did not confirm its bounded transport proof')
            result['factory_native'] = proof
            if any(reference.sha(frozen_factory / path) != digest or reference.sha(artifact / 'factory-source' / path) != digest
                   for path, digest in hashes.items()):
                raise ValueError('Frozen Factory source changed during native transport proof')
            result['released_bundle_mounts'] = json.loads(run(['exec', identity, 'python3', '-c', helper, 'release', bundle, json.dumps(mounts)]))
        fault = '''import os,urllib.request,urllib.error
path='/run/factorybox/worker.json'
os.rename(path,path+'.probe-held')
try:
 try: urllib.request.urlopen('http://127.0.0.1:8081/readyz',timeout=5);raise AssertionError('stale readiness accepted')
 except urllib.error.HTTPError as error: assert error.code==503 and error.read()==b''
finally: os.rename(path+'.probe-held',path)
'''
        run(['exec', identity, 'python3', '-c', fault])
        result['http_after_exact_marker_restore'] = json.loads(run(['exec', identity, 'python3', '-c', probe]))
        after = json.loads(run(['exec', identity, '/opt/factorybox/factorybox', 'worker-status']))
        if after != status:
            raise ValueError('native fault altered the exact worker identity')
        run(['stop', '--timeout', '45', identity])
        state = exact(identity)['State']
        result['final_state'] = state
        if state['Running'] or state['ExitCode'] != 0 or state['OOMKilled']:
            raise ValueError('worker did not drain cleanly')
        result['passed'] = True
    except Exception as error:
        result['error'] = str(error)
        raise
    finally:
        try:
            if attempted:
                reference.owned_delete('container', name, owner)
            if volume:
                reference.owned_delete('volume', name, owner)
            result['cleanup'] = True
            result['global_sysctls_after'] = consumer_worker_loss.sysctls()
            if result['global_sysctls_after'] != before:
                result['passed'] = False
                raise ValueError('global sysctls changed during native proof')
        finally:
            result['passed'] = result['passed'] and result['cleanup']
            (artifact / 'result.json').write_text(json.dumps(result, indent=2) + '\n')
            print(json.dumps({'evidence': str(artifact), 'passed': result['passed'], 'cleanup': result['cleanup']}), flush=True)


if __name__ == '__main__':
    main()
