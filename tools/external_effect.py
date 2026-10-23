"""Compose the real model with the separately owned uncertain-effect oracle."""
import difflib
import json
import shutil

import effect_lifecycle
import external_lifecycle
import provider_peer
import reference
import reference_lifecycle


def patched(artifact, target, after, label, *, patch_file=None):
    if (artifact / "result.json").exists() or target.is_symlink() or target.stat().st_nlink != 1:
        raise ValueError("combined fixture patches require unique new open inputs")
    before = target.read_text()
    if before == after:
        raise ValueError("combined fixture patch has no declared change: " + label)
    original = artifact / (label + ".before")
    if original.exists() or (artifact / (label + ".patch")).exists():
        raise ValueError("combined fixture patch already exists")
    original.write_text(before)
    patch = artifact / (label + ".patch")
    patch_name = patch_file or target.name
    patch.write_text(''.join(difflib.unified_diff(before.splitlines(True), after.splitlines(True),
                                               fromfile="a/" + patch_name, tofile="b/" + patch_name)))
    target.write_text(after)
    return {"before_sha256": reference.sha(original), "after_sha256": reference.sha(target),
            "patch_sha256": reference.sha(patch), "patch": patch.name, "before_file": original.name}


def apply_fixture(artifact, source):
    if artifact.resolve() != artifact or source.resolve() != source or not source.is_relative_to(artifact):
        raise ValueError("combined fixture source must belong to its new canonical artifact")
    declaration = reference.ROOT / "tests/compatibility/external-effect-gateway.json"
    value = json.loads(declaration.read_text())
    target = source / value["file"]
    if (value["schema"] != 1 or not value["assertions_preserved"] or value["upstream_assertions_modified"] or
            target.is_symlink() or target.stat().st_nlink != 1 or reference.sha(target) != value["before_sha256"]):
        raise ValueError("combined fixture requires its exact original consumer source copy")
    before = target.read_text()
    if before.count(value["anchor"]) != 1:
        raise ValueError("combined fixture lease boundary changed")
    after = before.replace(value["anchor"], value["insertion"] + value["anchor"])
    receipt = patched(artifact, target, after, "external-effect-go", patch_file=value["file"])
    if (receipt["after_sha256"] != value["after_sha256"] or
            receipt["patch_sha256"] != value["patch_sha256"] or
            reference.sha(reference.ROOT / value["patch"]) != value["patch_sha256"]):
        raise ValueError("combined fixture differs from its ordered declared patch")
    shutil.copyfile(declaration, artifact / declaration.name)
    return {**receipt, "declaration_sha256": reference.sha(declaration), "assertions_preserved": True}


def prepare_inputs(artifact, proof_name, proof, saved, result):
    if result["external_provider"]["gateway_sha256"] != saved["binary_sha256"]:
        raise ValueError("combined peers require the same independently qualified helper bytes")
    flow = artifact / "opencode-flow"
    client = flow / "client.cjs"
    before = client.read_text()
    old = "/opt/factorybox/opencode-flow/held-tool.cjs "
    ack = "and exit status from the tool result. Its nonzero exit is deliberate. Do not repeat it."
    if before.count(old) != 1 or before.count(ack) != 1:
        raise ValueError("actual external client command or result instruction changed")
    after = before.replace(old, "/opt/factorybox/opencode-flow/effect-held-tool.cjs ").replace(ack,
        ack + " Also report the exact FACTORYBOX_EFFECT_UNCERTAIN marker from the tool output and the exact "
        "FACTORYBOX_EFFECT_RECONCILED marker followed by DO_NOT_RETRY from its independently queried receipt. "
        "Never retry the outside effect or call another tool.")
    arm = patched(artifact, client, after, "external-effect-arm")
    shutil.copyfile(client, flow / "client.original.cjs")
    wrapped = patched(artifact, client, "require('./effect-client.cjs');\n", "external-effect-delegate")
    wrapper = flow / "effect-client.cjs"
    text = wrapper.read_text()
    timeout = "timeout: 25000, maxBuffer:"
    observed = "!parts.some(part => part.type === 'text' && part.text.includes(ack))"
    receipt = "    agent_received_reconciliation: true, tool_launches: 1};"
    if any(text.count(value) != 1 for value in [timeout, observed, receipt]):
        raise ValueError("original effect client assertion boundary changed")
    text = text.replace(timeout, "timeout: ['arm', 'finish'].includes(process.argv[2]) ? 85000 : 25000, maxBuffer:")
    text = text.replace(observed, "!parts.some(part => part.type === 'text' && part.text.includes(ack) && "
                        "part.text.includes('FACTORYBOX_EFFECT_UNCERTAIN_' + status.operation))")
    text = text.replace(receipt, "    agent_received_reconciliation: true, tool_launches: 1, tool_call_id: calls[0].id};")
    wrapper_patch = patched(artifact, wrapper, text, "external-effect-wrapper")
    held = flow / "held-tool.cjs"
    before = held.read_text()
    stdout = "process.stdout.write('FACTORYBOX_TOOL_RESULT ' + JSON.stringify(result) + '\\n', () => process.exit(37));"
    if before.count(stdout) != 1:
        raise ValueError("original private-RAM result boundary changed")
    after = before.replace(stdout, "process.stdout.write('FACTORYBOX_TOOL_RESULT ' + JSON.stringify(result) + "
                           "'\\n' + require('./effect-output.cjs')(root), () => process.exit(37));")
    output_patch = patched(artifact, held, after, "external-effect-output")
    shutil.copyfile(__file__, artifact / "external_effect.py")
    result["effect_oracle"] = {"gateway_proof": proof_name, "gateway_proof_sha256": reference.sha(proof / "result.json"),
        "gateway_sha256": saved["binary_sha256"], "effect_only": True, "selected": False,
        "assertions_preserved": True, "real_external_model": True,
        "arm": arm, "delegate": wrapped, "wrapper": wrapper_patch, "output": output_patch,
        "scope": "Actual external model and one independently committed outside mutation with lost reply. "
                 "All original native/client/RAM/watch/result assertions remain; separate gateways exit before captures. "
                 "Reconciliation is an explicit read-only operator lookup supplied as actual tool output. No general exactly-once guarantee."}


