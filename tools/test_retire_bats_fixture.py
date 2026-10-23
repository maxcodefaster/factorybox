import copy
from pathlib import Path
import tempfile
import unittest

import reference
import retire_superseded_failure as retirement


class BatsFixtureSupersession(unittest.TestCase):
    def test_real_failed_assertion_requires_same_upstream_bytes_and_executed_pass(self):
        with tempfile.TemporaryDirectory() as temporary:
            old_path, new_path = [Path(temporary) / name for name in ['old', 'new']]
            for path in [old_path, new_path]:
                path.mkdir()
                (path / 'source.tar').write_bytes(b'unchanged upstream assertions')
            old = {'suite': 'systemd', 'target_platform': 'linux/amd64', 'upstream_test_files_verified': 243,
                   'inputs_unchanged': True, 'components_unchanged': True, 'unchanged_upstream_test_files': 241,
                   'declared_upstream_test_changes': 2, 'expected_titles': ['mount overrides'],
                   'inputs_sha256': {'source.tar': reference.sha(old_path / 'source.tar')},
                   'observed': {'failed': ['mount overrides']}}
            new = copy.deepcopy(old)
            new['observed'] = {'complete': True, 'failed': [], 'cases': [{'name': 'mount overrides', 'passed': True, 'skipped': False}]}
            retirement.verify_bats_replacement(old_path, new_path, old, new)
            for scenario in ['skip', 'lost-assertion', 'changed-input', 'different-source', 'lost-verification']:
                with self.subTest(scenario=scenario):
                    changed = copy.deepcopy(new)
                    if scenario == 'skip':
                        changed['observed']['cases'][0]['skipped'] = True
                    elif scenario == 'lost-assertion':
                        changed['expected_titles'] = []
                    elif scenario == 'changed-input':
                        (new_path / 'source.tar').write_bytes(b'changed')
                    elif scenario == 'different-source':
                        changed['inputs_sha256']['source.tar'] = '0'*64
                    else:
                        changed['upstream_test_files_verified'] = 242
                    with self.assertRaises(ValueError):
                        retirement.verify_bats_replacement(old_path, new_path, old, changed)
                    (new_path / 'source.tar').write_bytes(b'unchanged upstream assertions')
