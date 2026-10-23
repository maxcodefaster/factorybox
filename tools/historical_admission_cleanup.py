#!/usr/bin/env python3
"""Replay the exact original failed admission binary, stopping at first failure."""
import json
import argparse
import importlib.util
from collections import Counter
import os
from pathlib import Path
import shutil
import re
import subprocess
import time
import uuid

from consumer_worker_loss import sysctls
import lifecycle_native
import native_base
import reference
import reference_lifecycle
import upstream_unit

ORIGINAL = 'factorybox-runtime-8829166825b0476888776bdfd2c7d5ae'
PATTERN = '^Test((Filesystem|Descriptor|ProcDirectory|DetachedFile|SocketQueue)Admission|CaptureSocketDescriptor)Native$'
COUNT = 3

def expected_outcomes(original_log, count):
    outcomes = re.findall(r'^\s*--- (?:PASS|FAIL): (\S+) \(',original_log,re.M)
    main = {'TestDescriptorAdmissionNative','TestDetachedFileAdmissionNative','TestFilesystemAdmissionNative',
            'TestProcDirectoryAdmissionNative','TestCaptureSocketDescriptorNative','TestSocketQueueAdmissionNative'}
    top = [name for name in outcomes if '/' not in name]
    if len(top) != 17 or len(outcomes)-len(top) != 78 or len(set(outcomes)) != len(outcomes) or not main.issubset(top):
        raise ValueError('original complete admission assertion inventory differs')
    return Counter({name:count if name.split('/')[0] in main else 1 for name in outcomes})

