import json
import pathlib
import tempfile
import unittest
from unittest.mock import patch

import filesystem_probe


class FrozenProbeSourceTests(unittest.TestCase):
    def test_modified_or_extra_compilation_inputs_are_rejected(self):
        name = "factorybox-pilot-baseline-" + "a" * 32
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            artifact = root / "artifacts" / name
            source = artifact / "context"
            source.mkdir(parents=True)
            module = source / "go.mod"
            module.write_text("module frozen\n")
            (artifact / "source-manifest.json").write_text(json.dumps({"go.mod": filesystem_probe.reference.sha(module)}))
            with patch.object(filesystem_probe.reference, "ROOT", root):
                self.assertEqual(filesystem_probe.frozen_source(name), source)
                unexpected = source / "unexpected.go"
                unexpected.write_text("package substituted\n")
                with self.assertRaisesRegex(ValueError, "source inventory differs"):
                    filesystem_probe.frozen_source(name)
                unexpected.unlink()
                module.write_text("module changed\n")
                with self.assertRaisesRegex(ValueError, "frozen baseline differs"):
                    filesystem_probe.frozen_source(name)
                module.unlink()
                outside = root / "outside.mod"
                outside.write_text("module frozen\n")
                module.symlink_to(outside)
                with self.assertRaisesRegex(ValueError, "frozen baseline differs"):
                    filesystem_probe.frozen_source(name)

    def test_unscoped_source_is_rejected(self):
        for name in ["../foreign", "factorybox-pilot-baseline-" + "b" * 31]:
            with self.assertRaisesRegex(ValueError, "exact frozen pilot baseline"):
                filesystem_probe.frozen_source(name)


if __name__ == "__main__":
    unittest.main()
