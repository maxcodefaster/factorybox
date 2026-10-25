#!/usr/bin/env python3
"""Share identical immutable source files of completed, verified local runs."""
import argparse
import json
import os
from pathlib import Path
import re
import uuid

import reference
import sources
import upstream_unit


def save(path, value):
    with path.open('x') as output:
        json.dump(value, output, indent=2)
        output.write('\n')
        output.flush()
        os.fsync(output.fileno())
    descriptor = os.open(path.parent, os.O_RDONLY | os.O_DIRECTORY)
    try:
        os.fsync(descriptor)
    finally:
        os.close(descriptor)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--include-closed-failed-native-inputs', action='store_true',
                        help='Also share byte-identical frozen Go sources of failed native runs with confirmed cleanup and unchanged inputs; never dumps or failed build contexts')
    parser.add_argument('--include-closed-shared-source-probes', action='store_true',
                        help='Also verify and share the combined runtime/actor source manifests of closed shared-source probes')
    parser.add_argument('--adopt-go-mtime', action='store_true',
                        help='Allow identical closed Go sources to adopt the canonical copy timestamp, retaining both timestamps in the receipt; never changes bytes, modes, owners or imported root sources')
    args = parser.parse_args()
    if reference.docker(['ps', '--filter', 'label=com.factorybox.run', '--format', '{{.ID}}']):
        raise ValueError('source sharing requires all Factorybox containers to be stopped')
    reference.artifact_storage.require_capacity(reference.ROOT, run_reserve=0)
    artifact = reference.ROOT / 'artifacts' / ('factorybox-storage-share-sources-' + uuid.uuid4().hex)
    artifact.mkdir()
    # Freeze this maintenance recipe without modifying any source evidence.
    (artifact / 'runner.py').write_bytes(Path(__file__).read_bytes())
    trees, actions, groups = [], [], {}
    for run in sorted((reference.ROOT / 'artifacts').iterdir()):
        result_path = run / 'result.json'
        if run.is_symlink() or result_path.is_symlink() or not result_path.is_file():
            continue
        result = json.loads(result_path.read_text())
        shared_probe = (args.include_closed_shared_source_probes and
                        re.fullmatch(r'factorybox-shared-source-[0-9a-f]{32}', run.name) and
                        result.get('cleanup') is True and result.get('inputs_unchanged') is True)
        closed_failure = (args.include_closed_failed_native_inputs and result.get('passed') is False and
                          result.get('cleanup') is True and result.get('inputs_unchanged') is True and
                          not run.name.startswith('factorybox-build-') and (isinstance(result.get('source_context'), dict) or shared_probe))
        if result.get('passed') is not True and not closed_failure:
            continue
        criu = re.fullmatch(r'factorybox-build-criu-retained-[0-9a-f]{32}', run.name)
        worker = re.fullmatch(r'factorybox-build-worker-[0-9a-f]{32}', run.name)
        if shared_probe:
            tree, kind = run / 'source', 'manifest'
            expected = {key: value for key, value in result['runtime_source_context'].items()
                        if result.get('diagnostic_runtime') or key in ['go.mod', 'go.sum']}
            expected.update({'tests/shared-source-actor/' + key: value for key, value in result['actor_source_context'].items()})
            if upstream_unit.context_manifest(tree) != expected:
                raise ValueError('closed shared-source runtime/actor input changed: ' + run.name)
        elif criu and result.get('cleanup') is True:
            tree, kind, expected = run / 'context', 'criu', result['context_sha256']
            if sources.tree_sha(tree) != expected:
                raise ValueError('completed CRIU source differs: ' + run.name)
        elif (result.get('cleanup') is True or worker) and isinstance(result.get('source_context'), dict):
            candidates = [run / name for name in ['source', 'context'] if (run / name).is_dir()]
            if len(candidates) != 1:
                continue
            tree, kind, expected = candidates[0], 'manifest', result['source_context']
            if upstream_unit.context_manifest(tree) != expected:
                raise ValueError('completed native/build source differs: ' + run.name)
        else:
            continue
        if tree.is_symlink() or tree.resolve() != tree:
            raise ValueError('completed source tree cannot be redirected')
        trees.append({'path': str(tree), 'kind': kind, 'expected': expected, 'original_passed': result['passed'],
                      'result': str(result_path), 'result_sha256': reference.sha(result_path)})
        for path in sorted(tree.rglob('*')):
            if path.is_symlink() or not path.is_file():
                continue
            info = path.stat()
            if info.st_uid != os.getuid():
                continue
            digest = reference.sha(path)
            canonical_mtime = args.adopt_go_mtime and kind == 'manifest' and path.suffix == '.go'
            key = (str(path.relative_to(tree)), digest, info.st_size, info.st_mode,
                   info.st_uid, info.st_gid, None if canonical_mtime else info.st_mtime_ns)
            source = groups.setdefault(key, path)
            if source == path or os.path.samefile(source, path) or info.st_nlink != 1 and not canonical_mtime:
                continue
            actions.append({'target': str(path), 'source': str(source), 'sha256': digest,
                            'identity': list(reference.artifact_storage.identity(path)),
                            'mode': info.st_mode, 'uid': info.st_uid, 'gid': info.st_gid,
                            'mtime_ns': info.st_mtime_ns, 'shared_mtime_ns': source.stat().st_mtime_ns,
                            'adopt_go_mtime': canonical_mtime,
                            'allocated_bytes': info.st_blocks * 512})
    plan = {'schema': 1, 'operation': 'share_identical_closed_verified_source_inputs',
            'include_closed_failed_native_inputs': args.include_closed_failed_native_inputs,
            'include_closed_shared_source_probes': args.include_closed_shared_source_probes,
            'adopt_go_mtime': args.adopt_go_mtime,
            'runner_sha256': reference.sha(artifact / 'runner.py'), 'trees': trees, 'actions': actions}
    save(artifact / 'plan.json', plan)
    completed, reclaimed = 0, 0
    try:
        for row in actions:
            target, source = Path(row['target']), Path(row['source'])
            info = source.stat()
            # Relinking another name of a shared old inode changes its ctime,
            # not its bytes, inode, size or mtime. Never ignore those identities.
            identity = list(reference.artifact_storage.identity(target))
            matches = identity[:4] == row['identity'][:4] if row['adopt_go_mtime'] else identity == row['identity']
            if (not matches or
                    reference.sha(target) != row['sha256'] or reference.sha(source) != row['sha256'] or
                    (info.st_mode, info.st_uid, info.st_gid, info.st_mtime_ns) !=
                    (row['mode'], row['uid'], row['gid'], row['shared_mtime_ns'])):
                raise ValueError('immutable source changed before sharing')
            old = target.stat()
            released = old.st_blocks * 512 if old.st_nlink == 1 else 0
            temporary = target.with_name(target.name + '.sharing-' + uuid.uuid4().hex)
            try:
                os.link(source, temporary, follow_symlinks=False)
                os.replace(temporary, target)
            finally:
                temporary.unlink(missing_ok=True)
            descriptor = os.open(target.parent, os.O_RDONLY | os.O_DIRECTORY)
            try:
                os.fsync(descriptor)
            finally:
                os.close(descriptor)
            if not os.path.samefile(target, source) or reference.sha(target) != row['sha256']:
                raise ValueError('shared source differs')
            completed += 1
            reclaimed += released
        for tree in trees:
            path = Path(tree['path'])
            actual = sources.tree_sha(path) if tree['kind'] == 'criu' else upstream_unit.context_manifest(path)
            if actual != tree['expected'] or reference.sha(Path(tree['result'])) != tree['result_sha256']:
                raise ValueError('original source manifest or result changed')
        save(artifact / 'result.json', {'schema': 1, 'passed': True, 'plan_sha256': reference.sha(artifact / 'plan.json'),
                                      'files_shared': completed, 'trees_verified': len(trees),
                                      'reclaimed_allocated_bytes': reclaimed,
                                      'scope': 'Only byte-, mode- and owner-identical immutable source files. Timestamps must match unless explicitly adopting a canonical timestamp for closed Go sources, with both timestamps retained. Failed native source inputs require explicit opt-in, confirmed cleanup and unchanged inputs. Every original source manifest and result is reverified. No dump, failure result/log, active run, failed build, build product, cache, imported root reference or historical experiment is changed.'})
    except Exception as error:
        save(artifact / 'result.json', {'schema': 1, 'passed': False, 'error': str(error), 'completed': completed})
        raise
    print(json.dumps({'evidence': str(artifact), 'files_shared': completed,
                      'reclaimed_allocated_bytes': reclaimed}), flush=True)


if __name__ == '__main__':
    main()
