"""Actual external cancellation transcripts, separate from deterministic proof."""
import difflib
import hashlib
import json
import re
import shutil

import reference


def prepare(artifact, result):
    declaration = reference.ROOT / "tests/compatibility/external-cancel-fixture.json"
    value = json.loads(declaration.read_text())
    if value["schema"] != 1 or not value["assertions_preserved"] or value["upstream_assertions_modified"]:
        raise ValueError("external cancellation changed its fixture contract")
    for name, row in value["files"].items():
        if (reference.sha(reference.ROOT / name) != row["after_sha256"] or
                reference.sha(reference.ROOT / row["patch"]) != row["patch_sha256"] or
                reference.sha(reference.ROOT / "artifacts" / row["before_file"]) != row["before_sha256"]):
            raise ValueError("external cancellation fixture bytes differ: " + name)
    shutil.copyfile(declaration, artifact / declaration.name)
    client = artifact / "opencode-flow/client.cjs"
    before = client.read_text()
    old = "/opt/factorybox/opencode-flow/held-tool.cjs "
    if before.count(old) != 1:
        raise ValueError("external cancellation lacks its exact held tool command")
    after = before.replace(old, "/opt/factorybox/opencode-flow/cancel-held-tool.cjs ")
    (artifact / "external-cancel-arm.before.cjs").write_text(before)
    patch = artifact / "external-cancel-arm.patch"
    patch.write_text(''.join(difflib.unified_diff(before.splitlines(True), after.splitlines(True),
                                               fromfile="a/client.cjs", tofile="b/client.cjs")))
    client.write_text(after)
    shutil.copyfile(reference.ROOT / "tests/opencode-external/cancel-client.cjs",
                    artifact / "opencode-flow/external-cancel-client.cjs")
    shutil.copyfile(__file__, artifact / "external_cancel.py")
    result["external_provider"]["cancellation"] = {
        "fixture_declaration_sha256": reference.sha(declaration),
        "arm_before_sha256": reference.sha(artifact / "external-cancel-arm.before.cjs"),
        "arm_after_sha256": reference.sha(client), "arm_patch_sha256": reference.sha(patch),
        "scope": "Real model, actual dynamic tool ID, original interruption error and new prompts in independent descendants. "
                 "No local model log substitutes for provider input; the host verifies every wire exchange."}


def require_continuation(recovered):
    agent = recovered["agent"]
    call = agent.get("tool_call_id", "")
    error = agent.get("tool_error", {}).get("message", "")
    if not isinstance(call, str) or not 1 <= len(call) <= 256 or not isinstance(error, str) or not error:
        raise ValueError("external cancellation lacks its actual call and structured error")
    continuations = recovered.get("continuations", {})
    if set(continuations) != {"recovered", "branch"}:
        raise ValueError("external cancellation requires both independent new prompts")
    tokens = []
    for receipt in continuations.values():
        token = receipt.get("prompt_token", "")
        if (receipt.get("session_id") != agent["session_id"] or receipt.get("call_sha256") != agent["call_sha256"] or
                receipt.get("completed") is not True or receipt.get("tool_error_consumed") is not True or
                receipt.get("launches") != 1 or receipt.get("tool_call_id") != call or
                not re.fullmatch(r"[0-9a-f]{32}", token) or receipt.get("ack") != "FACTORYBOX_CANCEL_ACK_" + token or
                receipt.get("error_sha256") != hashlib.sha256(error.encode()).hexdigest() or receipt.get("content_sha256", "") != "" or
                any(not receipt.get(key, "").startswith("msg_") for key in ["user_message_id", "assistant_message_id", "idle_message_id"])):
            raise ValueError("external continuation lost its actual error/response or replayed a tool")
        tokens.append(token)
    if len(set(tokens)) != 2:
        raise ValueError("external descendants reused a continuation prompt")
    return {"passed": True, "targets": ["recovered", "branch"], "distinct_prompts": True, "launches_each": 1,
            "actual_tool_call_id": call, "provider_wire_verification_required": True}


def require_exchanges(artifact, records, result):
    recovered = result["phases"][1]["agent_cancellation"]
    require_continuation(recovered)
    agent = recovered["agent"]
    if len(records) != 3 or [row["number"] for row in records] != [1, 2, 3] or any(
            not row["passed"] or not row["delivered"] or row["session"] != agent["session_id"] for row in records):
        raise ValueError("external cancellation requires exactly arm and two delivered continuation exchanges")
    receipts = {value["prompt_token"]: (target, value) for target, value in recovered["continuations"].items()}
    observations = []
    for row in records:
        path = artifact / "external-provider" / f'{row["number"]:02}.request.json'
        if reference.sha(path) != row["request_sha256"]:
            raise ValueError("actual external request bytes changed")
        messages = json.loads(path.read_text())["messages"]
        tools = [item for item in messages if item["role"] == "tool"]
        calls = [call for item in messages if item["role"] == "assistant" for call in item.get("tool_calls", [])]
        if row["number"] == 1:
            if tools or calls:
                raise ValueError("arm unexpectedly inherited a prior tool call")
            continue
        users = [item for item in messages if item["role"] == "user"]
        text = json.dumps(users[-1].get("content")) if users else ""
        tokens = re.findall(r"FACTORYBOX_CANCEL_CONTINUE_([0-9a-f]{32})", text)
        if len(tokens) != 1 or tokens[0] not in receipts or len(tools) != 1 or len(calls) != 1:
            raise ValueError("continuation provider input lacks its unique prompt and original tool")
        token = tokens[0]
        target, receipt = receipts.pop(token)
        tool = tools[0]
        content = tool.get("content")
        if (calls[0].get("id") != agent["tool_call_id"] or calls[0].get("function", {}).get("name") != "shell" or
                tool.get("tool_call_id") != agent["tool_call_id"] or not isinstance(content, str) or
                agent["tool_error"]["message"] not in content or "FACTORYBOX_TOOL_RESULT" in content or
                agent["tool_error"]["message"] in text):
            raise ValueError("provider did not receive its saved cancellation error exclusively as the actual tool result")
        observations.append({"target": target, "prompt_token": token, "request_sha256": row["request_sha256"],
                             "tool_call_id": tool["tool_call_id"], "actual_tool_content_sha256": hashlib.sha256(content.encode()).hexdigest(),
                             "error_sha256": receipt["error_sha256"], "passed": True})
    if receipts:
        raise ValueError("one external descendant lacks its actual provider exchange")
    result["external_provider"]["cancellation"]["exchanges"] = observations
    result["external_provider"]["cancellation"]["actual_error_ingestion_verified"] = True
