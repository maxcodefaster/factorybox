#!/usr/bin/env python3
"""Share exact closed Sysbox build outputs, preserving every path and byte."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import stat
import subprocess
import sys
import uuid

import reference

BUILD = re.compile(r'factorybox-build-sysbox-(?:repro-)?[0-9a-f]{32}')
ROLES = {'sysbox-runc', 'sysbox-fs', 'sysbox-mgr', 'integration.test', 'libcontainer.test'}


def sha(path):
    with Path(path).open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def checked(root, value):
    path = Path(value)
    relative = path.relative_to(root / 'artifacts')
    if (path.resolve() != path or len(relative.parts) != 3 or not BUILD.fullmatch(relative.parts[0]) or
            relative.parts[1] != 'output' or relative.parts[2] not in ROLES):
        raise ValueError('component sharing path is outside its exact closed build output')
    info = path.lstat()
    if not stat.S_ISREG(info.st_mode):
        raise ValueError('component sharing requires regular immutable outputs')
    return path, info


def apply(root, plan):
    root = Path(root)
    if root.resolve() != root or plan.get('schema') != 1 or plan.get('root') != str(root):
        raise ValueError('component sharing root or plan differs')
    # Validate every target before the first replacement. Root is used only
    # for protected hardlinks to already root-owned build files, never chown,
    # permission changes, installation or host configuration.
    for row in plan['actions']:
        source, si = checked(root, row['source'])
        target, ti = checked(root, row['target'])
        for path in [source, target]:
            result_path = path.parent.parent / 'result.json'
            result = json.loads(result_path.read_text())
            if (result_path.is_symlink() or sha(result_path) != plan['results_sha256'].get(str(result_path)) or
                    result.get('passed') is not True or result.get('platform') != 'linux/amd64' or
                    path.name.startswith('sysbox-') and result['binary_sha256'].get(path.name) != row['sha256']):
                raise ValueError('component output lacks its exact completed build result')
        if (si.st_mode, si.st_uid, si.st_gid) != (ti.st_mode, ti.st_uid, ti.st_gid) or \
                list(reference.artifact_storage.identity(target)) != row['identity'] or \
                sha(source) != row['sha256'] or sha(target) != row['sha256']:
            raise ValueError('component outputs differ before immutable sharing')
    for row in plan['actions']:
        source, si = checked(root, row['source'])
        target, ti = checked(root, row['target'])
        if (si.st_mode, si.st_uid, si.st_gid) != (ti.st_mode, ti.st_uid, ti.st_gid) or \
                list(reference.artifact_storage.identity(target)) != row['identity'] or \
                sha(source) != row['sha256'] or sha(target) != row['sha256']:
            raise ValueError('component output changed immediately before sharing')
        temporary = target.with_name(target.name + '.sharing-' + uuid.uuid4().hex)
        try:
            os.link(source, temporary, follow_symlinks=False)
            os.replace(temporary, target)
        finally:
            temporary.unlink(missing_ok=True)
        fd = os.open(target.parent, os.O_RDONLY | os.O_DIRECTORY)
        try:
            os.fsync(fd)
        finally:
            os.close(fd)
        if not target.samefile(source) or sha(target) != row['sha256']:
            raise ValueError('shared component output differs')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--apply-plan', type=Path, help=argparse.SUPPRESS)
    args = parser.parse_args()
    root = reference.ROOT
    if args.apply_plan:
        plan_path = args.apply_plan.absolute()
        if plan_path.resolve() != plan_path or not plan_path.is_relative_to(root / 'artifacts'):
            raise ValueError('component plan must stay inside this workspace')
        apply(root, json.loads(plan_path.read_text()))
        return
    if reference.docker(['ps', '--filter', 'label=com.factorybox.run', '--format', '{{.ID}}']):
        raise ValueError('component sharing requires all native runs to close')
    before = reference.artifact_storage.require_capacity(root, run_reserve=0)
    artifact = root / 'artifacts' / ('factorybox-storage-share-components-' + uuid.uuid4().hex)
    artifact.mkdir()
    shutil.copyfile(__file__, artifact / 'runner.py')
    groups, results, binaries = {}, {}, []
    for run in sorted((root / 'artifacts').iterdir()):
        if not BUILD.fullmatch(run.name) or run.resolve() != run:
            continue
        result_path = run / 'result.json'
        if result_path.is_symlink() or not result_path.is_file():
            continue
        result = json.loads(result_path.read_text())
        if result.get('passed') is not True or result.get('platform') != 'linux/amd64':
            continue
        results[str(result_path)] = sha(result_path)
        for role in sorted(ROLES):
            path = run / 'output' / role
            if not path.exists():
                continue
            path, info = checked(root, str(path))
            digest = sha(path)
            if role.startswith('sysbox-') and result['binary_sha256'].get(role) != digest:
                raise ValueError('closed Sysbox component differs from its build result')
            if info.st_uid not in {0, os.getuid()} or info.st_mode & 0o777 != 0o755:
                raise ValueError('component output has unexpected owner or permissions')
            binaries.append({'path': str(path), 'sha256': digest})
            key = (role, digest, info.st_size, info.st_mode, info.st_uid, info.st_gid)
            groups.setdefault(key, []).append(path)
    actions = []
    for paths in groups.values():
        paths.sort(key=lambda p: -p.stat().st_nlink)
        source = paths[0]
        seen = {source.stat().st_ino}
        for target in paths[1:]:
            info = target.stat()
            if info.st_ino in seen:
                continue
            seen.add(info.st_ino)
            actions.append({'source': str(source), 'target': str(target), 'sha256': sha(target),
                            'identity': list(reference.artifact_storage.identity(target)),
                            'original_mtime_ns': info.st_mtime_ns, 'shared_mtime_ns': source.stat().st_mtime_ns})
    plan = {'schema': 1, 'root': str(root), 'actions': actions, 'results_sha256': results, 'all_output_sha256': binaries,
            'scope': 'Only identical regular closed Sysbox components and generated native test executables. Keep all original paths, bytes, modes, owners, build results and inputs. Record original/shared mtimes explicitly. No archive, active input, host setting or historical experiment changes.'}
    (artifact / 'plan.json').write_text(json.dumps(plan, indent=2) + '\n')
    print(json.dumps({'sharing': str(artifact), 'files': len(actions)}), flush=True)
    if actions:
        subprocess.run(['sudo', '-n', sys.executable, str(Path(__file__).resolve()), '--apply-plan', str(artifact / 'plan.json')], check=True)
    if any(sha(Path(name)) != digest for name, digest in results.items()) or \
            any(sha(row['path']) != row['sha256'] for row in binaries):
        raise ValueError('original component output bytes or build results changed')
    after = reference.artifact_storage.require_capacity(root, run_reserve=0)
    (artifact / 'result.json').write_text(json.dumps({'schema': 1, 'passed': True, 'plan_sha256': sha(artifact / 'plan.json'),
        'files_shared': len(actions), 'capacity_before': before, 'capacity_after': after,
        'reclaimed_allocated_bytes': before['allocated_bytes'] - after['allocated_bytes'],
        'all_outputs_and_build_results_unchanged': True, 'production_qualified': False}, indent=2) + '\n')
    print(json.dumps({'evidence': str(artifact), 'passed': True,
                     'reclaimed_allocated_bytes': before['allocated_bytes'] - after['allocated_bytes']}), flush=True)


if __name__ == '__main__':
    main()
