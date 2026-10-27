import json
import shutil
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

import artifact_storage
import build_worker
import closed_worker_packages
import reference
import test_build_worker
import worker_bundle_transport as retention


@unittest.skipUnless(shutil.which('zstd'), 'transport test needs the pinned encoder')
class ExactBundleTransport(unittest.TestCase):
    architecture = 'amd64'

    def setUp(self):
        fixture = test_build_worker.WorkerBuildInputs()
        fixture.setUp()
        self.addCleanup(fixture.doCleanups)
        self.fixture = fixture
        if self.architecture == 'arm64':
            result_path = fixture.artifact / 'result.json'
            result = json.loads(result_path.read_text())
            result['platform'] = 'linux/arm64'
            (fixture.artifact / retention.PACKAGE).rename(fixture.artifact / retention.package_name(result))
            for path in [fixture.bundle / 'factorybox', *(fixture.bundle / 'components').iterdir()]:
                path.write_bytes(test_build_worker.elf('arm64', path.name.encode()))
            binaries = {p.name: reference.sha(p) for p in (fixture.bundle / 'components').iterdir()}
            (fixture.bundle / 'components.json').write_text(json.dumps({'schema': 1, 'binary_sha256': binaries}))
            recipe_path = fixture.bundle / 'build.json'
            recipe = json.loads(recipe_path.read_text())
            recipe.update(platform='linux/arm64', binary_sha256=binaries,
                          factorybox_sha256=reference.sha(fixture.bundle / 'factorybox'))
            recipe_path.write_text(json.dumps(recipe))
            result['bundle_sha256'] = {str(p.relative_to(fixture.bundle)): reference.sha(p)
                                      for p in fixture.bundle.rglob('*') if p.is_file()}
            result_path.write_text(json.dumps(result))
            fixture.docker.return_value = json.dumps([{
                'Id': 'sha256:fixture', 'Os': 'linux', 'Architecture': 'arm64',
                'RootFS': {'Layers': ['sha256:layer']}}])
        self.package = fixture.artifact / retention.package_name(json.loads((fixture.artifact / 'result.json').read_text()))
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


class ExactArm64BundleTransport(ExactBundleTransport):
    architecture = 'arm64'


class BoundedReplacementProof(unittest.TestCase):
    def test_complete_native_proof_is_bound_to_the_exact_worker_and_output_hashes(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary).resolve()
            name = 'factorybox-checkpoint-retirement-' + 'a' * 32
            directory = root / 'artifacts' / name
            (directory / 'output').mkdir(parents=True)
            for filename in ['runner.py', 'setup.sh']:
                (directory / filename).write_text('frozen fixture')
            digests = {}
            for label in ['retirement', 'retirement-base', 'retirement-base-actor', 'timer-state']:
                path = directory / 'output' / (label + '.json')
                path.write_text('{"passed":true}')
                digests[label] = reference.sha(path)
            worker = {'image_id': 'exact-image', 'platform': 'linux/arm64',
                      'source_context': {'internal/real.go': {'sha256': 'b' * 64}}}
            proof = {'passed': True, 'cleanup': True, 'inputs_unchanged': True,
                     'suite': 'retirement', 'passed_tests': 8, 'exit_code': 0,
                     'global_sysctls_before': {'a': 1}, 'global_sysctls_after': {'a': 1},
                     'worker_build': 'exact-worker', 'worker_image_id': worker['image_id'],
                     'platform': worker['platform'], 'source_context': worker['source_context'],
                     'proof_sha256': digests, 'runner_sha256': reference.sha(directory / 'runner.py'),
                     'setup_sha256': reference.sha(directory / 'setup.sh')}
            result = directory / 'result.json'
            result.write_text(json.dumps(proof))
            with patch.object(reference, 'ROOT', root), patch.object(build_worker, 'load', return_value=(None, worker)):
                self.assertEqual(retention.bounded_replacement(name), (result, proof))
                for key, value in [('passed', False), ('cleanup', False), ('inputs_unchanged', False),
                                   ('suite', 'diagnostic'), ('passed_tests', 7), ('worker_image_id', 'other'),
                                   ('source_context', {}), ('proof_sha256', {}),
                                   ('global_sysctls_after', {'a': 2}), ('runner_sha256', 'c' * 64)]:
                    result.write_text(json.dumps({**proof, key: value}))
                    with self.subTest(key=key), self.assertRaises(ValueError):
                        retention.bounded_replacement(name)
                result.write_text(json.dumps(proof))
                (directory / 'output/retirement.json').write_text('{"passed":false}')
                with self.assertRaises(ValueError):
                    retention.bounded_replacement(name)


@unittest.skipUnless(shutil.which('zstd'), 'legacy retention needs the pinned encoder')
class LegacyBundleTransport(unittest.TestCase):
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
        result.update(selected=False, package_bytes=raw['bytes'], package_sha256=raw['sha256'])
        self.result_path.write_text(json.dumps(result))
        fixture.docker.side_effect = lambda args, **kwargs: '' if args[0] == 'ps' else json.dumps([
            {'Id': 'sha256:fixture', 'Os': 'linux', 'Architecture': 'amd64',
             'RootFS': {'Layers': ['sha256:layer']}}])

    def encode(self):
        closed_worker_packages.archive([self.fixture.name])
        pointer = json.loads(self.package.with_name(retention.PACKAGE + '.retention.json').read_text())
        self.verification = reference.ROOT / pointer['verification']
        self.payload = self.verification.parent / 'package.tar.zst'

    def retire(self):
        return retention.retire_legacy(self.verification, {'path': 'fixture-proof', 'sha256': 'f' * 64})

    def test_exact_legacy_bytes_and_original_loader_survive_shared_encoding_retirement(self):
        self.encode()
        original_result = self.result_path.read_bytes()
        self.retire()
        self.assertFalse(self.payload.exists())
        directory, result = build_worker.load(self.fixture.name)
        restored = directory / 'restored.tar'
        with restored.open('xb') as output:
            actual = retention.transport(self.fixture.bundle, output)
        self.assertEqual(restored.read_bytes(), self.raw)
        self.assertEqual(actual['sha256'], result['package_sha256'])
        self.assertEqual(self.result_path.read_bytes(), original_result)
        cli_output = directory / 'cli-restored.tar'
        with patch('sys.argv', ['transport', '--restore-build', self.fixture.name, '--output', str(cli_output)]):
            retention.main()
        self.assertEqual(cli_output.read_bytes(), self.raw)
        (self.fixture.bundle / 'components/criu').write_bytes(b'changed')
        with self.assertRaises(ValueError):
            build_worker.load(self.fixture.name)

    def test_selected_legacy_owner_keeps_the_complete_encoding(self):
        result = json.loads(self.result_path.read_text())
        result['selected'] = True
        self.result_path.write_text(json.dumps(result))
        self.encode()
        with self.assertRaisesRegex(ValueError, 'Keep selected'):
            self.retire()
        self.assertTrue(self.payload.exists())

    def test_changed_legacy_receipt_is_refused(self):
        self.encode()
        self.retire()
        pointer = self.payload.with_name(self.payload.name + '.bundle-retention.json')
        saved = json.loads(pointer.read_text())
        saved['whole_original_decode_verified'] = False
        pointer.write_text(json.dumps(saved))
        with self.assertRaises(ValueError):
            build_worker.load(self.fixture.name)
