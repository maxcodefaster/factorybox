import copy
import unittest

import consumer_worker_loss


class AgentCancellationPhaseTests(unittest.TestCase):
    def test_cancellation_requires_actual_worker_replacement_phase(self):
        self.assertEqual(consumer_worker_loss.planned_phases("agent-cancel"), ("prepare", "recover"))

    def test_original_probe_phase_plans_remain(self):
        for probe in ["worker-loss", "incomplete-pause"]:
            with self.subTest(probe=probe):
                self.assertEqual(consumer_worker_loss.planned_phases(probe), ("prepare", "recover"))
        for probe in ["attached-exec", "topology", "lifecycle", "warm-preparation"]:
            with self.subTest(probe=probe):
                self.assertEqual(consumer_worker_loss.planned_phases(probe), ("prepare",))

    def test_unknown_probe_cannot_silently_become_one_phase(self):
        with self.assertRaises(ValueError):
            consumer_worker_loss.planned_phases("unplanned-new-case")

    def test_prepare_only_native_result_cannot_qualify_agent_recovery(self):
        # The first real cancellation run passed and closed only preparation.
        # Its historical result stays unchanged, but must fail this completion
        # boundary before any result can claim worker replacement.
        partial = {"phases": [{"phase": "prepare", "passed": True, "worker_removed": True}]}
        with self.assertRaisesRegex(ValueError, "both prepare and recover"):
            consumer_worker_loss.require_agent_cancel_recovery(partial)

    def test_recovery_requires_same_cancellation_new_epoch_and_independent_descendant(self):
        agent = {"session_id": "synthetic-session", "call_sha256": "a" * 64}
        result = {"phases": [
            {"phase": "prepare", "passed": True, "worker_removed": True,
             "startup": {"status": {"epoch": "first"}}, "agent_cancellation": {"agent": agent}},
            {"phase": "recover", "passed": True, "worker_removed": True,
             "startup": {"status": {"epoch": "second"}}, "agent_cancellation": {
                 "agent": copy.deepcopy(agent), "independent_branch": True, "source_deleted": True, "recovery_replay": True}},
        ]}
        self.assertTrue(consumer_worker_loss.require_agent_cancel_recovery(result)["passed"])
        for changed in ["passed", "worker_removed", "agent", "epoch", "independent_branch", "source_deleted", "recovery_replay"]:
            with self.subTest(changed=changed):
                candidate = copy.deepcopy(result)
                recovery = candidate["phases"][1]
                if changed in {"passed", "worker_removed"}:
                    recovery[changed] = False
                elif changed == "agent":
                    recovery["agent_cancellation"]["agent"]["call_sha256"] = "b" * 64
                elif changed == "epoch":
                    recovery["startup"]["status"]["epoch"] = "first"
                else:
                    recovery["agent_cancellation"][changed] = False
                with self.assertRaises(ValueError):
                    consumer_worker_loss.require_agent_cancel_recovery(candidate)
