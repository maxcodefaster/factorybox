import copy
import json
from pathlib import Path
import shutil
import tempfile
import unittest
from unittest.mock import Mock

import external_effect
import external_cancel
import provider_peer
import reference


class CombinedFixtureTests(unittest.TestCase):
    def test_ordered_peer_extension_keeps_the_existing_real_cancel_preparation(self):
        with tempfile.TemporaryDirectory() as directory:
            artifact = Path(directory)
            flow = artifact / "opencode-flow"
            shutil.copytree(reference.ROOT / "tests/opencode-flow", flow)
            shutil.copyfile(reference.ROOT / "tests/opencode-external/lifecycle-client.cjs", flow / "client.cjs")
            result = {"external_provider": {}}
            external_cancel.prepare(artifact, result)
            self.assertIn("cancel-held-tool.cjs", (flow / "client.cjs").read_text())
            self.assertIn("cancellation", result["external_provider"])

    def test_a_separate_socket_cannot_redirect_the_inference_peer(self):
        with tempfile.TemporaryDirectory() as directory:
            artifact = Path(directory)
            for effect, socket in [(False, "/outside-effect/provider.sock"), (True, "/unrelated/socket")]:
                with self.subTest(effect=effect, socket=socket), self.assertRaises(ValueError):
                    provider_peer.Peer("owned", "identity", "a" * 32, artifact, artifact,
                                       "b" * 64, effect_only=effect, relay_socket=socket)

    def test_preparation_preserves_original_sources_and_adds_only_selected_leases(self):
        with tempfile.TemporaryDirectory() as directory:
            artifact = Path(directory).resolve()
            source = artifact / "source"
            target = source / "tests/devx-lifecycle/opencode_flow_linux_test.go"
            target.parent.mkdir(parents=True)
            original = reference.ROOT / "tests/devx-lifecycle/opencode_flow_linux_test.go"
            shutil.copyfile(original, target)
            declaration = json.loads((reference.ROOT / "tests/compatibility/external-effect-gateway.json").read_text())
            external_effect.apply_fixture(artifact, source)
            self.assertEqual(target.read_text().replace(declaration["insertion"], ""), original.read_text())
            self.assertEqual(reference.sha(original), declaration["before_sha256"])
            with self.assertRaises(ValueError):
                external_effect.apply_fixture(artifact, source)

    def test_closed_or_shared_source_cannot_be_rewritten(self):
        for shared in [False, True]:
            with self.subTest(shared=shared), tempfile.TemporaryDirectory() as directory:
                artifact = Path(directory).resolve()
                source = artifact / "source"
                target = source / "tests/devx-lifecycle/opencode_flow_linux_test.go"
                target.parent.mkdir(parents=True)
                shutil.copyfile(reference.ROOT / "tests/devx-lifecycle/opencode_flow_linux_test.go", target)
                before = target.read_bytes()
                if shared:
                    (artifact / "shared.go").hardlink_to(target)
                else:
                    (artifact / "result.json").write_text('{}')
                with self.assertRaises(ValueError):
                    external_effect.apply_fixture(artifact, source)
                self.assertEqual(target.read_bytes(), before)

    def test_real_client_and_actual_output_patches_keep_original_result_assertions(self):
        with tempfile.TemporaryDirectory() as directory:
            artifact = Path(directory)
            flow = artifact / "opencode-flow"
            shutil.copytree(reference.ROOT / "tests/opencode-flow", flow)
            shutil.copyfile(reference.ROOT / "tests/opencode-external/lifecycle-client.cjs", flow / "client.cjs")
            proof = artifact / "proof"
            proof.mkdir()
            (proof / "result.json").write_text('{}')
            result = {"external_provider": {"gateway_sha256": "a" * 64}}
            external_effect.prepare_inputs(artifact, "synthetic-proof", proof, {"binary_sha256": "a" * 64}, result)
            self.assertTrue(result["effect_oracle"]["real_external_model"])
            self.assertIn("effect-held-tool.cjs", (flow / "client.original.cjs").read_text())
            self.assertIn("require('./effect-output.cjs')(root)", (flow / "held-tool.cjs").read_text())
            self.assertIn("part.text.includes('FACTORYBOX_EFFECT_UNCERTAIN_'", (flow / "effect-client.cjs").read_text())
            self.assertEqual(reference.sha(reference.ROOT / "tests/opencode-flow/held-tool.cjs"),
                             result["effect_oracle"]["output"]["before_sha256"])


