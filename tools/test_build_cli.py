import unittest
from unittest.mock import patch

import build_cli


class NativeCLIBuildTests(unittest.TestCase):
    def test_refuses_cross_compilation_before_building(self):
        with patch.object(build_cli.platform, 'system', return_value='Linux'), \
             patch.object(build_cli.platform, 'machine', return_value='x86_64'), \
             patch.object(build_cli.subprocess, 'check_output') as toolchain:
            with self.assertRaises(ValueError):
                build_cli.require_native_target('linux/arm64')
            toolchain.assert_not_called()

    def test_refuses_non_linux_hosts_and_mismatched_go_toolchains(self):
        with patch.object(build_cli.platform, 'system', return_value='Darwin'), \
             patch.object(build_cli.platform, 'machine', return_value='arm64'):
            with self.assertRaises(ValueError):
                build_cli.require_native_target('linux/arm64')
        with patch.object(build_cli.platform, 'system', return_value='Linux'), \
             patch.object(build_cli.platform, 'machine', return_value='aarch64'), \
             patch.object(build_cli.subprocess, 'check_output', return_value='linux\namd64\n'):
            with self.assertRaises(ValueError):
                build_cli.require_native_target('linux/arm64')

    def test_accepts_matching_native_architectures(self):
        for target, machine in [('linux/amd64', 'x86_64'), ('linux/arm64', 'aarch64')]:
            with self.subTest(target=target), \
                 patch.object(build_cli.platform, 'system', return_value='Linux'), \
                 patch.object(build_cli.platform, 'machine', return_value=machine), \
                 patch.object(build_cli.subprocess, 'check_output', return_value=target.replace('/', '\n')):
                build_cli.require_native_target(target)
