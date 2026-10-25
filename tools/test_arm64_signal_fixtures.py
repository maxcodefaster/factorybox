import hashlib
import json
from pathlib import Path
import re
import unittest


class SignalFixturePortTests(unittest.TestCase):
    def test_exact_port_keeps_every_original_assertion_body(self):
        self.verify_declaration("arm64-signal-fixtures.json")

    def test_fault_port_keeps_every_original_assertion_body(self):
        self.verify_declaration("arm64-timer-fault-fixtures.json")

    def verify_declaration(self, name):
        root = Path(__file__).resolve().parents[1]
        declared = json.loads((root / "tests/compatibility" / name).read_text())
        self.assertTrue(declared["assertions_preserved"])
        self.assertFalse(declared["upstream_assertions_modified"])
        sha = lambda value: hashlib.sha256(value).hexdigest()
        for path, row in declared["files"].items():
            with self.subTest(path=path):
                self.assertEqual(sha((root / path).read_bytes()), row["after_sha256"])
        for path, digest in declared["added_files"].items():
            with self.subTest(path=path):
                self.assertEqual(sha((root / path).read_bytes()), digest)
        for original, row in declared["original_functions"].items():
            text = (root / row["file"]).read_text()
            match = re.search(r"^func " + re.escape(row["function"]) + r"\([^\n]*(?:\n.*?)*?^}",
                              text, re.MULTILINE)
            with self.subTest(function=original):
                self.assertIsNotNone(match)
                body = match[0]
                for native, original_text, count in row["normalizations"]:
                    self.assertEqual(body.count(native), count)
                    body = body.replace(native, original_text)
                self.assertEqual(sha(body.encode()), row["original_sha256"])
        text = (root / "internal/lifecycle/timer_native_linux_test.go").read_text()
        for name, digest in declared["unchanged_literals"].items():
            with self.subTest(literal=name):
                match = re.search(r"^const " + re.escape(name) + r" = `([\s\S]*?)`", text, re.MULTILINE)
                self.assertIsNotNone(match)
                self.assertEqual(sha(match[1].encode()), digest)
