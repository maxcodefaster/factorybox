import unittest
from unittest.mock import patch

import filesystem_cow_native as native


class FilesystemWorkerPin(unittest.TestCase):
    def test_manifest_scope_requires_capture_and_all_original_component_cases(self):
        rows = ["--- PASS: " + name + " (0.01s)" for name in native.MANIFEST_TESTS]
        native.require_manifest_coverage("\n".join(rows))
        for index in range(len(rows)):
            with self.subTest(missing=native.MANIFEST_TESTS[index]), self.assertRaises(ValueError):
                native.require_manifest_coverage("\n".join(rows[:index] + rows[index+1:]))
        for output in ["PASS", "--- SKIP: required\n" + "\n".join(rows)]:
            with self.subTest(output=output), self.assertRaises(ValueError):
                native.require_manifest_coverage(output)

    def test_missing_build_alias_uses_only_the_exact_resolved_manifest_and_config(self):
        worker = {"image": "old-worker@sha256:" + "a" * 64,
                  "image_id": "sha256:" + "b" * 64}
        resolved = "registry.invalid/worker@sha256:" + "a" * 64
        with patch.object(native.build_config, "local_pinned_image",
                          return_value=(resolved, {"Id": worker["image_id"]})) as select:
            self.assertEqual(native.resolved_worker_image(worker), resolved)
            select.assert_called_once_with(worker["image"], worker["image_id"],
                                           native.reference.docker)


if __name__ == "__main__":
    unittest.main()
