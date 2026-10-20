import hashlib
import json
from pathlib import Path
import tempfile
import unittest
from unittest import mock

import criu_candidate


class CandidatePatchTests(unittest.TestCase):
    def fixture(self, root, extra):
        name = "factorybox-build-criu-retained-" + "a" * 32
        archive = root / "artifacts" / name
        archive.mkdir(parents=True)
        (root / "upstream").mkdir()
        (root / "upstream/lock.json").write_text("{}")
        contents = {
            "criu": b"frozen binary",
            "source-lock.json": b'{"components":{"criu":{"archive":"frozen-source.tar.gz"}}}',
            "retained.patch": b"frozen original patch",
            "early-hold.patch": b"frozen additional patch",
        }
        for filename, data in contents.items():
            (archive / filename).write_bytes(data)
        digest = lambda data: hashlib.sha256(data).hexdigest()
        result = {
            "passed": True, "cleanup": True, "diagnostic_only": True,
            "binary_sha256": digest(contents["criu"]),
            "source_lock_sha256": digest(contents["source-lock.json"]),
            "patch_sha256": digest(contents["retained.patch"]),
            "base_source_lock_sha256": digest(b"{}"), "context_sha256": "frozen-context",
        }
        if extra:
            result["candidate_patch_sha256"] = {
                "retained.patch": result["patch_sha256"],
                "early-hold.patch": digest(contents["early-hold.patch"]),
            }
        return name, archive, result

    def test_original_and_additional_frozen_patch_inputs_load(self):
        for extra in [False, True]:
            with self.subTest(extra=extra), tempfile.TemporaryDirectory() as directory:
                root = Path(directory)
                name, archive, result = self.fixture(root, extra)
                (archive / "result.json").write_text(json.dumps(result))
                with mock.patch.object(criu_candidate.reference, "ROOT", root), \
                        mock.patch.object(criu_candidate.sources, "tree_sha", return_value="frozen-context"), \
                        mock.patch.object(criu_candidate.sources, "validate_inputs"), \
                        mock.patch.object(criu_candidate.sources, "check_source", return_value=1030):
                    self.assertEqual(criu_candidate.load(name)[0], archive)

    def test_changed_additional_patch_refused_before_source_validation(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            name, archive, result = self.fixture(root, True)
            (archive / "result.json").write_text(json.dumps(result))
            (archive / "early-hold.patch").write_bytes(b"changed patch")
            with mock.patch.object(criu_candidate.reference, "ROOT", root), \
                    mock.patch.object(criu_candidate.sources, "validate_inputs") as validate:
                with self.assertRaisesRegex(ValueError, "candidate patch input changed"):
                    criu_candidate.load(name)
                validate.assert_not_called()

    def test_unrecognized_patch_path_refused(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            name, archive, result = self.fixture(root, True)
            result["candidate_patch_sha256"] = {"../outside-artifact": "unused"}
            (archive / "result.json").write_text(json.dumps(result))
            with mock.patch.object(criu_candidate.reference, "ROOT", root), \
                    mock.patch.object(criu_candidate.sources, "validate_inputs") as validate:
                with self.assertRaisesRegex(ValueError, "candidate patch input changed"):
                    criu_candidate.load(name)
                validate.assert_not_called()

    def test_overlay_followups_require_their_own_frozen_patch(self):
        for filename in ["nested-overlay.patch", "nested-overlay-stable-path.patch",
                         "nested-overlay-shared-root.patch"]:
            with self.subTest(patch=filename), tempfile.TemporaryDirectory() as directory:
                root = Path(directory)
                name, archive, result = self.fixture(root, True)
                patch = archive / filename
                patch.write_bytes(b"frozen additional overlay patch")
                result["candidate_patch_sha256"][filename] = hashlib.sha256(patch.read_bytes()).hexdigest()
                (archive / "result.json").write_text(json.dumps(result))
                with mock.patch.object(criu_candidate.reference, "ROOT", root), \
                        mock.patch.object(criu_candidate.sources, "tree_sha", return_value="frozen-context"), \
                        mock.patch.object(criu_candidate.sources, "validate_inputs"), \
                        mock.patch.object(criu_candidate.sources, "check_source", return_value=1030):
                    self.assertEqual(criu_candidate.load(name)[0], archive)
                    patch.write_bytes(b"changed overlay patch")
                    with self.assertRaisesRegex(ValueError, "candidate patch input changed"):
                        criu_candidate.load(name)
