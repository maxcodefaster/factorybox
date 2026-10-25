#!/usr/bin/env python3
"""Retain an unselected worker transport through its exact immutable bundle."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import tarfile
import uuid

import reference

PACKAGE = 'factorybox-worker-linux-amd64.tar'
MAXIMUM = 512 << 20


class Fingerprint:
    def __init__(self, output=None):
        self.digest, self.count, self.output = hashlib.sha256(), 0, output

    def write(self, data):
        self.count += len(data)
        if self.count > MAXIMUM:
            raise ValueError('reconstructed worker transport exceeds its bound')
        self.digest.update(data)
        if self.output is not None:
            self.output.write(data)
        return len(data)


def transport(bundle, output=None):
    """Schema 1 is the original build_worker's exact USTAR byte sequence."""
    sink = Fingerprint(output)
    with tarfile.open(fileobj=sink, mode='w|', format=tarfile.USTAR_FORMAT) as archive:
        for path in sorted(bundle.rglob('*')):
            if path.is_symlink() or not (path.is_dir() or path.is_file()):
                raise ValueError('immutable worker bundle contains a redirected or special entry')
            name = str(path.relative_to(bundle))
            entry = tarfile.TarInfo(name)
            entry.mode, entry.mtime = (0o700 if path.is_dir() else 0o600), 0
            if path.is_dir():
                entry.type = tarfile.DIRTYPE
                archive.addfile(entry)
            else:
                entry.size = path.stat().st_size
                if name == 'factorybox' or name.startswith('components/'):
                    entry.mode = 0o755
                with path.open('rb') as stream:
                    archive.addfile(entry, stream)
    return {'bytes': sink.count, 'sha256': sink.digest.hexdigest()}


def verify(directory, result):
    pointer = directory / (PACKAGE + '.bundle-retention.json')
    if pointer.resolve() != pointer or pointer.is_symlink():
        raise ValueError('worker bundle transport receipt is redirected')
    saved = json.loads(pointer.read_text())
    if (saved.get('schema') != 1 or saved.get('build_result_sha256') != reference.sha(directory / 'result.json') or
            saved.get('bundle_sha256') != result['bundle_sha256'] or
            saved.get('original_encoding') != result.get('package_encoding') or
            saved.get('whole_original_decode_verified') is not True or saved.get('complete_raw_transport_reconstructable') is not True):
        raise ValueError('worker bundle transport lacks its unchanged original build and complete byte proof')
    actual = transport(directory / 'image-context/bundle')
    if actual != saved.get('raw') or actual != {'bytes': result['package_bytes'], 'sha256': result['package_sha256']}:
        raise ValueError('worker bundle cannot reconstruct the exact original transport')
    return result['package_encoding']


def verify_legacy(verification, record, payload, build):
    """Verify one requested legacy owner through its unchanged runnable bundle."""
    pointer=payload.with_name(payload.name+'.bundle-retention.json')
    if pointer.is_symlink() or pointer.resolve()!=pointer:raise ValueError('Legacy bundle receipt is redirected')
    saved=json.loads(pointer.read_text())
    if (saved.get('schema')!=1 or saved.get('verification_sha256')!=reference.sha(verification) or
            saved.get('original')!=record['original'] or saved.get('encoded')!=record['encoded'] or
            saved.get('whole_original_decode_verified') is not True or
            saved.get('complete_raw_transport_reconstructable') is not True or
            saved.get('build_results_sha256')!=record['build_results_sha256'] or
            build.name not in saved['build_results_sha256'] or
            reference.sha(build/'result.json')!=saved['build_results_sha256'][build.name]):
        raise ValueError('Legacy transport receipt differs from its original verified owners')
    if transport(build/'image-context/bundle')!={key:record['original'][key] for key in ['bytes','sha256']}:
        raise ValueError('Legacy bundle cannot reconstruct the exact original transport')


