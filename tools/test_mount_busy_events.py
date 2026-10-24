import unittest

import mount_busy_events as trace


def event(operation, *, result=0, candidate=100, root=100, reserved=0, at=1000):
    return trace.EVENT.pack(at, 900, 12, root, candidate, 50,
                            7, 10, candidate // 10, 33, operation, result, 2,
                            0, 0, reserved, 0, b'control')


def sequence(*operations):
    return b''.join(event(item, at=1000 + index) if isinstance(item, int) else
                    event(item[0], at=1000 + index, **item[1])
                    for index, item in enumerate(operations))


class BusyEvents(unittest.TestCase):
    def test_complete_busy_call_retains_actual_count_and_errno(self):
        data = sequence(1, 3, (5, {'result': 3}), (4, {'result': 1}), (2, {'result': -16}))
        observed = trace.summarize(trace.decode(data))['unmounts'][0]
        self.assertEqual(observed['result'], -16)
        self.assertEqual(observed['decisions'][0]['result'], 1)
        self.assertEqual(observed['decisions'][0]['counts'][0]['result'], 3)

    def test_peer_count_keeps_distinct_candidate_identity(self):
        data = sequence(1, 3, (5, {'candidate': 200, 'result': 2}), (4, {'result': 1}), (2, {'result': -16}))
        count = trace.summarize(trace.decode(data))['unmounts'][0]['decisions'][0]['counts'][0]
        self.assertEqual((count['candidate'], count['candidate_id']), (200, 20))

    def test_truncated_or_reserved_layout_refuses(self):
        for data in (event(1)[:-1], event(1, reserved=1)):
            with self.assertRaises(ValueError):
                trace.decode(data)

    def test_unmatched_count_refuses(self):
        with self.assertRaises(ValueError):
            trace.summarize(trace.decode(event(1) + event(5) + event(2)))

    def test_changed_root_identity_refuses(self):
        with self.assertRaises(ValueError):
            trace.summarize(trace.decode(event(1) + event(2, root=200)))

    def test_unfinished_call_refuses(self):
        with self.assertRaises(ValueError):
            trace.summarize(trace.decode(event(1) + event(3)))

    def test_reordered_events_refuse(self):
        with self.assertRaisesRegex(ValueError, 'not ordered'):
            trace.summarize(trace.decode(event(1, at=1001) + event(2, at=1000)))


if __name__ == '__main__':
    unittest.main()