def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--tracer-build',help='exact successful owned-PID-namespace observer build')
    args=parser.parse_args()
    root = reference.ROOT
    source = root/'artifacts'/ORIGINAL
    before = reference.sha(source/'result.json')
    original = json.loads((source/'result.json').read_text())
    expected = expected_outcomes((source/'stdout.txt').read_text(),COUNT)
    if original['passed'] is not False or not all(original[k] for k in ['cleanup','inputs_unchanged','source_context_unchanged','components_unchanged']):
        raise ValueError('requires the exact retained failed suite with intact inputs')
    if upstream_unit.context_manifest(source/'context') != original['source_context']:
        raise ValueError('original frozen source changed')
    for name,digest in original['inputs_sha256'].items():
        path = source/name
        if name == 'lifecycle.test' and not path.exists():
            receipt = json.loads((source/(name+'.encoding.json')).read_text())
            if receipt['original']['sha256'] != digest: raise ValueError('original encoding identity differs')
            reference.artifact_storage.verify_compacted(path,receipt['original'])
        elif reference.sha(path) != digest: raise ValueError('original input differs: '+name)
    for name,digest in original['component_sha256'].items():
        if reference.sha(source/'components'/name) != digest: raise ValueError('original component changed')
    host = json.loads(reference.docker(['info','--format','{{json .}}']))
    native_base.require_native_host('linux/amd64',host)
    if original['host'] != {k:host[k] for k in original['host']}: raise ValueError('historical host differs')
    image,_ = native_base.load(original['native_base_build'],candidate_platform='linux/amd64')
    if reference.docker(['ps','--filter','label=com.factorybox.run','--format','{{.ID}}']): raise ValueError('other native runs must finish')
    owner = uuid.uuid4().hex
    name = 'factorybox-historical-admission-'+owner
    artifact = reference.new_artifact(name);artifact.mkdir();(artifact/'output').mkdir()
    shutil.copyfile(__file__,artifact/'runner.py')
    wrapper = ('#!/bin/bash\nset -euo pipefail\n'
               'test "$#" = 4\ntest "$1" = -test.v\ntest "$2" = -test.timeout=360s\n'
               'test "$3" = -test.run\ntest "$4" = '+"'"+PATTERN+"'\n"+
               'exec /original.test -test.v -test.timeout=360s -test.count=3 -test.failfast -test.run '+"'"+PATTERN+"'\n")
    trace = None
    trace_inputs = {}
    if args.tracer_build:
        if not re.fullmatch(r'factorybox-mount-ref-tracer-[0-9a-f]{32}',args.tracer_build): raise ValueError('requires an exact observer artifact')
        trace=root/'artifacts'/args.tracer_build
        if trace.resolve()!=trace or trace.is_symlink(): raise ValueError('observer cannot be redirected')
        built=json.loads((trace/'result.json').read_text())
        if built.get('passed') is not True or set(built['binary_sha256'])!={'trace.bpf.o','mount-ref-trace'}: raise ValueError('observer build incomplete')
        trace_inputs={str(trace/n):h for n,h in built['binary_sha256'].items()}
        if any(reference.sha(Path(n))!=h for n,h in trace_inputs.items()): raise ValueError('observer inputs changed')
        shutil.copyfile(root/'tools/mount_ref_events.py',artifact/'mount_ref_events.py')
        trace_inputs[str(artifact/'mount_ref_events.py')]=reference.sha(artifact/'mount_ref_events.py')
        shutil.copyfile('/sys/kernel/btf/vmlinux',artifact/'kernel-btf.original')
        startup='''/trace --object /trace.o --output /output/mount-refs.bin --ready /output/mount-trace-ready.json --result /output/mount-trace-result.json > /output/mount-trace.stdout 2> /output/mount-trace.stderr &
tracer=$!
trap 'kill -TERM "$tracer" 2>/dev/null || true; wait "$tracer" 2>/dev/null || true' EXIT
for attempt in $(seq 1 150); do
  test ! -s /output/mount-trace-ready.json || break
  kill -0 "$tracer"
  sleep 0.02
done
test -s /output/mount-trace-ready.json
set +e
'''
        completion='''status=$?
printf '%s\\n' "$status" >/output/native-test-exit.txt
# Passive observation after every original assertion, including a failed
# immediate unmount. No retry or delay is inserted before that assertion.
sleep 0.1
kill -TERM "$tracer"
wait "$tracer"
trace_status=$?
printf '%s\\n' "$trace_status" >/output/mount-trace-exit.txt
trap - EXIT
chown --reference=/output -- /output/mount-refs.bin
test "$status" = 0 || exit "$status"
exit "$trace_status"
'''
        before_wrapper=wrapper
        lines=wrapper.splitlines(True)
        wrapper=''.join(lines[:-1])+startup+lines[-1].removeprefix('exec ')+completion
        (artifact/'invocation-before-trace.sh').write_text(before_wrapper)
    (artifact/'invocation.sh').write_text(wrapper);(artifact/'invocation.sh').chmod(0o555)
    shutil.copyfile(source/'capture-archive.sh',artifact/'capture-archive.sh')
    result = {'schema':1,'passed':False,'cleanup':False,'owner':owner,'original':ORIGINAL,
              'original_result_sha256':before,'predetermined_full_suite_repetitions':COUNT,
              'diagnostic_only':True,'historical_failure_explained':False,'production_qualified':False,
              'scope':'Exact original test/CLI/components with three full admission repetitions and first-failure stop. Original immediate unmount assertions and resources retained. No inferred repair.',
              'invocation_sha256':reference.sha(artifact/'invocation.sh'),'runner_sha256':reference.sha(artifact/'runner.py'),
              'global_sysctls_before':sysctls()}
    result['expected_pass_outcomes'] = dict(expected)
    if trace:
        result.update(tracer_build=args.tracer_build,tracer_inputs_sha256=trace_inputs,
                      tracer_build_result_sha256=reference.sha(trace/'result.json'),kernel_btf_sha256=reference.sha(artifact/'kernel-btf.original'))
    attempted = volume = False
    failure = None
    started = time.monotonic()
    try:
        with (artifact/'original.test').open('xb') as f:
            subprocess.run(['zstd','-q','-dc','--',str(source/'lifecycle.test.zst')],stdout=f,check=True,timeout=30)
        if reference.sha(artifact/'original.test') != original['inputs_sha256']['lifecycle.test']: raise ValueError('reconstructed executable differs')
        (artifact/'original.test').chmod(0o555)
        command = original['command'][:]
        # Every non-mount environment, capability and resource option stays exact.
        for index,arg in enumerate(command):
            if arg == '--name': command[index+1] = name
            if arg == '--label':
                if command[index+1] != 'com.factorybox.run='+original['owner']: raise ValueError('unexpected old label')
                command[index+1] = 'com.factorybox.run='+owner
            if arg == '--mount':
                value = command[index+1]
                if value.startswith('type=volume,source='+ORIGINAL+','): value = value.replace('source='+ORIGINAL+',','source='+name+',',1)
                elif 'target=/output' in value: value = 'type=bind,source='+str(artifact/'output')+',target=/output'
                elif 'target=/lifecycle.test,' in value: value = 'type=bind,source='+str(artifact/'invocation.sh')+',target=/lifecycle.test,readonly'
                command[index+1] = value
        command[command.index('--entrypoint'):command.index('--entrypoint')] = ['--mount','type=bind,source='+str(artifact/'original.test')+',target=/original.test,readonly']
        if trace:
            index=command.index('--entrypoint')
            command[index:index]=['--mount',f'type=bind,source={trace/"mount-ref-trace"},target=/trace,readonly',
                                  '--mount',f'type=bind,source={trace/"trace.bpf.o"},target=/trace.o,readonly']
        result['command'] = command
        reference_lifecycle.write_json(artifact/'intent.json',result)
        volume = True;reference.docker(['volume','create','--label','com.factorybox.run='+owner,name])
        attempted = True
        print(json.dumps({'running':str(artifact)}),flush=True)
        with (artifact/'stdout.txt').open('x') as out,(artifact/'stderr.txt').open('x') as err:
            done = subprocess.run(command,stdout=out,stderr=err,timeout=420)
        state = json.loads(reference.docker(['container','inspect',name]))[0]['State']
        lines = (artifact/'stdout.txt').read_text().splitlines()
        top = [line for line in lines if line.startswith('--- PASS: ') or line.startswith('--- FAIL: ')]
        result.update(exit_code=done.returncode,container_state=state,
                      passed_tests=sum(line.startswith('--- PASS: ') for line in top),
                      passed_subtests=sum(line.lstrip().startswith('--- PASS: ') for line in lines)-sum(line.startswith('--- PASS: ') for line in lines),
                      failed_tests=[line for line in lines if line.lstrip().startswith('--- FAIL: ')],
                      skipped_tests=sum(line.lstrip().startswith('--- SKIP: ') for line in lines))
        observed = Counter(re.findall(r'^\s*--- PASS: (\S+) \(', '\n'.join(lines), re.M))
        result['observed_pass_outcomes'] = dict(observed)
        result['passed'] = done.returncode == 0 and not state['OOMKilled'] and observed == expected and result['skipped_tests'] == 0
        if trace:
            spec=importlib.util.spec_from_file_location('frozen_mount_ref_events',artifact/'mount_ref_events.py')
            decoder=importlib.util.module_from_spec(spec);spec.loader.exec_module(decoder)
            trace_result=json.loads((artifact/'output/mount-trace-result.json').read_text())
            result['trace_result']=trace_result
            result['native_exit_code']=int((artifact/'output/native-test-exit.txt').read_text())
            result['trace_summary']=decoder.summarize(decoder.decode((artifact/'output/mount-refs.bin').read_bytes()),
                                                    open_hook=trace_result.get('open_hook','security_file_open'))
            result['trace_events_sha256']=reference.sha(artifact/'output/mount-refs.bin')
            if (trace_result.get('passed') is not True or trace_result.get('kernel_probe_missed') != [0]*5 or
                    trace_result.get('kernel_error_counts') != [0]*3 or trace_result.get('kernel_btf_sha256')!=result['kernel_btf_sha256']):
                raise RuntimeError('observer incomplete; no ownership inference is qualified')
        if not result['passed']: raise RuntimeError('original full admission repetition failed; retain first failure')
    except Exception as error:
        result['error'] = str(error);failure = error
    finally:
        preserve = False
        try:
            if attempted:
                try: lifecycle_native.archive_capture(artifact,name,owner,result,image)
                except Exception as error:
                    preserve = True;result['archive_error'] = str(error);result['retained_source_volume'] = name;failure = failure or error
                reference.owned_delete('container',name,owner)
            if volume and not preserve: reference.owned_delete('volume',name,owner)
            result['cleanup'] = not preserve
            result['global_sysctls_after'] = sysctls()
            result['inputs_unchanged'] = reference.sha(source/'result.json') == before and upstream_unit.context_manifest(source/'context') == original['source_context'] and all(reference.sha(source/'components'/n)==h for n,h in original['component_sha256'].items()) and reference.sha(artifact/'original.test') == original['inputs_sha256']['lifecycle.test']
            if trace:
                result['inputs_unchanged'] = result['inputs_unchanged'] and all(reference.sha(Path(n))==h for n,h in trace_inputs.items())
            result['passed'] = bool(result['passed'] and result['cleanup'] and result['inputs_unchanged'] and result['global_sysctls_before']==result['global_sysctls_after'] and failure is None)
        except Exception as error: result['cleanup_error'] = str(error);result['passed'] = False;failure = failure or error
        result['outer_seconds'] = time.monotonic()-started
        reference_lifecycle.write_json(artifact/'result.json',result)
        print(json.dumps({'evidence':str(artifact),'passed':result['passed'],'error':result.get('error')}),flush=True)
        reference.artifact_storage.compact_closed_run(artifact,root)
    if failure or not result['passed']: raise RuntimeError('historical admission diagnostic failed: '+str(artifact)) from failure

if __name__ == '__main__': main()
