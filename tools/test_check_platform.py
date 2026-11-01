import unittest

import check


class CheckPlatformTests(unittest.TestCase):
    def test_root_metadata_lane_requires_each_actual_case(self):
        complete = "\n".join("--- PASS: " + name + " (0.01s)" for name in check.ROOT_METADATA_CASES)
        check.require_root_metadata_checks(complete + "\n--- SKIP: TestNativeRuntime (0.00s)")
        for missing in check.ROOT_METADATA_CASES:
            with self.subTest(missing=missing), self.assertRaisesRegex(ValueError, missing):
                check.require_root_metadata_checks(
                    "\n".join(line for line in complete.splitlines() if missing not in line))

    def test_root_metadata_lane_cannot_pass_with_skipped_or_failed_subcases(self):
        complete = "\n".join("--- PASS: " + name + " (0.01s)" for name in check.ROOT_METADATA_CASES)
        for outcome in ["SKIP", "FAIL"]:
            for name in check.ROOT_METADATA_CASES:
                with self.subTest(outcome=outcome, name=name), self.assertRaisesRegex(ValueError, name):
                    check.require_root_metadata_checks(
                        complete + "\n    --- " + outcome + ": " + name + "/original (0.00s)")

    def test_linux_keeps_every_original_go_check(self):
        packages = [
            "factorybox/cmd/factorybox",
            "factorybox/internal/fdstore",
            "factorybox/tests/devx-lifecycle",
        ]
        self.assertEqual(
            check.go_commands(packages),
            [
                ["go", "test", "factorybox/cmd/factorybox", "factorybox/tests/devx-lifecycle"],
                ["go", "test", "./internal/fdstore"],
                ["go", "test", "-tags=factorybox_worker_probe", "-run=^$", "./tests/devx-lifecycle"],
            ],
        )

    def test_portable_host_does_not_invoke_packages_excluded_by_go_build_constraints(self):
        self.assertEqual(
            check.go_commands(["factorybox/cmd/factorybox", "factorybox/internal/platform"]),
            [["go", "test", "factorybox/cmd/factorybox", "factorybox/internal/platform"]],
        )
