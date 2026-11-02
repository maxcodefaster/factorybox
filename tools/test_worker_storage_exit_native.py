import contextlib
import io
import json
from pathlib import Path
import runpy
import subprocess
import tempfile
import unittest
from unittest import mock

import build_worker
import descriptor_transfer_proof
import native_base
import reference
import upstream_bats


class StorageExitRunnerTests(unittest.TestCase):
    def test_unknown_create_outcome_keeps_ownership_and_cleans_the_exact_requested_name(self):
        with tempfile.TemporaryDirectory() as temporary:
            artifact = Path(temporary).resolve() / "native"
            deleted = []
            created = []

            def docker(arguments, timeout=15):
                if arguments[0] == "info":
                    return "{}"
                if arguments[:2] == ["volume", "create"]:
                    return arguments[-1]
                if arguments[0] == "run":
                    name = arguments[arguments.index("--name") + 1]
                    created.append(name)
                    raise subprocess.TimeoutExpired(arguments, timeout)
                self.fail("unexpected external action: " + repr(arguments))

            def run(command, **unused):
                if command[:3] == ["go", "test", "-c"]:
                    Path(command[command.index("-o") + 1]).write_bytes(b"fixture-unit-binary")
                return subprocess.CompletedProcess(command, 0, "owned container logs", "")

            with contextlib.ExitStack() as patches:
                patches.enter_context(mock.patch.object(build_worker, "load", return_value=(artifact, {
                    "platform": "linux/arm64", "image": "exact-native-worker", "source_context": {},
                })))
                patches.enter_context(mock.patch.object(native_base, "require_native_host"))
                patches.enter_context(mock.patch.object(descriptor_transfer_proof, "go_source_context", return_value={}))
                patches.enter_context(mock.patch.object(reference, "new_artifact", return_value=artifact))
                patches.enter_context(mock.patch.object(reference, "docker", side_effect=docker))
                patches.enter_context(mock.patch.object(reference, "owned_delete", side_effect=lambda *args: deleted.append(args)))
                patches.enter_context(mock.patch.object(upstream_bats, "archive_stopped_volumes"))
                patches.enter_context(mock.patch.object(subprocess, "run", side_effect=run))
                patches.enter_context(mock.patch("sys.argv", ["worker-storage-exit-native", "--worker-build", "exact"]))
                patches.enter_context(contextlib.redirect_stdout(io.StringIO()))
                with self.assertRaises(SystemExit):
                    runpy.run_path(str(Path(__file__).with_name("worker_storage_exit_native.py")), run_name="__main__")

            result = json.loads((artifact / "result.json").read_text())
            self.assertFalse(result["passed"])
            self.assertTrue(result["cleanup"])
            self.assertEqual(len(created), 1)
            self.assertIn(("container", created[0], result["owner"]), deleted)
            self.assertEqual(deleted[-1], ("volume", created[0].removesuffix("-1"), result["owner"]))
            intent = json.loads((artifact / "intent.json").read_text())
            self.assertEqual(intent["containers"], [{"name": created[0]}])


if __name__ == "__main__":
    unittest.main()