def retire_legacy(path, proof):
    import closed_worker_packages
    path=Path(path).absolute()
    relative=path.relative_to(reference.ROOT/'artifacts')
    if (path.resolve()!=path or path.is_symlink() or len(relative.parts)!=3 or
            not closed_worker_packages.RETENTION.fullmatch(relative.parts[0]) or relative.parts[2]!='verification.json'):
        raise ValueError('Expected an exact completed legacy package verification')
    record=json.loads(path.read_text())
    if not record.get('passed') or not record.get('whole_decompressed_payload_verified'):
        raise ValueError('Legacy encoding has no complete original verification')
    if record['encoded']['file']!='package.tar.zst':raise ValueError('Legacy encoding path differs')
    payload=path.parent/'package.tar.zst'
    closed_worker_packages.verify_encoding(payload,record['encoded'],record['original'])
    for name,sha in record['build_results_sha256'].items():
        if not re.fullmatch('factorybox-build-worker-[a-f0-9]{32}',name):raise ValueError('Legacy owner is not an exact build')
        build=reference.ROOT/'artifacts'/name
        original=json.loads((build/'result.json').read_text())
        if reference.sha(build/'result.json')!=sha or original.get('passed') is not True or original.get('selected') is not False:
            raise ValueError('Keep selected, failed or changed legacy builds')
        # This validates the original build pointer and complete encoding as
        # well as each independent bundle before retiring the shared duplicate.
        closed_worker_packages.retained_sha(build/PACKAGE,record['original']['sha256'])
        if transport(build/'image-context/bundle')!={key:record['original'][key] for key in ['bytes','sha256']}:
            raise ValueError('Legacy bundle uses another raw tar recipe')
    saved={'schema':1,'verification_sha256':reference.sha(path),'original':record['original'],
           'encoded':record['encoded'],'build_results_sha256':record['build_results_sha256'],
           'whole_original_decode_verified':True,'complete_raw_transport_reconstructable':True,
           'replacement_proof':proof,'scope':'Only a redundant shared encoded transport. Preserve every original raw byte through all unchanged directly usable bundles, component executables, source and original manifests.'}
    pointer=payload.with_name(payload.name+'.bundle-retention.json')
    with pointer.open('x') as stream:
        json.dump(saved,stream,indent=2);stream.write('\n');stream.flush();os.fsync(stream.fileno())
    parent=os.open(pointer.parent,os.O_RDONLY|os.O_DIRECTORY)
    try:os.fsync(parent)
    finally:os.close(parent)
    for name in saved['build_results_sha256']:verify_legacy(path,record,payload,reference.ROOT/'artifacts'/name)
    if reference.sha(payload)!=record['encoded']['sha256']:
        raise ValueError('Legacy encoding changed before its verified retirement')
    before=payload.stat().st_blocks*512;payload.unlink()
    parent=os.open(pointer.parent,os.O_RDONLY|os.O_DIRECTORY)
    try:os.fsync(parent)
    finally:os.close(parent)
    return {'verification':str(path),'retired_encoded_allocated_bytes':before,'receipt_sha256':reference.sha(pointer)}


