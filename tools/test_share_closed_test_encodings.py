import contextlib
import hashlib
import io
import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch

import share_closed_test_encodings as sharing


class EncodedRuntimeSharingTests(unittest.TestCase):
    def test_closed_diagnostic_components_share_without_changing_failed_result(self):
        with tempfile.TemporaryDirectory(dir=sharing.reference.ROOT / 'artifacts') as temporary:
            root = Path(temporary).resolve()
            artifacts = root / 'artifacts'
            artifacts.mkdir()
            paths, preserved, expected_freed = {}, {}, 0
            for index, owner in enumerate(['1' * 32, '2' * 32]):
                run = artifacts / ('factorybox-startup-manager-' + owner)
                (run / 'components').mkdir(parents=True)
                expected = {}
                for role in ['sysbox-fs', 'sysbox-mgr']:
                    payload = (role + ' exact native input\n').encode() * 8192
                    encoded = subprocess.run(['zstd', '-q', '-T1', '-1', '--stdout'], input=payload,
                                             capture_output=True, check=True).stdout
                    target = run / 'components' / (role + '.zst')
                    target.write_bytes(encoded)
                    paths.setdefault(role, []).append(target)
                    if index: expected_freed += target.stat().st_blocks * 512
                    digest = hashlib.sha256(payload).hexdigest()
                    expected[role] = digest
                    receipt = target.with_name(role + '.encoding.json')
                    receipt.write_text(json.dumps({'schema': 1, 'whole_decompressed_payload_verified': True,
                        'original': {'file': role, 'bytes': len(payload), 'sha256': digest},
                        'encoded': {'file': role + '.zst', 'bytes': len(encoded),
                                    'sha256': hashlib.sha256(encoded).hexdigest()}}))
                    preserved[receipt] = receipt.read_bytes()
                native = run / 'result.json'
                native.write_text(json.dumps({'owner': owner, 'passed': index == 0, 'cleanup': True,
                    'inputs_unchanged': True, 'container_state': {'Running': False, 'Pid': 0},
                    'inputs_sha256': expected}))
                preserved[native] = native.read_bytes()
            with patch.object(sharing.reference, 'ROOT', root), \
                 patch.object(sharing.reference, 'docker', return_value=''), \
                 patch('sys.argv', ['share_closed_test_encodings.py', '--include-closed-diagnostic-components']), \
                 contextlib.redirect_stdout(io.StringIO()):
                sharing.main()
            saved = json.loads(next(artifacts.glob('factorybox-storage-share-test-encodings-*/result.json')).read_text())
            self.assertTrue(saved['passed'])
            self.assertEqual(saved['reclaimed_allocated_bytes'], expected_freed)
            for pair in paths.values():
                self.assertTrue(pair[0].samefile(pair[1]))
            for path, before in preserved.items():
                self.assertEqual(path.read_bytes(), before)

    def test_diagnostic_component_source_change_or_foreign_owner_refuses(self):
        with tempfile.TemporaryDirectory() as temporary:
            run = Path(temporary) / ('factorybox-pid-inner-time-' + '1' * 32)
            (run / 'context').mkdir(parents=True)
            source = run / 'context/test.go'
            source.write_text('exact frozen diagnostic')
            outcome = {'cleanup': True, 'inputs_unchanged': True, 'state': {'Running': False, 'Pid': 0},
                       'source_context': sharing.upstream_unit.context_manifest(run / 'context'),
                       'component_inputs': {name: {'sha256': 'a' * 64} for name in
                                            ['criu', 'sysbox-runc', 'sysbox-fs', 'sysbox-mgr']}}
            self.assertEqual(len(sharing.diagnostic_components(run, outcome)), 4)
            with self.assertRaisesRegex(ValueError, 'owner differs'):
                sharing.diagnostic_components(run, {**outcome, 'owner': '2' * 32})
            source.write_text('changed source')
            with self.assertRaisesRegex(ValueError, 'source changed'):
                sharing.diagnostic_components(run, outcome)

    def test_concurrent_unrelated_cleanup_is_not_counted_as_shared_test_bytes(self):
        with tempfile.TemporaryDirectory(dir=sharing.reference.ROOT / 'artifacts') as temporary:
            root = Path(temporary).resolve()
            artifacts = root / 'artifacts'
            artifacts.mkdir()
            payload = b'closed native test input\n' * 8192
            encoded = subprocess.run(['zstd', '-q', '-T1', '-1', '--stdout'], input=payload,
                                     capture_output=True, check=True).stdout
            native_results = {}
            paths = []
            for owner in ['1' * 32, '2' * 32]:
                run = artifacts / ('factorybox-runtime-' + owner)
                run.mkdir()
                binary = run / 'lifecycle.test.zst'
                binary.write_bytes(encoded)
                paths.append(binary)
                digest = hashlib.sha256(payload).hexdigest()
                receipt = {'schema': 1, 'whole_decompressed_payload_verified': True,
                    'original': {'file': 'lifecycle.test', 'bytes': len(payload), 'sha256': digest},
                    'encoded': {'file': binary.name, 'bytes': len(encoded),
                                'sha256': hashlib.sha256(encoded).hexdigest()}}
                (run / 'lifecycle.test.encoding.json').write_text(json.dumps(receipt))
                result = run / 'result.json'
                result.write_text(json.dumps({'owner': owner, 'passed': True, 'cleanup': True,
                    'container_state': {'Running': False, 'Pid': 0},
                    'inputs_sha256': {'lifecycle.test': digest}}))
                native_results[result] = result.read_bytes()
            expected = paths[1].stat().st_blocks * 512
            unrelated = artifacts / 'unrelated-closed-payload'
            unrelated.write_bytes(b'x' * (2 << 20))
            replace = os.replace

            def replace_and_finish_unrelated_cleanup(source, destination):
                replace(source, destination)
                unrelated.unlink()

            with patch.object(sharing.reference, 'ROOT', root), \
                 patch.object(sharing.reference, 'docker', return_value=''), \
                 patch('sys.argv', ['share_closed_test_encodings.py', '--include-closed-runtime-tests']), \
                 patch.object(sharing.os, 'replace', side_effect=replace_and_finish_unrelated_cleanup), \
                 contextlib.redirect_stdout(io.StringIO()):
                sharing.main()
            results = list(artifacts.glob('factorybox-storage-share-test-encodings-*/result.json'))
            self.assertEqual(len(results), 1)
            saved = json.loads(results[0].read_text())
            self.assertTrue(saved['passed'])
            self.assertTrue(paths[0].samefile(paths[1]))
            self.assertEqual(paths[0].read_bytes(), encoded)
            self.assertEqual(saved['reclaimed_allocated_bytes'], expected)
            self.assertGreater(saved['observed_artifact_allocation_delta_bytes'], 1 << 20)
            for path, original in native_results.items():
                self.assertEqual(path.read_bytes(), original)


if __name__ == '__main__':
    unittest.main()
