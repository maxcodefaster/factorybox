import pathlib
import json
import hashlib
import shutil
import subprocess
import tempfile
import unittest
from unittest import mock

import repack_evidence


class EvidenceRepackBoundaryTests(unittest.TestCase):
    def test_shared_encoding_is_not_repacked_into_an_extra_allocation(self):
        with tempfile.TemporaryDirectory() as temporary:
            source = pathlib.Path(temporary) / "probe.test.zst"
            source.write_bytes(b"already shared exact bytes")
            alias = source.with_name("another-closed-run.zst")
            repack_evidence.os.link(source, alias)
            before = source.read_bytes()
            receipt = {"encoded": {"file": source.name}}
            with self.assertRaisesRegex(ValueError, "keep them shared"):
                repack_evidence.encoded_input(source, receipt, False)
            self.assertEqual(source.read_bytes(), before)
            self.assertEqual(source.stat().st_ino, alias.stat().st_ino)

    def test_native_test_requires_its_exact_closed_owner_and_executable_hash(self):
        run = pathlib.Path("factorybox-checkpoint-retirement-" + "a" * 32)
        source = run / "probe.test.zst"
        original = {"file": "probe.test", "sha256": "b" * 64}
        outcome = {"owner": "a" * 32, "cleanup": True, "inputs_unchanged": True,
                   "test_binary_sha256": "b" * 64}
        repack_evidence.require_payload(source, run, outcome, original)
        for key, value in [("cleanup", False), ("inputs_unchanged", False),
                           ("owner", "c" * 32), ("test_binary_sha256", "c" * 64)]:
            with self.subTest(key=key), self.assertRaisesRegex(ValueError, "exact closed native"):
                repack_evidence.require_payload(source, run, dict(outcome, **{key: value}), original)
        for path in [run / "factorybox.zst", run / "foreign.test.zst"]:
            with self.subTest(path=path), self.assertRaises(ValueError):
                repack_evidence.require_payload(path, run, outcome, original)
        with self.assertRaises(ValueError):
            repack_evidence.require_payload(source, pathlib.Path("factorybox-build-worker-" + "a" * 32),
                                           outcome, original)

    @unittest.skipUnless(shutil.which("zstd"), "requires the archive encoder")
    def test_native_executable_repack_preserves_the_original_reader_and_result(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = pathlib.Path(temporary).resolve()
            run = root / "artifacts" / ("factorybox-checkpoint-retirement-" + "a" * 32)
            run.mkdir(parents=True)
            payload = b"exact ELF test byte sequence remains reconstructable\n" * 4000
            expected = {"file": "probe.test", "bytes": len(payload),
                        "sha256": hashlib.sha256(payload).hexdigest()}
            source = run / "probe.test.zst"
            source.write_bytes(subprocess.check_output(["zstd", "-q", "-c"], input=payload) +
                               subprocess.check_output(["zstd", "-q", "-c"], input=b"") * 100)
            encoded = {"file": source.name, "bytes": source.stat().st_size,
                       "sha256": repack_evidence.file_sha(source)}
            receipt = run / "probe.test.encoding.json"
            receipt.write_text(json.dumps({"whole_decompressed_payload_verified": True,
                                          "original": expected, "encoded": encoded}))
            closed = run / "result.json"
            closed.write_text(json.dumps({"owner": "a" * 32, "cleanup": True, "passed": False,
                                          "inputs_unchanged": True, "test_binary_sha256": expected["sha256"]}))
            immutable = {path: path.read_bytes() for path in [receipt, closed]}
            with mock.patch.object(repack_evidence.reference, "ROOT", root), \
                    mock.patch.object(repack_evidence.reference.artifact_storage, "require_capacity", return_value={}), \
                    mock.patch.object(repack_evidence.shutil, "disk_usage", return_value=mock.Mock(free=100 << 30)), \
                    mock.patch("sys.argv", ["repack_evidence", str(source)]):
                repack_evidence.main()
            self.assertFalse(source.exists())
            repack_evidence.reference.artifact_storage.verify_compacted(run / "probe.test", expected)
            for path, before in immutable.items():
                self.assertEqual(path.read_bytes(), before)

    @unittest.skipUnless(shutil.which("zstd"), "requires the archive encoder")
    def test_full_refresh_keeps_exact_raw_bytes_and_one_original_pointer(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = pathlib.Path(temporary).resolve()
            run = root / "artifacts" / ("factorybox-runtime-" + "a" * 32)
            previous_run = root / "artifacts" / ("factorybox-storage-repack-" + "b" * 32)
            run.mkdir(parents=True)
            previous_run.mkdir()
            closed = run / "result.json"
            closed.write_text('{"cleanup": true, "passed": false}')
            original = run / "failed-volume-0.tar.zst"
            payload = b"exact closed failure remains reconstructable\n" * 4000
            expected = {"file": "failed-volume-0.tar", "bytes": len(payload),
                        "sha256": hashlib.sha256(payload).hexdigest()}
            encoded = {"file": original.name, "bytes": 1000000, "sha256": "c" * 64}
            receipt = original.with_name("failed-volume-0.tar.encoding.json")
            receipt.write_text(json.dumps({"whole_decompressed_payload_verified": True,
                                           "original": expected, "encoded": encoded}))
            physical = previous_run / original.name
            compressed = subprocess.check_output(["zstd", "-q", "-c"], input=payload)
            empty = subprocess.check_output(["zstd", "-q", "-c"], input=b"")
            # Extra valid empty frames enlarge the old encoding without changing
            # its decoded bytes, so a real refresh has a deterministic saving.
            physical.write_bytes(compressed + empty * 100)
            result = {"operation": "lossless_closed_archive_repack", "passed": True, "cleanup": True,
                      "source_removed": True, "whole_decoded_bytes_verified": True,
                      "source": str(original), "source_encoded_sha256": encoded["sha256"],
                      "source_encoded_bytes": encoded["bytes"], "new_archive": str(physical),
                      "encoded_sha256": repack_evidence.file_sha(physical),
                      "encoded_bytes": physical.stat().st_size,
                      "raw_bytes": expected["bytes"], "raw_sha256": expected["sha256"]}
            previous_result = previous_run / "result.json"
            previous_result.write_text(json.dumps(result))
            pointer = original.with_name(original.name + ".repacked.json")
            pointer.write_text(json.dumps({"schema": 1, "original_archive": str(original),
                "original_encoded_sha256": encoded["sha256"], "original_tar_bytes": expected["bytes"],
                "original_tar_sha256": expected["sha256"], "replacement_archive": str(physical),
                "replacement_encoded_sha256": result["encoded_sha256"],
                "verified_result": str(previous_result),
                "verified_result_sha256": repack_evidence.file_sha(previous_result)}))
            immutable = {path: path.read_bytes() for path in [closed, receipt, previous_result]}
            with mock.patch.object(repack_evidence.reference, "ROOT", root), \
                    mock.patch.object(repack_evidence.reference.artifact_storage, "require_capacity", return_value={}), \
                    mock.patch.object(repack_evidence.shutil, "disk_usage", return_value=mock.Mock(free=100 << 30)), \
                    mock.patch("sys.argv", ["repack_evidence", str(original), "--refresh", "--level", "19"]):
                repack_evidence.main()
            self.assertFalse(physical.exists())
            selected, current = repack_evidence.reference.artifact_storage.verified_replacement(
                original, encoded, expected)
            self.assertEqual(subprocess.check_output(["zstd", "-q", "-dc", "--long=31", str(selected)]), payload)
            self.assertEqual(json.loads(pointer.read_text())["original_archive"], str(original))
            self.assertEqual(json.loads((selected.parent / "result.json").read_text())["source"], str(original))
            self.assertEqual(repack_evidence.reference.artifact_storage.verify_compacted(
                original.with_suffix(""), expected)["sha256"], current["sha256"])
            for path, before in immutable.items():
                self.assertEqual(path.read_bytes(), before)

    def test_refresh_resolves_only_the_original_pinned_receipt(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = pathlib.Path(temporary).resolve()
            original = root / "original.tar.zst"
            physical = root / "replacement.tar.zst"
            physical.write_bytes(b"complete encoded archive")
            pointer = original.with_name(original.name + ".repacked.json")
            previous = b'{"pinned": "original"}'
            pointer.write_bytes(previous)
            encoding = {"file": physical.name, "bytes": physical.stat().st_size,
                        "sha256": repack_evidence.file_sha(physical)}
            receipt = {"whole_decompressed_payload_verified": True,
                       "original": {"sha256": "a" * 64},
                       "encoded": {"file": original.name, "bytes": 100, "sha256": "b" * 64}}
            with mock.patch.object(repack_evidence.reference.artifact_storage, "verified_replacement",
                                   return_value=(physical, encoding)) as resolve:
                with self.assertRaisesRegex(ValueError, "explicit"):
                    repack_evidence.encoded_input(original, receipt, False)
                resolve.assert_not_called()
                resolved, before, saved = repack_evidence.encoded_input(original, receipt, True)
                self.assertEqual((resolved, before[2], saved), (physical, physical.stat().st_size, previous))
                resolve.assert_called_once_with(original, receipt["encoded"], receipt["original"])
                physical.write_bytes(b"changed bytes")
                with self.assertRaisesRegex(ValueError, "differs"):
                    repack_evidence.encoded_input(original, receipt, True)

    def test_refresh_refuses_an_unresolved_original_alongside_its_replacement(self):
        with tempfile.TemporaryDirectory() as temporary:
            original = pathlib.Path(temporary) / "original.tar.zst"
            original.write_bytes(b"must remain")
            original.with_name(original.name + ".repacked.json").write_text("{}")
            with self.assertRaisesRegex(ValueError, "reconcile"):
                repack_evidence.encoded_input(original, {}, True)
            self.assertEqual(original.read_bytes(), b"must remain")

    def test_pointer_refresh_is_atomic_and_cannot_overwrite_another_selection(self):
        with tempfile.TemporaryDirectory() as temporary:
            pointer = pathlib.Path(temporary) / "original.tar.zst.repacked.json"
            previous = b'{"selection": "old"}'
            pointer.write_bytes(previous)
            with self.assertRaisesRegex(ValueError, "changed"):
                repack_evidence.commit_pointer(pointer, {"selection": "new"}, b"foreign")
            self.assertEqual(pointer.read_bytes(), previous)
            with mock.patch.object(repack_evidence.os, "replace", side_effect=OSError("write failed")):
                with self.assertRaises(OSError):
                    repack_evidence.commit_pointer(pointer, {"selection": "new"}, previous)
            self.assertEqual(pointer.read_bytes(), previous)
            repack_evidence.commit_pointer(pointer, {"selection": "new"}, previous)
            self.assertEqual(json.loads(pointer.read_text()), {"selection": "new"})
            self.assertEqual(list(pointer.parent.iterdir()), [pointer])

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
