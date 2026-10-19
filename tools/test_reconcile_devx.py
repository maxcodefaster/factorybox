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


if __name__ == "__main__":
    unittest.main()
