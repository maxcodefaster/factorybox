import hashlib
import json
from pathlib import Path
import shutil
import tempfile
import unittest

from verify_fixture_extensions import stopped_capture_fixture_before


class StoppedCaptureFixtureTests(unittest.TestCase):
    def test_exact_reverse_preserves_the_complete_original_runner(self):
        root = Path(__file__).resolve().parents[1]
        declaration = json.loads((root / "tests/compatibility/stopped-capture-delete.json").read_text())
        before = stopped_capture_fixture_before(root)
        name = "tools/bounded_store_native.py"
        self.assertEqual(hashlib.sha256(before[name]).hexdigest(), declaration["files"][name]["before_sha256"])
        self.assertIn(b"'stopped-capture-retirement'", (root / name).read_bytes())
        self.assertNotIn(b"'stopped-capture-retirement'", before[name])

    def test_changed_runner_or_patch_cannot_override_original_assertions(self):
        root = Path(__file__).resolve().parents[1]
        for changed in ["tools/bounded_store_native.py", "tests/compatibility/stopped-capture-delete.patch"]:
            with self.subTest(changed=changed), tempfile.TemporaryDirectory() as directory:
                target = Path(directory)
                for name in [
                    "tools/bounded_store_native.py",
                    "tests/compatibility/stopped-capture-delete.json",
                    "tests/compatibility/stopped-capture-delete.patch",
                    "tests/compatibility/pidfd-namespaces.json",
                    "tests/production-fixture-extensions.json",
                    "tests/lifecycle-native.sh",
                ]:
                    (target / name).parent.mkdir(parents=True, exist_ok=True)
                    shutil.copyfile(root / name, target / name)
                with (target / changed).open("ab") as file:
                    file.write(b"\nchanged\n")
                with self.assertRaises(ValueError):
                    stopped_capture_fixture_before(target)
