import json
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

import consumer_bounded_store
import consumer_epoch_retirement as epoch
import reference


class ConsumerEpochFixture(unittest.TestCase):
    def fixture(self, temporary):
        artifact = Path(temporary) / 'run'
        source = artifact / 'source'
        for name in consumer_bounded_store.SOURCE_FILES:
            target = source / name
            target.parent.mkdir(parents=True, exist_ok=True)
            shutil.copyfile(reference.ROOT / name, target)
        shutil.copyfile(reference.ROOT / 'tests/consumer-worker-setup.sh', artifact / 'consumer-worker-setup.sh')
        consumer_bounded_store.apply_fixture(artifact, source, False)
        return artifact, source

    def test_ordered_patch_reconstructs_and_preserves_every_other_original_byte(self):
        original = reference.sha(reference.ROOT / epoch.FILE)
        with tempfile.TemporaryDirectory() as temporary:
            artifact, source = self.fixture(temporary)
            before = (source / epoch.FILE).read_text()
            receipt = epoch.apply_fixture(artifact, source)
            self.assertTrue(receipt['assertions_preserved'])
            self.assertFalse(receipt['upstream_assertions_modified'])
            after = (source / epoch.FILE).read_text()
            self.assertEqual(after.replace(epoch.NEW, epoch.OLD), before)
            reconstruction = artifact / 'reconstruction'
            target = reconstruction / epoch.FILE
            target.parent.mkdir(parents=True)
            shutil.copyfile(artifact / 'epoch-fixture-before.go', target)
            subprocess.run(['git', 'apply', '--unsafe-paths', '--directory=' + str(reconstruction),
                            str(artifact / 'epoch-fixture.patch')], check=True, capture_output=True)
            self.assertEqual(target.read_text(), after)
            self.assertEqual(reference.sha(target), receipt['after_sha256'])
        self.assertEqual(reference.sha(reference.ROOT / epoch.FILE), original)

    def test_changed_original_assertion_is_refused_without_mutation(self):
        with tempfile.TemporaryDirectory() as temporary:
            artifact, source = self.fixture(temporary)
            target = source / epoch.FILE
            before = target.read_text().replace('restored.Record.Owner != replayed.Record.Owner', 'false')
            target.write_text(before)
            with self.assertRaisesRegex(ValueError, 'exact bounded original'):
                epoch.apply_fixture(artifact, source)
            self.assertEqual(target.read_text(), before)
            self.assertFalse((artifact / 'epoch-fixture.json').exists())

    def test_closed_run_cannot_be_rewritten(self):
        with tempfile.TemporaryDirectory() as temporary:
            artifact, source = self.fixture(temporary)
            (artifact / 'result.json').write_text('{}')
            with self.assertRaisesRegex(ValueError, 'new canonical artifact'):
                epoch.apply_fixture(artifact, source)

    def test_missing_successor_or_used_base_proof_is_not_accepted(self):
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / 'consumer-epoch-retirement.json'
            value = {key: True for key in ['passed', 'used_registered_base_retired', 'original_ram_images_retired',
                     'all_authorized_payloads_absent', 'base_provenance_preserved', 'caller_input_marker_preserved',
                     'request_replay_exact', 'independent_successor_resumed_and_useful']}
            value.update(report={'request': {'source_root': '/old'}, 'completed_at': 'done', 'acceptance_sha256': 'a'*64},
                         payload_roots=['bases/developer-base/rootfs', 'images', 'filesystem'])
            path.write_text(json.dumps(value))
            self.assertTrue(epoch.verify_output(temporary, '/old')['passed'])
            for flag in ['independent_successor_resumed_and_useful', 'used_registered_base_retired']:
                changed = dict(value, **{flag: False})
                path.write_text(json.dumps(changed))
                with self.assertRaisesRegex(ValueError, 'independent successor proof'):
                    epoch.verify_output(temporary, '/old')
