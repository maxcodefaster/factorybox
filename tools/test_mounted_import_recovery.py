import copy
import json
from pathlib import Path
import tempfile
import unittest

import bounded_store_loss


class MountedImportRecoveryEvidence(unittest.TestCase):
    def fixture(self):
        interrupted = {key: True for key in ['passed', 'exact_copy_child_stopped', 'exact_controller_sigkill',
            'mounted_inspection_refused', 'intent_and_target_absent']}
        interrupted.update(request={'source_root': '/old'}, source_root='/interrupted', target_id='mounted-import',
            controller_pid=42, controller_birth='123', mount={'mount_id': 13}, attachment={'mount': {'mount_id': 7}},
            mounted_inspection_error='owned tree crosses foreign mount: recovery/input/base/rootfs')
        proof = {key: True for key in ['passed', 'full_original_recovery_assertions_passed',
            'fenced_retained_controller_cancelled', 'permanent_reservation_unchanged', 'original_backing_unchanged',
            'exact_cancellation_replay', 'full_original_recovery_follows']}
        proof.update(interruption=copy.deepcopy(interrupted), current_root='/current',
            retained_inspection={'source_root': '/interrupted', 'target_id': 'mounted-import'})
        return interrupted, proof

    def verify(self, directory, interrupted, proof):
        path = Path(directory)
        (path / 'recovery-import-mounted-interrupted.json').write_text(json.dumps(interrupted))
        (path / 'recovery-import-mounted-recovered.json').write_text(json.dumps(proof))
        return bounded_store_loss.verify_mounted_import_recovery(path, {'request': {'source_root': '/old'}},
            {'status': {'lifecycle': {'root': '/current'}}})

    def test_interruption_and_full_original_success_are_both_required(self):
        with tempfile.TemporaryDirectory() as directory:
            interrupted, proof = self.fixture()
            self.assertEqual(self.verify(directory, interrupted, proof), proof)
            for key in [key for key, value in proof.items() if value is True]:
                with self.subTest(proof=key), self.assertRaises(ValueError):
                    self.verify(directory, interrupted, dict(proof, **{key: False}))
            for key in [key for key, value in interrupted.items() if value is True]:
                with self.subTest(interruption=key), self.assertRaises(ValueError):
                    changed = dict(interrupted, **{key: False})
                    self.verify(directory, changed, dict(proof, interruption=changed))

    def test_wrong_worker_mount_and_initial_stage_cannot_promote_success(self):
        with tempfile.TemporaryDirectory() as directory:
            for key, value in [('mount', {'mount_id': 7}), ('mount', {}), ('controller_pid', True),
                               ('controller_pid', 1), ('controller_birth', ''), ('target_id', 'foreign'),
                               ('mounted_inspection_error', 'unrelated metadata failure'),
                               ('request', {'source_root': '/foreign'}), ('source_root', '/current')]:
                interrupted, proof = self.fixture()
                interrupted[key] = value
                proof['interruption'] = copy.deepcopy(interrupted)
                if key == 'source_root':
                    proof['retained_inspection']['source_root'] = value
                with self.subTest(interruption=key), self.assertRaises(ValueError):
                    self.verify(directory, interrupted, proof)
            for key, value in [('source_root', '/foreign'), ('target_id', 'foreign'), ('reservation_absent', True)]:
                interrupted, proof = self.fixture()
                proof['retained_inspection'][key] = value
                with self.subTest(inspection=key), self.assertRaises(ValueError):
                    self.verify(directory, interrupted, proof)


if __name__ == '__main__':
    unittest.main()