def retire(build, proof):
    import build_worker
    if not re.fullmatch(r'factorybox-build-worker-[0-9a-f]{32}', build):
        raise ValueError('requires one exact completed worker')
    directory, result = build_worker.load(build)
    if result.get('selected') is not False or build == proof['worker_build'] or 'package_encoding' not in result:
        raise ValueError('keep selected, replacement and non-encoded input transports')
    encoded = directory / result['package_encoding']['file']
    if encoded.is_symlink() or encoded.resolve() != encoded:
        raise ValueError('original worker encoding is redirected')
    # load already verified the complete original decode, source and runnable
    # bundle. A second independent USTAR stream must have the identical hash.
    raw = transport(directory / 'image-context/bundle')
    if raw != {'bytes': result['package_bytes'], 'sha256': result['package_sha256']}:
        raise ValueError('original package uses another tar recipe; keep its encoding')
    receipt = {'schema': 1, 'build': build, 'build_result_sha256': reference.sha(directory / 'result.json'),
               'bundle_sha256': result['bundle_sha256'], 'original_encoding': result['package_encoding'],
               'raw': raw, 'whole_original_decode_verified': True, 'complete_raw_transport_reconstructable': True,
               'replacement_proof': proof['path'], 'replacement_proof_sha256': proof['sha256'],
               'reconstruct': ['mise', 'exec', '--', 'python3', 'tools/worker_bundle_transport.py', '--restore-build', build,
                               '--output', '<new-output-path>'],
               'scope': 'Only the redundant encoded transport. Every original raw tar byte is reconstructed from the unchanged runnable bundle. Keep original build, source, encoding receipt, all component executables and Docker image directly usable.'}
    pointer = directory / (PACKAGE + '.bundle-retention.json')
    with pointer.open('x') as stream:
        json.dump(receipt, stream, indent=2); stream.write('\n')
        stream.flush()
        os.fsync(stream.fileno())
    descriptor = os.open(directory, os.O_RDONLY | os.O_DIRECTORY)
    try:
        os.fsync(descriptor)
    finally:
        os.close(descriptor)
    verify(directory, result)
    original = encoded.stat().st_blocks * 512
    if reference.sha(encoded) != result['package_encoding']['sha256']:
        raise ValueError('encoded transport changed before its verified retirement')
    encoded.unlink()
    descriptor = os.open(directory, os.O_RDONLY | os.O_DIRECTORY)
    try:
        os.fsync(descriptor)
    finally:
        os.close(descriptor)
    build_worker.load(build)
    return {'build': build, 'raw': raw, 'retired_encoded_allocated_bytes': original,
            'receipt_sha256': reference.sha(pointer)}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    action = parser.add_mutually_exclusive_group(required=True)
    action.add_argument('--retire-build', action='append')
    action.add_argument('--retire-legacy-archive', action='append',type=Path)
    action.add_argument('--restore-build')
    parser.add_argument('--replacement-proof')
    parser.add_argument('--output', type=Path)
    args = parser.parse_args()
    import build_worker
    if args.restore_build:
        if args.output is None or args.output.exists() or args.output.is_symlink():
            raise ValueError('restore requires a new explicit output path')
        directory, result = build_worker.load(args.restore_build)
        # load verifies either the local encoding, the exact bundle receipt or
        # the legacy shared-archive receipt before any output is created.
        with args.output.open('xb') as output:
            actual = transport(directory / 'image-context/bundle', output)
        if actual != {'bytes': result['package_bytes'], 'sha256': result['package_sha256']}:
            raise ValueError('restored transport differs')
        print(json.dumps(actual))
        return
    if not re.fullmatch(r'factorybox-shared-source-[0-9a-f]{32}', args.replacement_proof or ''):
        raise ValueError('requires one exact delivered shared-source replacement proof')
    path = reference.ROOT / 'artifacts' / args.replacement_proof / 'result.json'
    proof = json.loads(path.read_text())
    if (path.resolve() != path or any(proof.get(key) is not True for key in ['passed','cleanup','exact_packaged_cli','inputs_unchanged']) or len(proof['phases']) < 3):
        raise ValueError('replacement proof did not complete the exact delivered image')
    if reference.docker(['ps','--filter','label=com.factorybox.run','--format','{{.ID}}']):
        raise ValueError('worker transport maintenance requires closed native workloads')
    reference.artifact_storage.require_capacity(reference.ROOT, run_reserve=0)
    artifact = reference.ROOT / 'artifacts' / ('factorybox-storage-bundle-transports-' + uuid.uuid4().hex)
    artifact.mkdir()
    shutil.copyfile(__file__, artifact / 'runner.py')
    outcome = {'schema':1, 'passed':False, 'builds':[]}
    try:
        for build in args.retire_build or []:
            outcome['builds'].append(retire(build, {'worker_build':proof['worker_build'],'path':str(path),'sha256':reference.sha(path)}))
        for legacy in args.retire_legacy_archive or []:
            outcome['builds'].append(retire_legacy(legacy,{'path':str(path),'sha256':reference.sha(path)}))
        outcome['passed'] = True
    finally:
        (artifact / 'result.json').write_text(json.dumps(outcome, indent=2)+'\n')
        print(json.dumps({'evidence':str(artifact),**outcome}))


if __name__ == '__main__':
    main()
