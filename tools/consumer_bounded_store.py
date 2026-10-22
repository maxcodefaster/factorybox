"""Relocate new consumer-fixture copies into the delivered bounded store."""
import difflib
import hashlib
import json
import os
from pathlib import Path
import shutil
import stat
import uuid

import reference

BOUNDED_ROOT = '/var/lib/sysbox/fixed-store'
STORE = BOUNDED_ROOT + '/data/store'
DECLARATION = 'tests/compatibility/consumer-bounded-paths.json'
SOURCE_FILES = {'tests/devx-lifecycle/lifecycle_linux_test.go', 'tests/devx-lifecycle/warm_preparation_probe_linux_test.go',
                'tests/devx-lifecycle/worker_loss_linux_test.go'}


def apply_fixture(artifact, source, features):
    artifact, source = Path(artifact).absolute(), Path(source).absolute()
    if artifact.resolve() != artifact or source.resolve() != source or not source.is_relative_to(artifact):
        raise ValueError('bounded fixture must use a new canonical artifact copy')
    if (artifact / 'result.json').exists() or (artifact / 'bounded-fixture.json').exists():
        raise ValueError('closed or already patched fixture cannot be rewritten')
    declaration = reference.ROOT / DECLARATION
    value = json.loads(declaration.read_text())
    if (value['schema'] != 1 or not value['assertions_preserved'] or value['upstream_assertions_modified'] or
            value['worker_bounded_root'] != BOUNDED_ROOT or value['worker_store'] != STORE):
        raise ValueError('bounded fixture declaration changed its scope')
    chosen = [row for row in value['files'] if row['features'] is None or row['features'] is features]
    if {r['file'] for r in chosen} != SOURCE_FILES | {'tests/consumer-worker-setup.sh'} or len(chosen) != 4:
        raise ValueError('bounded fixture selection differs')
    plan = []
    for row in chosen:
        name = row['file']
        target = source / name if name in SOURCE_FILES else artifact / 'consumer-worker-setup.sh'
        if target.is_symlink() or target.resolve() != target or not target.is_relative_to(artifact):
            raise ValueError('bounded fixture target is redirected')
        info = target.stat()
        if not stat.S_ISREG(info.st_mode) or info.st_uid != os.getuid() or info.st_nlink != 1:
            raise ValueError('bounded fixture may edit only unique owned copies')
        before = target.read_bytes()
        if reference.sha(target) != row['before_sha256'] or before.count(row['old_path'].encode()) != row['replacements']:
            raise ValueError('bounded fixture before bytes differ: ' + name)
        after = before.replace(row['old_path'].encode(), row['new_path'].encode())
        if hashlib.sha256(after).hexdigest() != row['after_sha256']:
            raise ValueError('bounded fixture after bytes differ: ' + name)
        plan.append((row, target, info.st_mode, before, after))
    records = []
    for number, (row, target, mode, before, after) in enumerate(plan, 1):
        frozen = artifact / 'bounded-fixture-before' / row['file']
        frozen.parent.mkdir(parents=True, exist_ok=True)
        with frozen.open('xb') as stream:
            stream.write(before)
        patch = artifact / f'bounded-fixture-{number:02d}.patch'
        patch.write_text(''.join(difflib.unified_diff(before.decode().splitlines(True), after.decode().splitlines(True),
                         fromfile='a/' + row['file'], tofile='b/' + row['file'])))
        temporary = target.with_name(target.name + '.bounded-' + uuid.uuid4().hex)
        try:
            with temporary.open('xb') as stream:
                stream.write(after)
            temporary.chmod(stat.S_IMODE(mode))
            os.replace(temporary, target)
        finally:
            temporary.unlink(missing_ok=True)
        records.append({**row, 'patch': patch.name, 'patch_sha256': reference.sha(patch),
                        'before_file': str(frozen.relative_to(artifact))})
    shutil.copyfile(declaration, artifact / 'consumer-bounded-paths.json')
    shutil.copyfile(Path(__file__), artifact / 'consumer-bounded-store.py')
    receipt = {'schema': 1, 'passed': True, 'assertions_preserved': True, 'upstream_assertions_modified': False,
               'declaration_sha256': reference.sha(declaration), 'files': records, 'scope': value['scope']}
    (artifact / 'bounded-fixture.json').write_text(json.dumps(receipt, indent=2) + '\n')
    return receipt


def verify_capacity(status, capacity, expected_bytes, owner):
    active = status['lifecycle'].get('bounded_store')
    if (not active or status['store'] != STORE or status['store_id'] != owner or
            active['target'] != BOUNDED_ROOT + '/data' or active['image']['path'] != BOUNDED_ROOT + '/store.img' or
            active['image']['owner'] != owner or active['image']['bytes'] != expected_bytes or not active['direct_io']):
        raise ValueError('consumer worker lacks its exact bounded-store identity')
    if (capacity.get('bounded_image_bytes') != expected_bytes or capacity['root'] != status['lifecycle']['root'] or
            not capacity['policy_enabled'] or capacity['min_free_bytes'] != 64 << 20 or capacity['write_reserve_bytes'] != 128 << 20 or
            not 0 < capacity['filesystem_total_bytes'] <= expected_bytes or
            capacity['directory']['device'] != active['block_device'] or not capacity.get('host') or
            capacity['host']['directory']['device'] == active['block_device']):
        raise ValueError('consumer capacity lacks separate bounded and host observations')
    return {'schema': 1, 'passed': True, 'active': active, 'capacity': capacity}


def verify_source(view):
    record = view['record']
    bundle = STORE + '/devx/bundle'
    if record['input_bundle'] != bundle or record['rootfs'] != bundle + '/rootfs' or record['phase'] != 'running':
        raise ValueError('full consumer source is outside the admitted bounded bundle')
    return {'schema': 1, 'passed': True, 'input_bundle': record['input_bundle'], 'rootfs': record['rootfs'],
            'scope': 'Delivered Start accepted this internal overlay layout; its bounded bundle/device admission remains enabled.'}
