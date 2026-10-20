import hashlib
import json
import shutil
import pathlib
import subprocess
import sys
import tempfile
import unittest
from unittest import mock

import reconcile_devx


class ReconcileTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = pathlib.Path(self.temporary.name)
        self.owner = "a" * 32
        self.run = "factorybox-devx-lifecycle-" + self.owner
        self.archive = self.root / "artifacts" / self.run
        self.previous = self.archive / "reconciliations" / ("b" * 32)
        self.previous.mkdir(parents=True)
        self.original = {
            "owner": self.owner, "passed": False, "cleanup": False,
            "retained_volumes": [self.run], "native_base_build": "unused",
        }
        self.original_bytes = json.dumps(self.original).encode()
        (self.archive / "result.json").write_bytes(self.original_bytes)
        payload = b"retained complete stopped runtime archive"
        (self.previous / "failed-volume-0.tar").write_bytes(payload)
        self.saved = {
            "run": self.run, "owner": self.owner, "passed": False, "cleanup": False,
            "error": "volume removal caller timed out",
            "failed_volume_archives": {self.run: {
                "file": "failed-volume-0.tar", "bytes": len(payload),
                "sha256": hashlib.sha256(payload).hexdigest(),
            }},
        }
        self.save()

    def save(self):
        (self.previous / "result.json").write_text(json.dumps(self.saved))

    def invoke(self, stderr=None):
        error = subprocess.CalledProcessError(
            1, ["docker", "volume", "inspect", self.run],
            stderr=stderr or f"get {self.run}: no such volume")
        with mock.patch.object(reconcile_devx.reference, "ROOT", self.root), \
                mock.patch.object(reconcile_devx.reference, "owned_delete") as delete, \
                mock.patch.object(reconcile_devx.reference, "docker", side_effect=error), \
                mock.patch.object(reconcile_devx.native_base, "load") as load, \
                mock.patch.object(reconcile_devx.upstream_bats, "archive_stopped_volumes") as archive, \
                mock.patch.object(sys, "argv", ["reconcile_devx", self.run]), \
                mock.patch("builtins.print"):
            try:
                reconcile_devx.main()
            finally:
                load.assert_not_called()
                archive.assert_not_called()
                self.assertEqual(delete.call_args_list, [
                    mock.call("container", self.run, self.owner),
                    mock.call("container", self.run + "-archive-0", self.owner),
                ])
                self.assertEqual((self.archive / "result.json").read_bytes(), self.original_bytes)

    def test_missing_volume_reuses_verified_archive_after_delete_timeout(self):
        self.invoke()
        receipts = list((self.archive / "reconciliations").glob("*/result.json"))
        receipt = json.loads(next(path for path in receipts if path.parent != self.previous).read_text())
        self.assertTrue(receipt["cleanup"])
        self.assertTrue(receipt["passed"])
        reused = receipt["archive_reused_from"]
        self.assertEqual(reused["reconciliation"], self.previous.name)
        self.assertEqual(reused["sha256"], self.saved["failed_volume_archives"][self.run]["sha256"])
        self.assertEqual(reused["receipt_sha256"], reconcile_devx.reference.sha(self.previous / "result.json"))

    def test_missing_volume_without_completed_archive_fails(self):
        del self.saved["failed_volume_archives"]
        self.save()
        with self.assertRaisesRegex(ValueError, "no verified completed archive"):
            self.invoke()

    @unittest.skipUnless(shutil.which("zstd"), "compaction requires zstd")
    def test_verified_compaction_can_still_prove_cleanup_without_recreating_a_raw_dump(self):
        self.saved["cleanup"] = True
        self.save()
        payload = self.previous / "failed-volume-0.tar"
        reconcile_devx.reference.artifact_storage.compact(payload, self.root)
        self.invoke()
        receipts = list((self.archive / "reconciliations").glob("*/result.json"))
        result = json.loads(next(path for path in receipts if path.parent != self.previous).read_text())
        self.assertTrue(result["cleanup"])
        self.assertEqual(result["archive_reused_from"]["encoding"]["file"], "failed-volume-0.tar.zst")
        self.assertFalse(payload.exists())

    def repack(self):
        payload = self.previous / "failed-volume-0.tar"
        storage = reconcile_devx.reference.artifact_storage
        self.saved["cleanup"] = True
        self.save()
        original = storage.compact(payload, self.root)
        source = self.previous / original["encoded"]["file"]
        replacement = self.root / "artifacts" / ("factorybox-storage-repack-" + "d" * 32)
        replacement.mkdir()
        encoded = replacement / source.name
        # A replacement is independent bytes with its own pinned receipt. The
        # codec need not change for this test of archive discovery and trust.
        shutil.copyfile(source, encoded)
        result = replacement / "result.json"
        result.write_text(json.dumps({
            "operation": "lossless_closed_archive_repack", "passed": True,
            "cleanup": True, "source_removed": True,
            "source": str(source), "source_encoded_sha256": original["encoded"]["sha256"],
            "source_encoded_bytes": original["encoded"]["bytes"],
            "new_archive": str(encoded), "encoded_sha256": storage_hash(encoded),
            "encoded_bytes": encoded.stat().st_size,
            "raw_bytes": original["original"]["bytes"],
            "raw_sha256": original["original"]["sha256"],
            "whole_decoded_bytes_verified": True,
        }))
        pointer = source.with_name(source.name + ".repacked.json")
        pointer.write_text(json.dumps({
            "schema": 1, "original_archive": str(source),
            "original_encoded_sha256": original["encoded"]["sha256"],
            "replacement_archive": str(encoded),
            "replacement_encoded_sha256": storage_hash(encoded),
            "original_tar_bytes": original["original"]["bytes"],
            "original_tar_sha256": original["original"]["sha256"],
            "verified_result": str(result), "verified_result_sha256": storage_hash(result),
        }))
        source.unlink()
        return payload, pointer, encoded, result

    @unittest.skipUnless(shutil.which("zstd"), "compaction requires zstd")
    def test_losslessly_repacked_archive_proves_cleanup_without_a_raw_copy(self):
        payload, _, encoded, _ = self.repack()
        self.invoke()
        receipts = list((self.archive / "reconciliations").glob("*/result.json"))
        result = json.loads(next(path for path in receipts if path.parent != self.previous).read_text())
        self.assertTrue(result["cleanup"])
        self.assertEqual(result["archive_reused_from"]["encoding"]["artifact_path"], str(encoded))
        self.assertFalse(payload.exists())

    @unittest.skipUnless(shutil.which("zstd"), "compaction requires zstd")
    def test_repacked_archive_rejects_changed_receipts_paths_and_contents(self):
        _, pointer, encoded, result = self.repack()
        saved_pointer, saved_result = pointer.read_text(), result.read_text()
        cases = [
            ("original_encoded_sha256", "0" * 64),
            ("original_tar_bytes", 1),
            ("original_tar_sha256", "0" * 64),
            ("replacement_encoded_sha256", "0" * 64),
            ("verified_result_sha256", "0" * 64),
            ("replacement_archive", str(self.root / encoded.name)),
        ]
        for key, value in cases:
            with self.subTest(key=key):
                record = json.loads(saved_pointer)
                record[key] = value
                pointer.write_text(json.dumps(record))
                with self.assertRaisesRegex(ValueError, "no longer matches"):
                    self.invoke()
                pointer.write_text(saved_pointer)
        result.write_text(saved_result + "\n")
        with self.assertRaisesRegex(ValueError, "no longer matches"):
            self.invoke()
        result.write_text(saved_result)
        content = encoded.read_bytes()
        encoded.write_bytes(b"X" + content[1:])
        with self.assertRaisesRegex(ValueError, "no longer matches"):
            self.invoke()
        encoded.write_bytes(content)
        foreign = self.root / "foreign.zst"
        encoded.rename(foreign)
        encoded.symlink_to(foreign)
        with self.assertRaisesRegex(ValueError, "no longer matches"):
            self.invoke()

    @unittest.skipUnless(shutil.which("zstd"), "compaction requires zstd")
    def test_repack_receipt_cannot_substitute_different_decoded_bytes(self):
        _, pointer, encoded, result = self.repack()
        original = b"retained complete stopped runtime archive"
        encoded.write_bytes(subprocess.check_output(
            ["zstd", "-q", "-c"], input=b"X" + original[1:]))
        saved_result = json.loads(result.read_text())
        saved_result.update(encoded_sha256=storage_hash(encoded),
                            encoded_bytes=encoded.stat().st_size)
        result.write_text(json.dumps(saved_result))
        saved_pointer = json.loads(pointer.read_text())
        saved_pointer.update(replacement_encoded_sha256=storage_hash(encoded),
                             verified_result_sha256=storage_hash(result))
        pointer.write_text(json.dumps(saved_pointer))
        with self.assertRaisesRegex(ValueError, "no longer matches"):
            self.invoke()

    def test_changed_archive_fails(self):
        path = self.previous / "failed-volume-0.tar"
        payload = path.read_bytes()
        path.write_bytes(b"X" + payload[1:])
        with self.assertRaisesRegex(ValueError, "no longer matches"):
            self.invoke()

    def test_foreign_archive_receipt_fails(self):
        self.saved["owner"] = "c" * 32
        self.save()
        with self.assertRaisesRegex(ValueError, "no verified completed archive"):
            self.invoke()

    def test_inspection_error_is_not_absence(self):
        with self.assertRaises(subprocess.CalledProcessError):
            self.invoke(stderr="permission denied")


