import pathlib
import json
import tempfile
import unittest
from unittest import mock

import repack_evidence


class EvidenceRepackBoundaryTests(unittest.TestCase):
    def test_reconciled_failure_requires_exact_completed_volume_and_original_result(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = pathlib.Path(temporary)
            owner = "a" * 32
            run = root / ("factorybox-consumer-worker-loss-" + owner)
            reconciliation = run / "reconciliations" / ("b" * 32)
            reconciliation.mkdir(parents=True)
            source = reconciliation / "failed-volume-0.tar.zst"
            original = {"owner": owner, "passed": False, "cleanup": False, "retained_volumes": [run.name]}
            (run / "result.json").write_text(json.dumps(original))
            saved = {"passed": True, "cleanup": True, "run": run.name, "owner": owner,
                     "volume_name": run.name, "original_result_sha256": repack_evidence.reference.sha(run / "result.json"),
                     "failed_volume_archives": {run.name: {"file": "failed-volume-0.tar"}}}
            receipt = reconciliation / "result.json"
            receipt.write_text(json.dumps(saved))
            self.assertEqual(repack_evidence.require_closed_source(run, source), receipt)
            for key, value in [("passed", False), ("cleanup", False), ("run", "foreign"),
                               ("owner", "c" * 32), ("volume_name", "foreign"),
                               ("original_result_sha256", "c" * 64), ("failed_volume_archives", {})]:
                with self.subTest(key=key):
                    receipt.write_text(json.dumps(dict(saved, **{key: value})))
                    with self.assertRaises(ValueError):
                        repack_evidence.require_closed_source(run, source)
            receipt.write_text(json.dumps(saved))
            for path in [run / "foreign" / ("b" * 32) / source.name,
                         run / "reconciliations" / "foreign" / source.name, run / source.name]:
                with self.subTest(path=path), self.assertRaises(ValueError):
                    repack_evidence.require_closed_source(run, path)
            (run / "result.json").write_text(json.dumps(dict(original, retained_volumes=["foreign"])))
            with self.assertRaises(ValueError):
                repack_evidence.require_closed_source(run, source)

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

    def test_reconciled_archive_bytes_must_match_the_successful_retention_receipt(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = pathlib.Path(temporary).resolve()
            owner = "a" * 32
            run = root / "artifacts" / ("factorybox-consumer-worker-loss-" + owner)
            reconciliation = run / "reconciliations" / ("b" * 32)
            reconciliation.mkdir(parents=True)
            source = reconciliation / "failed-volume-0.tar.zst"
            source.write_bytes(b"encoded bytes are never decoded on mismatched receipts")
            original = {"owner": owner, "passed": False, "cleanup": False, "retained_volumes": [run.name]}
            (run / "result.json").write_text(json.dumps(original))
            raw = {"file": "failed-volume-0.tar", "bytes": 100, "sha256": "c" * 64}
            (reconciliation / "result.json").write_text(json.dumps({
                "passed": True, "cleanup": True, "run": run.name, "owner": owner,
                "volume_name": run.name, "original_result_sha256": repack_evidence.reference.sha(run / "result.json"),
                "failed_volume_archives": {run.name: raw}}))
            (reconciliation / "failed-volume-0.tar.encoding.json").write_text(json.dumps({
                "original": dict(raw, bytes=101)}))
            with mock.patch.object(repack_evidence.reference, "ROOT", root), \
                    mock.patch("sys.argv", ["repack_evidence", str(source)]), \
                    mock.patch.object(repack_evidence.subprocess, "Popen", side_effect=AssertionError("decoder must not start")):
                with self.assertRaisesRegex(ValueError, "differs from its completed retention receipt"):
                    repack_evidence.main()
            self.assertEqual(source.read_bytes(), b"encoded bytes are never decoded on mismatched receipts")
