import argparse
import unittest
from unittest.mock import patch

import backing_retirement_previous_boot
import bounded_store_native
import release_native


class PreviousBootCoverageTests(unittest.TestCase):
    def test_extension_keeps_every_original_check_and_requires_the_previous_boot_proof(self):
        original = list(bounded_store_native.BACKING_RETIREMENT_UNIT_TESTS)
        with patch.object(bounded_store_native, "BACKING_RETIREMENT_UNIT_TESTS", original), \
                patch.object(bounded_store_native, "main") as run:
            backing_retirement_previous_boot.main()
            run.assert_called_once_with()
            self.assertEqual(bounded_store_native.BACKING_RETIREMENT_UNIT_TESTS,
                             [*original, "TestNativeRetainedMetadataAcrossPreviousBoot"])

    def test_native_release_uses_the_extension_for_backing_retirement_only(self):
        options = argparse.Namespace(platform="linux/arm64", builder="owned-builder",
                                     worker_build="owned-worker", consumer_build="owned-consumer",
                                     project_images="owned-projects", factory_repo="/owned/factory")
        worker = {"native_base_build": "owned-base", "sysbox_build": "owned-sysbox", "criu_build": "owned-criu"}
        commands = release_native.native_commands(options, worker, "owned-bats")
        for suite in release_native.RUNTIME_SUITES:
            command = dict(commands)["runtime-" + suite]
            self.assertEqual(command[1], "tools/backing_retirement_previous_boot.py"
                             if suite == "backing-retirement" else "tools/bounded_store_native.py")
            self.assertEqual(command[-2:], ["--suite", suite])


if __name__ == "__main__":
    unittest.main()
