import json
import pathlib
import subprocess
import tempfile
import unittest
from unittest.mock import patch

import native_core


class NativeCommandTests(unittest.TestCase):
    def test_build_output_names_cannot_replace_owned_container_name(self):
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            evidence = root / "evidence"
            evidence.mkdir()
            result = {}
            container_name = "factorybox-core-0123456789abcdef"

            def run(command, **kwargs):
                if command[0] == "go":
                    pathlib.Path(command[command.index("-o") + 1]).write_bytes(b"test-binary")
                    return subprocess.CompletedProcess(command, 0)
                return subprocess.CompletedProcess(
                    command, 0, stdout="--- PASS: example\n" * native_core.EXPECTED_NATIVE_TESTS,
                    stderr="")

            with patch.object(native_core, "ROOT", root), \
                 patch.object(native_core.subprocess, "run", side_effect=run), \
                 patch.object(native_core.reference, "docker",
                              return_value=json.dumps([{"State": {"OOMKilled": False}}])):
                native_core.execute(container_name, "owner", evidence, result)
            command = result["command"]
            self.assertEqual(command[command.index("--name") + 1], container_name)
