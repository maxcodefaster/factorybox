#!/usr/bin/env python3
"""Share identical encoded helpers of closed native runs without changing receipts."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import uuid

import reference
import upstream_unit


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
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--include-closed-consumer-helpers', action='store_true',
                        help='also share encoded consumer helpers bound to their exact native inputs_sha256')
    parser.add_argument('--include-closed-runtime-tests', action='store_true',
                        help='also share encoded lifecycle tests of passing stopped native runs, bound to their exact inputs_sha256')
    parser.add_argument('--include-closed-failed-runtime-tests', action='store_true',
                        help='also share identical encoded tests of stopped failed runtime suites with confirmed cleanup and unchanged complete sources/components; preserve failed results and dumps')
    args = parser.parse_args()
    if reference.docker(['ps', '--filter', 'label=com.factorybox.run', '--format', '{{.ID}}']):
        raise ValueError('encoded input sharing requires every native worker to close')
    before = reference.artifact_storage.require_capacity(reference.ROOT, run_reserve=0)
    groups, hashes = {}, {}
    for run in sorted((reference.ROOT / 'artifacts').iterdir()):
        native = re.fullmatch(r'factorybox-(?:checkpoint-retirement|bounded-store|bounded-store-loss|bounded-loss|storage-admission)-[0-9a-f]{32}', run.name)
        consumer = args.include_closed_consumer_helpers and re.fullmatch(r'factorybox-consumer-worker-loss-[0-9a-f]{32}', run.name)
        runtime = (args.include_closed_runtime_tests or args.include_closed_failed_runtime_tests) and re.fullmatch(r'factorybox-runtime-([0-9a-f]{32})', run.name)
        if not native and not consumer and not runtime:
            continue
        result = run / 'result.json'
        if run.is_symlink() or result.is_symlink() or not result.is_file():
            continue
        outcome = json.loads(result.read_text())
        if outcome.get('cleanup') is not True:
            continue
        if runtime:
            closed_failure = (args.include_closed_failed_runtime_tests and outcome.get('passed') is False and
                all(outcome.get(key) is True for key in ['inputs_unchanged','source_context_unchanged','components_unchanged']) and
                isinstance(outcome.get('source_context'), dict))
            if ((outcome.get('passed') is not True and not closed_failure) or
                    outcome.get('container_state', {}).get('Running') is not False or
                    outcome.get('container_state', {}).get('Pid') != 0):
                continue
            if outcome.get('owner') != runtime[1]:
                raise ValueError('closed runtime test owner differs from its directory')
            if closed_failure and upstream_unit.context_manifest(run/'context') != outcome['source_context']:
                raise ValueError('closed failed runtime test source changed')
        if runtime:
            candidates = {'lifecycle.test': outcome.get('inputs_sha256', {}).get('lifecycle.test')}
        elif native:
            candidates = {'probe.test': outcome.get('test_binary_sha256', outcome.get('probe_sha256'))}
        else:
            candidates = {name: outcome.get('inputs_sha256', {}).get(name) for name in
                ['consumer-worker.test', 'devx-actor', 'devx-registry', 'minimal-actor', 'ide-filesystem']}
        for name, expected in candidates.items():
            receipt, encoded = run / (name + '.encoding.json'), run / (name + '.zst')
            if not isinstance(expected, str) or not re.fullmatch(r'[0-9a-f]{64}', expected):
                continue
            if any(p.is_symlink() or not p.is_file() for p in (receipt, encoded)):
                continue
            encoding = json.loads(receipt.read_text())
            original, stored = encoding['original'], encoding['encoded']
            if (encoding.get('whole_decompressed_payload_verified') is not True or
                    original['file'] != name or stored['file'] != name + '.zst' or
                    original['sha256'] != expected):
                raise ValueError('closed helper differs from its native result: ' + run.name + '/' + name)
            info = encoded.stat()
            if info.st_uid != os.getuid() or encoded.resolve() != encoded:
                raise ValueError('encoded helper has another owner or a redirected path')
            if info.st_size != stored['bytes'] or sha(encoded) != stored['sha256']:
                raise ValueError('encoded helper bytes differ from receipt')
            hashes[str(result)], hashes[str(receipt)] = sha(result), sha(receipt)
            key = (original['sha256'], original['bytes'], stored['sha256'], stored['bytes'], info.st_mode, info.st_uid, info.st_gid)
            groups.setdefault(key, []).append({'path': str(encoded), 'identity': list(reference.artifact_storage.identity(encoded)),
                'mtime_ns': info.st_mtime_ns, 'mode': info.st_mode, 'uid': info.st_uid, 'gid': info.st_gid,
                'original': original, 'encoded': stored})
    rows = [group for group in groups.values() if len({tuple(row['identity'][:2]) for row in group}) > 1]
    for group in rows:
        group.sort(key=lambda row: -Path(row['path']).stat().st_nlink)
    artifact = reference.ROOT / 'artifacts' / ('factorybox-storage-share-test-encodings-' + uuid.uuid4().hex)
    artifact.mkdir()
    shutil.copyfile(__file__, artifact / 'runner.py')
    # A complete decode of each canonical plus identical encoded hashes binds
    # every duplicate to the original executable without expanding it to disk.
    for group in rows:
        reference.artifact_storage.verify_compacted(Path(group[0]['path']).with_suffix(''), group[0]['original'])
    save(artifact / 'intent.json', {'schema': 1, 'groups': rows, 'results_and_receipts': hashes,
                                  'capacity_before': before, 'canonical_complete_decodes_verified': True,
                                  'include_closed_consumer_helpers': args.include_closed_consumer_helpers,
                                  'include_closed_runtime_tests': args.include_closed_runtime_tests,
                                  'include_closed_failed_runtime_tests': args.include_closed_failed_runtime_tests})
    result = {'schema': 1, 'passed': False, 'completed': [], 'production_qualified': False}
    reclaimed = 0
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
                # Count only this operation's last-link replacements. Another
                # maintenance job may change the global allocation snapshot.
                released_blocks = info.st_blocks * 512 if info.st_nlink == 1 else 0
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
                reclaimed += released_blocks
                result['completed'].append({'path': str(target), 'original_mtime_ns': row['mtime_ns'],
                                            'shared_mtime_ns': canonical.stat().st_mtime_ns,
                                            'reclaimed_allocated_bytes': released_blocks})
        if any(sha(Path(path)) != expected for path, expected in hashes.items()):
            raise ValueError('closed native result or receipt changed')
        after = reference.artifact_storage.require_capacity(reference.ROOT, run_reserve=0)
        result.update(passed=True, capacity_after=after,
                      reclaimed_allocated_bytes=reclaimed,
                      observed_artifact_allocation_delta_bytes=before['allocated_bytes'] - after['allocated_bytes'],
                      scope='Only byte-identical encoded helpers of closed native runs. Optional consumer helpers and stopped runtime tests require exact inputs_sha256 bindings. Failed runtime tests require explicit opt-in, confirmed cleanup and unchanged complete sources/components. Full canonical decompression and all native executable/encoding identities verified. Every path remains usable; failed outcomes, logs, sources and encoding receipts unchanged. No failure dump, frozen build, active input or historical experiment changed.')
    except Exception as error:
        result['error'] = str(error)
        raise
    finally:
        save(artifact / 'result.json', result)
        print(json.dumps({'evidence': str(artifact), 'passed': result['passed'],
                          'files_shared': len(result['completed']), 'reclaimed_allocated_bytes': result.get('reclaimed_allocated_bytes')}), flush=True)


if __name__ == '__main__':
    main()
