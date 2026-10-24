import copy
import json
from pathlib import Path
import tempfile
import unittest

import bounded_store_loss


class InitialImportPressureEvidence(unittest.TestCase):
    def proof(self):
        value = {key: True for key in [
            'passed', 'full_original_recovery_assertions_passed', 'admitted_before_publication',
            'actual_initial_publication_enospc', 'initial_metadata_only', 'explicit_cancel_without_new_inode',
            'missing_entry_replay_refused', 'recreated_generation_refused', 'partial_publication_cancelled',
            'original_backing_unchanged', 'only_exact_owned_pressure_released', 'full_original_recovery_follows']}
        value.update(request={'source_root': '/owned/old'}, source_root='/owned/current',
            durable_reservation_created=False, inodes_before_import=1, inodes_at_cancellation=0,
            partial_inodes_at_cancellation=0,
            initial_inode_count=10, pressure_files_released=8, inodes_after_reclaim=11,
            error='checkpoint publication: no space left on device',
            first_inspection={'reservation_absent': True, 'source_root': '/owned/current',
                'target_id': 'initial-import', 'owner': '', 'preparation_sha256': '',
                'inventory': {'paths': ['forks/initial-import'], 'entries': [{'path': 'forks/initial-import'}]}},
            first_cancellation={'reservation_absent': True, 'target_id': 'initial-import', 'completed_at': 'completed'})
        return value

    def verify(self, directory, proof):
        (Path(directory) / 'recovery-import-initial.json').write_text(json.dumps(proof))
        return bounded_store_loss.verify_initial_import_pressure(Path(directory),
            {'request': {'source_root': '/owned/old'}},
            {'status': {'lifecycle': {'root': '/owned/current'}}})

    def test_complete_real_boundary_and_full_recovery_are_required(self):
        with tempfile.TemporaryDirectory() as directory:
            value = self.proof()
            self.assertEqual(self.verify(directory, value), value)
            for key in [key for key, value in value.items() if value is True]:
                with self.subTest(key=key):
                    changed = dict(value, **{key: False})
                    with self.assertRaises(ValueError):
                        self.verify(directory, changed)

    def test_partial_capacity_or_fabricated_reservation_cannot_promote_success(self):
        with tempfile.TemporaryDirectory() as directory:
            for key, changed_value in [
                ('inodes_before_import', 2), ('inodes_before_import', True),
                ('inodes_at_cancellation', 1), ('inodes_after_reclaim', 9),
                ('partial_inodes_at_cancellation', 1),
                ('inodes_after_reclaim', 'unknown'), ('pressure_files_released', 0),
                ('durable_reservation_created', True), ('source_root', '/foreign'),
                ('request', {'source_root': '/foreign'}), ('error', 'permission denied'),
            ]:
                with self.subTest(key=key, value=changed_value), self.assertRaises(ValueError):
                    self.verify(directory, dict(self.proof(), **{key: changed_value}))
            for key, changed_value in [('reservation_absent', False), ('owner', 'invented'),
                                       ('preparation_sha256', 'invented'), ('target_id', 'foreign')]:
                with self.subTest(inspection=key):
                    value = copy.deepcopy(self.proof())
                    value['first_inspection'][key] = changed_value
                    with self.assertRaises(ValueError):
                        self.verify(directory, value)
            value = self.proof()
            value['first_inspection']['inventory']['entries'].append({'path': 'forks/initial-import/recovery'})
            with self.assertRaises(ValueError):
                self.verify(directory, value)


if __name__ == '__main__':
    unittest.main()
