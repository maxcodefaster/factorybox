import hashlib
import json
from pathlib import Path
import re
import unittest


class RestoreHoldFixturePortTests(unittest.TestCase):
    def verify_declaration(self, filename):
        root = Path(__file__).resolve().parents[1]
        declared = json.loads((root / "tests/compatibility" / filename).read_text())
        self.assertEqual(declared["schema"], 1)
        self.assertTrue(declared["assertions_preserved"])
        self.assertFalse(declared["upstream_assertions_modified"])
        sha = lambda value: hashlib.sha256(value).hexdigest()
        for path, row in declared["files"].items():
            with self.subTest(path=path):
                self.assertEqual(sha((root / path).read_bytes()), row["after_sha256"])
        for path, digest in declared["added_files"].items():
            with self.subTest(path=path):
                self.assertEqual(sha((root / path).read_bytes()), digest)
        for path, functions in declared["unchanged_functions"].items():
            text = (root / path).read_text()
            for name, digest in functions.items():
                with self.subTest(path=path, function=name):
                    # These fixed fixture functions close at column zero.
                    # Hash the complete original body, not an assertion count.
                    match = re.search(r"^func " + re.escape(name) + r"\([^\n]*(?:\n.*?)*?^}",
                                      text, re.MULTILINE)
                    self.assertIsNotNone(match)
                    self.assertEqual(sha(match[0].encode()), digest)
        for path, literals in declared["unchanged_literals"].items():
            text = (root / path).read_text()
            for name, digest in literals.items():
                with self.subTest(path=path, literal=name):
                    match = re.search(r"^const " + re.escape(name) + r" = `([\s\S]*?)`",
                                      text, re.MULTILINE)
                    self.assertIsNotNone(match)
                    self.assertEqual(sha(match[1].encode()), digest)

    def test_declared_port_preserves_every_original_function_and_amd64_counter(self):
        self.verify_declaration("arm64-restore-hold.json")

    def test_guard_ports_preserve_every_original_function(self):
        self.verify_declaration("arm64-timer-guard-fixtures.json")
