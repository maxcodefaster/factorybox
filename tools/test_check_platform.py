import unittest

import check


class CheckPlatformTests(unittest.TestCase):
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
