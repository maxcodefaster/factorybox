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
