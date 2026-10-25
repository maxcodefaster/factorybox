import hashlib
import json
import unittest

import reference


class SharedSourcePlatformTests(unittest.TestCase):
    def test_linux_constraint_preserves_the_complete_original_workload(self):
        root = reference.ROOT
        declaration = json.loads(
            (root / "tests/compatibility/shared-source-actor-linux.json").read_text()
        )
        source = (root / declaration["file"]).read_bytes()
        prefix = b"//go:build linux\n\n"
        self.assertTrue(source.startswith(prefix))
        self.assertEqual(
            hashlib.sha256(source).hexdigest(), declaration["after_sha256"]
        )
        self.assertEqual(
            hashlib.sha256(source[len(prefix):]).hexdigest(),
            declaration["before_sha256"],
        )
        self.assertTrue(declaration["assertions_preserved"])
        self.assertFalse(declaration["upstream_assertions_modified"])
