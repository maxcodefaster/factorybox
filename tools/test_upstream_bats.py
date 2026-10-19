import json
import pathlib
import tempfile
import unittest
from unittest import mock

import upstream_bats


class BatsEvidenceTests(unittest.TestCase):
    def test_zero_exit_cannot_hide_a_missing_or_failed_assertion(self):
        expected = ["first", "second"]
        observed = upstream_bats.observations("1..2\nok 1 first\nnot ok 2 second\n", expected)
        self.assertTrue(observed["complete"])
        self.assertEqual(observed["failed"], ["second"])
        for text in ["1..2\nok 1 first\n", "1..2\nok 1 first\nok 1 second\n",
                     "1..2\nok 1 first\nok 2 changed\n", "1..2\n1..2\nok 1 first\nok 2 second\n"]:
            with self.subTest(text=text):
                self.assertFalse(upstream_bats.observations(text, expected)["complete"])

    def test_skipped_upstream_case_is_not_an_executed_pass(self):
        observed = upstream_bats.observations(
            "1..2\nok 1 first # skip cgroup v1 required\nok 2 second\n", ["first", "second"])
        self.assertTrue(observed["complete"])
        self.assertEqual(observed["skipped"], ["first"])
        self.assertEqual(observed["executed_passes"], 1)

    def test_image_archive_rejects_changed_payload_and_failed_acquisition(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = pathlib.Path(temporary)
            name = "factorybox-bats-images-" + "a" * 32
            archive = root / "artifacts" / name
            (archive / "output").mkdir(parents=True)
            (root / "upstream").mkdir()
            (root / "build").mkdir()
            pins = root / "upstream/bats-images.json"
            pins.write_text(json.dumps({"images": [{"alias": "original:latest", "image": "original@sha256:" + "b" * 64}]}))
            dockerfile = root / "build/upstream-bats.Dockerfile"
            dockerfile.write_text("FROM pinned\n")
            rows = [{"alias": "original:latest", "image": "original@sha256:" + "b" * 64,
                     "id": "sha256:" + "c" * 64}]
            image_json = archive / "output/images.json"
            image_json.write_text(json.dumps(rows))
            payload = archive / "output/images.tar"
            payload.write_bytes(b"original archive payload")
            result = {
                "passed": True, "cleanup": True, "native_base_build": "exact-base",
                "inputs_sha256": {"pins.json": upstream_bats.file_sha(pins),
                                 "context/Dockerfile": upstream_bats.file_sha(dockerfile)},
                "images_json_sha256": upstream_bats.file_sha(image_json),
                "images_tar_sha256": upstream_bats.file_sha(payload), "images": rows,
            }
            receipt = archive / "result.json"
            receipt.write_text(json.dumps(result))
            with mock.patch.object(upstream_bats, "ROOT", root):
                directory, _ = upstream_bats.image_archive(name, "exact-base")
                self.assertEqual(directory, archive / "output")
                payload.write_bytes(b"substituted image archive")
                with self.assertRaises(ValueError):
                    upstream_bats.image_archive(name, "exact-base")
                payload.write_bytes(b"original archive payload")
                result["passed"] = False
                receipt.write_text(json.dumps(result))
                with self.assertRaises(ValueError):
                    upstream_bats.image_archive(name, "exact-base")


if __name__ == "__main__":
    unittest.main()
