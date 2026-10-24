"""Add storage-pressure assertions only to a new declared consumer fixture."""
import difflib
import json
import os
from pathlib import Path
import stat

import consumer_epoch_retirement
import reference

FILE = consumer_epoch_retirement.FILE
OLD = '\tstart := time.Now()\n\trestored := f.view(request...)\n'
NEW = '\tcheckConsumerRecoveryPressure(t, f, proof, request)\n' + OLD


def apply_fixture(artifact, source):
    artifact, source = Path(artifact).absolute(), Path(source).absolute()
    if (artifact.resolve() != artifact or source.resolve() != source or not source.is_relative_to(artifact) or
            (artifact / 'result.json').exists() or (artifact / 'recovery-pressure-fixture.json').exists()):
        raise ValueError('recovery pressure requires a new canonical consumer copy')
    receipt = json.loads((artifact / 'epoch-fixture.json').read_text())
    target = source / FILE
    info = target.lstat()
    before = target.read_text()
    if (target.resolve() != target or not stat.S_ISREG(info.st_mode) or info.st_uid != os.getuid() or
            info.st_nlink != 1 or receipt.get('passed') is not True or
            reference.sha(target) != receipt['after_sha256'] or before.count(OLD) != 1):
        raise ValueError('recovery pressure differs from the exact ordered epoch fixture')
    frozen = artifact / 'recovery-pressure-before.go'
    frozen.write_text(before)
    after = before.replace(OLD, NEW)
    patch = artifact / 'recovery-pressure.patch'
    patch.write_text(''.join(difflib.unified_diff(before.splitlines(True), after.splitlines(True),
                     fromfile='a/' + FILE, tofile='b/' + FILE)))
    target.write_text(after)
    record = {'schema': 1, 'passed': True, 'file': FILE, 'before_sha256': reference.sha(frozen),
        'after_sha256': reference.sha(target), 'patch_sha256': reference.sha(patch),
        'assertions_preserved': True, 'upstream_assertions_modified': False,
        'scope': 'Add actual post-admission recovery ENOSPC, unchanged original retained checkpoint, '
                 'exact incomplete-request refusal, full partial-input preservation and exact owned pressure release. '
                 'Every original complete consumer/epoch assertion remains byte-identical; vet and deadlines remain.'}
    (artifact / 'recovery-pressure-fixture.json').write_text(json.dumps(record, indent=2) + '\n')
    return record


def verify_output(output, source_root):
    record = json.loads((Path(output) / 'recovery-pressure.json').read_text())
    required = ['passed', 'admitted_before_copy', 'actual_import_enospc', 'original_metadata_unchanged',
                'old_checkpoint_fully_verified_under_pressure', 'incomplete_target_refused',
                'partial_inventory_unchanged_by_retry', 'partial_retained_after_full_consumer',
                'partial_inventory_preserved_at_cleanup', 'full_original_consumer_passed',
                'only_exact_owned_pressure_released', 'admission_restored']
    if (any(record.get(key) is not True for key in required) or record.get('source_root') != source_root or
            'no space left on device' not in record.get('import_error', '').lower() or
            record['before']['bounded_image_bytes'] != 32 << 30 or
            record['under_pressure']['new_write_admitted'] is not True or
            record['after_reclaim']['new_write_admitted'] is not True or
            record['inspection']['full_content_verified'] is not True or
            record['inspection']['files'] <= 0 or record['inspection']['filesystem_trees'] != 9 or
            not record['partial_inventory']['entries']):
        raise ValueError('consumer recovery pressure lacks actual ENOSPC, intact retained state or safe reclaim')
    return record
