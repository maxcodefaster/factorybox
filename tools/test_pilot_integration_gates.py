import copy
import unittest

import lifecycle_native
import pilot_integration_gates as gates


class NegativeControlAcceptance(unittest.TestCase):
    def fixture(self):
        saved = {"passed": False, "cleanup": True, "inputs_unchanged": True,
                 "corruption_control": True, "exit_code": 1,
                 "container_state": {"OOMKilled": False},
                 "passed_tests": lifecycle_native.EXPECTED_ADAPTER_TESTS,
                 "passed_subtests": lifecycle_native.EXPECTED_ADAPTER_SUBTESTS}
        output = ("    interruption_native_linux_test.go:596: read-only diagnostic found changed original 32-MiB RAM <nil> <nil> 33554432\n"
                  "--- FAIL: TestInterruptedCaptureNative (3.50s)\n")
        return saved, output

    def test_deliberate_ram_change_requires_its_original_assertion_to_go_red(self):
        saved, output = self.fixture()
        self.assertTrue(gates.corruption_oracle_rejected(saved, output, 1))
        self.assertFalse(gates.corruption_oracle_rejected(saved, output, 0))
        self.assertFalse(gates.corruption_oracle_rejected(saved, output.replace("found changed", "did not check"), 1))

    def test_expected_ram_failure_cannot_hide_another_failure_skip_oom_or_cleanup_loss(self):
        saved, output = self.fixture()
        for suffix in ("--- FAIL: TestOther (1.00s)\n", "--- SKIP: TestOther (0.00s)\n", "panic: bad fixture\n"):
            self.assertFalse(gates.corruption_oracle_rejected(saved, output + suffix, 1))
        for key, value in (("cleanup", False), ("inputs_unchanged", False), ("corruption_control", False),
                           ("passed_tests", 0), ("container_state", {"OOMKilled": True})):
            changed = copy.deepcopy(saved)
            changed[key] = value
            self.assertFalse(gates.corruption_oracle_rejected(changed, output, 1))


class UpstreamCoverage(unittest.TestCase):
    def test_original_counts_and_skip_set_are_required(self):
        suite = {"passed_top_level_tests": 47, "passed_subtests": 0,
                 "skipped_tests": list(gates.ORIGINAL_INTEGRATION_SKIPS)}
        self.assertTrue(gates.original_upstream_coverage("runc-integration", {"runc-integration": suite}))
        for change in ("extra-skip", "missing-test", "extra-suite"):
            suites = {"runc-integration": copy.deepcopy(suite)}
            if change == "extra-skip":
                suites["runc-integration"]["skipped_tests"].append("TestNewlySkipped")
            elif change == "missing-test":
                suites["runc-integration"]["passed_top_level_tests"] -= 1
            else:
                suites["foreign"] = suite
            self.assertFalse(gates.original_upstream_coverage("runc-integration", suites))
