import unittest
import hashlib
import json
from pathlib import Path
import tempfile

import historical_admission_cleanup as diagnostic

class HistoricalAdmissionInventory(unittest.TestCase):
    def original_log(self):
        main = ['TestDescriptorAdmissionNative','TestDetachedFileAdmissionNative','TestFilesystemAdmissionNative',
                'TestProcDirectoryAdmissionNative','TestCaptureSocketDescriptorNative','TestSocketQueueAdmissionNative']
        adapters = ['TestFilesystemCopyNative'] + ['TestAdapter'+str(i) for i in range(10)]
        rows = adapters+main
        rows += ['TestFilesystemCopyNative/case'+str(i) for i in range(61)]
        rows += ['TestDescriptorAdmissionNative/case'+str(i) for i in range(15)]
        rows += ['TestProcDirectoryAdmissionNative/held-foreign-proc-directory']
        rows += ['TestProcDirectoryAdmissionNative/transient-foreign-proc-directory']
        return '\n'.join('--- '+('FAIL' if name.endswith('transient-foreign-proc-directory') else 'PASS')+': '+name+' (0.01s)' for name in rows)

    def test_frozen_failure_preserves_all_assertions_at_their_actual_frequency(self):
        log = self.original_log()
        expected = diagnostic.expected_outcomes(log,3)
        self.assertEqual(sum(n for name,n in expected.items() if '/' not in name),29)
        self.assertEqual(sum(n for name,n in expected.items() if '/' in name),112)
        self.assertEqual(expected['TestProcDirectoryAdmissionNative/transient-foreign-proc-directory'],3)
        self.assertEqual(expected['TestFilesystemCopyNative'],1)
        damaged = log.replace('--- FAIL: TestProcDirectoryAdmissionNative/transient-foreign-proc-directory','--- MISSING: TestProcDirectoryAdmissionNative/transient-foreign-proc-directory')
        with self.assertRaisesRegex(ValueError,'assertion inventory differs'):
            diagnostic.expected_outcomes(damaged,3)

    def test_proc_focus_preserves_both_original_branches_and_all_adapters(self):
        log=self.original_log()
        expected=diagnostic.expected_outcomes(log,20,proc_only=True)
        self.assertEqual(sum(n for name,n in expected.items() if '/' not in name),31)
        self.assertEqual(sum(n for name,n in expected.items() if '/' in name),101)
        self.assertEqual(expected['TestProcDirectoryAdmissionNative/held-foreign-proc-directory'],20)
        self.assertEqual(expected['TestProcDirectoryAdmissionNative/transient-foreign-proc-directory'],20)
        self.assertEqual(expected['TestFilesystemCopyNative'],1)
        self.assertNotIn('TestDescriptorAdmissionNative',expected)
        extended=diagnostic.expected_outcomes(log,40,proc_only=True)
        self.assertEqual(sum(n for name,n in extended.items() if '/' not in name),51)
        self.assertEqual(sum(n for name,n in extended.items() if '/' in name),141)
        self.assertEqual(extended['TestProcDirectoryAdmissionNative/held-foreign-proc-directory'],40)
        self.assertEqual(extended['TestProcDirectoryAdmissionNative/transient-foreign-proc-directory'],40)
        self.assertEqual(extended['TestFilesystemCopyNative'],1)
        renamed=log.replace('held-foreign-proc-directory','unknown-proc-directory')
        with self.assertRaisesRegex(ValueError,'complete proc assertion inventory differs'):
            diagnostic.expected_outcomes(renamed,20,proc_only=True)

    def test_post_failure_build_rejects_changed_runtime_assertions_or_missing_proof(self):
        for fault in [None,'baseline','vet','runtime','assertion','binary']:
            with self.subTest(fault=fault), tempfile.TemporaryDirectory() as temporary:
                root=Path(temporary)
                source=root/'artifacts/original'
                build=root/'artifacts'/('factorybox-historical-busy-observer-'+'0'*32)
                engine='internal/lifecycle/engine_linux_test.go'
                (source/'context/internal/lifecycle').mkdir(parents=True)
                (build/'context/internal/lifecycle').mkdir(parents=True)
                old=b'\t\tif err := unix.Unmount(bundle+"/rootfs", 0); err != nil {\n\t\t\tt.Errorf("release test overlay: %v", err)\n\t\t}\n'
                (source/'context'/engine).write_bytes(old)
                (source/'context/internal/lifecycle/engine_linux.go').write_text('original production source')
                before=diagnostic.upstream_unit.context_manifest(source/'context')
                (source/'result.json').write_text('{"passed":false}')
                (build/'context'/engine).write_bytes(old.replace(b'err != nil {\n',b'err != nil {\n\t\t\trecordBusyOverlay(t, bundle, err)\n'))
                (build/'context/internal/lifecycle/engine_linux.go').write_text('changed runtime' if fault=='runtime' else 'original production source')
                for name in ['busy_overlay_native_linux_test.go','historical_busy_control_native_linux_test.go']:
                    (build/'context/internal/lifecycle'/name).write_text('separate diagnostic fixture')
                if fault=='assertion':
                    path=build/'context'/engine
                    path.write_bytes(path.read_bytes().replace(b't.Errorf',b't.Logf'))
                after=diagnostic.upstream_unit.context_manifest(build/'context')
                intent={'source_context_before':before,'source_context_after':after}
                (build/'intent.json').write_text(json.dumps(intent))
                (build/'lifecycle.test').write_bytes(b'diagnostic binary')
                digest=lambda p:hashlib.sha256(p.read_bytes()).hexdigest()
                original={'source_context':before,'inputs_sha256':{'lifecycle.test':'historical-binary-hash'}}
                result={'passed':True,'baseline_matches_historical':fault!='baseline',
                        'baseline_sha256':'historical-binary-hash','vet_passed':fault!='vet',
                        'original_result_sha256':digest(source/'result.json'),'intent_sha256':digest(build/'intent.json'),
                        'source_context_after':after,'binary_sha256':digest(build/'lifecycle.test')}
                (build/'result.json').write_text(json.dumps(result))
                if fault=='binary':(build/'lifecycle.test').write_bytes(b'changed binary')
                if fault is None:
                    binary,sha,context=diagnostic.observer_binary(root,build.name,source,original)
                    self.assertEqual((binary,sha,context),(build/'lifecycle.test',result['binary_sha256'],after))
                else:
                    with self.assertRaises(ValueError):diagnostic.observer_binary(root,build.name,source,original)

if __name__ == '__main__': unittest.main()
