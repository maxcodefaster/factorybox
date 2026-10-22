import copy
import unittest

import benchmark_consumer_start as consumer
import test_benchmark_lifecycle as legacy_tests


class ConsumerComparisonQualification(unittest.TestCase):
    def rows(self):
        rows = legacy_tests.ComparisonGates().rows()
        for row in rows:
            metrics = row["observation"]["metrics_seconds"]
            metrics["prepared-start-first-use"] = 8 if row["configuration"] == "reference" else 5
            # This change is not required to halve unrelated checkpoints.
            for name in ["checkpoint-source", "checkpoint-fork", "checkpoint-resumed", "pause", "fork-first-use", "fork-of-fork-first-use", "fork-of-resumed-first-use"]:
                metrics[name] = 10
        return rows

    def test_same_lifecycle_can_qualify_only_the_measured_fresh_start_gain(self):
        report = consumer.summarize(self.rows())
        self.assertTrue(report["fresh_start_target_met"])
        self.assertTrue(report["regression_limits_met"])
        self.assertNotIn("factory_halving_target_met", report)
        self.assertFalse(report["p95_estimated"])

    def test_failure_or_small_gain_does_not_qualify(self):
        rows = self.rows()
        rows[0]["passed"] = False
        self.assertFalse(consumer.summarize(rows)["fresh_start_target_met"])
        rows = self.rows()
        for row in rows:
            if row["configuration"] == "candidate":
                row["observation"]["metrics_seconds"]["prepared-start-first-use"] = 7.5
        self.assertFalse(consumer.summarize(rows)["fresh_start_target_met"])

    def test_factory_fork_regression_blocks_selection_despite_faster_start(self):
        rows = self.rows()
        for row in rows:
            if row["example"] == "factory" and row["configuration"] == "candidate":
                row["observation"]["metrics_seconds"]["fork-of-resumed-first-use"] = 12.1
        report = consumer.summarize(rows)
        self.assertTrue(report["fresh_start_target_met"])
        self.assertFalse(report["regression_limits_met"])

    def test_mixed_fixture_or_component_bytes_cannot_enter_the_matrix(self):
        record = {"passed": True, "cleanup": True, "example": "factory", "probe": "lifecycle", "storage_driver": "overlay2",
                  "consumer_request_profile": False, "baseline_input": None, "consumer_empty_features": False,
                  "opencode_flow": True, "fixture_verification": {"production_extensions": 1},
                  **{key: "fixed" for key in ["worker_image_id", "lifecycle_cli_sha256", "lifecycle_criu_sha256", "consumer_rootfs_sha256", "project_images_sha256", "example_manifest", "source_context"]}}
        expected = consumer.exact_inputs(record, None, "factory", "reference")
        for field in ["worker_image_id", "source_context", "example_manifest"]:
            changed = copy.deepcopy(record)
            changed[field] = "other"
            with self.subTest(field=field), self.assertRaises(ValueError):
                consumer.exact_inputs(changed, expected, "factory", "reference")
        with self.assertRaises(ValueError):
            consumer.exact_inputs({**record, "consumer_request_profile": True}, expected, "factory", "reference")


if __name__ == "__main__":
    unittest.main()
