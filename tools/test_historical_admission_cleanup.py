import unittest

import historical_admission_cleanup as diagnostic

class HistoricalAdmissionInventory(unittest.TestCase):
    def test_frozen_failure_preserves_all_assertions_at_their_actual_frequency(self):
        main = ['TestDescriptorAdmissionNative','TestDetachedFileAdmissionNative','TestFilesystemAdmissionNative',
                'TestProcDirectoryAdmissionNative','TestCaptureSocketDescriptorNative','TestSocketQueueAdmissionNative']
        adapters = ['TestFilesystemCopyNative'] + ['TestAdapter'+str(i) for i in range(10)]
        rows = adapters+main
        rows += ['TestFilesystemCopyNative/case'+str(i) for i in range(61)]
        rows += ['TestProcDirectoryAdmissionNative/case'+str(i) for i in range(16)]
        rows += ['TestProcDirectoryAdmissionNative/transient-foreign-proc-directory']
        log = '\n'.join('--- '+('FAIL' if name.endswith('transient-foreign-proc-directory') else 'PASS')+': '+name+' (0.01s)' for name in rows)
        expected = diagnostic.expected_outcomes(log,3)
        self.assertEqual(sum(n for name,n in expected.items() if '/' not in name),29)
        self.assertEqual(sum(n for name,n in expected.items() if '/' in name),112)
        self.assertEqual(expected['TestProcDirectoryAdmissionNative/transient-foreign-proc-directory'],3)
        self.assertEqual(expected['TestFilesystemCopyNative'],1)
        damaged = log.replace('--- FAIL: TestProcDirectoryAdmissionNative/transient-foreign-proc-directory','--- MISSING: TestProcDirectoryAdmissionNative/transient-foreign-proc-directory')
        with self.assertRaisesRegex(ValueError,'assertion inventory differs'):
            diagnostic.expected_outcomes(damaged,3)

if __name__ == '__main__': unittest.main()
