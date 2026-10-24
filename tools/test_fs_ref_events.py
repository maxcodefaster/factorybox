import unittest

import fs_ref_events


def event(ns, operation, *, fs=10, generation=1, root=20, pwd=30, result=0):
    unmount = operation >= 6
    return fs_ref_events.EVENT.pack(ns, 0 if unmount else fs, root, 0 if unmount else pwd,
        0 if unmount else generation, 100 << 32 | 101, 2, 8, 0 if unmount else 9,
        operation, result, b'owned-control')


class ContextLifetimeTests(unittest.TestCase):
    def test_reused_context_generation_survives_old_free_return(self):
        data = b''.join([
            event(1, 1), event(2, 4),
            event(3, 1, generation=2, root=40, pwd=50),
            event(4, 5),
            event(5, 6, root=50), event(6, 7, root=50, result=-16)])
        summary = fs_ref_events.summarize(fs_ref_events.decode(data))
        held = summary['unmounts'][0]['observed_contexts_at_return']
        self.assertEqual([(row['generation'], row['matching_references']) for row in held], [(2, ['pwd'])])
        self.assertFalse(summary['complete_mount_reference_inventory'])

    def test_chdir_does_not_release_a_root_reference(self):
        data = b''.join([
            event(1, 1, root=30), event(2, 2, root=30, pwd=40),
            event(3, 6, root=30), event(4, 7, root=30, result=-16),
            event(5, 4, root=30, pwd=40), event(6, 5, root=30, pwd=40),
            event(7, 6, root=30), event(8, 7, root=30)])
        summary = fs_ref_events.summarize(fs_ref_events.decode(data))
        self.assertEqual(summary['unmounts'][0]['observed_contexts_at_return'][0]['matching_references'], ['root'])
        self.assertEqual(summary['unmounts'][1]['observed_contexts_at_return'], [])


if __name__ == '__main__':
    unittest.main()
