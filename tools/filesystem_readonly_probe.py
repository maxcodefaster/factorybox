#!/usr/bin/env python3
"""Profile exact committed trees without changing their worker or capture."""
import argparse
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import tarfile
import uuid

import build_config
import build_worker
import native_base
import reference
import upstream_unit


def probe_inputs(status, sandbox, snapshot, record, filesystem):
    root = status['lifecycle']['root']
    if (not re.fullmatch(r'runtime-[a-f0-9]{32}', sandbox) or
            not re.fullmatch(r'[a-f0-9]{32}', snapshot) or
            record.get('id') != sandbox or record.get('phase') != 'paused' or
            record.get('pause', {}).get('snapshot_id') != snapshot or
            not record['pause'].get('completed_at')):
        raise ValueError('probe requires the exact committed paused capture')
    prefix = root + '/objects/' + sandbox + '/captures/' + snapshot + '/filesystem/'
    selected = []
    for tree in filesystem['roots']:
        if tree['name'] not in {'upper', 'volumes/docker'}:
            continue
        if (tree.get('immutable') is not None or not tree.get('sha256') or
                tree['saved'] != prefix + tree['name']):
            raise ValueError('probe tree is outside its original committed capture')
        selected.append({'path': '/proc/1/root' + tree['saved'], 'expected': tree})
    if len(selected) != 2:
        raise ValueError('probe requires both original writable capture roots')
    return selected


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--worker-build', required=True)
    parser.add_argument('--worker-container', required=True)
    parser.add_argument('--sandbox', required=True)
    parser.add_argument('--snapshot', required=True)
    parser.add_argument('--baseline-ref', default='HEAD')
    options = parser.parse_args()
    if not re.fullmatch(r'[a-f0-9]{64}', options.worker_container):
        raise ValueError('probe requires one exact existing worker container ID')
    _, worker = build_worker.load(options.worker_build)
    image, _ = build_config.local_pinned_image(worker['image'], worker['image_id'], reference.docker)
    host = json.loads(reference.docker(['info', '--format', '{{json .}}']))
    native_base.require_native_host(worker['platform'], host)
    container = json.loads(reference.docker(['inspect', options.worker_container]))[0]
    if (container['Id'] != options.worker_container or container['Image'] != worker['image_id'] or
            not container['State']['Running'] or container['RestartCount'] != 0):
        raise ValueError('probe worker is not the exact live verified image')
    status = json.loads(reference.docker(['exec', options.worker_container,
                                         '/opt/factorybox/factorybox', 'worker-status']))
    request = {'root': status['lifecycle']['root'], 'sandbox': options.sandbox,
               'snapshot': options.snapshot}
    # Validate identifiers before using them as private relative path segments.
    if (not re.fullmatch(r'runtime-[a-f0-9]{32}', options.sandbox) or
            not re.fullmatch(r'[a-f0-9]{32}', options.snapshot)):
        raise ValueError('probe capture identifiers are invalid')
    program = """import json,pathlib,sys
r=json.loads(sys.argv[1]);root=pathlib.Path(r['root'])/'objects'/r['sandbox']
print(json.dumps({'record':json.loads((root/'record.json').read_text()),
 'filesystem':json.loads((root/'captures'/r['snapshot']/'filesystem.json').read_text())}))
"""
    observed = json.loads(reference.docker(['exec', options.worker_container, 'python3', '-c',
                                           program, json.dumps(request)]))
    inputs = probe_inputs(status, options.sandbox, options.snapshot,
                          observed['record'], observed['filesystem'])
    owner = uuid.uuid4().hex
    name = 'factorybox-filesystem-readonly-' + owner
    evidence = reference.new_artifact(name)
    evidence.mkdir()
    result = {'schema': 1, 'owner': owner, 'passed': False, 'cleanup': False,
              'scope': __doc__, 'worker_container': options.worker_container,
              'worker_image': image, 'status': status, 'inputs': inputs,
              'snapshot_record': observed['record'], 'platform': worker['platform'],
              'native_lifecycle_qualified': False, 'production_qualified': False}
    (evidence / 'intent.json').write_text(json.dumps(result, indent=2))
    attempted = False
    try:
        baseline = subprocess.check_output(['git', 'rev-parse', options.baseline_ref + '^{commit}'],
                                           cwd=reference.ROOT, text=True).strip()
        result['baseline_commit'] = baseline
        archive = evidence / 'baseline.tar'
        subprocess.run(['git', 'archive', '--format=tar', '--output=' + str(archive), baseline,
                        'internal', 'cmd', 'go.mod', 'go.sum'], cwd=reference.ROOT, check=True)
        baseline_root = evidence / 'context-baseline'
        baseline_root.mkdir()
        with tarfile.open(archive) as source:
            source.extractall(baseline_root, filter='data')
        current_root = evidence / 'context-current'
        for directory in ['internal', 'cmd']:
            shutil.copytree(reference.ROOT / directory, current_root / directory)
        for file in ['go.mod', 'go.sum']:
            shutil.copyfile(reference.ROOT / file, current_root / file)
        fixture = 'internal/sysboxcapture/filesystem_probe_linux_test.go'
        shutil.copyfile(reference.ROOT / fixture, baseline_root / fixture)
        binaries = evidence / 'binaries'
        binaries.mkdir()
        result['contexts'] = {}
        env = dict(os.environ, GOOS='linux', GOARCH=build_config.architecture(worker['platform']),
                   CGO_ENABLED='0')
        for label, source in [('baseline', baseline_root), ('current', current_root)]:
            result['contexts'][label] = upstream_unit.context_manifest(source)
            subprocess.run(['go', 'test', '-c', '-trimpath', '-buildvcs=false',
                            '-o', str(binaries / (label + '.test')), './internal/sysboxcapture'],
                           cwd=source, env=env, check=True)
        result['binaries_sha256'] = {p.name: reference.sha(p) for p in binaries.iterdir()}
        output = evidence / 'output'
        output.mkdir()
        for file in ['digest.jsonl', 'baseline.cpu.pprof', 'current.cpu.pprof']:
            (output / file).touch(mode=0o600)
        order = ['baseline', 'current', 'current', 'baseline']
        script = ''
        for index, label in enumerate(order):
            profile = f' -test.cpuprofile=/output/{label}.cpu.pprof' if index < 2 else ''
            script += (f'FACTORYBOX_PROBE_CONFIGURATION={label} /binaries/{label}.test '
                       "-test.run='^$' -test.bench='^BenchmarkFilesystemExistingDigestNative$' "
                       f'-test.benchtime=1x{profile}\n')
        command = ['run', '--pull=never', '--name', name, '--label', 'com.factorybox.run=' + owner,
                   '--pid=container:' + options.worker_container, '--cpus=3', '--memory=3g',
                   '--pids-limit=128', '--network=none', '--read-only', '--cap-drop=ALL',
                   '--cap-add=SYS_PTRACE', '--cap-add=DAC_READ_SEARCH', '--cap-add=FOWNER',
                   '--cap-add=SYS_ADMIN',
                   '--tmpfs', '/tmp:rw,nosuid,nodev,size=64m',
                   '--mount', f'type=bind,source={binaries},target=/binaries,readonly',
                   '--mount', f'type=bind,source={output},target=/output',
                   '--env', 'GOMAXPROCS=2', '--env', 'GOMEMLIMIT=256MiB',
                   '--env', 'FACTORYBOX_NATIVE_FILESYSTEM_READONLY_PROBE=1',
                   '--env', 'FACTORYBOX_FILESYSTEM_READONLY_INPUTS=' + json.dumps(inputs),
                   '--entrypoint', '/bin/sh', image, '-ec', script]
        result['command'] = command
        attempted = True
        (evidence / 'intent.json').write_text(json.dumps(result, indent=2))
        completed = subprocess.run([*reference.DOCKER, *command], capture_output=True,
                                   text=True, timeout=420)
        (evidence / 'stdout.log').write_text(completed.stdout)
        (evidence / 'stderr.log').write_text(completed.stderr)
        completed.check_returncode()
        result['measurements'] = [json.loads(line) for line in (output / 'digest.jsonl').read_text().splitlines()]
        if len(result['measurements']) != 12:
            raise ValueError('read-only comparison omitted a complete verified root')
        after = json.loads(reference.docker(['inspect', options.worker_container]))[0]
        latest = json.loads(reference.docker(['exec', options.worker_container,
                                             '/opt/factorybox/factorybox', 'worker-status']))
        if (after['Id'] != container['Id'] or after['State']['Pid'] != container['State']['Pid'] or
                not after['State']['Running'] or after['RestartCount'] != 0 or
                latest['epoch'] != status['epoch'] or latest['store_id'] != status['store_id']):
            raise ValueError('read-only probe worker identity changed')
        for label, source in [('baseline', baseline_root), ('current', current_root)]:
            if upstream_unit.context_manifest(source) != result['contexts'][label]:
                raise ValueError('probe compilation context changed')
        result['passed'] = True
    except Exception as error:
        result['error'] = str(error)
        raise
    finally:
        if attempted:
            reference.owned_delete('container', name, owner)
        result['cleanup'] = True
        (evidence / 'result.json').write_text(json.dumps(result, indent=2))
        print(json.dumps({'evidence': str(evidence), 'passed': result['passed'],
                          'cleanup': result['cleanup'], 'error': result.get('error')}), flush=True)


if __name__ == '__main__':
    main()
