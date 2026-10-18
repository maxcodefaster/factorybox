import json
import pathlib
import tempfile
import unittest

import upstream_unit


class UpstreamObservationTests(unittest.TestCase):
    def test_pass_counts_distinguish_top_level_from_subtests(self):
        with tempfile.TemporaryDirectory() as temporary:
            output = pathlib.Path(temporary)
            (output / "fs.exit-code").write_text("0\n")
            records = [
                {"Action": "pass", "Test": "TestRegister", "Package": "example/fs"},
                {"Action": "pass", "Test": "TestRegister/live_namespace", "Package": "example/fs"},
                {"Action": "skip", "Test": "TestUnsupported", "Package": "example/fs"},
                {"Action": "pass", "Package": "example/fs"},
            ]
            (output / "fs.jsonl").write_text("\n".join(json.dumps(r) for r in records))
            observed = upstream_unit.observations(output)["fs"]
            self.assertEqual(observed["passed_tests"], 2)
            self.assertEqual(observed["passed_top_level_tests"], 1)
            self.assertEqual(observed["passed_subtests"], 1)
            self.assertEqual(observed["skipped_tests"], ["TestUnsupported"])

    def test_failure_event_survives_a_broken_upstream_testmain_exit(self):
        with tempfile.TemporaryDirectory() as temporary:
            output = pathlib.Path(temporary)
            (output / "fs.exit-code").write_text("0\n")
            records = [
                {"Action": "fail", "Test": "TestNativeInvariant", "Package": "example/fs"},
                {"Action": "pass", "Package": "example/fs"},
            ]
            (output / "fs.jsonl").write_text("\n".join(json.dumps(r) for r in records))
            observed = upstream_unit.observations(output)["fs"]
            self.assertEqual(observed["exit_code"], 0)
            self.assertEqual(observed["failed"], ["TestNativeInvariant"])
            self.assertEqual(observed["passed_tests"], 0)

    def test_skipped_tests_remain_explicit(self):
        with tempfile.TemporaryDirectory() as temporary:
            output = pathlib.Path(temporary)
            (output / "mgr.exit-code").write_text("0\n")
            (output / "mgr.jsonl").write_text(json.dumps(
                {"Action": "skip", "Test": "TestShiftfs", "Package": "example/mgr"}))
            self.assertEqual(upstream_unit.observations(output)["mgr"]["skipped_tests"],
                             ["TestShiftfs"])


if __name__ == "__main__":
    unittest.main()
