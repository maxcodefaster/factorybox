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
import consumer_loop_holder
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
    parser.add_argument('--retirement-fence', action='store_true',
                        help='retain every original assertion, then test durable retirement protection across another worker loss')
    parser.add_argument('--epoch-retirement', action='store_true',
                        help='full public epoch admission, exact partial deletion, worker loss and completion with useful successor')
    args = parser.parse_args()
    if args.epoch_retirement and args.retirement_fence:
        raise ValueError('choose full epoch retirement or the internal fence fixture')
    args.retained_selection = args.retained_selection or args.retirement_fence or args.epoch_retirement
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
    if args.retirement_fence or args.epoch_retirement:
        setup = 'epoch-retirement-setup.sh' if args.epoch_retirement else 'epoch-retirement-fence-setup.sh'
        shutil.copyfile(consumer_loop_holder.__file__, artifact / 'consumer_loop_holder.py')
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
              'retirement_fence': args.retirement_fence,
              'epoch_retirement': args.epoch_retirement,
              'runner_sha256': reference.sha(artifact / 'runner.py'),
              'setup_sha256': reference.sha(artifact / 'setup.sh'), 'rootfs_sha256': busybox['sha256'],
              'global_sysctls_before': consumer_worker_loss.sysctls(), 'workers': [],
              'scope': 'Actual same-host/same-boot bounded worker replacement after a committed 32-MiB anonymous '
                       'RAM pause. All lifecycle actions use the delivered CLI. Exact image preservation, new epoch, '
                       'negative hash, exact recovery replay, independent descendants and fork of resumed descendant. '
                       'Not full consumer, reboot, migration or production qualification.'}
    if args.retirement_fence or args.epoch_retirement:
        result['loop_holder_input_sha256'] = reference.sha(artifact / 'consumer_loop_holder.py')
    reference_lifecycle.write_json(artifact / 'intent.json', result)
    volume = preserve = False
    loop_holder = None
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
            if args.retirement_fence:
                command += ['--env', 'FACTORYBOX_NATIVE_EPOCH_RETIREMENT_FENCE=1']
            if args.epoch_retirement:
                command += ['--env', 'FACTORYBOX_NATIVE_EPOCH_RETIREMENT=1']
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
            if args.retirement_fence:
                test = 'TestNativeEpochRetirementFence'
            if args.epoch_retirement:
                test = 'TestNativeEpochRetirement'
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
                        'full_original_recovery_assertions_passed', 'retained_checkpoint_inspected']
            if (any(selection.get(key) is not True for key in required) or
                    selection.get('wrong_requests_refused') != 6 or
                    selection.get('old_lifecycle_fence_preserved') != (args.loss == 'planned')):
                raise ValueError('retained selection release proof incomplete')
            result['retained_selection_sha256'] = reference.sha(selection_file)
        if args.retirement_fence or args.epoch_retirement:
            # The accepted fence, not a still-running process or flock, must
            # protect every successor while its owning worker disappears.
            consumer_worker_loss.exact_worker(replacement['name'], owner, replacement['id'])
            reference.docker(['kill', '--signal', 'KILL', replacement['id']])
            replacement['stopped'] = consumer_worker_loss.exact_worker(replacement['name'], owner, replacement['id'])['State']
            if replacement['stopped']['Running'] or replacement['stopped']['OOMKilled'] or replacement['stopped']['ExitCode'] != 137:
                raise ValueError('accepted-retirement worker did not finish exact SIGKILL')
            logs = subprocess.run([*reference.DOCKER, 'logs', replacement['id']], capture_output=True, text=True, timeout=5)
            (artifact / 'accepted-worker.log').write_text(logs.stdout+logs.stderr)
            reference.owned_delete('container', replacement['name'], owner)
            replacement['removed'] = True
            if reference.docker(['ps', '-aq', '--no-trunc', '--filter', 'id=' + replacement['id']]):
                raise ValueError('accepted-retirement worker still exists; replacement refused')
            loop_holder = consumer_loop_holder.new_case(name)
            result['replacement_loop_holder'] = loop_holder
            consumer_loop_holder.start(loop_holder, worker['image'], replacement['startup']['status'], deadline, consumer_worker_loss.ready)
            final = create_worker('maintenance-replacement')
            consumer_loop_holder.verify_replacement(loop_holder, final['startup']['status'])
            run_stage(final, 'Finish')
            finished_file = output / ('epoch-retirement-finished.json' if args.epoch_retirement else 'epoch-retirement-fence-finished.json')
            finished = json.loads(finished_file.read_text())
            required = ['passed', 'worker_loss_after_acceptance', 'loop_device_changed', 'exact_acceptance_replayed',
                        'successor_recovered_and_useful', 'same_byte_successor_fence_refused',
                        'external_fence_link_refused', 'premature_completion_refused',
                        'source_tombstone_preserved', 'exact_finish_replayed']
            if args.epoch_retirement:
                required = ['passed', 'public_epoch_retirement_passed', 'partial_payloads_removed_before_worker_loss',
                            'all_authorized_epoch_payloads_absent', 'source_metadata_preserved', 'request_replay_exact',
                            'independent_successor_recovered_and_useful', 'source_tombstone_retained', 'global_fence_cleared_exactly',
                            'prepared_source_reader_available', 'unknown_source_entry_refused', 'foreign_covering_mount_refused',
                            'active_selection_preserved', 'full_original_lifecycle_assertions_passed',
                            'fork_resume_epoch_retired', 'descendant_handover_verified',
                            'foreign_entries_after_acceptance_refused', 'public_unlink_interrupted',
                            'registered_base_retired', 'caller_base_input_preserved']
                if finished.get('wrong_requests_refused', 0) < 10:
                    raise ValueError('public epoch retirement lacks required admission refusals')
                before = finished.get('report', {}).get('before', {}).get('filesystem_available_bytes')
                after = finished.get('report', {}).get('after', {}).get('filesystem_available_bytes')
                if type(before) is not int or type(after) is not int or after <= before:
                    raise ValueError('public epoch retirement did not observe returned filesystem capacity')
                result['retired_epoch_observed_capacity_bytes'] = after - before
                interruption_file = output / 'epoch-retirement-interruption.json'
                interruption = json.loads(interruption_file.read_text())
                base_file = output / 'epoch-retirement-base.json'
                base = json.loads(base_file.read_text())
                base_root = finished['report']['request']['source_root'] + '/bases/' + base['id'] + '/rootfs'
                if (any(interruption.get(key) is not True for key in
                        ['passed', 'public_unlink_interrupted', 'exact_child_sigkill', 'completion_absent']) or
                        interruption.get('request') != finished['report']['request'] or
                        interruption.get('acceptance_sha256') != finished['report']['acceptance_sha256'] or
                        interruption.get('pid', 0) <= 1 or not interruption.get('process_start') or
                        base['tree']['saved'] != base_root or
                        interruption.get('removed_original_file') != base_root + '/retirement-window/4999' or
                        interruption.get('surviving_original_file') != base_root + '/retirement-window/0000'):
                    raise ValueError('public unlink interruption is not bound to its exact completed request and registered base')
                result['public_unlink_interruption_sha256'] = reference.sha(interruption_file)
                result['registered_base_sha256'] = reference.sha(base_file)
                enospc_file = output / 'epoch-retirement-enospc.json'
                enospc = json.loads(enospc_file.read_text())
                if (any(enospc.get(key) is not True for key in
                        ['passed', 'actual_preparation_enospc', 'original_backing_unchanged',
                         'intact_recovery_reader_available', 'retained_checkpoint_inspected',
                         'no_acceptance_or_completion', 'global_acceptance_fence_absent',
                         'public_preparation_inspected', 'public_preparation_cancelled',
                         'public_cancellation_preserved_original_backing', 'public_cancellation_under_pressure']) or
                        enospc.get('request') != finished['report']['request'] or
                        enospc.get('test_owned_partial_stage_cleanup') is not False or
                        enospc.get('public_cancellation_refusals', 0) < 5 or
                        enospc.get('available_blocks_at_cancellation', -1) < 0 or
                        enospc['available_blocks_at_cancellation'] * enospc['block_bytes'] > 512 << 10 or
                        enospc.get('available_blocks_after_pressure', -1) < 0 or
                        enospc['available_blocks_after_pressure'] * enospc['block_bytes'] > 512 << 10):
                    raise ValueError('actual preparation ENOSPC lacks public cancellation and original-data preservation')
                result['preparation_enospc_sha256'] = reference.sha(enospc_file)
                refusal_file = output / 'epoch-preparation-accepted-refusals.json'
                refusal = json.loads(refusal_file.read_text())
                if (any(refusal.get(key) is not True for key in
                        ['passed', 'inspection_refused', 'cancellation_refused', 'accepted_metadata_unchanged']) or
                        refusal.get('request') != finished['report']['request'] or
                        refusal.get('acceptance_sha256') != finished['report']['acceptance_sha256']):
                    raise ValueError('accepted retirement lacks exact public cancellation refusals')
                result['accepted_cancellation_refusals_sha256'] = reference.sha(refusal_file)
            if any(finished.get(key) is not True for key in required) or finished.get('full_epoch_retirement_qualified') is not False:
                raise ValueError('retirement fence proof incomplete or overclaims full epoch admission')
            result['retirement_fence_sha256'] = reference.sha(finished_file)
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
            if loop_holder is not None:
                consumer_loop_holder.close(loop_holder, consumer_worker_loss.exact_worker, artifact)
                if not loop_holder['passed']:
                    result['passed'] = False
                if not loop_holder['cleanup']:
                    preserve = True
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
            if args.retirement_fence or args.epoch_retirement:
                result['inputs_unchanged'] = (result['inputs_unchanged'] and
                    reference.sha(artifact / 'consumer_loop_holder.py') == result['loop_holder_input_sha256'])
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
