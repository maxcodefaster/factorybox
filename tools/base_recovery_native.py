#!/usr/bin/env python3
"""Verify strict and authorized base recovery in an owned delivered worker."""
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
import reference
import upstream_bats
import upstream_unit


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--worker-build', required=True)
    parser.add_argument('--retained-inspection', action='store_true',
                        help='also require all sixteen read-only retained base/snapshot controls')
    args = parser.parse_args()
    build, worker = build_worker.load(args.worker_build)
    if reference.docker(['ps', '--filter', 'label=com.factorybox.run', '--format', '{{.ID}}']):
        raise ValueError('other native workers must close before base recovery checks')
    owner = uuid.uuid4().hex
    name = 'factorybox-base-recovery-' + owner
    artifact = reference.new_artifact(name)
    artifact.mkdir()
    source = artifact / 'source'
    for directory in ['internal', 'cmd/factorybox']:
        shutil.copytree(reference.ROOT / directory, source / directory)
    for file in ['go.mod', 'go.sum']:
        shutil.copyfile(reference.ROOT / file, source / file)
    shutil.copyfile(__file__, artifact / 'runner.py')
    manifest = upstream_unit.context_manifest(source)
    core = lambda rows: {k: v for k, v in rows.items() if not k.endswith('_test.go')}
    if core(manifest) != core(worker['source_context']):
        raise ValueError('native base checks differ from the delivered core')
    result = {'schema': 1, 'passed': False, 'cleanup': False, 'production_qualified': False,
              'owner': owner, 'worker_build': args.worker_build, 'package_sha256': worker['package_sha256'],
              'worker_result_sha256': reference.sha(build / 'result.json'), 'source_context': manifest,
              'global_sysctls_before': consumer_worker_loss.sysctls(),
              'retained_inspection': args.retained_inspection,
              'scope': 'Original base-library tests plus opt-in read-only retained inspection controls; the device-verifier cases use the owned 512-MiB ext4 store. No sandbox, throughput or full-consumer claim.'}
    attempted = volume_created = False
    preserve = False
    try:
        binary = artifact / 'probe.test'
        subprocess.run(['go', 'test', '-c', '-trimpath', '-buildvcs=false', '-o', str(binary), './internal/sysboxcapture'],
                       cwd=source, env=dict(os.environ, CGO_ENABLED='0'), check=True, timeout=120)
        result['binary_sha256'] = reference.sha(binary)
        reference.docker(['volume', 'create', '--label', 'com.factorybox.run=' + owner, name])
        volume_created = True
        command = ['run', '--detach', '--pull=never', '--name', name, '--label', 'com.factorybox.run=' + owner,
                   '--privileged', '--cgroupns=private', '--cpus=3', '--memory=3g', '--pids-limit=512', '--network=none',
                   '--env', 'GOMAXPROCS=2', '--env', 'GOMEMLIMIT=256MiB',
                   '--mount', 'type=volume,source=' + name + ',target=/var/lib/sysbox',
                   '--mount', 'type=bind,source=' + str(binary) + ',target=/probe.test,readonly',
                   worker['image'], '--store-id', owner, '--bounded-root', '/var/lib/sysbox/fixed-store',
                   '--bounded-bytes', str(512 << 20)]
        result['command'] = command
        attempted = True
        identity = reference.docker(command)
        result['identity'] = identity
        deadline = time.monotonic() + 420
        result['startup'] = consumer_worker_loss.ready(name, owner, identity, deadline)
        pattern = '^TestFilesystemBase(Device)?RecoveryNative$'
        tests = ['TestFilesystemBaseRecoveryNative', 'TestFilesystemBaseDeviceRecoveryNative']
        if args.retained_inspection:
            pattern = '^Test(FilesystemBase(Device)?RecoveryNative|RetainedFilesystemNative)$'
            tests += ['TestRetainedFilesystemNative']
        command = [*reference.DOCKER, 'exec', '--env', 'FACTORYBOX_NATIVE_RETAINED_CAPTURE=1',
                   '--env', 'FACTORYBOX_DEVICE_RECOVERY_TEST_ROOT=' + result['startup']['status']['store'],
                   identity, '/probe.test', '-test.v', '-test.run=' + pattern, '-test.timeout=60s']
        result['probe_command'] = command
        with (artifact / 'stdout.txt').open('x') as out, (artifact / 'stderr.txt').open('x') as err:
            done = subprocess.run(command, stdout=out, stderr=err, timeout=min(65, deadline-time.monotonic()))
        result['exit_code'] = done.returncode
        output = (artifact / 'stdout.txt').read_text()
        if done.returncode or '--- SKIP:' in output or any('--- PASS: ' + test + ' ' not in output for test in tests):
            raise ValueError('base recovery assertions failed or skipped')
        if args.retained_inspection:
            result['retained_inspection_controls'] = sum(line.strip().startswith('--- PASS: TestRetainedFilesystemNative/') for line in output.splitlines())
            if result['retained_inspection_controls'] != 16:
                raise ValueError('read-only retained inspection controls incomplete')
        consumer_worker_loss.exact_worker(name, owner, identity)
        remaining = deadline - time.monotonic()
        if remaining <= 2:
            raise TimeoutError('base checks exhausted the 420-second lifecycle')
        reference.docker(['container', 'stop', '--time=' + str(min(45, int(remaining)-1)), identity], timeout=remaining)
        state = consumer_worker_loss.exact_worker(name, owner, identity)['State']
        result['state'] = state
        if state['Running'] or state['Pid'] or state['OOMKilled'] or state['ExitCode']:
            raise ValueError('base checks left the bounded worker unable to Stop')
        result['inputs_unchanged'] = upstream_unit.context_manifest(source) == manifest and reference.sha(binary) == result['binary_sha256']
        result['passed'] = result['inputs_unchanged']
    except Exception as error:
        result['error'] = str(error)
        raise
    finally:
        try:
            if attempted:
                logs = subprocess.run([*reference.DOCKER, 'logs', name], capture_output=True, text=True, timeout=5)
                (artifact / 'worker.log').write_text(logs.stdout + logs.stderr)
                reference.owned_delete('container', name, owner)
            if volume_created and not result['passed']:
                try:
                    upstream_bats.archive_stopped_volumes(artifact, name, owner, [name], worker['image'], result, timeout=120, compressed=True)
                except Exception as error:
                    preserve = True
                    result.update(archive_error=str(error), retained_volumes=[name])
            if volume_created and not preserve:
                reference.owned_delete('volume', name, owner)
            result['cleanup'] = not preserve
            result['global_sysctls_after'] = consumer_worker_loss.sysctls()
            result['passed'] = result['passed'] and result['cleanup'] and result['global_sysctls_after'] == result['global_sysctls_before']
        finally:
            (artifact / 'result.json').write_text(json.dumps(result, indent=2) + '\n')
            print(json.dumps({'evidence': str(artifact), 'passed': result['passed'], 'cleanup': result['cleanup']}), flush=True)
    if not result['passed']:
        raise SystemExit(1)


if __name__ == '__main__':
    main()
