from pathlib import Path
import tempfile
import unittest

import build_worker
import reference


class OriginalWorkerLicenseTests(unittest.TestCase):
    def test_internal_unlicensed_builds_remain_unpublished_and_choose_no_license(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary).resolve()
            bundle = root / "bundle"
            bundle.mkdir()
            self.assertIsNone(build_worker.copy_original_license(root, bundle))
            self.assertFalse((bundle / "licenses/factorybox/LICENSE").exists())
            self.assertFalse((root / "LICENSE").exists())

    def test_only_the_exact_nonempty_regular_original_notice_enters_the_bundle(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary).resolve()
            bundle = root / "bundle"
            bundle.mkdir()
            original = root / "LICENSE"
            original.write_text("Operator-approved original-code license fixture.\n")
            digest = build_worker.copy_original_license(root, bundle)
            self.assertEqual(digest, reference.sha(original))
            self.assertEqual((bundle / "licenses/factorybox/LICENSE").read_bytes(), original.read_bytes())

    def test_empty_or_redirected_license_is_not_accepted(self):
        for redirected in [False, True]:
            with self.subTest(redirected=redirected), tempfile.TemporaryDirectory() as temporary:
                root = Path(temporary).resolve()
                bundle = root / "bundle"
                bundle.mkdir()
                if redirected:
                    (root / "other").write_text("not the original path")
                    (root / "LICENSE").symlink_to(root / "other")
                else:
                    (root / "LICENSE").touch()
                with self.assertRaises(ValueError):
                    build_worker.copy_original_license(root, bundle)
                self.assertFalse((bundle / "licenses/factorybox/LICENSE").exists())
