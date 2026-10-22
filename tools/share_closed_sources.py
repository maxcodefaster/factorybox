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
        closed_failure = (args.include_closed_failed_native_inputs and result.get('passed') is False and
                          result.get('cleanup') is True and result.get('inputs_unchanged') is True and
                          not run.name.startswith('factorybox-build-') and isinstance(result.get('source_context'), dict))
        if result.get('passed') is not True and not closed_failure:
            continue
        criu = re.fullmatch(r'factorybox-build-criu-retained-[0-9a-f]{32}', run.name)
        worker = re.fullmatch(r'factorybox-build-worker-[0-9a-f]{32}', run.name)
        if criu and result.get('cleanup') is True:
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
            key = (str(path.relative_to(tree)), digest, info.st_size, info.st_mode,
                   info.st_uid, info.st_gid, info.st_mtime_ns)
            source = groups.setdefault(key, path)
            if source == path or os.path.samefile(source, path) or info.st_nlink != 1:
                continue
            actions.append({'target': str(path), 'source': str(source), 'sha256': digest,
                            'identity': list(reference.artifact_storage.identity(path)),
                            'mode': info.st_mode, 'uid': info.st_uid, 'gid': info.st_gid,
                            'mtime_ns': info.st_mtime_ns, 'allocated_bytes': info.st_blocks * 512})
    plan = {'schema': 1, 'operation': 'share_identical_closed_verified_source_inputs',
            'include_closed_failed_native_inputs': args.include_closed_failed_native_inputs,
            'runner_sha256': reference.sha(artifact / 'runner.py'), 'trees': trees, 'actions': actions}
    save(artifact / 'plan.json', plan)
    completed = 0
    try:
        for row in actions:
            target, source = Path(row['target']), Path(row['source'])
            info = source.stat()
            if (list(reference.artifact_storage.identity(target)) != row['identity'] or
                    reference.sha(target) != row['sha256'] or reference.sha(source) != row['sha256'] or
                    (info.st_mode, info.st_uid, info.st_gid, info.st_mtime_ns) !=
                    (row['mode'], row['uid'], row['gid'], row['mtime_ns'])):
                raise ValueError('immutable source changed before sharing')
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
        for tree in trees:
            path = Path(tree['path'])
            actual = sources.tree_sha(path) if tree['kind'] == 'criu' else upstream_unit.context_manifest(path)
            if actual != tree['expected'] or reference.sha(Path(tree['result'])) != tree['result_sha256']:
                raise ValueError('original source manifest or result changed')
        save(artifact / 'result.json', {'schema': 1, 'passed': True, 'plan_sha256': reference.sha(artifact / 'plan.json'),
                                      'files_shared': completed, 'trees_verified': len(trees),
                                      'reclaimed_allocated_bytes': sum(row['allocated_bytes'] for row in actions),
                                      'scope': 'Only byte-, metadata- and mtime-identical immutable source files. Failed native source inputs require explicit opt-in, confirmed cleanup and unchanged inputs. Every original source manifest and result is reverified. No dump, failure result/log, active run, failed build, build product, cache, imported reference or historical experiment is changed.'})
    except Exception as error:
        save(artifact / 'result.json', {'schema': 1, 'passed': False, 'error': str(error), 'completed': completed})
        raise
    print(json.dumps({'evidence': str(artifact), 'files_shared': completed,
                      'reclaimed_allocated_bytes': sum(row['allocated_bytes'] for row in actions)}), flush=True)


if __name__ == '__main__':
    main()
