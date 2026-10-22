import hashlib
from pathlib import Path
import tempfile
import unittest

import devcontainer_empty_features as consumer


class ExactConsumerPatch(unittest.TestCase):
    def patch(self, data=b"original consumer"):
        return {"schema": 1, "order": 1, "file": "cli.js", "assertions_preserved": True,
                "upstream_assertions_modified": False, "find": "original", "replace": "changed",
                "before_sha256": hashlib.sha256(data).hexdigest(),
                "after_sha256": hashlib.sha256(data.replace(b"original", b"changed")).hexdigest()}

    def test_wrong_bytes_or_declared_output_are_never_patched(self):
        patch = self.patch()
        with self.assertRaises(ValueError):
            consumer.transform(b"already changed consumer", patch)
        with self.assertRaises(ValueError):
            consumer.transform(b"original consumer", {**patch, "after_sha256": "0" * 64})
        with self.assertRaises(ValueError):
            consumer.transform(b"original consumer", {**patch, "upstream_assertions_modified": True})

    def test_alias_or_parent_escape_cannot_write_another_input(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory).resolve()
            original = root / "original.js"
            original.write_bytes(b"original consumer")
            (root / "cli.js").symlink_to(original)
            with self.assertRaises(ValueError):
                consumer.apply(root, self.patch())
            self.assertEqual(original.read_bytes(), b"original consumer")
            (root / "cli.js").unlink()
            (root / "cli.js").hardlink_to(original)
            with self.assertRaises(ValueError):
                consumer.apply(root, self.patch())
            with self.assertRaises(ValueError):
                consumer.apply(root, {**self.patch(), "file": "../original.js"})
            self.assertEqual(original.read_bytes(), b"original consumer")

    def test_apply_records_exact_bytes_and_refuses_repeat(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory).resolve()
            target = root / "cli.js"
            target.write_bytes(b"original consumer")
            target.chmod(0o755)
            receipt = consumer.apply(root, self.patch())
            self.assertEqual(target.read_bytes(), b"changed consumer")
            self.assertEqual(target.stat().st_mode & 0o777, 0o755)
            self.assertEqual(receipt["after_sha256"], hashlib.sha256(target.read_bytes()).hexdigest())
            with self.assertRaises(ValueError):
                consumer.apply(root, self.patch())


if __name__ == "__main__":
    unittest.main()
