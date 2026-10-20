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
    def test_completed_component_inputs_share_verified_bytes_without_another_disk_copy(self):
        import hashlib
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            build = root / "artifacts/factorybox-build-sysbox-frozen"
            build.mkdir(parents=True)
            (build / "result.json").write_text('{"passed": true}')
            source = build / "sysbox-runc"
            source.write_bytes(os.urandom(8192))
            source.chmod(0o755)
            target = root / "artifacts/factorybox-runtime-new/sysbox-runc"
            target.parent.mkdir()
            digest = hashlib.sha256(source.read_bytes()).hexdigest()
            before = artifact_storage.allocated_bytes(root / "artifacts")
            artifact_storage.share_frozen_input(source, target, root, digest)
            self.assertEqual(source.stat().st_ino, target.stat().st_ino)
            self.assertEqual(artifact_storage.allocated_bytes(root / "artifacts"), before)
            self.assertEqual(target.stat().st_mode & 0o777, 0o755)
            with self.assertRaises(FileExistsError):
                artifact_storage.share_frozen_input(source, target, root, digest)
            target.unlink()
            self.assertEqual(hashlib.sha256(source.read_bytes()).hexdigest(), digest)

    def test_shared_component_rejects_changed_unfinished_or_symlink_inputs(self):
        import hashlib
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            build = root / "artifacts/factorybox-build-criu-frozen"
            build.mkdir(parents=True)
            result = build / "result.json"
            result.write_text('{"passed": true}')
            source = build / "criu"
            source.write_bytes(b"original")
            target = root / "artifacts/new/criu"
            target.parent.mkdir()
            digest = hashlib.sha256(source.read_bytes()).hexdigest()
            link = build / "linked"
            link.symlink_to(source)
            for supplied, expected in [(source, "0" * 64), (link, digest)]:
                with self.subTest(source=str(supplied)), self.assertRaises(ValueError):
                    artifact_storage.share_frozen_input(supplied, target, root, expected)
                self.assertFalse(target.exists())
            result.write_text('{"passed": false}')
            with self.assertRaisesRegex(ValueError, "did not pass"):
                artifact_storage.share_frozen_input(source, target, root, digest)
            self.assertFalse(target.exists())

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

    @unittest.skipUnless(shutil.which("zstd"), "compaction requires zstd")
    def test_closed_run_retention_preserves_complete_failed_evidence_and_its_result(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            run = root / "artifacts/factorybox-runtime-closed"
            output = run / "output"
            output.mkdir(parents=True)
            raw = output / "capture-source.tar"
            raw.write_bytes(b"failed native evidence\n" * 450000)
            original = {"file": raw.name, "bytes": raw.stat().st_size,
                        "sha256": reference.sha(raw)}
            result = run / "result.json"
            result.write_text('{"cleanup": true, "passed": false}\n')
            report = artifact_storage.compact_closed_run(run, root)
            self.assertFalse(raw.exists())
            self.assertEqual(result.read_text(), '{"cleanup": true, "passed": false}\n')
            self.assertEqual(report["errors"], [])
            self.assertEqual(artifact_storage.verify_compacted(raw, original),
                             report["compacted"][0]["encoded"])
            self.assertEqual(artifact_storage.compact_closed_run(run, root),
                             {"compacted": [], "errors": []})

    def test_automatic_retention_defers_live_runs_and_preserves_raw_data_on_storage_failure(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            run = root / "artifacts/factorybox-runtime-active"
            run.mkdir(parents=True)
            raw = run / "failed-volume-0.tar"
            raw.write_bytes(b"x" * (8 << 20))
            result = run / "result.json"
            result.write_text('{"cleanup": false}')
            with mock.patch.object(artifact_storage, "compact",
                                   side_effect=ValueError("storage exhausted")) as codec:
                self.assertIsNone(artifact_storage.compact_closed_run(run, root))
                codec.assert_not_called()
                result.write_text('{"cleanup": true, "passed": false}')
                report = artifact_storage.compact_closed_run(run, root)
            self.assertEqual(report["errors"][0]["error"], "storage exhausted")
            self.assertEqual(raw.read_bytes(), b"x" * (8 << 20))
            self.assertFalse(raw.with_name(raw.name + ".zst").exists())

    def test_compaction_refuses_to_allocate_past_the_artifact_budget(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            artifacts = root / "artifacts"
            artifacts.mkdir()
            raw = artifacts / "offline.tar"
            raw.write_bytes(b"retained original")
            with mock.patch.object(artifact_storage, "allocated_bytes",
                                   return_value=artifact_storage.ARTIFACT_LIMIT), \
                    mock.patch.object(artifact_storage.subprocess, "run") as codec:
                with self.assertRaisesRegex(ValueError, "artifact budget"):
                    artifact_storage.compact(raw, root)
                codec.assert_not_called()
            self.assertEqual(raw.read_bytes(), b"retained original")

    @unittest.skipUnless(shutil.which("zstd"), "stream archiving requires zstd")
    def test_decoder_consumes_the_stream_before_the_producer_finishes(self):
        import subprocess
        import sys
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            archive = root / "artifacts/stream-order"
            archive.mkdir(parents=True)
            observed = root / "decoder-started"
            original = artifact_storage.subprocess.Popen
            passthrough = """
import os,sys
from pathlib import Path
source=open(sys.argv[1],'rb') if len(sys.argv)>2 else sys.stdin.buffer
while data:=source.read(65536):
 if len(sys.argv)>1:Path(sys.argv[-1]).touch()
 sys.stdout.buffer.write(data)
 sys.stdout.buffer.flush()
"""
            def codec(args, **kwargs):
                if args[0] == "zstd":
                    arguments = []
                    if "-dc" in args:
                        if "--" in args:
                            arguments.append(args[-1])
                        arguments.append(str(observed))
                    args = [sys.executable, "-B", "-S", "-c", passthrough, *arguments]
                return original(args, **kwargs)
            producer = """
from pathlib import Path
import sys,time
sys.stdout.buffer.write(b'a'*(1<<20));sys.stdout.buffer.flush()
while not Path(sys.argv[1]).exists():time.sleep(.01)
sys.stdout.buffer.write(b'b'*(1<<20))
"""
            with (archive / "log").open("xb") as log, \
                    mock.patch.object(artifact_storage.subprocess, "Popen", side_effect=codec):
                record = artifact_storage.archive_stream(
                    [sys.executable, "-B", "-S", "-c", producer, str(observed)],
                    archive / "failed-volume-0.tar", root, log, maximum=8 << 20, timeout=2)
            self.assertEqual(record["original"]["bytes"], 2 << 20)
            self.assertTrue(record["whole_decompressed_payload_verified"])

    @unittest.skipUnless(shutil.which("zstd"), "stream archiving requires zstd")
    def test_stream_archive_preserves_more_raw_data_than_its_storage_budget_without_a_raw_copy(self):
        import hashlib
        import subprocess
        import sys
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            archive = root / "artifacts/closed-source"
            archive.mkdir(parents=True)
            source = root / "source"
            content = b"complete original source\n" * (1 << 20)
            source.write_bytes(content)
            raw = archive / "failed-volume-0.tar"
            command = [sys.executable, "-B", "-S", "-c",
                       "import sys;sys.stdout.buffer.write(open(sys.argv[1],'rb').read())", str(source)]
            with (archive / "log").open("xb") as log:
                record = artifact_storage.archive_stream(
                    command, raw, root, log, maximum=8 << 20, timeout=30)
            self.assertFalse(raw.exists())
            self.assertGreater(record["original"]["bytes"], 8 << 20)
            self.assertEqual(record["original"]["sha256"], hashlib.sha256(content).hexdigest())
            self.assertTrue(record["original_stream_hashed"])
            self.assertEqual(artifact_storage.verify_compacted(raw, record["original"]),
                             record["encoded"])
            self.assertEqual(source.read_bytes(), content)

    @unittest.skipUnless(shutil.which("zstd"), "stream archiving requires zstd")
    def test_failed_source_stream_cannot_publish_an_archive_receipt(self):
        import sys
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            archive = root / "artifacts/failed-source"
            archive.mkdir(parents=True)
            raw = archive / "failed-volume-0.tar"
            command = [sys.executable, "-B", "-S", "-c",
                       "import os;os.write(1,b'only a partial source');raise SystemExit(7)"]
            with (archive / "log").open("xb") as log:
                with self.assertRaisesRegex(ValueError, "source tar or bounded encoder failed"):
                    artifact_storage.archive_stream(
                        command, raw, root, log, maximum=8 << 20, timeout=30)
            self.assertEqual(sorted(p.name for p in archive.iterdir()), ["log"])

    @unittest.skipUnless(shutil.which("zstd"), "stream archiving requires zstd")
    def test_stream_archive_rejects_corruption_before_original_volume_cleanup_is_allowed(self):
        import sys
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            archive = root / "artifacts/corrupt-stream"
            archive.mkdir(parents=True)
            raw = archive / "failed-volume-0.tar"
            original = artifact_storage.subprocess.Popen
            def corrupt(args, **kwargs):
                if args[:3] == ["zstd", "-q", "-dc"]:
                    args = [sys.executable, "-B", "-S", "-c",
                            "import sys;sys.stdin.buffer.read();sys.stdout.buffer.write(b'corrupted decode')"]
                return original(args, **kwargs)
            with (archive / "log").open("xb") as log, \
                    mock.patch.object(artifact_storage.subprocess, "Popen", side_effect=corrupt):
                with self.assertRaisesRegex(ValueError, "decoded archive differs"):
                    artifact_storage.archive_stream(
                        [sys.executable, "-B", "-S", "-c", "print('complete original stream')"],
                        raw, root, log, maximum=8 << 20, timeout=30)
            self.assertEqual(sorted(p.name for p in archive.iterdir()), ["log"])

    @unittest.skipUnless(shutil.which("zstd"), "stream archiving requires zstd")
    def test_stored_archive_corruption_after_decode_cannot_publish_a_receipt(self):
        import sys
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            archive = root / "artifacts/changed-encoded-stream"
            archive.mkdir(parents=True)
            original = artifact_storage.hashlib.file_digest
            def changed(stream, algorithm):
                with Path(stream.name).open("r+b") as writable:
                    value = writable.read(1)
                    writable.seek(0)
                    writable.write(bytes([value[0] ^ 1]))
                return original(stream, algorithm)
            with (archive / "log").open("xb") as log, \
                    mock.patch.object(artifact_storage.hashlib, "file_digest", side_effect=changed):
                with self.assertRaisesRegex(ValueError, "encoded archive changed"):
                    artifact_storage.archive_stream(
                        [sys.executable, "-B", "-S", "-c", "print('complete retained original')"],
                        archive / "failed-volume-0.tar", root, log, maximum=8 << 20, timeout=5)
            self.assertEqual(sorted(p.name for p in archive.iterdir()), ["log"])

    @unittest.skipUnless(shutil.which("zstd"), "stream archiving requires zstd")
    def test_encoded_storage_cap_reaps_the_pipeline_and_removes_its_incomplete_copy(self):
        import subprocess
        import sys
        original, children = artifact_storage.subprocess.Popen, []
        def launch(*args, **kwargs):
            child = original(*args, **kwargs)
            children.append(child)
            return child
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            archive = root / "artifacts/full-encoded-storage"
            archive.mkdir(parents=True)
            source = root / "source"
            content = os.urandom(3 << 20)
            source.write_bytes(content)
            with (archive / "log").open("xb") as log, \
                    mock.patch.object(artifact_storage.subprocess, "Popen", side_effect=launch):
                with self.assertRaises((ValueError, BrokenPipeError)):
                    artifact_storage.archive_stream(
                        [sys.executable, "-B", "-S", "-c",
                         "import sys;sys.stdout.buffer.write(open(sys.argv[1],'rb').read())", str(source)],
                        archive / "failed-volume-0.tar", root, log, maximum=2 << 20, timeout=5)
            self.assertTrue(children)
            self.assertTrue(all(child.poll() is not None for child in children))
            self.assertEqual(source.read_bytes(), content)
            self.assertEqual(sorted(p.name for p in archive.iterdir()), ["log"])

    @unittest.skipUnless(shutil.which("zstd"), "stream archiving requires zstd")
    def test_stream_deadline_reaps_codec_and_source_without_changing_parent_limits(self):
        import resource
        import subprocess
        import sys
        before = resource.getrlimit(resource.RLIMIT_FSIZE)
        original, children = artifact_storage.subprocess.Popen, []
        def launch(*args, **kwargs):
            child = original(*args, **kwargs)
            children.append(child)
            return child
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            archive = root / "artifacts/timed-out-stream"
            archive.mkdir(parents=True)
            with (archive / "log").open("xb") as log, \
                    mock.patch.object(artifact_storage.subprocess, "Popen", side_effect=launch):
                with self.assertRaises(subprocess.TimeoutExpired):
                    artifact_storage.archive_stream(
                        [sys.executable, "-B", "-S", "-c", "import time;time.sleep(30)"],
                        archive / "failed-volume-0.tar", root, log, maximum=8 << 20, timeout=0.2)
            self.assertTrue(children)
            self.assertTrue(all(child.poll() is not None for child in children))
            self.assertEqual(sorted(p.name for p in archive.iterdir()), ["log"])
        self.assertEqual(resource.getrlimit(resource.RLIMIT_FSIZE), before)
