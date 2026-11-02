import argparse
from pathlib import Path
import unittest

import capture_budget_native
import release_native


class CaptureBudgetNativeTests(unittest.TestCase):
    def complete(self):
        return {
            "copy_delay_seconds": 65, "pause_seconds": 65.2,
            "copy_delay_observed": True, "committed_pause": True,
            "anonymous_memory_released": True, "same_ram_after_resume": True,
            "useful_after_resume": True, "sandbox_deleted": True,
            "worker_stopped_cleanly": True, "kernel_storage_released": True,
            "host_sysctls_unchanged": True, "inputs_unchanged": True,
        }

    def test_requires_the_real_delayed_capture_and_every_preservation_gate(self):
        complete = self.complete()
        capture_budget_native.require_complete(complete)
        for key, value in complete.items():
            changed = dict(complete)
            changed[key] = False if isinstance(value, bool) else 60
            with self.subTest(key=key):
                with self.assertRaisesRegex(ValueError, "capture budget omitted"):
                    capture_budget_native.require_complete(changed)

    def test_both_release_architectures_require_the_delivered_cli_budget_case(self):
        for platform in ["linux/arm64", "linux/amd64"]:
            options = argparse.Namespace(
                platform=platform, worker_build="exact-worker", consumer_build="exact-consumer",
                project_images="exact-project-images", factory_repo=Path("/exact-factory"),
                builder="exact-builder")
            worker = {"native_base_build": "exact-base", "sysbox_build": "exact-sysbox",
                      "criu_build": "exact-criu"}
            commands = dict(release_native.native_commands(options, worker, "exact-bats"))
            self.assertIn("capture-budget", commands)
            self.assertEqual(commands["capture-budget"][1:], [
                "tools/capture_budget_native.py", "--worker-build", "exact-worker",
            ])


if __name__ == "__main__":
    unittest.main()
