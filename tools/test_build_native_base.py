import json
import pathlib
import tempfile
import types
import unittest
from unittest.mock import patch

import build_native_base


class NativeBaseRetentionTests(unittest.TestCase):
    def run_build(self, root, *, fail_hold=False):
        artifact = root / "artifacts" / ("factorybox-build-native-base-" + "a" * 32)
        (root / "build").mkdir()
        (root / "build/native-base.Dockerfile").write_text("FROM pinned-fixture\n")
        image = artifact.name + "@sha256:" + "b" * 64
        config = "sha256:" + "c" * 64
        calls = []

        def docker(command):
            calls.append(command)
            if command[:2] == ["image", "inspect"]:
                return json.dumps([{"Id": config, "RepoDigests": [image],
                                    "Architecture": "arm64", "Os": "linux",
                                    "Config": {"Volumes": {"/var/lib/sysbox": {}}}}])
            if command[0] == "create":
                if fail_hold:
                    raise ValueError("owned image hold failed")
                return "exact-owned-input-container"
            raise AssertionError(command)

        def run(command, **kwargs):
            if "build" in command:
                metadata = pathlib.Path(command[command.index("--metadata-file") + 1])
                metadata.write_text(json.dumps({"containerimage.digest": "sha256:" + "b" * 64,
                                                "containerimage.config.digest": config}))
            return types.SimpleNamespace(returncode=0, stdout=b"fixture\t1.0\tarm64\n", stderr=b"")

        with patch.object(build_native_base.reference, "ROOT", root), \
                patch.object(build_native_base.reference, "new_artifact", return_value=artifact), \
                patch.object(build_native_base.reference, "docker", side_effect=docker), \
                patch.object(build_native_base.reference, "owned_delete") as delete, \
                patch.object(build_native_base.subprocess, "run", side_effect=run), \
                patch.object(build_native_base.uuid, "uuid4", return_value=types.SimpleNamespace(hex="a" * 32)), \
                patch("sys.argv", ["build_native_base.py", "--platform", "linux/arm64"]):
            if fail_hold:
                with self.assertRaisesRegex(RuntimeError, "native base build failed"):
                    build_native_base.main()
            else:
                build_native_base.main()
            delete.assert_called_once_with("container", artifact.name + "-inspect", "a" * 32)
        return json.loads((artifact / "result.json").read_text()), calls

    def test_completed_base_retains_its_exact_image_before_inspection_cleanup(self):
        with tempfile.TemporaryDirectory() as temporary:
            result, calls = self.run_build(pathlib.Path(temporary))
            self.assertTrue(result["passed"] and result["cleanup"])
            self.assertEqual(result["image_hold"]["image_id"], result["image_id"])
            self.assertEqual(result["image_hold"]["container_id"], "exact-owned-input-container")
            hold = next(command for command in calls if command[0] == "create")
            self.assertIn("--read-only", hold)
            self.assertIn("--pull=never", hold)
            self.assertIn("com.factorybox.run=" + "a" * 32, hold)
            self.assertEqual(hold[-1], result["image"])

    def test_unretained_base_cannot_publish_a_successful_build(self):
        with tempfile.TemporaryDirectory() as temporary:
            result, _ = self.run_build(pathlib.Path(temporary), fail_hold=True)
            self.assertFalse(result["passed"])
            self.assertTrue(result["cleanup"])
            self.assertEqual(result["error"], "owned image hold failed")
