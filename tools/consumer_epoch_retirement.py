"""Replace direct source removal only in a new declared consumer test copy."""
import difflib
import json
import os
from pathlib import Path
import stat

import reference

FILE = 'tests/devx-lifecycle/worker_loss_linux_test.go'
OLD = '\tif err := os.RemoveAll(proof.SourceRoot); err != nil {\n\t\tt.Fatal("remove only old saved consumer controller", err)\n\t}\n'
NEW = '\trestored = retireConsumerEpoch(t, f, proof, restored)\n'


def apply_fixture(artifact, source):
    artifact, source = Path(artifact).absolute(), Path(source).absolute()
    if (artifact.resolve() != artifact or source.resolve() != source or not source.is_relative_to(artifact) or
            (artifact / 'result.json').exists() or (artifact / 'epoch-fixture.json').exists()):
        raise ValueError('epoch fixture requires a new canonical artifact copy')
    bounded = json.loads((artifact / 'bounded-fixture.json').read_text())
    rows = [row for row in bounded['files'] if row['file'] == FILE]
    target = source / FILE
    info = target.lstat()
    if (target.resolve() != target or not stat.S_ISREG(info.st_mode) or info.st_uid != os.getuid() or info.st_nlink != 1):
        raise ValueError('epoch fixture may edit only its unique owned copy')
    before = target.read_text()
    if (bounded.get('passed') is not True or len(rows) != 1 or
            reference.sha(target) != rows[0]['after_sha256'] or before.count(OLD) != 1):
        raise ValueError('epoch fixture differs from its exact bounded original')
    frozen = artifact / 'epoch-fixture-before.go'
    frozen.write_text(before)
    after = before.replace(OLD, NEW)
    patch = artifact / 'epoch-fixture.patch'
    patch.write_text(''.join(difflib.unified_diff(before.splitlines(True), after.splitlines(True),
                     fromfile='a/' + FILE, tofile='b/' + FILE)))
    target.write_text(after)
    receipt = {'schema': 1, 'passed': True, 'file': FILE, 'before_sha256': reference.sha(frozen),
               'after_sha256': reference.sha(target), 'patch_sha256': reference.sha(patch),
               'assertions_preserved': True, 'upstream_assertions_modified': False,
               'scope': 'Replace only direct old-source RemoveAll with the delivered public epoch-retirement protocol, '
                        'a committed independent successor, exact replay, all authorized payloads absent and useful resume. '
                        'Every other original consumer assertion remains byte-identical; vet stays enabled.'}
    (artifact / 'epoch-fixture.json').write_text(json.dumps(receipt, indent=2) + '\n')
    return receipt


def verify_output(output, source_root):
    proof = json.loads((Path(output) / 'consumer-epoch-retirement.json').read_text())
    flags = ['passed', 'used_registered_base_retired', 'original_ram_images_retired', 'all_authorized_payloads_absent',
             'base_provenance_preserved', 'caller_input_marker_preserved', 'request_replay_exact',
             'independent_successor_resumed_and_useful']
    if (any(proof.get(key) is not True for key in flags) or proof['report']['request']['source_root'] != source_root or
            not proof['report']['completed_at'] or len(proof['report']['acceptance_sha256']) != 64 or
            len(proof['payload_roots']) < 3 or 'bases/developer-base/rootfs' not in proof['payload_roots']):
        raise ValueError('consumer public epoch retirement lacks its complete independent successor proof')
    return proof
