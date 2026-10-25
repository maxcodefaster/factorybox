import contextlib
import io
import json
import os
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

import share_closed_sources as sharing


class ClosedGoSourceSharingTests(unittest.TestCase):
    def test_timestamp_adoption_is_explicit_and_preserves_native_source_receipts(self):
        for adopt in [False, True]:
            with self.subTest(adopt=adopt), tempfile.TemporaryDirectory() as temporary:
                root = Path(temporary).resolve()
                artifacts = root / 'artifacts'
                artifacts.mkdir()
                sources, metadata, receipts = [], [], {}
                for index in [1, 2, 3]:
                    run = artifacts / ('factorybox-runtime-' + str(index) * 32)
                    source = run / 'source'
                    source.mkdir(parents=True)
                    go = source / 'exact.go'
                    if index == 3:
                        os.link(sources[1], go)
                    else:
                        go.write_text('package exact\n')
                    other = source / 'exact.txt'
                    other.write_text('unchanged non-Go metadata\n')
                    for path in [go, other]:
                        timestamp = (min(index, 2) if path == go else index) * 1000000000
                        os.utime(path, ns=(timestamp, timestamp))
                    sources.append(go)
                    metadata.append(other)
                    receipt = run / 'result.json'
                    receipt.write_text(json.dumps({
                        'passed': True, 'cleanup': True,
                        'source_context': sharing.upstream_unit.context_manifest(source),
                    }))
                    receipts[receipt] = receipt.read_bytes()
                args = ['share_closed_sources.py', *( ['--adopt-go-mtime'] if adopt else [])]
                with patch.object(sharing.reference, 'ROOT', root), \
                     patch.object(sharing.reference, 'docker', return_value=''), \
                     patch('sys.argv', args), contextlib.redirect_stdout(io.StringIO()):
                    sharing.main()
                self.assertEqual(sources[0].samefile(sources[1]), adopt)
                self.assertEqual(sources[0].samefile(sources[2]), adopt)
                self.assertFalse(metadata[0].samefile(metadata[1]))
                for path, before in receipts.items():
                    self.assertEqual(path.read_bytes(), before)
                plan = json.loads(next(artifacts.glob('factorybox-storage-share-sources-*/plan.json')).read_text())
                self.assertEqual(plan['adopt_go_mtime'], adopt)
                if adopt:
                    self.assertEqual(len(plan['actions']), 2)
                    action = plan['actions'][0]
                    self.assertEqual(action['mtime_ns'], 2000000000)
                    self.assertEqual(action['shared_mtime_ns'], 1000000000)
                    result = json.loads(next(artifacts.glob('factorybox-storage-share-sources-*/result.json')).read_text())
                    self.assertEqual(result['reclaimed_allocated_bytes'], sources[0].stat().st_blocks * 512)
