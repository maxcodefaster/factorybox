import unittest

from consumer_cgroup_observer import validate_request


class CgroupObserverTest(unittest.TestCase):
    def test_only_original_positive_kernel_identities_are_accepted(self):
        request = {'runtime_id': 'fbx-'+'a'*32, 'observer_id': 'b'*32,
                   'cgroup': {'device': 32, 'inode': 123, 'mount_id': 456}}
        self.assertEqual(validate_request(request), request)
        for invalid in [None, [], 'foreign']:
            with self.assertRaises(ValueError):
                validate_request(invalid)
        for patch in [{'runtime_id': '../foreign'}, {'observer_id': 'foreign'},
                      {'cgroup': {'device': 32, 'inode': True, 'mount_id': 456}},
                      {'cgroup': {'device': 32, 'inode': 123, 'mount_id': 0}},
                      {'write_freezer': True}]:
            with self.assertRaises(ValueError):
                validate_request({**request, **patch})
