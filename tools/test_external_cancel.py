import copy
import hashlib
import json
from pathlib import Path
import tempfile
import unittest

import external_cancel


class ExternalCancellationWireTests(unittest.TestCase):
    def fixture(self, directory):
        artifact = Path(directory)
        output = artifact / "external-provider"
        output.mkdir()
        error = "Tool execution interrupted"
        agent = {"session_id": "ses_original", "call_sha256": "a" * 64,
                 "tool_call_id": "actual-provider-dynamic-call", "tool_error": {"message": error}}
        continuations = {}
        for target, token in [("recovered", "b" * 32), ("branch", "c" * 32)]:
            continuations[target] = {"session_id": agent["session_id"], "call_sha256": agent["call_sha256"],
                "tool_call_id": agent["tool_call_id"], "prompt_token": token, "ack": "FACTORYBOX_CANCEL_ACK_" + token,
                "completed": True, "tool_error_consumed": True, "launches": 1,
                "error_sha256": hashlib.sha256(error.encode()).hexdigest(),
                "user_message_id": "msg_user_" + token, "assistant_message_id": "msg_assistant_" + token,
                "idle_message_id": "msg_idle_" + token}
        result = {"phases": [{}, {"agent_cancellation": {"agent": agent, "continuations": continuations}}],
                  "external_provider": {"cancellation": {}}}
        requests = [{"messages": [{"role": "user", "content": "initial tool request"}]}]
        for receipt in continuations.values():
            requests.append({"messages": [
                {"role": "user", "content": "FACTORYBOX_CANCEL_CONTINUE_" + receipt["prompt_token"]},
                {"role": "assistant", "tool_calls": [{"id": agent["tool_call_id"], "function": {"name": "shell"}}]},
                {"role": "tool", "tool_call_id": agent["tool_call_id"], "content": error}]})
        records = []
        for number, request in enumerate(requests, 1):
            path = output / f"{number:02}.request.json"
            path.write_text(json.dumps(request))
            records.append({"number": number, "passed": True, "delivered": True, "session": agent["session_id"],
                            "request_sha256": hashlib.sha256(path.read_bytes()).hexdigest()})
        return artifact, records, result

    def test_dynamic_original_call_and_both_real_tool_errors_are_required(self):
        with tempfile.TemporaryDirectory() as directory:
            artifact, records, result = self.fixture(directory)
            external_cancel.require_exchanges(artifact, records, result)
            proof = result["external_provider"]["cancellation"]
            self.assertTrue(proof["actual_error_ingestion_verified"])
            self.assertEqual({row["target"] for row in proof["exchanges"]}, {"recovered", "branch"})

    def test_forged_model_input_duplicate_tool_or_prompt_cannot_qualify(self):
        for fault in ["call", "missing_error", "duplicate_tool", "error_in_prompt", "same_prompt", "changed_bytes"]:
            with self.subTest(fault=fault), tempfile.TemporaryDirectory() as directory:
                artifact, records, result = self.fixture(directory)
                path = artifact / "external-provider/03.request.json"
                request = json.loads(path.read_text())
                if fault == "call":
                    request["messages"][-1]["tool_call_id"] = "unrelated-call"
                elif fault == "missing_error":
                    request["messages"][-1]["content"] = "unrelated content"
                elif fault == "duplicate_tool":
                    request["messages"].append(copy.deepcopy(request["messages"][-1]))
                elif fault == "error_in_prompt":
                    request["messages"][0]["content"] += " Tool execution interrupted"
                elif fault == "same_prompt":
                    request["messages"][0]["content"] = "FACTORYBOX_CANCEL_CONTINUE_" + "b" * 32
                path.write_text(json.dumps(request) + "\n")
                if fault != "changed_bytes":
                    records[2]["request_sha256"] = hashlib.sha256(path.read_bytes()).hexdigest()
                with self.assertRaises(ValueError):
                    external_cancel.require_exchanges(artifact, records, result)

    def test_failure_missing_branch_or_replayed_shell_never_passes(self):
        for fault in ["missing_branch_exchange", "lost_response", "foreign_session", "replayed_shell", "wrong_error"]:
            with self.subTest(fault=fault), tempfile.TemporaryDirectory() as directory:
                artifact, records, result = self.fixture(directory)
                branch = result["phases"][1]["agent_cancellation"]["continuations"]["branch"]
                if fault == "missing_branch_exchange":
                    records.pop()
                elif fault == "lost_response":
                    records[2]["delivered"] = False
                elif fault == "foreign_session":
                    records[2]["session"] = "ses_foreign"
                elif fault == "replayed_shell":
                    branch["launches"] = 2
                elif fault == "wrong_error":
                    branch["error_sha256"] = "d" * 64
                with self.assertRaises(ValueError):
                    external_cancel.require_exchanges(artifact, records, result)
