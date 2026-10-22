import json
from pathlib import Path
import tempfile
import unittest

import benchmark_lifecycle as benchmark


PHASES = ["prepared-start-first-use", "fork-first-use", "source-advance", "branch-advance",
          "source-independent", "fork-of-fork-first-use", "resume-first-use", "fork-of-resumed-first-use"]


class ComparisonGates(unittest.TestCase):
    def rows(self):
        rows = []
        for _ in range(5):
            for example in ["factory", "minimal"]:
                for configuration in ["reference", "candidate"]:
                    metrics = {name: 1. for name in PHASES}
                    metrics.update({name: 10. for name in [*benchmark.CHECKPOINTS.values(), "pause"]})
                    for name in benchmark.FORKS.values(): metrics[name] = 10.
                    if example == "factory":
                        metrics.update({"theia-"+name: value for name,value in list(metrics.items()) if name in PHASES})
                    if example == "factory" and configuration == "candidate":
                        for name in [*benchmark.CHECKPOINTS.values(), *benchmark.FORKS.values(), "pause"]: metrics[name] *= .4
                    rows.append({"passed": True, "configuration": configuration, "example": example,
                                 "observation": {"metrics_seconds": metrics}})
        return rows

    def test_complete_matrix_meets_separate_speed_and_regression_gates(self):
        report = benchmark.summarize(self.rows())
        self.assertTrue(report["complete_five_per_configuration"])
        self.assertTrue(report["factory_halving_target_met"])
        self.assertTrue(report["regression_limits_met"])
        self.assertFalse(report["p95_estimated"])

    def test_failure_cannot_be_counted_as_a_complete_observation(self):
        rows = self.rows(); rows[0]["passed"] = False
        report = benchmark.summarize(rows)
        self.assertFalse(report["complete_five_per_configuration"])
        self.assertFalse(report["factory_halving_target_met"])

    def test_one_unmet_phase_blocks_halving_claim(self):
        rows = self.rows()
        for row in rows:
            if row["example"] == "factory" and row["configuration"] == "candidate":
                row["observation"]["metrics_seconds"]["pause"] = 5.1
        self.assertFalse(benchmark.summarize(rows)["factory_halving_target_met"])

    def test_minimal_regression_blocks_acceptance_even_with_factory_speedup(self):
        rows = self.rows()
        for row in rows:
            if row["example"] == "minimal" and row["configuration"] == "candidate":
                row["observation"]["metrics_seconds"]["resume-first-use"] = 1.3
        report = benchmark.summarize(rows)
        self.assertTrue(report["factory_halving_target_met"])
        self.assertFalse(report["regression_limits_met"])

    def test_phase_sets_must_match_across_all_observations(self):
        # Missing a required target must not silently shrink the comparison.
        rows = self.rows()
        del rows[0]["observation"]["metrics_seconds"]["checkpoint-source"]
        with self.assertRaises((ValueError,KeyError)):
            benchmark.summarize(rows)


class FixedLifecycleExtraction(unittest.TestCase):
    def fixture(self, path):
        output = path/"prepare-output"; output.mkdir()
        result = {"passed": True, "cleanup": True, "probe": "lifecycle", "example": "minimal",
                  "lifecycle_cli_sha256": "old", "lifecycle_criu_sha256": "criu", "worker_image_id": "same", "storage_driver": "vfs",
                  "phases": [{"observations": [{"phase": name, "milliseconds": 3000, "worker_resources": {}} for name in PHASES],
                              "preparation": {"seconds": 20}, "startup": {"seconds": .5}}]}
        (path/"result.json").write_text(json.dumps(result))
        for letter in benchmark.CHECKPOINTS:
            (output/("cli-"+letter+".timing.json")).write_text(json.dumps({"arguments": ["checkpoint", "--snapshot-id", letter*32, "source"], "seconds": 10}))
            (output/("cli-"+letter+".json")).write_text(json.dumps({"filesystem": {"roots": [
                {"name": "lower", "file_bytes": 1000, "immutable": {}}, {"name": "volumes/docker", "file_bytes": 100}]},
                "files": [{"name": "images/pages-1.img", "bytes": 32}, {"name": "criu.conf", "bytes": 2}]}))
        (output/"cli-pause.timing.json").write_text(json.dumps({"arguments": ["pause", "--snapshot-id", "c"*32, "branch"], "seconds": 12}))
        (output/"cli-pause.json").write_text(json.dumps({"record": {"pause": {"snapshot": {"resident_bytes_after": 0}}}}))

    def test_capture_plus_fork_and_cold_preparation_remain_visible(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory); self.fixture(path)
            metrics = benchmark.observations(path)["metrics_seconds"]
            self.assertEqual(metrics["capture-plus-fork-first-use"], 13)
            self.assertEqual(metrics["fork-first-use"], 3)
            self.assertEqual(metrics["cold-worker-to-useful"], 23.5)
            payload = benchmark.observations(path)["checkpoint_payloads"]["checkpoint-source"]
            self.assertEqual(payload["independent_mutable_file_bytes"], 100)
            self.assertEqual(payload["image_file_bytes"], 32)

    def test_missing_capture_prevents_a_qualified_sample(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory); self.fixture(path)
            (path/"prepare-output/cli-d.timing.json").unlink()
            with self.assertRaises(ValueError): benchmark.observations(path)


class BetweenRunResume(unittest.TestCase):
    def prefix(self):
        rows=[{"position":position,"configuration":configuration,"example":example,"passed":True,"native_artifact":str(index)}
              for index,(position,configuration,example) in enumerate(
                  (position,configuration,example) for position,configuration in enumerate(benchmark.ORDER) for example in ["factory","minimal"])]
        return {"passed":False,"error":"artifact storage uses 24.03 GiB","predetermined_order":benchmark.ORDER,"rows":rows[:14]}

    def test_exact_completed_prefix_can_continue_without_remeasurement(self):
        self.assertEqual(benchmark.resume_prefix(self.prefix()),14)

    def test_failed_native_or_changed_order_cannot_be_hidden_by_resuming(self):
        for change in ["failure","order","duplicate","wrong-reason"]:
            result=self.prefix()
            if change=="failure":result["rows"][-1]["passed"]=False
            if change=="order":result["rows"][0]["position"]=3
            if change=="duplicate":result["rows"][-1]["native_artifact"]=result["rows"][0]["native_artifact"]
            if change=="wrong-reason":result["error"]="native lifecycle failed"
            with self.assertRaises(ValueError):benchmark.resume_prefix(result)
