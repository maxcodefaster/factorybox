import pathlib
import tempfile
import unittest
from unittest import mock

import repack_evidence


class EvidenceRepackBoundaryTests(unittest.TestCase):
    def test_nested_build_output_remains_a_frozen_input(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = pathlib.Path(temporary).resolve()
            output = root / "artifacts/factorybox-build-test/output"
            output.mkdir(parents=True)
            archive = output / "input.tar.zst"
            archive.write_bytes(b"frozen input must remain unchanged")
            # A misleading result beside a nested output must never make the
            # containing frozen build look like disposable closed evidence.
            (output / "result.json").write_text('{"cleanup": true}')
            with mock.patch.object(repack_evidence.reference, "ROOT", root), \
                    mock.patch("sys.argv", ["repack_evidence", str(archive)]), \
                    mock.patch.object(repack_evidence.subprocess, "Popen", side_effect=AssertionError("decoder must not start")):
                with self.assertRaisesRegex(ValueError, "frozen"):
                    repack_evidence.main()
            self.assertEqual(archive.read_bytes(), b"frozen input must remain unchanged")
            self.assertEqual(list((root / "artifacts").iterdir()), [output.parent])
