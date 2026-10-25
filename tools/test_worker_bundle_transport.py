import json
import shutil
import unittest

import artifact_storage
import build_worker
import reference
import test_build_worker
import worker_bundle_transport as retention


@unittest.skipUnless(shutil.which('zstd'), 'transport test needs the pinned encoder')
class ExactBundleTransport(unittest.TestCase):
    def setUp(self):
        fixture = test_build_worker.WorkerBuildInputs()
        fixture.setUp()
        self.addCleanup(fixture.doCleanups)
        self.fixture = fixture
        self.package = fixture.artifact / retention.PACKAGE
        with self.package.open('wb') as output:
            raw = retention.transport(fixture.bundle, output)
        self.raw = self.package.read_bytes()
        self.result_path = fixture.artifact / 'result.json'
        result = json.loads(self.result_path.read_text())
        self.result_path.unlink()
        encoding = artifact_storage.compact(self.package, reference.ROOT, new_worker_package=True)
        result.update(selected=False, package_bytes=raw['bytes'], package_sha256=raw['sha256'], package_encoding=encoding['encoded'])
        self.result_path.write_text(json.dumps(result))
        self.result_bytes = self.result_path.read_bytes()
        self.encoded = fixture.artifact / encoding['encoded']['file']

    def retire(self):
        return retention.retire(self.fixture.name, {'worker_build':'different-completed-build','path':'fixture-proof','sha256':'f'*64})

    def test_original_transport_reconstructs_exactly_and_loader_still_verifies(self):
        self.retire()
        self.assertFalse(self.encoded.exists())
        self.assertEqual(self.result_path.read_bytes(), self.result_bytes)
        directory, result = build_worker.load(self.fixture.name)
        restored = directory / 'restored.tar'
        with restored.open('xb') as output:
            actual = retention.transport(self.fixture.bundle, output)
        self.assertEqual(restored.read_bytes(), self.raw)
        self.assertEqual(actual['sha256'], result['package_sha256'])

    def test_changed_bundle_or_result_is_refused(self):
        self.retire()
        (self.fixture.bundle / 'components/criu').write_bytes(b'changed')
        with self.assertRaises(ValueError):
            build_worker.load(self.fixture.name)

    def test_other_tar_recipe_and_selected_input_keep_their_encoding(self):
        result = json.loads(self.result_bytes)
        result['selected'] = True
        self.result_path.write_text(json.dumps(result))
        with self.assertRaisesRegex(ValueError, 'keep selected'):
            self.retire()
        self.assertTrue(self.encoded.exists())

    def test_changed_original_result_or_redirected_receipt_is_refused(self):
        self.retire()
        self.result_path.write_bytes(self.result_bytes + b'\n')
        with self.assertRaises(ValueError):
            build_worker.load(self.fixture.name)
        self.result_path.write_bytes(self.result_bytes)
        pointer = self.package.with_name(self.package.name + '.bundle-retention.json')
        saved = pointer.with_name('saved.json')
        pointer.rename(saved)
        pointer.symlink_to(saved)
        with self.assertRaises(ValueError):
            build_worker.load(self.fixture.name)
