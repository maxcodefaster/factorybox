#!/usr/bin/env python3
"""Build an unselected CRIU delta for live same-worker file descriptions."""
import argparse
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import uuid

import criu_candidate
import native_base
import reference
import sources


PATCH = reference.ROOT / 'upstream/patches/criu-shared-fds-candidate.patch'


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--base-candidate', required=True)
    parser.add_argument('--prepared', help='One new, unbuilt preparation from this task; never a completed input')
    args = parser.parse_args()
    original, previous, _, _ = criu_candidate.load(args.base_candidate)
    changed = [line.removeprefix('+++ b/') for line in PATCH.read_text().splitlines() if line.startswith('+++ b/')]
    if len(changed) != 8 or len(set(changed)) != 8 or any(not value.startswith('criu/') or '..' in Path(value).parts for value in changed):
        raise ValueError('shared-description delta changed an undeclared source boundary')
    if args.prepared:
        if not re.fullmatch(r'factorybox-build-criu-retained-[0-9a-f]{32}', args.prepared):
            raise ValueError('requires one exact private preparation')
        artifact = reference.ROOT / 'artifacts' / args.prepared
        preparation = json.loads((artifact / 'preparation.json').read_text())
        if (artifact.resolve() != artifact or (artifact / 'intent.json').exists() or (artifact / 'result.json').exists() or
                preparation['base_candidate'] != original.name or preparation['base_result_sha256'] != reference.sha(original / 'result.json') or
                set(preparation['changed_files']) != set(changed)):
            raise ValueError('prepared source is completed, redirected or belongs to different inputs')
    else:
        artifact = reference.new_artifact('factorybox-build-criu-retained-' + uuid.uuid4().hex)
        artifact.mkdir()
        shutil.copy2(PATCH, artifact / 'shared-fds.patch')
        shutil.copyfile(__file__, artifact / 'runner.py')
        preparation = {'schema': 1, 'base_candidate': original.name,
                       'base_result_sha256': reference.sha(original / 'result.json'),
                       'changed_files': changed, 'patch_sha256': reference.sha(artifact / 'shared-fds.patch')}
        (artifact / 'preparation.json').write_text(json.dumps(preparation, indent=2) + '\n')
        def copy(source, target):
            if str(Path(source).relative_to(original / 'context')) in changed:
                return shutil.copy2(source, target)
            os.link(source, target)
            return target
        shutil.copytree(original / 'context', artifact / 'context', copy_function=copy, symlinks=True)
        try:
            patched = subprocess.run(['patch', '--batch', '--forward', '--fuzz=0', '-p1', '-i', str(artifact / 'shared-fds.patch')],
                                     cwd=artifact / 'context', capture_output=True, text=True, timeout=30)
            (artifact / 'preparation-patch.log').write_text(patched.stdout + patched.stderr)
            patched.check_returncode()
        except Exception as error:
            (artifact / 'preparation-error.json').write_text(json.dumps({'schema': 1, 'passed': False,
                'error': str(error), 'native_started': False, 'base_candidate': original.name}, indent=2) + '\n')
            raise
    # Independently reconstruct the exact ordered delta using only independent
    # copies of its eight targets. Unchanged archived sources are never edited.
    reconstruction = artifact / 'shared-fds-reconstruction'
    reconstruction.mkdir()
    before = {}
    for name in changed:
        source = original / 'context' / name
        before[name] = reference.sha(source)
        target = reconstruction / name
        target.parent.mkdir(parents=True, exist_ok=True)
        shutil.copy2(source, target)
    reconstructed = subprocess.run(['patch', '--batch', '--forward', '--fuzz=0', '-p1', '-i', str(PATCH)],
                                   cwd=reconstruction, capture_output=True, text=True, timeout=30)
    (artifact / 'shared-fds-patch.log').write_text(reconstructed.stdout + reconstructed.stderr)
    reconstructed.check_returncode()
    after = {name: reference.sha(artifact / 'context' / name) for name in changed}
    if any(reference.sha(reconstruction / name) != after[name] for name in changed):
        raise ValueError('prepared source differs from the reviewed ordered patch')
    for name in ['retained.patch', *previous['candidate_patch_sha256'], *previous.get('candidate_overlay_sha256', {}), 'criu-build.json']:
        target = artifact / name
        if not target.exists():
            shutil.copy2(original / name, target)
    if not (artifact / 'shared-fds.patch').exists():
        shutil.copy2(PATCH, artifact / 'shared-fds.patch')
        shutil.copyfile(__file__, artifact / 'runner.py')
    if reference.sha(artifact / 'shared-fds.patch') != reference.sha(PATCH):
        raise ValueError('frozen shared-description patch differs from the current preparation')
    shutil.copyfile(reference.ROOT / 'tests/criu-owned-scope.c', artifact / 'criu-owned-scope.c')
    lock = json.loads((original / 'source-lock.json').read_text())
    component = lock['components']['criu']
    relative = str(PATCH.relative_to(reference.ROOT))
    if relative in component['patches']:
        raise ValueError('shared-description delta is already applied')
    component['patches'].append(relative)
    component['local_inputs_sha256'][relative] = reference.sha(PATCH)
    component['prepared_files_sha256'].update(after)
    (artifact / 'source-lock.json').write_text(json.dumps(lock, indent=2) + '\n')
    sources.validate_inputs(component)
    unchanged_tests = sources.check_source(artifact / 'context', reference.ROOT / '.cache/downloads' / component['archive'], component)
    if unchanged_tests != previous['unchanged_upstream_test_files']:
        raise ValueError('shared-description delta changed an upstream assertion')
    declaration = {'schema': 1, 'before_sha256': before, 'after_sha256': after,
                   'patch_sha256': reference.sha(PATCH), 'upstream_assertions_unchanged': True}
    (artifact / 'shared-fds-source.json').write_text(json.dumps(declaration, indent=2) + '\n')
    builder, base = native_base.load(previous['native_base_build'], for_build=True)
    owner = artifact.name.rsplit('-', 1)[1]
    epoch = json.loads((artifact / 'criu-build.json').read_text())['source_date_epoch']
    command = [*reference.DOCKER, 'run', '--pull=never', '--name', artifact.name,
               '--user', f'{os.getuid()}:{os.getgid()}', '--label', 'com.factorybox.run=' + owner,
               '--cpus=2', '--memory=1g', '--pids-limit=128', '--network=none', '--read-only',
               '--cgroupns=private', '--cap-drop=ALL', '--security-opt=no-new-privileges',
               '--env', 'SOURCE_DATE_EPOCH=' + str(epoch), '--tmpfs', '/tmp:rw,exec,nosuid,nodev,size=384m',
               '--mount', f'type=bind,source={artifact / "context"},target=/source,readonly',
               '--mount', f'type=bind,source={artifact},target=/output', '--entrypoint', '/bin/sh', builder, '-ec',
               'cc --version > /output/toolchain.txt\nmake --version >> /output/toolchain.txt\n'
               'cc -std=c11 -Wall -Wextra -Werror -iquote /source/criu/include /output/criu-owned-scope.c -o /tmp/scope\n'
               '/tmp/scope > /output/owned-scope.log 2>&1\ncp -R /source /tmp/criu-source\n'
               'make -C /tmp/criu-source -j2 criu/criu > /output/build.log 2>&1\n'
               'cp /tmp/criu-source/criu/criu /output/criu\n/output/criu --version > /output/version.txt\n'
               '/output/criu factorybox-features > /output/features.json\n']
    result = {'schema': 1, 'owner': owner, 'passed': False, 'cleanup': False, 'diagnostic_only': True,
              'production_qualified': False, 'native_lifecycle_qualified': False, 'platform': base['platform'],
              'base_candidate': original.name, 'base_candidate_result_sha256': reference.sha(original / 'result.json'),
              'native_base_build': previous['native_base_build'], 'builder': builder, 'command': command,
              'base_source_lock_sha256': previous['base_source_lock_sha256'],
              'source_lock_sha256': reference.sha(artifact / 'source-lock.json'),
              'patch_sha256': reference.sha(artifact / 'retained.patch'),
              'candidate_patch_sha256': {**previous['candidate_patch_sha256'], 'shared-fds.patch': reference.sha(PATCH)},
              'candidate_overlay_sha256': previous.get('candidate_overlay_sha256', {}),
              'context_sha256': sources.tree_sha(artifact / 'context'),
              'build_inputs_sha256': reference.sha(artifact / 'criu-build.json'),
              'scope_test_sha256': reference.sha(artifact / 'criu-owned-scope.c'),
              'native_base_packages_sha256': base['packages_sha256'], 'unchanged_upstream_test_files': unchanged_tests,
              'shared_fds_source_sha256': reference.sha(artifact / 'shared-fds-source.json'),
              'scope': __doc__}
    (artifact / 'intent.json').write_text(json.dumps(result, indent=2) + '\n')
    attempted = False
    print(json.dumps({'building': str(artifact), 'unchanged_upstream_tests': unchanged_tests}), flush=True)
    try:
        attempted = True
        completed = subprocess.run(command, capture_output=True, text=True, timeout=300)
        (artifact / 'stdout.txt').write_text(completed.stdout)
        (artifact / 'stderr.txt').write_text(completed.stderr)
        result['exit_code'] = completed.returncode
        result['container_state'] = json.loads(reference.docker(['container', 'inspect', artifact.name]))[0]['State']
        completed.check_returncode()
        if result['container_state']['OOMKilled']:
            raise ValueError('shared-description builder exceeded its memory bound')
        result['scope_gate_passed'] = 'owned-scope-cases=31 failures=0' in (artifact / 'owned-scope.log').read_text()
        result['features'] = json.loads((artifact / 'features.json').read_text())
        result['inputs_unchanged'] = (sources.tree_sha(artifact / 'context') == result['context_sha256'] and
                                      sources.tree_sha(original / 'context') == previous['context_sha256'])
        if not result['scope_gate_passed'] or not result['inputs_unchanged'] or result['features'].get('shared_file_descriptions') is not True:
            raise ValueError('shared-description build recipe or feature proof differed')
        result['binary_sha256'] = reference.sha(artifact / 'criu')
        result['passed'] = True
    except Exception as error:
        result['error'] = str(error)
        raise
    finally:
        try:
            if attempted:
                reference.owned_delete('container', artifact.name, owner)
            result['cleanup'] = True
        finally:
            result['passed'] = result['passed'] and result['cleanup']
            (artifact / 'result.json').write_text(json.dumps(result, indent=2) + '\n')
            print(json.dumps({'evidence': str(artifact), 'passed': result['passed'], 'cleanup': result['cleanup']}), flush=True)


if __name__ == '__main__':
    main()
