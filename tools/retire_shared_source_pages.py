#!/usr/bin/env python3
"""Retire superseded shared-source failure RAM, preserving all other archive members."""
import argparse
import gzip
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import tarfile
import uuid

import reference


def digest_stream(stream):
    digest = hashlib.sha256()
    size = 0
    while chunk := stream.read(1 << 20):
        digest.update(chunk)
        size += len(chunk)
    return size, digest.hexdigest()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--replacement', required=True)
    parser.add_argument('--verify-bad-size-log', action='store_true', help='Require the original atomic-save restore log to prove the exact held.txt size failure when the CLI only reported its guard timeout')
    parser.add_argument('runs', nargs='+')
    args = parser.parse_args()
    if reference.docker(['ps', '--filter', 'label=com.factorybox.run', '--format', '{{.ID}}']):
        raise ValueError('retirement requires closed native workloads')
    reference.artifact_storage.require_capacity(reference.ROOT, run_reserve=0)
    pattern = re.compile(r'factorybox-shared-source-[0-9a-f]{32}')
    if not pattern.fullmatch(args.replacement) or any(not pattern.fullmatch(name) or name == args.replacement for name in args.runs):
        raise ValueError('requires exact separate shared-source runs')
    replacement = reference.ROOT / 'artifacts' / args.replacement / 'result.json'
    proof = json.loads(replacement.read_text())
    if (proof.get('passed') is not True or proof.get('cleanup') is not True or proof.get('inputs_unchanged') is not True or
            proof.get('case') != 'repeat' or len(proof.get('phases', [])) < 3 or
            any(phase.get('queued_watch_preserved') is not True for phase in proof['phases']) or
            not any(phase['kind'] == 'open-file-atomic-replacement' for phase in proof['phases'])):
        raise ValueError('replacement must prove repeated capture, atomic replacement and queued events')
    artifact = reference.ROOT / 'artifacts' / ('factorybox-storage-retire-shared-pages-' + uuid.uuid4().hex)
    artifact.mkdir()
    shutil.copyfile(__file__, artifact / 'runner.py')
    outcome = {'schema': 1, 'passed': False, 'replacement': str(replacement),
               'replacement_sha256': reference.sha(replacement), 'runs': []}
    try:
        for name in args.runs:
            root = reference.ROOT / 'artifacts' / name
            result_path = root / 'result.json'
            saved = json.loads(result_path.read_text())
            source = root / 'diagnostic.tar.gz.zst'
            raw = root / 'diagnostic.tar.gz'
            pointer = root / 'diagnostic-pages.retirement.json'
            if (root.resolve() != root or result_path.is_symlink() or source.is_symlink() or pointer.exists() or
                    saved.get('passed') is not False or saved.get('cleanup') is not True or
                    not saved.get('diagnostic', {}).get('whole_decoding_verified') or
                    not args.verify_bad_size_log and not any(text in saved.get('error', '') for text in ['bad size', 'has no retained original kernel object', 'watch events queued', 'lookup mount='])):
                raise ValueError('failure is active, redirected, already retired or outside the resolved cases')
            encoding = json.loads((root / 'diagnostic.tar.gz.encoding.json').read_text())
            if encoding['original']['sha256'] != saved['diagnostic']['sha256']:
                raise ValueError('archive differs from the original failure')
            reference.artifact_storage.verify_compacted(raw, encoding['original'])
            before = reference.artifact_storage.identity(source)
            target = artifact / (name + '-metadata.tar.gz')
            kept, retired = {}, {}
            exact_bad_size_log = False
            decoder = subprocess.Popen(['zstd', '-qdc', '--', str(source)], stdout=subprocess.PIPE)
            try:
                with tarfile.open(fileobj=decoder.stdout, mode='r|gz') as archive, tarfile.open(target, 'w:gz') as output:
                    for member in archive:
                        if member.name.startswith('/') or '..' in Path(member.name).parts or member.name in kept or member.name in retired:
                            raise ValueError('diagnostic has an unsafe or duplicated member')
                        if member.isfile() and re.search(r'/images/pages-[0-9]+\.img$', member.name):
                            size, digest = digest_stream(archive.extractfile(member))
                            if size != member.size:
                                raise ValueError('retired RAM member is incomplete')
                            retired[member.name] = {'bytes': size, 'sha256': digest}
                        elif member.isfile():
                            # Non-RAM members are bounded metadata and logs in
                            # this diagnostic format, never consumer rootfs.
                            if member.size > 32 << 20:
                                raise ValueError('unexpected large non-RAM member; retain the complete archive')
                            data = archive.extractfile(member).read()
                            if member.name.endswith('/restore.log') and b'File home/coder/project/held.txt has bad size 8192 (expect 4096)' in data:
                                exact_bad_size_log = True
                            import io
                            output.addfile(member, io.BytesIO(data))
                            kept[member.name] = {'bytes': len(data), 'sha256': hashlib.sha256(data).hexdigest(), 'type': member.type.decode()}
                        else:
                            output.addfile(member)
                            kept[member.name] = {'type': member.type.decode(), 'link': member.linkname}
                if decoder.wait(timeout=30) or not retired or args.verify_bad_size_log and (not exact_bad_size_log or saved.get('phases', [{}])[-1].get('kind') != 'open-file-atomic-replacement'):
                    raise ValueError('original decoding failed or no superseded RAM was found')
            finally:
                if decoder.poll() is None:
                    decoder.kill()
                    decoder.wait()
            actual = {}
            with tarfile.open(target) as archive:
                for member in archive:
                    if member.isfile():
                        size, digest = digest_stream(archive.extractfile(member))
                        actual[member.name] = {'bytes': size, 'sha256': digest, 'type': member.type.decode()}
                    else:
                        actual[member.name] = {'type': member.type.decode(), 'link': member.linkname}
            if actual != kept or reference.artifact_storage.identity(source) != before:
                raise ValueError('kept archive members or original archive changed')
            row = {'run': name, 'original_result_sha256': reference.sha(result_path),
                   'original_encoding_sha256': reference.sha(root / 'diagnostic.tar.gz.encoding.json'),
                   'original_archive_sha256': reference.sha(source), 'original_archive_bytes': before[2],
                   'metadata_archive': str(target), 'metadata_archive_sha256': reference.sha(target),
                   'every_kept_member_verified': True, 'kept': kept, 'retired_ram_members': retired,
                   'retired_ram_reconstructable': False, 'replacement': args.replacement,
                   'replacement_result_sha256': reference.sha(replacement)}
            receipt = artifact / (name + '.json')
            receipt.write_text(json.dumps(row, indent=2) + '\n')
            pointer.write_text(json.dumps({'schema': 1, 'receipt': str(receipt), 'sha256': reference.sha(receipt),
                'scope': 'The resolved failure RAM payload is deliberately retired. All other archive member bytes, original results, source, logs and prior encoding receipts are preserved. This is not a lossless replacement for the withdrawn RAM.'}, indent=2) + '\n')
            for path in [target, receipt, pointer]:
                with path.open('rb') as stream:
                    os.fsync(stream.fileno())
            if reference.artifact_storage.identity(source) != before or reference.sha(result_path) != row['original_result_sha256']:
                raise ValueError('failure changed before RAM retirement')
            source.unlink()
            outcome['runs'].append({'run': name, 'receipt_sha256': reference.sha(receipt),
                                    'reclaimed_bytes': before[2] - target.stat().st_size})
        outcome['passed'] = True
    finally:
        (artifact / 'result.json').write_text(json.dumps(outcome, indent=2) + '\n')
        print(json.dumps({'evidence': str(artifact), 'passed': outcome['passed'],
                          'reclaimed_bytes': sum(row['reclaimed_bytes'] for row in outcome['runs'])}), flush=True)


if __name__ == '__main__':
    main()
