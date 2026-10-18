import hashlib
import io
import json
import pathlib
import tarfile
import tempfile
import unittest
from unittest.mock import patch

import sources


class SourceIntegrityTests(unittest.TestCase):
    def test_changed_upstream_assertion_fails_verification(self):
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            archive = root / "upstream.tar"
            with tarfile.open(archive, "w") as tf:
                info = tarfile.TarInfo("upstream/test/core.c")
                payload = b"assert(state_preserved);\n"
                info.size = len(payload)
                tf.addfile(info, io.BytesIO(payload))
            tree = root / "prepared"
            (tree / "test").mkdir(parents=True)
            (tree / "test/core.c").write_bytes(payload)
            self.assertEqual(sources.check_source(tree, archive, {"prefix": "upstream"}), 1)
            (tree / "test/core.c").write_text("/* assertion removed */\n")
            with self.assertRaisesRegex(ValueError, "upstream test changed"):
                sources.check_source(tree, archive, {"prefix": "upstream"})

    def test_umbrella_shell_tests_are_also_immutable(self):
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            archive = root / "upstream.tar"
            payload = b"@test preserved { check_original_behavior; }\n"
            with tarfile.open(archive, "w") as tf:
                info = tarfile.TarInfo("upstream/tests/core.bats")
                info.size = len(payload)
                tf.addfile(info, io.BytesIO(payload))
            tree = root / "prepared"
            (tree / "tests").mkdir(parents=True)
            (tree / "tests/core.bats").write_bytes(payload)
            self.assertEqual(sources.check_source(tree, archive, {"prefix": "upstream"}), 1)
            (tree / "tests/core.bats").write_text("@test always_passes { true; }\n")
            with self.assertRaisesRegex(ValueError, "upstream test changed"):
                sources.check_source(tree, archive, {"prefix": "upstream"})

    def test_archive_traversal_cannot_write_outside_staging(self):
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            cache = root / ".cache/downloads"
            cache.mkdir(parents=True)
            archive = cache / "bad.tar"
            with tarfile.open(archive, "w") as tf:
                info = tarfile.TarInfo("../outside")
                info.size = 1
                tf.addfile(info, io.BytesIO(b"x"))
            component = {
                "archive": "bad.tar", "sha256": hashlib.sha256(archive.read_bytes()).hexdigest(),
                "prefix": "upstream", "local_inputs_sha256": {}, "overlays": {}, "patches": [],
            }
            with patch.object(sources, "ROOT", root):
                with self.assertRaises(tarfile.FilterError):
                    sources.prepare("test", component, True)
            self.assertFalse((root / ".cache/sources/outside").exists())

    def test_changed_patch_is_rejected_before_preparation(self):
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            (root / "fix.patch").write_bytes(b"changed")
            component = {"local_inputs_sha256": {
                "fix.patch": hashlib.sha256(b"original").hexdigest()}}
            with patch.object(sources, "ROOT", root):
                with self.assertRaisesRegex(ValueError, "local source input differs"):
                    sources.validate_inputs(component)

    def test_lock_uses_only_immutable_archive_commits(self):
        lock = json.loads((sources.ROOT / "upstream/lock.json").read_text())
        for component in lock["components"].values():
            self.assertRegex(component["commit"], r"^[0-9a-f]{40}$")
            self.assertRegex(component["sha256"], r"^[0-9a-f]{64}$")
            self.assertTrue(component["url"].endswith("/" + component["commit"]))
