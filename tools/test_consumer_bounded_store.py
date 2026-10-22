import copy
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

import consumer_bounded_store as bounded
import reference


class BoundedConsumerFixture(unittest.TestCase):
    def fixture(self, root, features=False):
        artifact = Path(root) / 'run'
        source = artifact / 'source'
        for name in bounded.SOURCE_FILES:
            target = source / name
            target.parent.mkdir(parents=True, exist_ok=True)
            shutil.copyfile(reference.ROOT / name, target)
        setup = artifact / 'consumer-worker-setup.sh'
        shutil.copyfile(reference.ROOT / 'tests/consumer-worker-setup.sh', setup)
        if features:
            anchor = '  chmod 0755 "$root/devx-actor" "$root/devx-registry" "$root/bin/devcontainer"'
            injection = '\n  python3 /consumer-patch.py --root /var/lib/sysbox/devx/bundle/lower --patch /consumer-patch.json --receipt /output/consumer-optimization.json'
            setup.write_text(setup.read_text().replace(anchor, anchor + injection))
        return artifact, source

    def test_exact_patches_reconstruct_both_real_setup_variants(self):
        original_sha = {name: reference.sha(reference.ROOT / name) for name in bounded.SOURCE_FILES}
        for features in [False, True]:
            with self.subTest(features=features), tempfile.TemporaryDirectory() as temporary:
                artifact, source = self.fixture(temporary, features)
                receipt = bounded.apply_fixture(artifact, source, features)
                self.assertEqual(len(receipt['files']), 4)
                reconstruction = artifact / 'reconstruction'
                shutil.copytree(artifact / 'bounded-fixture-before', reconstruction)
                for row in receipt['files']:
                    subprocess.run(['git', 'apply', '--unsafe-paths', '--directory=' + str(reconstruction),
                                    str(artifact / row['patch'])], check=True, capture_output=True)
                    target = source / row['file'] if row['file'] in bounded.SOURCE_FILES else artifact / 'consumer-worker-setup.sh'
                    self.assertEqual((reconstruction / row['file']).read_bytes(), target.read_bytes())
                    self.assertEqual(reference.sha(target), row['after_sha256'])
                    self.assertEqual(target.read_bytes().replace(row['new_path'].encode(), row['old_path'].encode()),
                                     (artifact / row['before_file']).read_bytes())
                self.assertEqual(original_sha, {name: reference.sha(reference.ROOT / name) for name in bounded.SOURCE_FILES})

    def test_changed_assertion_refuses_before_any_copy_is_patched(self):
        with tempfile.TemporaryDirectory() as temporary:
            artifact, source = self.fixture(temporary)
            changed = source / sorted(bounded.SOURCE_FILES)[0]
            changed.write_bytes(changed.read_bytes() + b'\n// changed fixture input\n')
            before = {str(p): p.read_bytes() for p in source.rglob('*') if p.is_file()}
            with self.assertRaisesRegex(ValueError, 'before bytes differ'):
                bounded.apply_fixture(artifact, source, False)
            self.assertEqual(before, {str(p): p.read_bytes() for p in source.rglob('*') if p.is_file()})
            self.assertFalse((artifact / 'bounded-fixture.json').exists())

    def test_hardlinked_input_is_never_rewritten(self):
        with tempfile.TemporaryDirectory() as temporary:
            artifact, source = self.fixture(temporary)
            target = source / sorted(bounded.SOURCE_FILES)[0]
            outside = Path(temporary) / 'archived-input.go'
            target.rename(outside)
            os.link(outside, target)
            before = outside.read_bytes()
            with self.assertRaisesRegex(ValueError, 'unique owned copies'):
                bounded.apply_fixture(artifact, source, False)
            self.assertEqual(outside.read_bytes(), before)

    def test_closed_run_is_not_patched(self):
        with tempfile.TemporaryDirectory() as temporary:
            artifact, source = self.fixture(temporary)
            (artifact / 'result.json').write_text(json.dumps({'passed': True}))
            with self.assertRaisesRegex(ValueError, 'closed or already patched'):
                bounded.apply_fixture(artifact, source, False)

    def test_capacity_requires_exact_store_and_independent_host_device(self):
        size = 8 << 30
        status = {'store': bounded.STORE, 'store_id': 'owned', 'lifecycle': {'root': bounded.STORE + '/epochs/current/controller',
                  'bounded_store': {'target': bounded.BOUNDED_ROOT + '/data', 'direct_io': True, 'block_device': 100,
                                    'image': {'path': bounded.BOUNDED_ROOT + '/store.img', 'owner': 'owned', 'bytes': size}}}}
        capacity = {'root': status['lifecycle']['root'], 'bounded_image_bytes': size, 'policy_enabled': True,
                    'min_free_bytes': 64 << 20, 'write_reserve_bytes': 128 << 20, 'filesystem_total_bytes': 7 << 30,
                    'directory': {'device': 100}, 'host': {'directory': {'device': 200}}}
        self.assertTrue(bounded.verify_capacity(status, capacity, size, 'owned')['passed'])
        for mutation in ['host-alias', 'different-size', 'foreign-owner']:
            with self.subTest(mutation=mutation):
                s, c = copy.deepcopy(status), copy.deepcopy(capacity)
                if mutation == 'host-alias':
                    c['host']['directory']['device'] = 100
                elif mutation == 'different-size':
                    c['bounded_image_bytes'] += 1
                else:
                    s['lifecycle']['bounded_store']['image']['owner'] = 'foreign'
                with self.assertRaises(ValueError):
                    bounded.verify_capacity(s, c, size, 'owned')

    def test_source_receipt_must_name_the_internal_admitted_bundle(self):
        record = {'input_bundle': bounded.STORE + '/devx/bundle', 'rootfs': bounded.STORE + '/devx/bundle/rootfs', 'phase': 'running'}
        self.assertTrue(bounded.verify_source({'record': record})['passed'])
        record['input_bundle'] = '/var/lib/sysbox/devx/bundle'
        with self.assertRaisesRegex(ValueError, 'outside the admitted'):
            bounded.verify_source({'record': record})
