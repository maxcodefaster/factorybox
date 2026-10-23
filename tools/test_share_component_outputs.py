import json
from pathlib import Path
import tempfile
import unittest

import reference
import share_component_outputs as sharing


class ClosedComponentSharing(unittest.TestCase):
    def fixture(self, directory):
        root = Path(directory)
        paths = [root / 'artifacts' / ('factorybox-build-sysbox-' + token*32) / 'output/sysbox-runc' for token in ['a', 'b']]
        for path in paths:
            path.parent.mkdir(parents=True)
            path.write_bytes(b'identical immutable executable')
            path.chmod(0o755)
            (path.parent.parent / 'result.json').write_text(json.dumps({'passed': True, 'platform': 'linux/amd64',
                'binary_sha256': {'sysbox-runc': reference.sha(path)}}))
        row = {'source': str(paths[0]), 'target': str(paths[1]), 'sha256': reference.sha(paths[0]),
               'identity': list(reference.artifact_storage.identity(paths[1]))}
        results = {str(p.parent.parent / 'result.json'): reference.sha(p.parent.parent / 'result.json') for p in paths}
        return root, paths, {'schema': 1, 'root': str(root), 'actions': [row], 'results_sha256': results}

    def test_exact_output_sharing_preserves_every_path_byte_and_owner(self):
        with tempfile.TemporaryDirectory() as directory:
            root, paths, plan = self.fixture(directory)
            before = [(p.read_bytes(), p.stat().st_mode, p.stat().st_uid, p.stat().st_gid) for p in paths]
            sharing.apply(root, plan)
            self.assertTrue(paths[0].samefile(paths[1]))
            self.assertEqual(before, [(p.read_bytes(), p.stat().st_mode, p.stat().st_uid, p.stat().st_gid) for p in paths])

    def test_changed_or_redirected_output_is_refused_before_mutation(self):
        for scenario in ['changed', 'symlink', 'outside', 'mode', 'changed-result']:
            with self.subTest(scenario=scenario), tempfile.TemporaryDirectory() as directory:
                root, paths, plan = self.fixture(directory)
                if scenario == 'changed':
                    paths[1].write_bytes(b'different')
                elif scenario == 'symlink':
                    paths[1].unlink()
                    paths[1].symlink_to(paths[0])
                elif scenario == 'outside':
                    outside = root / 'foreign'
                    outside.write_bytes(paths[0].read_bytes())
                    plan['actions'][0]['target'] = str(outside)
                elif scenario == 'mode':
                    paths[1].chmod(0o644)
                else:
                    (paths[1].parent.parent / 'result.json').write_text('{}')
                original = paths[0].read_bytes()
                with self.assertRaises(ValueError):
                    sharing.apply(root, plan)
                self.assertEqual(paths[0].read_bytes(), original)
