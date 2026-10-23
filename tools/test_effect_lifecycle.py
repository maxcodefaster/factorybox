import copy
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

import consumer_worker_loss
import effect_lifecycle


class EffectRecoveryCompletionTests(unittest.TestCase):
    def test_inference_only_proof_cannot_start_an_outside_effect_service(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory).resolve()
            name = "factorybox-provider-gateway-" + "a" * 32
            proof = root / "artifacts" / name
            proof.mkdir(parents=True)
            (proof / "result.json").write_text(json.dumps({"effect_only": False}))
            with patch.object(effect_lifecycle.reference, "ROOT", root), patch.object(effect_lifecycle.shutil, "copyfile") as copy_file:
                with self.assertRaisesRegex(ValueError, "complete native qualification"):
                    effect_lifecycle.prepare(root / "new-artifact", name, "synthetic-worker", {})
                copy_file.assert_not_called()

    def fixture(self):
        status = {"schema": 1, "operation": "a" * 64, "applications": 1,
                  "last_effect_sha256": "b" * 64, "observer_pid": 12345}
        receipt = {"operation": status["operation"], "tool_launches": 1, "unknown_sha256": "c" * 64,
                   "result_unknown_before_reconciliation": True, "agent_received_uncertainty": True,
                   "agent_received_reconciliation": True, "reconciliation": {**status, "operator_action": "do-not-retry"}}
        result = {"phases": [{"phase": phase, "passed": True, "worker_removed": True,
                              "startup": {"status": {"epoch": epoch}}} for phase, epoch in [("prepare", "first"), ("recover", "second")]]}
        return result, status, {"recovered": copy.deepcopy(receipt), "branch": copy.deepcopy(receipt)}

    def test_complete_plan_requires_both_workers(self):
        self.assertEqual(consumer_worker_loss.planned_phases("agent-effect"), ("prepare", "recover"))
        result, status, receipts = self.fixture()
        self.assertTrue(effect_lifecycle.require_completion(result, status, receipts)["passed"])
        for changed in ["missing-recovery", "same-epoch", "unclosed", "repeated-effect", "missing-branch", "different-outside-process", "automatic-retry", "changed-uncertainty", "missing-ack"]:
            with self.subTest(changed=changed):
                candidate, actual, completions = copy.deepcopy((result, status, receipts))
                if changed == "missing-recovery":
                    candidate["phases"].pop()
                elif changed == "same-epoch":
                    candidate["phases"][1]["startup"]["status"]["epoch"] = "first"
                elif changed == "unclosed":
                    candidate["phases"][1]["worker_removed"] = False
                elif changed == "repeated-effect":
                    actual["applications"] = 2
                elif changed == "missing-branch":
                    del completions["branch"]
                elif changed == "different-outside-process":
                    completions["branch"]["reconciliation"]["observer_pid"] = 23456
                elif changed == "automatic-retry":
                    completions["branch"]["reconciliation"]["operator_action"] = "retry"
                elif changed == "changed-uncertainty":
                    completions["branch"]["unknown_sha256"] = "d" * 64
                else:
                    completions["branch"]["agent_received_reconciliation"] = False
                with self.assertRaises(ValueError):
                    effect_lifecycle.require_completion(candidate, actual, completions)
