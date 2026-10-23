#!/usr/bin/env python3
"""Recover a committed pause after exact bounded worker removal on this host/boot."""
import argparse
import json
import os
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
    parser.add_argument('--loss', choices=['planned', 'abrupt'], required=True)
    parser.add_argument('--retained-selection', action='store_true',
                        help='release exact old recovery selections through the replacement before all original recovery assertions')
    args = parser.parse_args()
    _, worker = build_worker.load(args.worker_build)
    native_base.require_native_host('linux/amd64', json.loads(reference.docker(['info', '--format', '{{json .}}'])))
    if reference.docker(['ps', '--filter', 'label=com.factorybox.run', '--format', '{{.ID}}']):
        raise ValueError('other Factorybox runs must finish before this serialized test')
    owner = uuid.uuid4().hex
    name = 'factorybox-bounded-loss-' + owner
    artifact = reference.new_artifact(name)
    artifact.mkdir()
    source = artifact / 'source'
    for directory in ['internal', 'cmd/factorybox']:
        shutil.copytree(reference.ROOT / directory, source / directory)
    for file in ['go.mod', 'go.sum']:
        shutil.copyfile(reference.ROOT / file, source / file)
    shutil.copyfile(__file__, artifact / 'runner.py')
    setup = 'retained-selection-setup.sh' if args.retained_selection else 'bounded-loss-setup.sh'
    shutil.copyfile(reference.ROOT / 'tests' / setup, artifact / 'setup.sh')
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
              'loss': args.loss, 'retained_selection': args.retained_selection,
              'runner_sha256': reference.sha(artifact / 'runner.py'),
              'setup_sha256': reference.sha(artifact / 'setup.sh'), 'rootfs_sha256': busybox['sha256'],
              'global_sysctls_before': consumer_worker_loss.sysctls(), 'workers': [],
              'scope': 'Actual same-host/same-boot bounded worker replacement after a committed 32-MiB anonymous '
                       'RAM pause. All lifecycle actions use the delivered CLI. Exact image preservation, new epoch, '
                       'negative hash, exact recovery replay, independent descendants and fork of resumed descendant. '
                       'Not full consumer, reboot, migration or production qualification.'}
    reference_lifecycle.write_json(artifact / 'intent.json', result)
    volume = preserve = False
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
        deadline = time.monotonic() + 420

        def create_worker(suffix):
            row = {'name': name + '-' + suffix, 'removed': False}
            result['workers'].append(row)
            command = ['run', '--detach', '--pull=never', '--name', row['name'], '--label', 'com.factorybox.run=' + owner,
                       '--privileged', '--cgroupns=private', '--cpus=3', '--memory=3g', '--pids-limit=512', '--network=none',
                       '--env', 'GOMAXPROCS=2', '--env', 'GOMEMLIMIT=256MiB', '--env', 'FACTORYBOX_NATIVE_BOUNDED_LOSS=1',
                       '--mount', f'type=volume,source={name},target=/var/lib/sysbox']
            if args.retained_selection:
                command += ['--env', 'FACTORYBOX_NATIVE_RETAINED_SELECTION=1']
            for path, target, readonly in [(binary, '/probe.test', True), (rootfs, '/busybox-rootfs.tar.gz', True),
                                           (artifact / 'setup.sh', '/setup.sh', True), (output, '/output', False)]:
                command += ['--mount', f'type=bind,source={path},target={target}' + (',readonly' if readonly else '')]
            command += [worker['image'], '--store-id', owner, '--bounded-root', '/var/lib/sysbox/fixed-store', '--bounded-bytes', str(1 << 30)]
            row['command'] = command
            row['id'] = reference.docker(command)
            row['startup'] = consumer_worker_loss.ready(row['name'], owner, row['id'], deadline)
            return row

        def run_stage(row, stage):
            stamp = time.monotonic()
            with (artifact / (stage + '.stdout')).open('x') as stdout, (artifact / (stage + '.stderr')).open('x') as stderr:
                done = subprocess.run([*reference.DOCKER, 'exec', row['id'], '/bin/bash', '/setup.sh', stage],
                                      stdout=stdout, stderr=stderr, timeout=max(.1, deadline-time.monotonic()))
            row[stage] = {'seconds': time.monotonic()-stamp, 'exit_code': done.returncode}
            state = consumer_worker_loss.exact_worker(row['name'], owner, row['id'])['State']
            log = (artifact / (stage + '.stdout')).read_text()
            test = 'TestNativeRetainedSelection' if args.retained_selection else 'TestNativeBoundedLoss'
            if (done.returncode or state['OOMKilled'] or not state['Running'] or
                    sum(line.startswith('--- PASS: ' + test + stage + ' ') for line in log.splitlines()) != 1 or
                    '--- SKIP:' in log):
                raise ValueError('native bounded replacement stage failed or skipped: ' + stage)

        old = create_worker('old')
        run_stage(old, 'Prepare')
        pause_file = output / 'bounded-loss-pause.json'
        paused = json.loads(pause_file.read_text())
        if (paused.get('passed') is not True or paused['state']['ballast_bytes'] != 32 << 20 or
                paused['pause']['snapshot']['resident_bytes_after'] != 0 or paused['pause']['after']['anon'] != 0):
            raise ValueError('old worker did not publish a complete released pause')
        result['pause_sha256'] = reference.sha(pause_file)
        consumer_worker_loss.exact_worker(old['name'], owner, old['id'])
        reference.docker(['stop', '--time', '10', old['id']] if args.loss == 'planned' else ['kill', '--signal', 'KILL', old['id']])
        old['stopped'] = consumer_worker_loss.exact_worker(old['name'], owner, old['id'])['State']
        if old['stopped']['Running'] or old['stopped']['OOMKilled'] or old['stopped']['ExitCode'] != (0 if args.loss == 'planned' else 137):
            raise ValueError('old bounded worker did not finish the requested exact shutdown')
        logs = subprocess.run([*reference.DOCKER, 'logs', old['id']], capture_output=True, text=True, timeout=5)
        (artifact / 'old-worker.log').write_text(logs.stdout+logs.stderr)
        reference.owned_delete('container', old['name'], owner)
        old['removed'] = True
        if reference.docker(['ps', '-aq', '--no-trunc', '--filter', 'id=' + old['id']]):
            raise ValueError('old worker still exists; replacement refused')
        replacement = create_worker('replacement')
        run_stage(replacement, 'Recover')
        recovered_file = output / 'bounded-loss-recovered.json'
        proof = json.loads(recovered_file.read_text())
        required = ['passed', 'same_exact_image', 'new_epoch', 'wrong_hash_refused', 'exact_replay', 'private_files_preserved',
                    'independent_fork', 'fork_of_resumed_descendant', 'useful_after_source_delete', 'exact_sandbox_cleanup']
        if (any(proof.get(key) is not True for key in required) or proof['restored_private_ram_bytes'] != 32 << 20 or
                proof['status']['lifecycle']['bounded_store']['image'] != paused['status']['lifecycle']['bounded_store']['image']):
            raise ValueError('bounded replacement proof lacks required assertions')
        result['recovered_sha256'] = reference.sha(recovered_file)
        if args.retained_selection:
            selection_file = output / 'retained-selection-released.json'
            selection = json.loads(selection_file.read_text())
            required = ['passed', 'exact_release_replayed', 'other_selection_preserved',
                        'unresumed_pause_still_protected', 'original_backing_inventory_unchanged',
                        'full_original_recovery_assertions_passed']
            if (any(selection.get(key) is not True for key in required) or
                    selection.get('wrong_requests_refused') != 6 or
                    selection.get('old_lifecycle_fence_preserved') != (args.loss == 'planned')):
                raise ValueError('retained selection release proof incomplete')
            result['retained_selection_sha256'] = reference.sha(selection_file)
        result['passed'] = True
    except Exception as error:
        result['error'] = str(error)
    finally:
        try:
            for row in result['workers']:
                if row['removed']:
                    continue
                if row.get('id'):
                    consumer_worker_loss.exact_worker(row['name'], owner, row['id'])
                logs = subprocess.run([*reference.DOCKER, 'logs', row['name']], capture_output=True, text=True, timeout=5)
                (artifact / (row['name'].removeprefix(name + '-') + '-cleanup.log')).write_text(logs.stdout+logs.stderr)
                reference.owned_delete('container', row['name'], owner)
                row['removed'] = True
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
