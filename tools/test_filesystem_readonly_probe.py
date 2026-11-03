import copy
import unittest

import filesystem_readonly_probe as probe


class CommittedProbeInputs(unittest.TestCase):
    def setUp(self):
        self.sandbox = 'runtime-' + 'a' * 32
        self.snapshot = 'b' * 32
        self.status = {'lifecycle': {'root': '/var/lib/sysbox/fixed-store/data/store/epochs/' + 'c' * 32 + '/controller'}}
        self.record = {'id': self.sandbox, 'phase': 'paused', 'pause': {
            'snapshot_id': self.snapshot, 'completed_at': 'confirmed'}}
        self.prefix = (self.status['lifecycle']['root'] + '/objects/' + self.sandbox +
                       '/captures/' + self.snapshot + '/filesystem/')
        self.filesystem = {'roots': [
            {'name': name, 'saved': self.prefix + name, 'sha256': 'd' * 64}
            for name in ['upper', 'volumes/docker']]}

    def test_exact_capture_paths_and_digests_are_retained_without_mutation(self):
        before = copy.deepcopy(self.filesystem)
        inputs = probe.probe_inputs(self.status, self.sandbox, self.snapshot,
                                    self.record, self.filesystem)
        self.assertEqual(len(inputs), 2)
        self.assertEqual(inputs[0]['path'], '/proc/1/root' + self.prefix + 'upper')
        self.assertEqual(inputs[0]['expected'], before['roots'][0])
        self.assertEqual(self.filesystem, before)

    def test_running_changed_or_uncommitted_captures_are_refused(self):
        for mutate in [lambda r: r.update(phase='running'),
                       lambda r: r.update(id='another'),
                       lambda r: r['pause'].update(snapshot_id='e' * 32),
                       lambda r: r['pause'].update(completed_at='')]:
            record = copy.deepcopy(self.record)
            mutate(record)
            with self.assertRaisesRegex(ValueError, 'exact committed'):
                probe.probe_inputs(self.status, self.sandbox, self.snapshot, record, self.filesystem)

    def test_redirected_missing_or_borrowed_roots_are_refused(self):
        for mutate in [lambda f: f['roots'][0].update(saved='/foreign/upper'),
                       lambda f: f['roots'][0].update(sha256=''),
                       lambda f: f['roots'][0].update(immutable={'borrowed': True}),
                       lambda f: f['roots'].pop()]:
            filesystem = copy.deepcopy(self.filesystem)
            mutate(filesystem)
            with self.assertRaisesRegex(ValueError, 'tree|roots'):
                probe.probe_inputs(self.status, self.sandbox, self.snapshot, self.record, filesystem)


if __name__ == '__main__':
    unittest.main()
