import json
import os
from pathlib import Path
import shutil
import tempfile
import unittest
from unittest import mock

import artifact_storage
import reference


class ArtifactStorageTests(unittest.TestCase):
    def test_accounting_counts_hardlinks_once_and_does_not_follow_foreign_symlinks(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            artifacts = root / "artifacts"
            artifacts.mkdir()
            payload = artifacts / "payload"
            payload.write_bytes(os.urandom(8192))
            os.link(payload, artifacts / "duplicate")
            foreign = root / "foreign"
            foreign.mkdir()
            (foreign / "large").write_bytes(os.urandom(16384))
            (artifacts / "foreign").symlink_to(foreign, target_is_directory=True)
            self.assertEqual(artifact_storage.allocated_bytes(artifacts),
                             payload.stat().st_blocks * 512)

    def test_run_is_rejected_before_a_directory_is_created_when_budget_is_exhausted(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            with mock.patch.object(reference, "ROOT", root), \
                    mock.patch.object(artifact_storage, "require_capacity",
                                      side_effect=ValueError("storage exhausted")):
                with self.assertRaisesRegex(ValueError, "storage exhausted"):
                    reference.new_artifact("factorybox-run-test")
            self.assertFalse((root / "artifacts").exists())

    def test_both_artifact_limit_and_host_reserve_reject_a_new_run(self):
        with tempfile.TemporaryDirectory() as directory:
            with mock.patch.object(artifact_storage, "allocated_bytes", return_value=25), \
                    mock.patch.object(artifact_storage.shutil, "disk_usage") as usage:
                usage.return_value.free = 100
                with self.assertRaisesRegex(ValueError, "artifact storage"):
                    artifact_storage.require_capacity(
                        directory, limit=32, run_reserve=8, free_reserve=12)
                self.assertEqual(artifact_storage.require_capacity(
                    directory, limit=33, run_reserve=8, free_reserve=12)["allocated_bytes"], 25)
                usage.return_value.free = 19
                with self.assertRaisesRegex(ValueError, "reserved free disk"):
                    artifact_storage.require_capacity(
                        directory, limit=33, run_reserve=8, free_reserve=12)

    @unittest.skipUnless(shutil.which("zstd"), "compaction requires zstd")
    def test_real_compaction_preserves_every_byte_and_publishes_a_reconstruction_receipt(self):
        import hashlib
        import subprocess
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            artifacts = root / "artifacts"
            artifacts.mkdir()
            payload = artifacts / "offline.tar"
            content = b"retained evidence\n" * 65536
            payload.write_bytes(content)
            record = artifact_storage.compact(payload, root)
            self.assertFalse(payload.exists())
            self.assertEqual(record["original"]["sha256"], hashlib.sha256(content).hexdigest())
            self.assertTrue(record["whole_decompressed_payload_verified"])
            encoded = artifacts / record["encoded"]["file"]
            self.assertEqual(subprocess.check_output(["zstd", "-q", "-dc", str(encoded)]), content)
            self.assertEqual(json.loads((artifacts / "offline.tar.encoding.json").read_text()), record)

    @unittest.skipUnless(shutil.which("zstd"), "compaction requires zstd")
    def test_distant_identical_ram_payloads_are_compacted_without_changing_contents(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            artifacts = root / "artifacts"
            artifacts.mkdir()
            memory = os.urandom(2 << 20)
            content = b"".join(memory + os.urandom(512 << 10) for _ in range(4))
            payload = artifacts / "repeated-memory.tar"
            payload.write_bytes(content)
            record = artifact_storage.compact(payload, root)
            self.assertLess(record["encoded"]["bytes"], len(content) * 0.6)
            self.assertEqual(artifact_storage.verify_compacted(
                payload, record["original"]), record["encoded"])

    @unittest.skipUnless(shutil.which("zstd"), "compaction requires zstd")
    def test_corrupted_compression_preserves_the_only_original(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            artifacts = root / "artifacts"
            artifacts.mkdir()
            payload = artifacts / "offline.tar"
            payload.write_bytes(b"only original retained data")
            real_run = artifact_storage.subprocess.run
            def corrupt(command, **kwargs):
                completed = real_run(command, **kwargs)
                (artifacts / "offline.tar.zst.partial").write_bytes(b"corrupt")
                return completed
            with mock.patch.object(artifact_storage.subprocess, "run", side_effect=corrupt):
                with self.assertRaisesRegex(ValueError, "incomplete"):
                    artifact_storage.compact(payload, root)
            self.assertEqual(payload.read_bytes(), b"only original retained data")
            self.assertFalse((artifacts / "offline.tar.zst").exists())
            self.assertFalse((artifacts / "offline.tar.zst.partial").exists())

    def test_compaction_refuses_foreign_and_symlink_payloads(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            artifacts = root / "artifacts"
            artifacts.mkdir()
            foreign = root / "foreign"
            foreign.write_bytes(b"do not change")
            link = artifacts / "linked.tar"
            link.symlink_to(foreign)
            for path in [foreign, link]:
                with self.subTest(path=path), self.assertRaises(ValueError):
                    artifact_storage.compact(path, root)
            self.assertEqual(foreign.read_bytes(), b"do not change")

    def test_compaction_refuses_active_runs_and_frozen_inputs_before_launching_a_codec(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            for name in ["factorybox-runtime-active", "factorybox-devx-inputs-frozen"]:
                run = root / "artifacts" / name
                run.mkdir(parents=True)
                payload = run / "payload.tar"
                payload.write_bytes(b"preserve")
                if "inputs" in name:
                    (run / "result.json").write_text('{"cleanup": true}')
                with mock.patch.object(artifact_storage.subprocess, "run") as command:
                    with self.assertRaises(ValueError):
                        artifact_storage.compact(payload, root)
                    command.assert_not_called()
                self.assertEqual(payload.read_bytes(), b"preserve")