class IdeReconcileTests(unittest.TestCase):
    def test_failed_ide_cleanup_archives_and_removes_only_its_exact_resources(self):
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            owner = "a" * 32
            run = "factorybox-devx-lifecycle-" + owner
            archive = root / "artifacts" / run
            (archive / "theia").mkdir(parents=True)
            volume = run + "-ide-work"
            original = {"owner": owner, "passed": False, "cleanup": True,
                        "native_base_build": "frozen", "theia": {
                            "owner": owner, "cleanup": False, "container_name": run + "-ide",
                            "setup_name": run + "-ide-setup", "volume_name": volume,
                            "cleanup_error": "owned container removal timed out"}}
            original_bytes = json.dumps(original).encode()
            (archive / "result.json").write_bytes(original_bytes)
            observed = [{"Name": volume, "Labels": {"com.factorybox.run": owner}}]
            with mock.patch.object(reconcile_devx.reference, "ROOT", root), \
                    mock.patch.object(reconcile_devx.reference, "docker",
                                      return_value=json.dumps(observed)), \
                    mock.patch.object(reconcile_devx.reference, "owned_delete") as remove, \
                    mock.patch.object(reconcile_devx.native_base, "load", return_value=("image", {})), \
                    mock.patch.object(reconcile_devx.upstream_bats, "archive_stopped_volumes") as retain, \
                    mock.patch.object(sys, "argv", ["reconcile_devx", run, "--ide"]), \
                    mock.patch("builtins.print"):
                reconcile_devx.main()
            self.assertEqual(remove.call_args_list, [
                mock.call("container", run + "-ide-setup", owner),
                mock.call("container", run + "-ide", owner),
                mock.call("container", run + "-ide-archive-0", owner),
                mock.call("volume", volume, owner),
            ])
            args = retain.call_args.args
            self.assertEqual(args[1:5], (run + "-ide", owner, [volume], "image"))
            self.assertEqual((archive / "result.json").read_bytes(), original_bytes)
            receipts = list((archive / "theia/reconciliations").glob("*/result.json"))
            self.assertEqual(len(receipts), 1)
            result = json.loads(receipts[0].read_text())
            self.assertTrue(result["cleanup"])
            self.assertEqual(result["resource"], "ide")

    def test_foreign_ide_identity_is_refused_before_resource_actions(self):
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            owner = "a" * 32
            run = "factorybox-devx-lifecycle-" + owner
            archive = root / "artifacts" / run
            archive.mkdir(parents=True)
            (archive / "result.json").write_text(json.dumps({
                "owner": owner, "passed": False, "cleanup": True,
                "theia": {"owner": "foreign", "cleanup": False, "volume_name": run + "-ide-work"}}))
            with mock.patch.object(reconcile_devx.reference, "ROOT", root), \
                    mock.patch.object(reconcile_devx.reference, "owned_delete") as remove, \
                    mock.patch.object(sys, "argv", ["reconcile_devx", run, "--ide"]), \
                    mock.patch("builtins.print"):
                with self.assertRaisesRegex(ValueError, "matching unresolved IDE"):
                    reconcile_devx.main()
                remove.assert_not_called()


def storage_hash(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


if __name__ == "__main__":
    unittest.main()