class CombinedWireTests(unittest.TestCase):
    def fixture(self, directory):
        artifact = Path(directory)
        (artifact / "external-provider").mkdir()
        (artifact / "recover-output").mkdir()
        status = {"schema": 1, "operation": "a" * 64, "applications": 1,
                  "last_effect_sha256": "b" * 64, "observer_pid": 1234}
        receipt = {**status, "operator_action": "do-not-retry"}
        records = [{"number": n, "passed": True, "delivered": True, "status": 200, "session": "ses_original"} for n in [1, 2, 3]]
        result = {"external_provider": {"requests": records}, "effect_oracle": {"phase_snapshots": [{"status": status}]}, "effect_completion": {}}
        for number, target in [(2, "recovered"), (3, "branch")]:
            nonce, ram = "c" * 64, "d" * 64
            completed = {"session_id": "ses_original", "result": {"nonce": nonce, "ram_sha256": ram},
                         "external_effect": {"tool_call_id": "actual-dynamic-call"}}
            (artifact / "recover-output" / ("opencode-result-" + target + ".json")).write_text(json.dumps(completed))
            content = "FACTORYBOX_EFFECT_UNCERTAIN_" + status["operation"] + "\n" + nonce + " " + ram + "\n" + "FACTORYBOX_EFFECT_RECONCILIATION " + json.dumps(receipt)
            request = {"messages": [{"role": "user", "content": "run the one authorized tool"},
                {"role": "assistant", "tool_calls": [{"id": "actual-dynamic-call", "function": {"name": "shell"}}]},
                {"role": "tool", "tool_call_id": "actual-dynamic-call", "content": content}]}
            path = artifact / "external-provider" / f"{number:02}.request.json"
            path.write_text(json.dumps(request))
            records[number - 1]["request_sha256"] = reference.sha(path)
        return artifact, result

    def test_actual_tool_output_contains_original_uncertainty_and_outside_receipt(self):
        with tempfile.TemporaryDirectory() as directory:
            artifact, result = self.fixture(directory)
            external_effect.require_exchanges(artifact, result)
            self.assertEqual(len(result["effect_completion"]["actual_external_error_and_reconciliation_ingestion"]), 2)

    def test_forged_outside_receipt_repeated_effect_and_extra_inference_fail(self):
        for fault in ["changed_receipt", "missing_uncertainty", "user_supplied_receipt", "unrelated_call", "extra_inference", "provider_error"]:
            with self.subTest(fault=fault), tempfile.TemporaryDirectory() as directory:
                artifact, result = self.fixture(directory)
                path = artifact / "external-provider/03.request.json"
                request = json.loads(path.read_text())
                if fault == "changed_receipt":
                    request["messages"][-1]["content"] = request["messages"][-1]["content"].replace('"applications": 1', '"applications": 2')
                elif fault == "missing_uncertainty":
                    request["messages"][-1]["content"] = request["messages"][-1]["content"].replace('FACTORYBOX_EFFECT_UNCERTAIN_', 'unrelated_')
                elif fault == "user_supplied_receipt":
                    request["messages"][0]["content"] += "b" * 64
                elif fault == "unrelated_call":
                    request["messages"][-1]["tool_call_id"] = "different-call"
                elif fault == "extra_inference":
                    result["external_provider"]["requests"].append(copy.deepcopy(result["external_provider"]["requests"][-1]))
                elif fault == "provider_error":
                    result["external_provider"]["requests"][-1]["passed"] = False
                path.write_text(json.dumps(request))
                result["external_provider"]["requests"][2]["request_sha256"] = reference.sha(path)
                with self.assertRaises(ValueError):
                    external_effect.require_exchanges(artifact, result)

    def test_cleanup_attempts_both_peers_if_one_fails(self):
        peer = external_effect.Peer.__new__(external_effect.Peer)
        peer.effect = Mock(closed=True, result={"passed": False})
        peer.inference = Mock(closed=True, result={"passed": True})
        peer.effect.close.side_effect = ValueError('one release failed')
        with self.assertRaisesRegex(ValueError, 'one release failed'):
            peer.close(aborted=True)
        peer.inference.close.assert_called_once_with(aborted=True)
        self.assertFalse(peer.result['passed'])
