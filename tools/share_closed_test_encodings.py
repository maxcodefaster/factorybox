#!/usr/bin/env python3
"""Share identical encoded probes of closed native runs without changing receipts."""
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import uuid

import reference


def sha(path):
    with path.open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def save(path, value):
    with path.open('x') as stream:
        json.dump(value, stream, indent=2)
        stream.write('\n')
        stream.flush()
        os.fsync(stream.fileno())


def main():
    if reference.docker(['ps', '--filter', 'label=com.factorybox.run', '--format', '{{.ID}}']):
        raise ValueError('encoded input sharing requires every native worker to close')
    before = reference.artifact_storage.require_capacity(reference.ROOT, run_reserve=0)
    groups, hashes = {}, {}
    for run in sorted((reference.ROOT / 'artifacts').iterdir()):
        if not re.fullmatch(r'factorybox-(?:checkpoint-retirement|bounded-store|bounded-store-loss|bounded-loss|storage-admission)-[0-9a-f]{32}', run.name):
            continue
        result, receipt, encoded = run / 'result.json', run / 'probe.test.encoding.json', run / 'probe.test.zst'
        if any(p.is_symlink() or not p.is_file() for p in (result, receipt, encoded)):
            continue
        outcome, encoding = json.loads(result.read_text()), json.loads(receipt.read_text())
        if outcome.get('cleanup') is not True:
            continue
        expected = outcome.get('test_binary_sha256', outcome.get('probe_sha256'))
        if not expected:
            continue  # Other drivers retain a different input manifest.
        original, stored = encoding['original'], encoding['encoded']
        if (encoding.get('whole_decompressed_payload_verified') is not True or
                original['file'] != 'probe.test' or stored['file'] != 'probe.test.zst' or
                original['sha256'] != expected):
            raise ValueError('closed probe differs from its native result: ' + run.name)
        info = encoded.stat()
        if info.st_uid != os.getuid() or encoded.resolve() != encoded:
            raise ValueError('encoded probe has another owner or a redirected path')
        if info.st_size != stored['bytes'] or sha(encoded) != stored['sha256']:
            raise ValueError('encoded probe bytes differ from receipt')
        hashes[str(result)], hashes[str(receipt)] = sha(result), sha(receipt)
        key = (original['sha256'], original['bytes'], stored['sha256'], stored['bytes'], info.st_mode, info.st_uid, info.st_gid)
        groups.setdefault(key, []).append({'path': str(encoded), 'identity': list(reference.artifact_storage.identity(encoded)),
            'mtime_ns': info.st_mtime_ns, 'mode': info.st_mode, 'uid': info.st_uid, 'gid': info.st_gid,
            'original': original, 'encoded': stored})
    rows = [group for group in groups.values() if len({tuple(row['identity'][:2]) for row in group}) > 1]
    artifact = reference.ROOT / 'artifacts' / ('factorybox-storage-share-test-encodings-' + uuid.uuid4().hex)
    artifact.mkdir()
    shutil.copyfile(__file__, artifact / 'runner.py')
    # A complete decode of each canonical plus identical encoded hashes binds
    # every duplicate to the original executable without expanding it to disk.
    for group in rows:
        reference.artifact_storage.verify_compacted(Path(group[0]['path']).with_suffix(''), group[0]['original'])
    save(artifact / 'intent.json', {'schema': 1, 'groups': rows, 'results_and_receipts': hashes,
                                  'capacity_before': before, 'canonical_complete_decodes_verified': True})
    result = {'schema': 1, 'passed': False, 'completed': [], 'production_qualified': False}
    try:
        for group in rows:
            canonical = Path(group[0]['path'])
            for row in group[1:]:
                target = Path(row['path'])
                if target.samefile(canonical):
                    continue
                info = target.stat()
                # Relinking another name of this same old inode can change
                # its ctime. Bytes, inode, mtime, mode and ownership must agree.
                if (list(reference.artifact_storage.identity(target))[:4] != row['identity'][:4] or
                        (info.st_mode, info.st_uid, info.st_gid) != (row['mode'], row['uid'], row['gid']) or
                        sha(target) != row['encoded']['sha256']):
                    raise ValueError('closed probe changed before sharing')
                temporary = target.with_name(target.name + '.sharing-' + uuid.uuid4().hex)
                try:
                    os.link(canonical, temporary, follow_symlinks=False)
                    os.replace(temporary, target)
                finally:
                    temporary.unlink(missing_ok=True)
                descriptor = os.open(target.parent, os.O_RDONLY | os.O_DIRECTORY)
                try:
                    os.fsync(descriptor)
                finally:
                    os.close(descriptor)
                if not target.samefile(canonical) or sha(target) != row['encoded']['sha256']:
                    raise ValueError('shared encoded probe verification failed')
                result['completed'].append({'path': str(target), 'original_mtime_ns': row['mtime_ns'],
                                            'shared_mtime_ns': canonical.stat().st_mtime_ns})
        if any(sha(Path(path)) != expected for path, expected in hashes.items()):
            raise ValueError('closed native result or receipt changed')
        after = reference.artifact_storage.require_capacity(reference.ROOT, run_reserve=0)
        result.update(passed=True, capacity_after=after,
                      reclaimed_allocated_bytes=before['allocated_bytes'] - after['allocated_bytes'],
                      scope='Only byte-identical encoded compiled probes of closed native runs. Full canonical decompression and all native executable/encoding identities verified. Every path remains usable; results, logs, sources and encoding receipts unchanged. No failure dump, frozen build, active input or historical experiment changed.')
    except Exception as error:
        result['error'] = str(error)
        raise
    finally:
        save(artifact / 'result.json', result)
        print(json.dumps({'evidence': str(artifact), 'passed': result['passed'],
                          'files_shared': len(result['completed']), 'reclaimed_allocated_bytes': result.get('reclaimed_allocated_bytes')}), flush=True)


if __name__ == '__main__':
    main()
