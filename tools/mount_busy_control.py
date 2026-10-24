#!/usr/bin/env python3
"""Qualify read-only busy-decision observation with owned native controls."""
import argparse
import json
from pathlib import Path
import re
import shutil
import subprocess
import traceback
import uuid

from consumer_worker_loss import sysctls
import mount_busy_events
import native_base
import reference
import upstream_bats


MODES = ['file', 'cwd', 'root', 'submount', 'peer']


def tracer_inputs(root, name):
    if not re.fullmatch(r'factorybox-mount-busy-tracer-[0-9a-f]{32}', name):
        raise ValueError('requires one exact completed busy-decision observer build')
    build = root / 'artifacts' / name
    built = json.loads((build / 'result.json').read_text())
    intent = json.loads((build / 'intent.json').read_text())
    if (not all(built.get(key) is True for key in ['passed', 'vet_passed', 'inputs_unchanged']) or
            set(built.get('binary_sha256', {})) != {'busy-mount-trace', 'trace.bpf.o'} or
            built.get('intent_sha256') != reference.sha(build / 'intent.json') or
            intent.get('event_bytes') != mount_busy_events.EVENT.size):
        raise ValueError('busy-decision build is incomplete or its intent changed')
    inputs = {str(build / name): digest for name, digest in built['binary_sha256'].items()}
    inputs.update(intent['source_sha256'])
    inputs.update({str(build / name): reference.sha(build / name) for name in ['result.json', 'intent.json']})
    if any(Path(path).resolve() != Path(path) or Path(path).is_symlink() or
           reference.sha(Path(path)) != digest for path, digest in inputs.items()):
        raise ValueError('busy-decision observer input changed or was redirected')
    return build, inputs


