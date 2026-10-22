import json
import pathlib
import tempfile
import unittest
from unittest.mock import patch

import devx_lifecycle


class RuntimeInputTests(unittest.TestCase):
    def test_exact_package_and_component_source_identities_are_required(self):
        name = "factorybox-build-runtime-" + "a" * 32
        host = {"OSType": "linux", "Architecture": "x86_64", "KernelVersion": "qualified-test-kernel"}
        with tempfile.TemporaryDirectory() as directory:
            archive = pathlib.Path(directory)
            bundle = archive / "bundle"
            bundle.mkdir()
            context = archive / "context"
            context.mkdir()
            (context / "go.mod").write_text("module fixture\n")
            binaries = {}
            for file in ["factorybox", "criu", "sysbox-runc", "sysbox-fs", "sysbox-mgr"]:
                (bundle / file).write_text("exact-" + file)
                binaries[file] = devx_lifecycle.reference.sha(bundle / file)
            components = {k: v for k, v in binaries.items() if k != "factorybox"}
            profile = {"name": "test-package", "status": "reference", "platform": {
                "os": "linux", "architecture": "amd64", "kernel": host["KernelVersion"]}}
            (bundle / "profile.json").write_text(json.dumps(profile))
            profile_sha = devx_lifecycle.reference.sha(bundle / "profile.json")
            manifest = {"binary_sha256": binaries, "profile_sha256": profile_sha}
            (bundle / "manifest.json").write_text(json.dumps(manifest))
            (archive / "test-package.tar").write_bytes(b"exact packaged bytes")
            result = {"inputs_unchanged": True, "binary_sha256": binaries,
                      "profile_sha256": profile_sha,
                      "package_sha256": devx_lifecycle.reference.sha(archive / "test-package.tar"),
                      "source_context": devx_lifecycle.upstream_unit.context_manifest(context)}
            with patch.object(devx_lifecycle.reference_lifecycle, "component_build", return_value=(archive, result)):
                binary, receipt = devx_lifecycle.runtime_input(name, host, components, False)
                self.assertEqual(binary.read_text(), "exact-factorybox")
                self.assertEqual(receipt["binary_sha256"], binaries["factorybox"])
                with self.assertRaisesRegex(ValueError, "execution mode"):
                    devx_lifecycle.runtime_input(name, host, components, True)
                with self.assertRaisesRegex(ValueError, "execution mode"):
                    devx_lifecycle.runtime_input(name, dict(host, KernelVersion="foreign-kernel"), components, False)
                with self.assertRaisesRegex(ValueError, "selected native components"):
                    devx_lifecycle.runtime_input(name, host, dict(components, criu="0" * 64), False)
                (bundle / "factorybox").write_text("modified-executable")
                with self.assertRaisesRegex(ValueError, "executable changed"):
                    devx_lifecycle.runtime_input(name, host, components, False)
                (bundle / "factorybox").write_text("exact-factorybox")
                (context / "go.mod").write_text("module modified\n")
                with self.assertRaisesRegex(ValueError, "frozen source changed"):
                    devx_lifecycle.runtime_input(name, host, components, False)
                (context / "go.mod").write_text("module fixture\n")
                (archive / "test-package.tar").write_bytes(b"modified package")
                with self.assertRaisesRegex(ValueError, "package changed"):
                    devx_lifecycle.runtime_input(name, host, components, False)
                (archive / "test-package.tar").write_bytes(b"exact packaged bytes")
                (bundle / "profile.json").write_text("{}")
                with self.assertRaisesRegex(ValueError, "profile changed"):
                    devx_lifecycle.runtime_input(name, host, components, False)

    def test_unscoped_runtime_is_rejected_before_reading_any_input(self):
        with patch.object(devx_lifecycle.reference_lifecycle, "component_build") as load:
            for name in ["../foreign", "factorybox-build-runtime-invalid", "factorybox-build-runtime-" + "b" * 31]:
                with self.assertRaisesRegex(ValueError, "exact completed runtime"):
                    devx_lifecycle.runtime_input(name, {}, {}, False)
            load.assert_not_called()


if __name__ == "__main__":
    unittest.main()