def require_exchanges(artifact, result):
    records = sorted(result["external_provider"]["requests"], key=lambda row: row["number"])
    if len(records) != 3 or [r["number"] for r in records] != [1, 2, 3] or any(
            not r["passed"] or not r["delivered"] or r["status"] != 200 for r in records):
        raise ValueError("real outside-effect proof requires exactly three successful actual model exchanges")
    status = result["effect_oracle"]["phase_snapshots"][-1]["status"]
    observations = []
    for number, target in [(2, "recovered"), (3, "branch")]:
        completed = json.loads((artifact / "recover-output" / ("opencode-result-" + target + ".json")).read_text())
        effect = completed["external_effect"]
        path = artifact / "external-provider" / f"{number:02}.request.json"
        if reference.sha(path) != records[number - 1]["request_sha256"]:
            raise ValueError("actual model request changed after its recorded exchange")
        messages = json.loads(path.read_text())["messages"]
        tools = [row for row in messages if row["role"] == "tool"]
        calls = [call for row in messages if row["role"] == "assistant" for call in row.get("tool_calls", [])]
        if (len(tools) != 1 or len(calls) != 1 or not effect.get("tool_call_id") or
                tools[0].get("tool_call_id") != effect["tool_call_id"] or calls[0].get("id") != effect["tool_call_id"] or
                calls[0].get("function", {}).get("name") != "shell" or records[number - 1]["session"] != completed["session_id"]):
            raise ValueError("actual model did not receive its single original outside-effect tool result")
        content = tools[0].get("content")
        marker = "FACTORYBOX_EFFECT_RECONCILIATION "
        receipts = [json.loads(line.split(marker, 1)[1]) for line in content.splitlines() if line.startswith(marker)] if isinstance(content, str) else []
        if (len(receipts) != 1 or receipts[0] != {**status, "operator_action": "do-not-retry"} or
                "FACTORYBOX_EFFECT_UNCERTAIN_" + status["operation"] not in content or
                completed["result"]["nonce"] not in content or completed["result"]["ram_sha256"] not in content or
                status["last_effect_sha256"] in json.dumps([row for row in messages if row["role"] == "user"])):
            raise ValueError("actual model lacks its original uncertainty and independently reconciled outside receipt")
        observations.append({"target": target, "request_sha256": records[number - 1]["request_sha256"],
                             "tool_call_id": effect["tool_call_id"], "passed": True})
    if len({row["tool_call_id"] for row in observations}) != 1 or len({row["session"] for row in records}) != 1:
        raise ValueError("independent outside-effect descendants changed their original agent session/call")
    result["effect_completion"]["actual_external_error_and_reconciliation_ingestion"] = observations


class Service:
    def __init__(self, artifact, result):
        self.artifact, self.result = artifact, result
        self.inference = external_lifecycle.Service(artifact, result)
        try:
            self.effect = effect_lifecycle.Service(artifact, result)
        except BaseException:
            self.inference.close()
            raise
        self.directory = self.inference.directory
        self.effect_directory = self.effect.directory

    def snapshot(self, phase):
        self.effect.snapshot(phase)

    def require_requests(self):
        self.effect.require_requests()
        self.inference.require_requests()
        self.result["external_provider"]["requests"] = list(self.inference.relay.records)
        require_exchanges(self.artifact, self.result)

    def close(self):
        try:
            self.effect.close()
        finally:
            self.inference.close()


class Peer:
    def __init__(self, worker, identity, owner, archive, output, digest):
        outside_archive, outside_output = archive / "outside-peer", output / "effect"
        outside_archive.mkdir()
        outside_output.mkdir(mode=0o700)
        self.inference = provider_peer.Peer(worker, identity, owner, archive, output, digest)
        self.effect = provider_peer.Peer(worker, identity, owner, outside_archive, outside_output, digest,
                                         effect_only=True, relay_socket="/outside-effect/provider.sock")
        self.closed = False
        self.result = {"passed": False, "cleanup": False}

    def pump(self):
        self.inference.pump()
        self.effect.pump()

    def close(self, aborted=False):
        failures = []
        for peer in [self.effect, self.inference]:
            try:
                peer.close(aborted=aborted)
            except Exception as error:
                failures.append(error)
        self.closed = self.effect.closed and self.inference.closed
        self.result = {"passed": all(p.result["passed"] for p in [self.effect, self.inference]),
                       "cleanup": self.closed, "inference": self.inference.result, "outside_effect": self.effect.result}
        if failures:
            raise failures[0]