def check_observation(control, traced, ready, rows):
    if (not traced.get('passed') or traced.get('kernel_error_counts') != [0, 0, 0] or
            traced.get('kernel_probe_missed') != [0] * 6 or traced.get('event_bytes') != 112 or
            any(traced.get('active_maps', {}).get(name, 0) != 0 for name in ['unmounts', 'decisions', 'queries']) or
            traced.get('pid_namespace_inode') != control['pid_namespace'] or
            ready.get('pid_namespace_inode') != control['pid_namespace'] or
            traced.get('kernel_btf_sha256') != ready.get('kernel_btf_sha256') or
            traced.get('decision_hook') != 'propagate_mount_busy' or
            traced.get('count_hook') != 'mnt_get_count' or traced.get('events') != len(rows)):
        raise ValueError('observer did not complete an intact zero-loss native observation')
    summary = mount_busy_events.summarize(rows)
    coverage = []
    for case in control['cases']:
        calls = {}
        for phase, kernel_result in [('blocked', -16), ('success', 0)]:
            window = case[phase]
            found = [call for call in summary['unmounts']
                     if window['start_ns'] <= call['start_ns'] <= call['end_ns'] <= window['end_ns'] and
                     call['root_id'] == case['target_mount_id'] and call['local_pid'] == control['caller_pid']]
            if len(found) != 1 or found[0]['result'] != kernel_result or found[0]['flags'] != 0:
                raise ValueError('actual ordinary unmount missing or ambiguous: ' + case['mode'] + '/' + phase)
            call = found[0]
            if len(call['decisions']) != 1 or call['namespace'] != control['mount_namespace']:
                raise ValueError('requires the exact kernel busy decision in the owned mount namespace')
            decision = call['decisions'][0]
            if decision['refcnt'] != 2 or decision['result'] != (1 if phase == 'blocked' else 0):
                raise ValueError('actual busy decision or reference threshold differs')
            if any(item['namespace'] != control['mount_namespace'] or item['result'] < 0
                   for item in decision['counts']):
                raise ValueError('candidate count is not from the exact owned mount namespace')
            calls[phase] = call
        decision = calls['blocked']['decisions'][0]
        counts = decision['counts']
        root_counts = [item for item in counts if item['candidate'] == calls['blocked']['root'] and
                       item['candidate_id'] == case['target_mount_id']]
        if case['mode'] in ['file', 'cwd', 'root']:
            if len(root_counts) != 1 or root_counts[0]['result'] <= decision['refcnt']:
                raise ValueError('held file/cwd/root reference was not observed at its actual busy count')
        elif case['mode'] == 'submount':
            if not decision['root_has_children']:
                raise ValueError('busy root did not retain its controlled descendant mount')
        elif case['mode'] == 'peer':
            peers = [item for item in counts if item['candidate'] != calls['blocked']['root'] and
                     item['candidate_id'] == case['held_mount_id'] and item['result'] > 1]
            if len(root_counts) != 1 or root_counts[0]['result'] != 2 or len(peers) != 1:
                raise ValueError('distinct controlled propagation-peer reference was not observed')
        coverage.append({'mode': case['mode'], 'blocked': calls['blocked'], 'success': calls['success']})
    return summary, coverage


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--tracer-build', required=True)
    args = parser.parse_args()
    root = reference.ROOT
    trace, trace_inputs = tracer_inputs(root, args.tracer_build)
    host = json.loads(reference.docker(['info', '--format', '{{json .}}']))
    native_base.require_native_host('linux/amd64', host)
    if reference.docker(['ps', '--filter', 'label=com.factorybox.run', '--format', '{{.ID}}']):
        raise ValueError('other owned native runs must finish first')
    image, _ = native_base.load('factorybox-build-native-base-b8dc7327368e4b9bab47bca533070f4a',
                                candidate_platform='linux/amd64')
    owner = uuid.uuid4().hex
    name = 'factorybox-mount-busy-control-' + owner
    evidence = reference.new_artifact(name)
    evidence.mkdir()
    (evidence / 'output').mkdir()
    for source, destination in [(root / 'tools/diagnostics/mountbusy/control.py', 'control.py'),
                                (root / 'tools/diagnostics/mountbusy/control.sh', 'control.sh'),
                                (root / 'tools/mount_busy_events.py', 'decoder.py'), (Path(__file__), 'runner.py')]:
        shutil.copyfile(source, evidence / destination)
    inputs = dict(trace_inputs)
    inputs.update({str(evidence / name): reference.sha(evidence / name)
                   for name in ['control.py', 'control.sh', 'decoder.py', 'runner.py']})
    command = [*reference.DOCKER, 'run', '--pull=never', '--name', name,
               '--label', 'com.factorybox.run=' + owner, '--privileged', '--cgroupns=private',
               '--cpus=3', '--memory=3g', '--pids-limit=64', '--network=none',
               '--mount', f'type=volume,source={name},target=/work',
               '--mount', f'type=bind,source={evidence / "output"},target=/output']
    for source, target in [(trace / 'busy-mount-trace', '/trace'), (trace / 'trace.bpf.o', '/trace.o'),
                           (evidence / 'control.sh', '/control.sh'), (evidence / 'control.py', '/control.py')]:
        command += ['--mount', f'type=bind,source={source},target={target},readonly']
    command += ['--entrypoint', '/bin/bash', image, '/control.sh']
    result = {'schema': 1, 'passed': False, 'cleanup': False, 'diagnostic_only': True,
              'production_qualified': False, 'native_kernel_controls_passed': False,
              'observer_busy_coverage_passed': False, 'tracer_build': trace.name,
              'tracer_result_sha256': reference.sha(trace / 'result.json'), 'owner': owner,
              'command': command, 'inputs_sha256': inputs, 'global_sysctls_before': sysctls(),
              'host': {key: host[key] for key in ['OSType', 'Architecture', 'KernelVersion']},
              'scope': 'Five actual ordinary EBUSY/release/success native controls. '
                       'Observer qualification only; no historical attribution or runtime repair.'}
    (evidence / 'intent.json').write_text(json.dumps(result, indent=2) + '\n')
    attempted = volume = False
    try:
        reference.docker(['volume', 'create', '--label', 'com.factorybox.run=' + owner, name])
        volume = attempted = True
        with (evidence / 'stdout.txt').open('x') as out, (evidence / 'stderr.txt').open('x') as err:
            done = subprocess.run(command, stdout=out, stderr=err, timeout=60)
        result['exit_code'] = done.returncode
        assert done.returncode == 0
        control = json.loads((evidence / 'output/controls.json').read_text())
        assert control['passed'] and [row['mode'] for row in control['cases']] == MODES
        assert all(case['blocked']['result'] == -1 and case['blocked']['errno'] == 16 and
                   case['success']['result'] == 0 and case['success']['errno'] == 0 and
                   case['helper']['mount_id'] == case['held_mount_id'] and
                   case['helper']['mount_namespace'] == control['mount_namespace'] and
                   case['helper']['pid_namespace'] != control['pid_namespace'] and
                   case['helper']['inner_pid'] == 1 for case in control['cases'])
        result['control'] = control
        result['native_kernel_controls_passed'] = True
        traced = json.loads((evidence / 'output/trace-result.json').read_text())
        ready = json.loads((evidence / 'output/trace-ready.json').read_text())
        rows = mount_busy_events.decode((evidence / 'output/busy-events.bin').read_bytes())
        summary, coverage = check_observation(control, traced, ready, rows)
        (evidence / 'trace-summary.json').write_text(json.dumps(summary, indent=2) + '\n')
        result.update(passed=True, observer_busy_coverage_passed=True, coverage=coverage, trace_events=len(rows))
    except Exception as error:
        result['error'], result['traceback'] = repr(error), traceback.format_exc()
    finally:
        try:
            if attempted:
                state = json.loads(reference.docker(['container', 'inspect', name]))[0]
                assert state['Name'] == '/' + name and state['Config']['Labels']['com.factorybox.run'] == owner
                if state['State']['Running']:
                    reference.docker(['container', 'stop', '--time=2', name])
                    state = json.loads(reference.docker(['container', 'inspect', name]))[0]
                assert not state['State']['Running'] and state['State']['Pid'] == 0
                result['state'] = state['State']
                if not result['passed']:
                    upstream_bats.archive_stopped_volumes(evidence, name, owner, [name], image, result,
                                                         timeout=60, compressed=True)
                reference.owned_delete('container', name, owner)
            if volume:
                reference.owned_delete('volume', name, owner)
            result['cleanup'] = True
        except Exception as error:
            result['cleanup_error'] = repr(error)
        result['global_sysctls_after'] = sysctls()
        result['inputs_unchanged'] = all(reference.sha(Path(path)) == digest for path, digest in inputs.items())
        result['passed'] = (result['passed'] and result['cleanup'] and result['inputs_unchanged'] and
                            result['global_sysctls_before'] == result['global_sysctls_after'])
        (evidence / 'result.json').write_text(json.dumps(result, indent=2) + '\n')
        print(json.dumps({'evidence': str(evidence), 'passed': result['passed'], 'cleanup': result['cleanup'],
                          'native_kernel_controls_passed': result['native_kernel_controls_passed'],
                          'observer_busy_coverage_passed': result['observer_busy_coverage_passed'],
                          'error': result.get('error')}), flush=True)
    return 0 if result['passed'] else 1


if __name__ == '__main__':
    raise SystemExit(main())
