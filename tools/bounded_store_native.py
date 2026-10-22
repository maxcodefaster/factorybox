#!/usr/bin/env python3
"""Exercise the exact delivered worker with its opt-in fixed filesystem."""
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
import reference
import reference_lifecycle
import upstream_bats
import upstream_unit


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--worker-build', required=True)
    args = parser.parse_args()
    _, worker = build_worker.load(args.worker_build)
    native_base.require_native_host('linux/amd64', json.loads(reference.docker(['info', '--format', '{{json .}}'])))
    if reference.docker(['ps', '--filter', 'label=com.factorybox.run', '--format', '{{.ID}}']):
        raise ValueError('other Factorybox runs must finish before this serialized test')
    owner = uuid.uuid4().hex
    name = 'factorybox-bounded-store-' + owner
    artifact = reference.new_artifact(name)
    artifact.mkdir()
    source = artifact / 'source'
    for directory in ['internal', 'cmd/factorybox']:
        shutil.copytree(reference.ROOT / directory, source / directory)
    for file in ['go.mod', 'go.sum']:
        shutil.copyfile(reference.ROOT / file, source / file)
    shutil.copyfile(__file__, artifact / 'runner.py')
    shutil.copyfile(reference.ROOT / 'tests/bounded-store-setup.sh', artifact / 'setup.sh')
    manifest = upstream_unit.context_manifest(source)
    core = lambda rows: {k: v for k, v in rows.items() if not k.endswith('_test.go')}
    if core(manifest) != core(worker['source_context']):
        raise ValueError('native core differs from delivered image')
    busybox = json.loads((reference.ROOT / 'upstream/test-inputs.json').read_text())['busybox-amd64']
    rootfs = reference.ROOT / '.cache/downloads' / busybox['archive']
    if reference.sha(rootfs) != busybox['sha256']:
        raise ValueError('native rootfs changed')
    output = artifact / 'output'
    output.mkdir()
    result = {'schema': 1, 'owner': owner, 'passed': False, 'cleanup': False, 'production_qualified': False,
              'worker_build': args.worker_build, 'worker_image_id': worker['image_id'], 'source_context': manifest,
              'runner_sha256': reference.sha(artifact / 'runner.py'), 'setup_sha256': reference.sha(artifact / 'setup.sh'),
              'rootfs_sha256': busybox['sha256'], 'global_sysctls_before': consumer_worker_loss.sysctls(),
              'scope': 'Exact delivered bounded worker, 512-MiB fixed filesystem, full RAM/filesystem checkpoint/fork, '
                       'pause/resume, actual guest ENOSPC, distinct host/internal admission, external writable bind '
                       'refusal and preserved prior checkpoint. Not worker replacement, broad consumer or production qualification.'}
    reference_lifecycle.write_json(artifact / 'intent.json', result)
    volume = container = preserve = False
    identity = None
    started = time.monotonic()
    print(json.dumps({'running': str(artifact)}), flush=True)
    try:
        binary = artifact / 'probe.test'
        with (artifact / 'build.log').open('x') as log:
            subprocess.run(['go', 'test', '-c', '-mod=readonly', '-trimpath', '-buildvcs=false',
                            '-tags=factorybox_worker_probe', '-o', str(binary), './internal/lifecycle'],
                           cwd=source, env=dict(os.environ, GOOS='linux', GOARCH='amd64', CGO_ENABLED='0'),
                           stdout=log, stderr=subprocess.STDOUT, check=True, timeout=120)
        result['test_binary_sha256'] = reference.sha(binary)
        volume = True
        reference.docker(['volume', 'create', '--label', 'com.factorybox.run=' + owner, name])
        command = ['run', '--detach', '--pull=never', '--name', name, '--label', 'com.factorybox.run=' + owner,
                   '--privileged', '--cgroupns=private', '--cpus=3', '--memory=3g', '--pids-limit=512', '--network=none',
                   '--env', 'GOMAXPROCS=2', '--env', 'GOMEMLIMIT=256MiB', '--env', 'FACTORYBOX_NATIVE_BOUNDED_STORE=1',
                   '--mount', f'type=volume,source={name},target=/var/lib/sysbox']
        for path, target, readonly in [(binary, '/probe.test', True), (rootfs, '/busybox-rootfs.tar.gz', True),
                                       (artifact / 'setup.sh', '/setup.sh', True), (output, '/output', False)]:
            command += ['--mount', f'type=bind,source={path},target={target}' + (',readonly' if readonly else '')]
        command += [worker['image'], '--store-id', owner, '--bounded-root', '/var/lib/sysbox/fixed-store', '--bounded-bytes', str(512 << 20)]
        result['command'] = command
        container = True
        identity = reference.docker(command)
        result['container_id'] = identity
        deadline = time.monotonic() + 420
        result['startup'] = consumer_worker_loss.ready(name, owner, identity, deadline)
        with (artifact / 'stdout.txt').open('x') as stdout, (artifact / 'stderr.txt').open('x') as stderr:
            done = subprocess.run([*reference.DOCKER, 'exec', identity, '/bin/bash', '/setup.sh'], stdout=stdout, stderr=stderr,
                                  timeout=max(.1, deadline-time.monotonic()))
        result['exit_code'] = done.returncode
        state = consumer_worker_loss.exact_worker(name, owner, identity)['State']
        result['state'] = state
        log = (artifact / 'stdout.txt').read_text()
        result['passed_tests'] = sum(line.startswith('--- PASS: TestNativeBoundedWorkerStore') for line in log.splitlines())
        if done.returncode or not state['Running'] or state['OOMKilled'] or result['passed_tests'] != 1 or '--- SKIP:' in log:
            raise ValueError('native bounded worker assertions failed or skipped')
        proof_path = output / 'bounded-store.json'
        proof = json.loads(proof_path.read_text())
        required = ['passed', 'direct_io', 'external_writable_bind_refused', 'checkpoint_fork', 'branch_ram_independent',
                    'resume_ram_preserved', 'capture_refused_at_limit', 'prior_checkpoint_preserved', 'owned_truncate_released_blocks']
        if (any(proof.get(key) is not True for key in required) or
                proof['full_capacity']['filesystem_available_bytes'] != 0 or
                'no space left' not in proof['guest_actual_enospc'].lower() or
                proof['pause']['snapshot']['resident_bytes_after'] != 0):
            raise ValueError('bounded worker proof lacks required assertions')
        result['proof_sha256'] = reference.sha(proof_path)
        result['passed'] = True
    except Exception as error:
        result['error'] = str(error)
    finally:
        try:
            if container:
                if identity:
                    consumer_worker_loss.exact_worker(name, owner, identity)
                logs = subprocess.run([*reference.DOCKER, 'logs', name], capture_output=True, text=True, timeout=5)
                (artifact / 'worker.log').write_text(logs.stdout+logs.stderr)
                reference.owned_delete('container', name, owner)
            if volume and not result['passed']:
                try:
                    upstream_bats.archive_stopped_volumes(artifact, name, owner, [name], worker['image'], result,
                                                          timeout=120, compressed=True)
                except Exception as error:
                    preserve = True
                    result['archive_error'] = str(error)
            if volume and not preserve:
                reference.owned_delete('volume', name, owner)
            result['cleanup'] = not preserve
        finally:
            result['global_sysctls_after'] = consumer_worker_loss.sysctls()
            result['inputs_unchanged'] = (upstream_unit.context_manifest(source) == manifest and
                                          reference.sha(artifact / 'runner.py') == result['runner_sha256'] and
                                          reference.sha(artifact / 'setup.sh') == result['setup_sha256'])
            result['passed'] = (result['passed'] and result['cleanup'] and result['inputs_unchanged'] and
                                result['global_sysctls_after'] == result['global_sysctls_before'])
            result['outer_seconds'] = time.monotonic()-started
            reference_lifecycle.write_json(artifact / 'result.json', result)
            print(json.dumps({'evidence': str(artifact), 'passed': result['passed'], 'cleanup': result['cleanup'],
                              'error': result.get('error')}), flush=True)
    if not result['passed']:
        raise SystemExit(1)


if __name__ == '__main__':
    main()
