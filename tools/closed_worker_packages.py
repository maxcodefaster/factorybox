#!/usr/bin/env python3
"""Retain exact completed legacy worker transports in separately verified encodings."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import resource
import shutil
import stat
import subprocess
import uuid

import artifact_storage
import reference

PACKAGE = 'factorybox-worker-linux-amd64.tar'
BUILD = re.compile(r'factorybox-build-worker-[0-9a-f]{32}')
RETENTION = re.compile(r'factorybox-storage-worker-packages-[0-9a-f]{32}')
MAXIMUM_PACKAGE = 512 << 20


def sha(path):
    with Path(path).open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def canonical(path):
    path = Path(path).absolute()
    if path.resolve() != path or path.is_symlink() or not path.is_file():
        raise ValueError('retained package path is redirected or unavailable')
    return path


def save(path, value):
    with path.open('x') as output:
        json.dump(value, output, indent=2)
        output.write('\n')
        output.flush()
        os.fsync(output.fileno())
    sync(path.parent)


def sync(directory):
    descriptor = os.open(directory, os.O_RDONLY | os.O_DIRECTORY)
    try:
        os.fsync(descriptor)
    finally:
        os.close(descriptor)


def verify_encoding(payload, encoded, original):
    payload = canonical(payload)
    before = artifact_storage.identity(payload)
    if (type(original.get('bytes')) is not int or not 0 < original['bytes'] <= MAXIMUM_PACKAGE or
            payload.stat().st_size != encoded['bytes'] or sha(payload) != encoded['sha256']):
        raise ValueError('retained package encoding differs from its receipt')
    digest, count = hashlib.sha256(), 0
    with subprocess.Popen(['zstd', '-q', '-dc', '--long=27', '--', str(payload)],
                          stdout=subprocess.PIPE, stderr=subprocess.PIPE) as decoder:
        try:
            while data := decoder.stdout.read(1 << 20):
                count += len(data)
                if count > original['bytes']:
                    raise ValueError('retained package exceeds its complete raw bound')
                digest.update(data)
            if (decoder.wait(timeout=30) or count != original['bytes'] or
                    digest.hexdigest() != original['sha256'] or artifact_storage.identity(payload) != before):
                raise ValueError('retained package complete decompression differs')
        finally:
            if decoder.poll() is None:
                decoder.kill()
                decoder.wait()


def retained_sha(package, expected):
    """Verify a legacy package without changing its original completed manifest."""
    root = reference.ROOT
    package = Path(package).absolute()
    if (package.resolve() != package or package.name != PACKAGE or
            not BUILD.fullmatch(package.parent.name) or package.parent.parent != root / 'artifacts'):
        raise ValueError('retained package is outside its exact completed worker build')
    pointer = json.loads(canonical(package.with_name(PACKAGE + '.retention.json')).read_text())
    verification = root / pointer['verification']
    relative = verification.relative_to(root / 'artifacts')
    if (len(relative.parts) != 3 or not RETENTION.fullmatch(relative.parts[0]) or
            relative.parts[2] != 'verification.json' or sha(canonical(verification)) != pointer['verification_sha256']):
        raise ValueError('retained package verification is outside its frozen retention receipt')
    record = json.loads(verification.read_text())
    result_path = canonical(package.parent / 'result.json')
    if (pointer.get('schema') != 1 or record.get('schema') != 1 or
            record.get('passed') is not True or record.get('whole_decompressed_payload_verified') is not True or
            record['original'].get('file') != PACKAGE or record['original']['sha256'] != expected or
            record['build_results_sha256'].get(package.parent.name) != sha(result_path)):
        raise ValueError('retained package does not bind its unchanged original build')
    encoded = record['encoded']
    payload = verification.parent / encoded['file']
    if Path(encoded['file']).name != encoded['file'] or encoded['file'] != 'package.tar.zst':
        raise ValueError('retained package encoding path escapes its verification')
    verify_encoding(payload, encoded, record['original'])
    return expected


def archive(builds):
    import build_worker

    root = reference.ROOT
    if reference.docker(['ps', '--filter', 'label=com.factorybox.run', '--format', '{{.ID}}']):
        raise ValueError('worker transport retention requires stopped native workloads')
    # Maintenance creates no build or native workload and must remain possible
    # when the normal 8-GiB start reserve is unavailable.
    before = artifact_storage.require_capacity(root, run_reserve=0)
    groups = {}
    for name in builds:
        if not BUILD.fullmatch(name):
            raise ValueError('expected one exact completed worker build name')
        directory, result = build_worker.load(name)
        package = canonical(directory / PACKAGE)
        info = package.stat()
        if ('package_encoding' in result or not 0 < info.st_size <= MAXIMUM_PACKAGE or
                info.st_uid != os.getuid() or not stat.S_ISREG(info.st_mode) or
                package.with_name(PACKAGE + '.retention.json').exists()):
            raise ValueError('only an owned completed legacy raw transport is eligible')
        row = {'build': name, 'package': str(package), 'identity': list(artifact_storage.identity(package)),
               'result_sha256': sha(directory / 'result.json')}
        groups.setdefault(result['package_sha256'], []).append(row)
    artifact = root / 'artifacts' / ('factorybox-storage-worker-packages-' + uuid.uuid4().hex)
    artifact.mkdir()
    shutil.copyfile(__file__, artifact / 'runner.py')
    save(artifact / 'plan.json', {'schema': 1, 'runner_sha256': sha(artifact / 'runner.py'),
         'groups': groups, 'scope': 'Only complete already-verified legacy worker transports. Preserve every raw package byte in a fresh lossless encoding, all original build manifests, source, bundles, image identities and layers. Never overwrite an archived input. The original loader verifies the complete decoded transport through a separate retention receipt; raw bytes remain exactly reconstructable. No component executable, native input, failure image, reference or historical experiment is removed.'})
    completed = []
    try:
        for digest, rows in groups.items():
            capacity = artifact_storage.require_capacity(root, run_reserve=0)
            directory = artifact / digest
            directory.mkdir()
            source = Path(rows[0]['package'])
            original = {'file': PACKAGE, 'bytes': source.stat().st_size, 'sha256': digest}
            payload = directory / 'package.tar.zst'
            maximum = min(artifact_storage.ARTIFACT_LIMIT - capacity['allocated_bytes'],
                          shutil.disk_usage(root).free - artifact_storage.FREE_RESERVE)
            def limit_encoder():
                soft, hard = resource.getrlimit(resource.RLIMIT_FSIZE)
                cap = maximum if hard == resource.RLIM_INFINITY else min(maximum, hard)
                resource.setrlimit(resource.RLIMIT_FSIZE, (cap, cap))
            with payload.open('xb') as output, (directory / 'encode.log').open('xb') as log:
                subprocess.run(['zstd', '-q', '-T1', '-1', '--long=27', '-c', '--', str(source)],
                               stdout=output, stderr=log, check=True, timeout=60, preexec_fn=limit_encoder)
                output.flush()
                os.fsync(output.fileno())
            encoded = {'file': payload.name, 'bytes': payload.stat().st_size, 'sha256': sha(payload)}
            if encoded['bytes'] >= original['bytes']:
                raise ValueError('encoding does not reduce storage; keep every raw input')
            verify_encoding(payload, encoded, original)
            for row in rows:
                package = canonical(row['package'])
                if (list(artifact_storage.identity(package)) != row['identity'] or sha(package) != digest or
                        sha(package.parent / 'result.json') != row['result_sha256']):
                    raise ValueError('completed raw package changed before its retention proof')
            proof = {'schema': 1, 'passed': True, 'whole_decompressed_payload_verified': True,
                     'complete_raw_package_reconstructable': True, 'original': original, 'encoded': encoded,
                     'build_results_sha256': {row['build']: row['result_sha256'] for row in rows},
                     'original_inputs': rows,
                     'reconstruct': {row['build']: ['zstd', '-d', '--keep', str(payload), '-o', row['package']] for row in rows}}
            verification = directory / 'verification.json'
            save(verification, proof)
            for row in rows:
                package = Path(row['package'])
                save(package.with_name(PACKAGE + '.retention.json'), {
                    'schema': 1, 'verification': str(verification.relative_to(root)),
                    'verification_sha256': sha(verification)})
                retained_sha(package, digest)
                # Removing another known hardlink changes ctime, but never the
                # preserved inode, mtime, bytes or original manifest.
                if (list(artifact_storage.identity(package))[:4] != row['identity'][:4] or
                        sha(package) != digest or sha(package.parent / 'result.json') != row['result_sha256']):
                    raise ValueError('raw package changed immediately before retirement')
                package.unlink()
                sync(package.parent)
                _, checked = build_worker.load(row['build'])
                if checked['package_sha256'] != digest or sha(package.parent / 'result.json') != row['result_sha256']:
                    raise ValueError('original completed loader or manifest changed')
            completed.append({'builds': [r['build'] for r in rows], 'raw_sha256': digest,
                              'encoded_bytes': encoded['bytes'], 'verification_sha256': sha(verification)})
            print(json.dumps({'retained_packages': completed[-1]}), flush=True)
        after = artifact_storage.require_capacity(root, run_reserve=0)
        save(artifact / 'result.json', {'schema': 1, 'passed': True, 'production_qualified': False,
             'plan_sha256': sha(artifact / 'plan.json'), 'rows': completed, 'capacity_before': before,
             'capacity_after': after, 'reclaimed_allocated_bytes': before['allocated_bytes'] - after['allocated_bytes']})
    except Exception as error:
        save(artifact / 'result.json', {'schema': 1, 'passed': False, 'error': str(error), 'completed': completed,
             'scope': 'Inspect per-group verification and pointers before continuing; do not automatically rerun.'})
        raise
    print(json.dumps({'evidence': str(artifact), 'passed': True,
                      'reclaimed_allocated_bytes': before['allocated_bytes'] - after['allocated_bytes']}), flush=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--build', action='append', required=True)
    options = parser.parse_args()
    if len(options.build) != len(set(options.build)):
        parser.error('duplicate build')
    archive(options.build)


if __name__ == '__main__':
    main()
