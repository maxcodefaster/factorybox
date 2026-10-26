import unittest

from read_failed_consumer_image import failure_image_bytes


class FailureImageTest(unittest.TestCase):
    def test_full_consumer_requires_a_closed_committed_pause(self):
        name = 'factorybox-factory-consumer-' + 'a' * 32
        result = {'passed': False, 'cleanup': True,
                  'pause': {'record': {'pause': {'completed_at': 'committed'}}}}
        self.assertEqual(failure_image_bytes(name, result), 8 << 30)
        for change in [{'passed': True}, {'cleanup': False}, {'pause': {}}]:
            with self.assertRaises(ValueError):
                failure_image_bytes(name, {**result, **change})

    def test_peer_store_size_and_committed_pause_are_exact(self):
        name = 'factorybox-checkpoint-retirement-' + 'b' * 32
        result = {'passed': False, 'cleanup': True, 'suite': 'standby-peers',
                  'command': ['run', 'worker-image', '--bounded-bytes', str(1 << 30)]}
        proof = {'original_pause': {'record': {'pause': {'completed_at': 'committed'}}}}
        self.assertEqual(failure_image_bytes(name, result, proof), 1 << 30)
        for change in [{'suite': 'retirement'}, {'command': ['--bounded-bytes', str(8 << 30)]},
                       {'command': ['--bounded-bytes', str(1 << 30), '--bounded-bytes', str(1 << 30)]}]:
            with self.assertRaises(ValueError):
                failure_image_bytes(name, {**result, **change}, proof)
        with self.assertRaises(ValueError):
            failure_image_bytes(name, result, {})

    def test_other_names_never_select_a_path(self):
        for name in ['../foreign', 'factorybox-checkpoint-retirement-' + 'b' * 31,
                     'factorybox-checkpoint-retirement-' + 'B' * 32]:
            with self.assertRaises(ValueError):
                failure_image_bytes(name, {'passed': False, 'cleanup': True}, {})
