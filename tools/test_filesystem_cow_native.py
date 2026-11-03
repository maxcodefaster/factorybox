import unittest
from unittest.mock import patch

import filesystem_cow_native as native


class FilesystemWorkerPin(unittest.TestCase):
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
