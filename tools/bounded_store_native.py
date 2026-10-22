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
    parser.add_argument('--frozen-core', action='store_true', help='Compile current test files against the selected worker build\'s exact frozen non-test sources; retain the complete original test tree and overlay current test files only')
    parser.add_argument('--suite', choices=['capacity', 'retirement', 'retirement-base-diagnostic', 'timer-restart-prototype', 'timer-state-reader', 'timer-exit-guard', 'timer-signal', 'timer-remainder', 'timer-remainder-protection', 'timer-kernel-activation-prototype', 'restore-hold'], default='capacity')
    parser.add_argument('--timer-probe', action='store_true', help='Read one frozen owned task restart block through an unpinned temporary BPF iterator; diagnostic only')
    parser.add_argument('--timer-observe-exit', action='store_true', help='Prototype only: seize without interrupt and record natural signal/exit trace events')
    args = parser.parse_args()
    if args.timer_probe and args.suite == 'capacity':
        parser.error('--timer-probe requires a retirement suite')
    prototype = args.suite == 'timer-restart-prototype'
    reader_only = args.suite == 'timer-state-reader'
    exit_guard = args.suite == 'timer-exit-guard'
    signal_boundary = args.suite == 'timer-signal'
    remainder = args.suite == 'timer-remainder'
    kernel_prototype = args.suite == 'timer-kernel-activation-prototype'
    protection = args.suite == 'timer-remainder-protection' or kernel_prototype
    restore_hold = args.suite == 'restore-hold'
    if args.timer_observe_exit and not prototype:
        parser.error('--timer-observe-exit requires timer-restart-prototype')
    if args.suite == 'retirement' or prototype or reader_only:
        args.timer_probe = True  # The complete required suite includes the timer regression.
    worker_path, worker = build_worker.load(args.worker_build)
    native_base.require_native_host('linux/amd64', json.loads(reference.docker(['info', '--format', '{{json .}}'])))
    if reference.docker(['ps', '--filter', 'label=com.factorybox.run', '--format', '{{.ID}}']):
        raise ValueError('other Factorybox runs must finish before this serialized test')
    owner = uuid.uuid4().hex
    retirement = args.suite.startswith('retirement') or prototype or reader_only or exit_guard or signal_boundary or remainder or protection or restore_hold
    name = ('factorybox-checkpoint-retirement-' if retirement else 'factorybox-bounded-store-') + owner
    artifact = reference.new_artifact(name)
    artifact.mkdir()
    source = artifact / 'source'
    core_source = worker_path / 'source' if args.frozen_core else reference.ROOT
    for directory in ['internal', 'cmd/factorybox']:
        shutil.copytree(core_source / directory, source / directory)
    for file in ['go.mod', 'go.sum']:
        shutil.copyfile(core_source / file, source / file)
    if args.frozen_core:
        for directory in ['internal', 'cmd/factorybox']:
            for path in sorted((reference.ROOT / directory).rglob('*_test.go')):
                if path.is_symlink() or not path.is_file():
                    raise ValueError('current test overlay must be a regular source file')
                target = source / path.relative_to(reference.ROOT)
                target.parent.mkdir(parents=True, exist_ok=True)
                shutil.copyfile(path, target)
    shutil.copyfile(__file__, artifact / 'runner.py')
    setup = 'checkpoint-retirement-setup.sh' if retirement else 'bounded-store-setup.sh'
    shutil.copyfile(reference.ROOT / 'tests' / setup, artifact / 'setup.sh')
    if args.timer_probe:
        shutil.copyfile(reference.ROOT / 'tests/timer-probe/probe.py', artifact / 'timer-probe.py')
    prototype_inputs = {}
    if prototype:
        for source_name, target in [('prepare.py', 'timer-prepare.py'), ('restart.S', 'timer-restart.S')]:
            shutil.copyfile(reference.ROOT / 'tests/timer-probe' / source_name, artifact / target)
            prototype_inputs[target] = reference.sha(artifact / target)
    if kernel_prototype:
        shutil.copyfile(reference.ROOT / 'tests/timer-probe/restart.S', artifact / 'timer-kernel.S')
        prototype_inputs['timer-kernel.S'] = reference.sha(artifact / 'timer-kernel.S')
    manifest = upstream_unit.context_manifest(source)
    core = lambda rows: {k: v for k, v in rows.items() if not k.endswith('_test.go')}
    if core(manifest) != core(worker['source_context']):
        # A separate read-only component may be tested against the old CLI
        # only while it is not linked by that CLI. This exception never applies
        # to lifecycle qualification or changed existing runtime source files.
        unlinked = lambda rows: {k: v for k, v in core(rows).items() if not k.startswith('internal/timerstate/')}
        if not reader_only or unlinked(manifest) != unlinked(worker['source_context']):
            raise ValueError('native core differs from delivered image')
        dependencies = subprocess.check_output(['go', 'list', '-mod=readonly', '-deps', './cmd/factorybox'], cwd=source, text=True).splitlines()
        if 'factorybox/internal/timerstate' in dependencies:
            raise ValueError('linked timer reader requires a new delivered worker build')
    busybox = json.loads((reference.ROOT / 'upstream/test-inputs.json').read_text())['busybox-amd64']
    rootfs = reference.ROOT / '.cache/downloads' / busybox['archive']
    if reference.sha(rootfs) != busybox['sha256']:
        raise ValueError('native rootfs changed')
    output = artifact / 'output'
    output.mkdir()
    result = {'schema': 1, 'owner': owner, 'passed': False, 'cleanup': False, 'production_qualified': False,
              'worker_build': args.worker_build, 'worker_image_id': worker['image_id'], 'source_context': manifest, 'suite': args.suite,
              'current_test_overlay_on_frozen_core': args.frozen_core,
              'runner_sha256': reference.sha(artifact / 'runner.py'), 'setup_sha256': reference.sha(artifact / 'setup.sh'),
              'rootfs_sha256': busybox['sha256'], 'global_sysctls_before': consumer_worker_loss.sysctls(),
              'scope': 'Exact delivered bounded worker, 512-MiB fixed filesystem, full RAM/filesystem checkpoint/fork, '
                       'pause/resume, actual guest ENOSPC, distinct host/internal admission, external writable bind '
                       'refusal and preserved prior checkpoint. Not worker replacement, broad consumer or production qualification.'}
    if retirement:
        result['scope'] = 'Delivered CLI recovery selections, released selection replay, committed-pause protection, live independent fork/resume after checkpoint retirement, foreign mount/successor refusal, actual SIGKILL during partial deletion, retry with unknown entry refusal, verified immutable base alias cleanup and a required read-only-request/NULL-remainder relative wait through source/fork with preserved syscall registers and original deadline. Not epoch retirement or production qualification.'
    if args.suite == 'retirement-base-diagnostic':
        result['scope'] = 'Isolated retained-sleep and immutable-alias diagnostic only; does not qualify the full required retirement suite.'
    if args.timer_probe:
        result['timer_probe_sha256'] = reference.sha(artifact / 'timer-probe.py')
    if prototype:
        result['scope'] = 'Experimental single-thread, native AMD64, zero time namespace offset timer continuation before capture. Exact original deadline, read-only request, NULL remainder and full syscall register checks in source and fork. Delivered runtime and original required tests unchanged; not production qualification.'
        result['prototype_inputs'] = prototype_inputs
        result['natural_exit_observer'] = args.timer_observe_exit
    if reader_only:
        result['scope'] = 'Read-only Go kernel timer component only, including bulk owned PIDs/non-leader TIDs, exact birth, namespace filtering, independent Python comparison and release of all temporary BPF objects. Existing delivered CLI core unchanged. Not timer integration or lifecycle qualification.'
        result['unlinked_component_only'] = 'internal/timerstate/'
    if exit_guard:
        result['scope'] = 'Production timer exit guard against a controlled test-only tracer, including real normal/SIGKILL exit while the exact original source cgroup is thawed. Confirm source hold before kernel auto-detach. Not actual partial timer mutation rollback, controller-loss or production qualification.'
    if signal_boundary:
        result['scope'] = 'Direct syscall signal controls and actual SIGUSR1 queued after completed Go timer preparation while the exact source is frozen and its capture worker stopped before CRIU starts. Original wait must return raw EINTR after resume with one handler and unchanged registers/request. No production qualification.'
    if remainder:
        result['scope'] = 'Real one-second signal handler, uncaptured native control and delivered prepared wait. Original non-NULL remainder must describe interruption time before the handler, with raw EINTR and the original register/flags/stack/read-only request checks. Not production qualification.'
    if protection:
        result['scope'] = 'Real caught signal handler protects its dedicated remainder page after a one-second delay. Native control and prepared wait must both retain raw EINTR and all original ABI checks, with the kernel-compatible copy ordered before handler protection. Not production qualification.'
    if kernel_prototype:
        result['scope'] = 'Test-only single-thread native kernel-wait activation after actual delivered source checkpoint, fork and resume, including a signal queued while frozen after syscall-entry activation, with the production independent tracer-exit guard. Original interruption-time deadline, handler-protection/EINTR/ABI assertions retained. Restores already exposed their ppoll continuation before test activation. Current CLI unchanged; no production pre-exposure integration, multi-thread, failure repair or production qualification.'
        result['prototype_inputs'] = prototype_inputs
    if restore_hold:
        result['scope'] = 'Pinned Sysbox private sealed restore/resume pre-detach hold, exact persisted cgroup and application counter through ACK, CRIU detach and explicit thaw. Actual socket disconnect and foreign ACK refuse. Hold protocol is opt-in and production lifecycle activation remains disabled; not production qualification.'
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
        if retirement:
            command += ['--env', 'FACTORYBOX_NATIVE_RETIREMENT=1', '--env', 'FACTORYBOX_NATIVE_BOUNDED_LOSS=1']
        if args.suite == 'retirement-base-diagnostic':
            pattern = '^TestNativeCheckpointRetirementImmutableAlias$'
            if args.timer_probe:
                pattern += '|^TestNativeCheckpointRetirementTimerState$'
            command += ['--env', 'FACTORYBOX_RETIREMENT_TEST=' + pattern]
        if prototype:
            command += ['--env', 'FACTORYBOX_TIMER_RESTART_PROTOTYPE=1',
                        '--env', 'FACTORYBOX_RETIREMENT_TEST=^TestNativeTimerRestartPrototype$']
            if args.timer_observe_exit:
                command += ['--env', 'FACTORYBOX_TIMER_OBSERVE_EXIT=1']
        if reader_only:
            command += ['--env', 'FACTORYBOX_NATIVE_TIMER_READER=1', '--env', 'FACTORYBOX_RETIREMENT_TEST=^TestNativeTimerReader$']
        if exit_guard:
            command += ['--env', 'FACTORYBOX_NATIVE_TIMER_EXIT_GUARD=1', '--env', 'FACTORYBOX_RETIREMENT_TEST=^TestNativeTimerExitGuard$']
        if signal_boundary:
            command += ['--env', 'FACTORYBOX_NATIVE_TIMER_SIGNAL=1', '--env', 'FACTORYBOX_RETIREMENT_TEST=^TestNativeTimerSignalBoundary$']
        if remainder:
            command += ['--env', 'FACTORYBOX_NATIVE_TIMER_REMAINDER=1', '--env', 'FACTORYBOX_RETIREMENT_TEST=^TestNativeTimerRemainder$']
        if protection:
            command += ['--env', 'FACTORYBOX_NATIVE_TIMER_REMAINDER_PROTECTION=1', '--env', 'FACTORYBOX_RETIREMENT_TEST=^TestNativeTimerRemainderProtection$']
        if kernel_prototype:
            command += ['--env', 'FACTORYBOX_TIMER_KERNEL_PROTOTYPE=1']
        if restore_hold:
            command += ['--env', 'FACTORYBOX_NATIVE_RESTORE_HOLD=1', '--env', 'FACTORYBOX_RETIREMENT_TEST=^TestNativeRestoreHold$']
        for path, target, readonly in [(binary, '/probe.test', True), (rootfs, '/busybox-rootfs.tar.gz', True),
                                       (artifact / 'setup.sh', '/setup.sh', True), (output, '/output', False)]:
            command += ['--mount', f'type=bind,source={path},target={target}' + (',readonly' if readonly else '')]
        if args.timer_probe:
            command += ['--mount', f'type=bind,source={artifact / "timer-probe.py"},target=/timer-probe.py,readonly']
        for target in prototype_inputs:
            command += ['--mount', f'type=bind,source={artifact / target},target=/{target},readonly']
        capacity = 1 << 30 if retirement else 512 << 20
        command += [worker['image'], '--store-id', owner, '--bounded-root', '/var/lib/sysbox/fixed-store', '--bounded-bytes', str(capacity)]
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
        prefix = 'TestNativeRestoreHold' if restore_hold else 'TestNativeTimerRemainderProtection' if protection else 'TestNativeTimerRemainder' if remainder else 'TestNativeTimerSignalBoundary' if signal_boundary else 'TestNativeTimerExitGuard' if exit_guard else 'TestNativeTimerReader' if reader_only else 'TestNativeTimerRestartPrototype' if prototype else 'TestNativeCheckpointRetirement' if retirement else 'TestNativeBoundedWorkerStore'
        count = 1 if prototype or reader_only or exit_guard or signal_boundary or remainder or protection or restore_hold else 4 if args.suite == 'retirement' else 2 if args.timer_probe else 1
        result['passed_tests'] = sum(line.startswith('--- PASS: ' + prefix) for line in log.splitlines())
        if done.returncode or not state['Running'] or state['OOMKilled'] or result['passed_tests'] != count or '--- SKIP:' in log:
            raise ValueError('native bounded worker assertions failed or skipped')
        if args.suite == 'retirement':
            required = {
                'retirement': ['unresumed_pause_protected', 'selection_protected', 'explicit_release_replayed', 'released_selection_not_reactivated',
                               'foreign_mount_preserved', 'source_resume_preserved', 'independent_fork_preserved', 'new_fork_refused',
                               'exact_completion_replayed', 'foreign_successor_preserved', 'partial_retry_preserved_foreign',
                               'partial_retry_completed', 'descendant_useful_after_source_delete'],
                'retirement-base': ['exact_alias_retired', 'shared_base_preserved', 'source_running'],
                'retirement-base-actor': ['exact_alias_retired', 'shared_base_preserved', 'source_running'],
                'timer-state': ['read_only_request', 'null_remainder', 'syscall_registers_preserved', 'source_and_fork_original_deadline'],
            }
            result['proof_sha256'] = {}
            for proof_name, keys in required.items():
                proof_path = output / (proof_name + '.json')
                proof = json.loads(proof_path.read_text())
                if proof.get('passed') is not True or any(proof.get(key) is not True for key in keys):
                    raise ValueError('native retirement proof incomplete: ' + proof_name)
                if proof_name == 'retirement' and (proof.get('actual_controller_sigkill', 0) <= 1 or
                        proof['first']['after']['filesystem_available_bytes'] <= proof['first']['before']['filesystem_available_bytes']):
                    raise ValueError('native retirement lacks actual interruption and observed capacity recovery')
                result['proof_sha256'][proof_name] = reference.sha(proof_path)
        elif args.suite == 'capacity':
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
        elif reader_only:
            proof_path = output / 'timer-reader.json'
            proof = json.loads(proof_path.read_text())
            if any(proof.get(key) is not True for key in ['passed', 'non_leader_threads', 'independent_python_agreement',
                                                        'wrong_birth_refused_and_released', 'duplicate_pid_refused',
                                                        'pid_namespace_separated', 'task_memory_unchanged', 'sources_running']):
                raise ValueError('read-only Go timer component proof incomplete')
            result['proof_sha256'] = reference.sha(proof_path)
        elif exit_guard:
            proof_path = output / 'timer-exit-guard.json'
            proof = json.loads(proof_path.read_text())
            if any(proof.get(key) is not True for key in ['passed', 'normal_exit', 'actual_sigkill', 'held_before_auto_detach', 'no_preparation_claim']):
                raise ValueError('timer exit guard proof incomplete')
            result['proof_sha256'] = reference.sha(proof_path)
        elif signal_boundary:
            proof_path = output / 'timer-signal.json'
            proof = json.loads(proof_path.read_text())
            if any(proof.get(key) is not True for key in ['passed', 'uncaptured_control', 'active_wait_eintr', 'prepared_boundary_eintr', 'recaptured_boundary_eintr']):
                raise ValueError('timer signal boundary proof incomplete')
            result['proof_sha256'] = reference.sha(proof_path)
        elif remainder:
            proof_path = output / 'timer-remainder.json'
            proof = json.loads(proof_path.read_text())
            if any(proof.get(key) is not True for key in ['passed', 'uncaptured_control', 'interruption_time_remainder', 'raw_eintr_and_abi']):
                raise ValueError('timer interruption-time remainder proof incomplete')
            result['proof_sha256'] = reference.sha(proof_path)
        elif restore_hold:
            proof_path = output / 'restore-hold.json'
            proof = json.loads(proof_path.read_text())
            cases = proof.get('cases', {})
            if proof.get('passed') is not True or set(cases) != {'fork', 'resumed', 'fork-disconnect', 'resumed-disconnect', 'wrong-ack'}:
                raise ValueError('native restore hold cases incomplete')
            for mode, case in cases.items():
                keys = ['held_before_ack', 'application_counter_unchanged']
                keys += ['held_after_criu_detach', 'explicit_thaw_runs_application'] if mode in {'fork', 'resumed'} else ['failed_closed']
                if any(case.get(key) is not True for key in keys):
                    raise ValueError('native restore hold proof incomplete: ' + mode)
            result['proof_sha256'] = reference.sha(proof_path)
        elif protection:
            proof_path = output / 'timer-remainder-protection.json'
            proof = json.loads(proof_path.read_text())
            if any(proof.get(key) is not True for key in ['passed', 'uncaptured_control', 'copy_before_handler_protection', 'raw_eintr_and_abi']):
                raise ValueError('timer remainder copy-order proof incomplete')
            result['proof_sha256'] = reference.sha(proof_path)
            if kernel_prototype:
                activation = output / 'kernel-activation-prototype.json'
                activated = json.loads(activation.read_text())
                cases = activated.get('cases', {})
                if (activated.get('passed') is not True or activated.get('native_frozen_remainder_control') is not True or
                        set(cases) != {'prepared', 'fork', 'resumed', 'queued'} or
                        set(proof.get('cases', {})) != {'uncaptured', 'frozen-control', *cases}):
                    raise ValueError('native kernel activation experiment incomplete')
                for mode, case in cases.items():
                    if (case.get('passed') is not True or case.get('syscall_entry_atomic_mask') is not True or
                            case.get('kernel_timer_armed') is not True or
                            case.get('source_held') is not True or case.get('native_syscall') != 230 or
                            case.get('code_sha256') != '02f23238267d39195a106632b38e3b1c4ba58fa6d7d95adeae531e5ad553e15f' or
                            mode == 'queued' and (case.get('queued_before_thaw') is not True or
                                                  case.get('queued_caught_signal') is not True)):
                        raise ValueError('native kernel activation case incomplete: ' + mode)
                result['activation_proof_sha256'] = reference.sha(activation)
        elif not prototype:
            result['scope'] = 'Isolated immutable-alias retirement diagnostic only; full required retirement suite is not qualified.'
        else:
            proof_path = output / 'timer-state.json'
            proof = json.loads(proof_path.read_text())
            if any(proof.get(key) is not True for key in ['passed', 'read_only_request', 'null_remainder',
                                                        'syscall_registers_preserved', 'source_and_fork_original_deadline']):
                raise ValueError('experimental timer continuation proof incomplete')
            result['proof_sha256'] = reference.sha(proof_path)
            prototype_proof_path = output / 'timer-prototype.json'
            prototype_proof = json.loads(prototype_proof_path.read_text())
            if any(prototype_proof.get(key) is not True for key in ['passed', 'second_checkpoint_delivered_cli',
                                                                  'restored_child_pause_resume', 'guest_root_pinned']):
                raise ValueError('experimental repeated capture/pause/resume proof incomplete')
            if prototype_proof['natural_exit_observer'] != args.timer_observe_exit or prototype_proof['paused_snapshot_resident_bytes'] != 0:
                raise ValueError('experimental timer observation or pause RAM differs')
            result['prototype_proof_sha256'] = reference.sha(prototype_proof_path)
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
            if args.timer_probe:
                result['inputs_unchanged'] = result['inputs_unchanged'] and reference.sha(artifact / 'timer-probe.py') == result['timer_probe_sha256']
            if prototype or kernel_prototype:
                result['inputs_unchanged'] = result['inputs_unchanged'] and all(reference.sha(artifact / name) == sha for name, sha in prototype_inputs.items())
            result['passed'] = (result['passed'] and result['cleanup'] and result['inputs_unchanged'] and
                                result['global_sysctls_after'] == result['global_sysctls_before'])
            result['outer_seconds'] = time.monotonic()-started
            reference_lifecycle.write_json(artifact / 'result.json', result)
            print(json.dumps({'evidence': str(artifact), 'passed': result['passed'], 'cleanup': result['cleanup'],
                              'error': result.get('error')}), flush=True)
            reference.artifact_storage.compact_closed_run(artifact, reference.ROOT)
    if not result['passed']:
        raise SystemExit(1)


if __name__ == '__main__':
    main()
