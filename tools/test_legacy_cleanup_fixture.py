from pathlib import Path
import tempfile
import unittest

import bounded_store_native


class LegacyCleanupFixtureTests(unittest.TestCase):
    def fixture(self, root):
        current, frozen = root / "current", root / "frozen"
        for directory in [current, frozen]:
            (directory / "internal/lifecycle").mkdir(parents=True)
            (directory / "cmd/factorybox").mkdir(parents=True)
        name = "internal/lifecycle/original_test.go"
        (frozen / name).write_text("unchanged original assertions\n")
        (current / name).write_text("current assertions for a different API\n")
        (current / "internal/lifecycle/capture_stop_wait_linux_test.go").write_text("new API tests\n")
        for name in [
            "internal/lifecycle/stopped_capture_delete_native_linux_test.go",
            "internal/lifecycle/stopped_capture_retirement_native_linux_test.go",
        ]:
            (current / name).write_text("explicit native maintenance fixture\n")
        return current, frozen

    def test_maintenance_extends_the_original_tests_without_overlaying_new_api_tests(self):
        with tempfile.TemporaryDirectory() as directory:
            current, frozen = self.fixture(Path(directory))
            receipt = bounded_store_native.overlay_frozen_tests(current, frozen, maintenance=True)
            self.assertEqual(receipt["mode"], "standalone-stopped-capture-cleanup")
            self.assertEqual(len(receipt["added_tests"]), 2)
            self.assertEqual((frozen / "internal/lifecycle/original_test.go").read_text(),
                             "unchanged original assertions\n")
            self.assertFalse((frozen / "internal/lifecycle/capture_stop_wait_linux_test.go").exists())
            self.assertEqual(receipt["original_tests_preserved"], True)

    def test_maintenance_refuses_to_replace_original_tests(self):
        with tempfile.TemporaryDirectory() as directory:
            current, frozen = self.fixture(Path(directory))
            name = "internal/lifecycle/stopped_capture_delete_native_linux_test.go"
            (frozen / name).write_text("original preserved assertions\n")
            with self.assertRaisesRegex(ValueError, "original test"):
                bounded_store_native.overlay_frozen_tests(current, frozen, maintenance=True)
            self.assertEqual((frozen / name).read_text(), "original preserved assertions\n")

    def test_default_overlay_keeps_the_existing_complete_current_test_behavior(self):
        with tempfile.TemporaryDirectory() as directory:
            current, frozen = self.fixture(Path(directory))
            receipt = bounded_store_native.overlay_frozen_tests(current, frozen, maintenance=False)
            self.assertEqual(receipt["mode"], "all-current-tests")
            self.assertEqual((frozen / "internal/lifecycle/original_test.go").read_text(),
                             "current assertions for a different API\n")
            self.assertTrue((frozen / "internal/lifecycle/capture_stop_wait_linux_test.go").exists())
