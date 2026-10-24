import json
import os
from pathlib import Path
import shutil
import subprocess
import unittest
from unittest.mock import patch

import build_worker
import closed_worker_packages as retention
import reference
import test_build_worker


@unittest.skipUnless(shutil.which('zstd'), 'complete transport retention requires zstd')
class LegacyTransportRetention(unittest.TestCase):
    def setUp(self):
        fixture = test_build_worker.WorkerBuildInputs()
        fixture.setUp()
        self.addCleanup(fixture.doCleanups)
        self.fixture = fixture
        self.directory = fixture.artifact
        self.package = self.directory / retention.PACKAGE
        self.original = b'complete immutable legacy transport\n' * 4096
        self.package.write_bytes(self.original)
        self.result_path = self.directory / 'result.json'
        result = json.loads(self.result_path.read_text())
        result['package_sha256'] = reference.sha(self.package)
        self.result_path.write_text(json.dumps(result))
        self.result_bytes = self.result_path.read_bytes()
        self.expected = result['package_sha256']
        fixture.docker.side_effect = lambda args, **kwargs: '' if args[0] == 'ps' else json.dumps([
            {'Id': 'sha256:fixture', 'RootFS': {'Layers': ['sha256:layer']}}])

    def encode(self):
        retention.archive([self.fixture.name])
        pointer = json.loads(self.package.with_name(retention.PACKAGE + '.retention.json').read_text())
        proof = reference.ROOT / pointer['verification']
        record = json.loads(proof.read_text())
        return proof.parent / record['encoded']['file'], record

    def test_complete_transport_and_original_manifest_remain_usable(self):
        payload, record = self.encode()
        self.assertFalse(self.package.exists())
        self.assertEqual(self.result_path.read_bytes(), self.result_bytes)
        self.assertEqual(build_worker.load(self.fixture.name)[1]['package_sha256'], self.expected)
        self.assertTrue(record['whole_decompressed_payload_verified'])
        self.assertTrue(record['complete_raw_package_reconstructable'])
        decoded = subprocess.check_output(['zstd', '-q', '-dc', '--', str(payload)])
        self.assertEqual(decoded, self.original)

    def test_corruption_and_changed_original_result_are_refused(self):
        payload, _ = self.encode()
        encoded = payload.read_bytes()
        payload.write_bytes(encoded + b'changed encoding')
        with self.assertRaisesRegex(ValueError, 'encoding differs'):
            build_worker.load(self.fixture.name)
        payload.write_bytes(encoded)
        self.result_path.write_bytes(self.result_bytes + b'\n')
        with self.assertRaisesRegex(ValueError, 'unchanged original build'):
            build_worker.load(self.fixture.name)

    def test_external_verification_and_symlink_are_refused(self):
        payload, _ = self.encode()
        pointer_path = self.package.with_name(retention.PACKAGE + '.retention.json')
        pointer = pointer_path.read_bytes()
        changed = json.loads(pointer)
        changed['verification'] = '../outside/verification.json'
        pointer_path.write_text(json.dumps(changed))
        with self.assertRaises(ValueError):
            build_worker.load(self.fixture.name)
        pointer_path.write_bytes(pointer)
        original = payload.with_name('saved-encoding')
        payload.rename(original)
        payload.symlink_to(original)
        with self.assertRaisesRegex(ValueError, 'redirected'):
            build_worker.load(self.fixture.name)

    def test_failed_build_or_failed_verification_keeps_every_raw_byte(self):
        failed = json.loads(self.result_bytes)
        failed['passed'] = False
        self.result_path.write_text(json.dumps(failed))
        with self.assertRaisesRegex(ValueError, 'did not complete'):
            retention.archive([self.fixture.name])
        self.assertEqual(self.package.read_bytes(), self.original)
        self.result_path.write_bytes(self.result_bytes)
        with patch.object(retention, 'verify_encoding', side_effect=ValueError('complete decode failed')):
            with self.assertRaisesRegex(ValueError, 'complete decode failed'):
                retention.archive([self.fixture.name])
        self.assertEqual(self.package.read_bytes(), self.original)
        self.assertEqual(self.result_path.read_bytes(), self.result_bytes)
        self.assertFalse(self.package.with_name(retention.PACKAGE + '.retention.json').exists())

    def test_interruption_after_unlink_keeps_a_complete_usable_transport(self):
        original_load = build_worker.load
        calls = 0
        def interrupted(name):
            nonlocal calls
            calls += 1
            if calls == 2:
                raise RuntimeError('interrupted after exact raw unlink')
            return original_load(name)
        with patch.object(build_worker, 'load', side_effect=interrupted):
            with self.assertRaisesRegex(RuntimeError, 'interrupted after exact raw unlink'):
                retention.archive([self.fixture.name])
        self.assertFalse(self.package.exists())
        self.assertEqual(original_load(self.fixture.name)[1]['package_sha256'], self.expected)
        self.assertEqual(self.result_path.read_bytes(), self.result_bytes)

    def test_all_known_raw_hardlinks_share_one_verified_encoding(self):
        second_name = 'factorybox-build-worker-' + 'e' * 32
        second = self.directory.with_name(second_name)
        shutil.copytree(self.directory, second)
        (second / retention.PACKAGE).unlink()
        os.link(self.package, second / retention.PACKAGE)
        retention.archive([self.fixture.name, second_name])
        for name in [self.fixture.name, second_name]:
            directory, result = build_worker.load(name)
            self.assertFalse((directory / retention.PACKAGE).exists())
            self.assertEqual(result['package_sha256'], self.expected)
            self.assertEqual((directory / 'result.json').read_bytes(), self.result_bytes)
        encodings = list((reference.ROOT / 'artifacts').glob('factorybox-storage-worker-packages-*/*/package.tar.zst'))
        self.assertEqual(len(encodings), 1)
