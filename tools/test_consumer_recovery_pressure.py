import json
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

import consumer_bounded_store
import consumer_epoch_retirement
import consumer_recovery_pressure as pressure
import reference


class RecoveryPressureFixture(unittest.TestCase):
    def fixture(self, temporary):
        artifact = Path(temporary) / 'run'
        source = artifact / 'source'
        for name in consumer_bounded_store.SOURCE_FILES:
            target = source / name
            target.parent.mkdir(parents=True, exist_ok=True)
            shutil.copyfile(reference.ROOT / name, target)
        shutil.copyfile(reference.ROOT / 'tests/consumer-worker-setup.sh', artifact / 'consumer-worker-setup.sh')
        consumer_bounded_store.apply_fixture(artifact, source, False)
        consumer_epoch_retirement.apply_fixture(artifact, source)
        return artifact, source

    def test_additive_ordered_patch_preserves_every_original_byte(self):
        original = reference.sha(reference.ROOT / pressure.FILE)
        with tempfile.TemporaryDirectory() as temporary:
            artifact, source = self.fixture(temporary)
            before = (source / pressure.FILE).read_text()
            receipt = pressure.apply_fixture(artifact, source)
            after = (source / pressure.FILE).read_text()
            self.assertTrue(receipt['assertions_preserved'])
            self.assertFalse(receipt['upstream_assertions_modified'])
            self.assertEqual(after.replace(pressure.NEW, pressure.OLD).replace(pressure.CANCEL, pressure.RETIRE), before)
            self.assertLess(after.index('worker loss changed complete minimal RAM/files'), after.index('cancelRecoveryImport()'))
            reconstruction = artifact / 'reconstruction'
            target = reconstruction / pressure.FILE
            target.parent.mkdir(parents=True)
            shutil.copyfile(artifact / 'recovery-pressure-before.go', target)
            subprocess.run(['git', 'apply', '--unsafe-paths', '--directory=' + str(reconstruction),
                            str(artifact / 'recovery-pressure.patch')], check=True, capture_output=True)
            self.assertEqual(target.read_text(), after)
            self.assertEqual(reference.sha(target), receipt['after_sha256'])
        self.assertEqual(reference.sha(reference.ROOT / pressure.FILE), original)

    def test_changed_assertion_and_closed_run_refuse_without_mutation(self):
        with tempfile.TemporaryDirectory() as temporary:
            artifact, source = self.fixture(temporary)
            target = source / pressure.FILE
            before = target.read_text().replace('restored.Record.Owner != replayed.Record.Owner', 'false')
            target.write_text(before)
            with self.assertRaisesRegex(ValueError, 'exact ordered epoch fixture'):
                pressure.apply_fixture(artifact, source)
            self.assertEqual(target.read_text(), before)
            self.assertFalse((artifact / 'recovery-pressure-fixture.json').exists())
            (artifact / 'result.json').write_text('{}')
            with self.assertRaisesRegex(ValueError, 'new canonical consumer copy'):
                pressure.apply_fixture(artifact, source)

    def test_partial_or_preflight_only_outcome_is_not_useful_recovery(self):
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / 'recovery-pressure.json'
            path.write_text(json.dumps({'passed': True, 'admitted_before_copy': True,
                                        'actual_import_enospc': True, 'source_root': '/old'}))
            with self.assertRaisesRegex(ValueError, 'actual ENOSPC, intact retained state or safe reclaim'):
                pressure.verify_output(temporary, '/old')

    def test_successful_pressure_subprobe_cannot_promote_a_failed_full_consumer(self):
        with tempfile.TemporaryDirectory() as temporary:
            value = {key: True for key in ['passed', 'admitted_before_copy', 'actual_import_enospc',
                'original_metadata_unchanged', 'old_checkpoint_fully_verified_under_pressure',
                'incomplete_target_refused', 'partial_inventory_unchanged_by_retry',
                'partial_retained_through_useful_recovery', 'explicit_cancellation_completed',
                'cancellation_replayed_exactly', 'cancellation_under_full_store',
                'cancelled_reservation_preserved_at_cleanup',
                'only_exact_owned_pressure_released', 'admission_restored']}
            value.update(source_root='/old', full_original_consumer_passed=False)
            (Path(temporary) / 'recovery-pressure.json').write_text(json.dumps(value))
            with self.assertRaisesRegex(ValueError, 'actual ENOSPC, intact retained state or safe reclaim'):
                pressure.verify_output(temporary, '/old')
