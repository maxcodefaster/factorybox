import copy
from pathlib import Path
import re
import unittest

import bounded_store_native
from verify_fixture_extensions import pidfd_fixture_before


class PidfdNativeCoverageTests(unittest.TestCase):
    def test_existing_fixture_delegates_with_nil_without_changing_original_body(self):
        root = Path(__file__).resolve().parents[1]
        name = "internal/lifecycle/timer_fixture_native_linux_test.go"
        original = pidfd_fixture_before(root)[name].decode()
        current = (root / name).read_text()
        pattern = r"^func %s\([^\n]*(?:\n.*?)*?^}"
        old = re.search(pattern % "timerReaderFixtureFilesWithCleanup", original, re.MULTILINE)[0]
        new = re.search(pattern % "timerReaderFixtureFilesWithConfiguration", current, re.MULTILINE)[0]
        restored = new.replace("timerReaderFixtureFilesWithConfiguration", "timerReaderFixtureFilesWithCleanup", 1)
        restored = restored.replace(", configure func(map[string]any)) View", ") View", 1)
        restored = restored.replace("\tif configure != nil {\n\t\tconfigure(spec)\n\t}\n", "", 1)
        self.assertEqual(restored, old)
        wrapper = re.search(pattern % "timerReaderFixtureFilesWithCleanup", current, re.MULTILINE)[0]
        self.assertEqual(wrapper, old.split("\n", 1)[0] + "\n"
                         "\treturn timerReaderFixtureFilesWithConfiguration(t, status, id, command, actor, files, cleanup, nil)\n}")

    def proof(self):
        required = {key: True for key in [
            "passed", "exact_nested_target_signaled_before",
            "exact_nested_target_signaled_after", "same_sandbox"]}
        return {**required, "cases": {
            mode: {**required, **{key: True for key in [
                "aliases_and_independent_flags", "dead_pidfd_stays_dead",
                "self_pidfd_stays_live", "fork_targets_independent", "fork_pause_resume"]},
                "shared_file_table": mode == "shared-table",
                "cycles": [{"snapshot": str(n), "original_handle_signaled_exact_target": True}
                           for n in range(3)]}
            for mode in ["flat", "nested", "shared-table"]}}

    def test_only_complete_exact_target_cycles_and_description_controls_pass(self):
        proof = self.proof()
        bounded_store_native.require_pidfd_namespace_coverage(proof)
        for mode, row in proof["cases"].items():
            for key in row:
                changed = copy.deepcopy(proof)
                del changed["cases"][mode][key]
                with self.subTest(mode=mode, missing=key), self.assertRaises(ValueError):
                    bounded_store_native.require_pidfd_namespace_coverage(changed)
            changed = copy.deepcopy(proof)
            del changed["cases"][mode]
            with self.subTest(missing_mode=mode), self.assertRaises(ValueError):
                bounded_store_native.require_pidfd_namespace_coverage(changed)
        for cycles in [
            proof["cases"]["nested"]["cycles"][:2],
            [proof["cases"]["nested"]["cycles"][0]] * 3,
            [{"snapshot": str(n), "original_handle_signaled_exact_target": n != 1}
             for n in range(3)],
        ]:
            changed = copy.deepcopy(proof)
            changed["cases"]["nested"]["cycles"] = cycles
            with self.assertRaises(ValueError):
                bounded_store_native.require_pidfd_namespace_coverage(changed)
