import hashlib
import json
import pathlib
import shutil
import sys
import tempfile
import unittest
from unittest.mock import patch

import restore_root_native


class RestoreRootNativeProof(unittest.TestCase):
    def test_lost_volume_reply_keeps_intent_and_cleans_only_its_original_owner(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = pathlib.Path(temporary)
            component = root / "component"
            (component / "output").mkdir(parents=True)
            payload = b"unit-fixture"
            (component / "output" / restore_root_native.TEST_BINARY).write_bytes(payload)
            (component / "result.json").write_text('{"passed":true}\n')
            artifact = root / "owned-proof"
            image = "worker@sha256:" + "a" * 64

            def docker(command):
                if command[0] == "info":
                    return "{}"
                if command[:2] == ["volume", "create"]:
                    intent = json.loads((artifact / "intent.json").read_text())
                    self.assertEqual(command[-1], intent["volume_name"])
                    self.assertIn("com.factorybox.run=" + intent["owner"], command)
                    raise RuntimeError("create reply lost")
                raise AssertionError(command)

            with patch.object(sys, "argv", ["restore_root_native.py", "--sysbox-build", "component",
                                           "--worker-build", "worker"]), \
                    patch.object(restore_root_native.build_worker, "load", return_value=(None, {
                        "platform": "linux/arm64", "image": image, "image_id": "sha256:" + "b" * 64})), \
                    patch.object(restore_root_native.reference_lifecycle, "component_build", return_value=(component, {
                        "platform": "linux/arm64",
                        "native_test_sha256": {restore_root_native.TEST_BINARY: hashlib.sha256(payload).hexdigest()}})), \
                    patch.object(restore_root_native.native_base, "require_native_host"), \
                    patch.object(restore_root_native.build_config, "require_elf"), \
                    patch.object(restore_root_native.build_config, "local_pinned_image", return_value=(image, {})), \
                    patch.object(restore_root_native.reference, "new_artifact", return_value=artifact), \
                    patch.object(restore_root_native.artifact_storage, "share_frozen_input",
                                 side_effect=lambda source, target, *_: shutil.copyfile(source, target)), \
                    patch.object(restore_root_native.reference, "docker", side_effect=docker), \
                    patch.object(restore_root_native.reference, "owned_delete") as cleanup:
                with self.assertRaisesRegex(RuntimeError, "create reply lost"):
                    restore_root_native.main()
                intent = json.loads((artifact / "intent.json").read_text())
                cleanup.assert_called_once_with("volume", intent["volume_name"], intent["owner"])
                outcome = json.loads((artifact / "result.json").read_text())
                self.assertFalse(outcome["passed"])
                self.assertTrue(outcome["cleanup"])

    def test_all_original_native_and_refusal_cases_are_required(self):
        rows = ["--- PASS: TestFactoryboxNativeRootMapping/" + name + " (0.01s)"
                for name in restore_root_native.CASES]
        restore_root_native.require_coverage("\n".join(rows))
        for index in range(len(rows)):
            with self.subTest(missing=restore_root_native.CASES[index]), self.assertRaises(ValueError):
                restore_root_native.require_coverage("\n".join(rows[:index] + rows[index+1:]))
        for invalid in ["PASS", "--- SKIP: required\n" + "\n".join(rows)]:
            with self.subTest(output=invalid), self.assertRaises(ValueError):
                restore_root_native.require_coverage(invalid)

    def test_native_binary_compiles_before_pristine_runc_replaces_the_source(self):
        recipe = (pathlib.Path(__file__).resolve().parents[1] / "build/sysbox.Dockerfile").read_text()
        native = "-o /out/" + restore_root_native.TEST_BINARY + " ./libcontainer/factoryboxrestore"
        self.assertIn(native, recipe)
        self.assertLess(recipe.index(native), recipe.index("RUN rm -rf /src/sysbox-runc"))
        self.assertIn("-trimpath -buildvcs=false -tags idmapped_mnt", recipe)


if __name__ == "__main__":
    unittest.main()
