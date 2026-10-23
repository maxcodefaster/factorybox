"""Optional outside-effect counter for unchanged full consumer assertions."""
import difflib
import json
import os
from pathlib import Path
import re
import shutil
import tempfile
import threading

import effect_ledger
import reference
import reference_lifecycle
import upstream_unit


def prepare(artifact, proof_name, worker_image, result):
    if not re.fullmatch(r"factorybox-provider-gateway-[0-9a-f]{32}", proof_name):
        raise ValueError("requires one exact outside-effect gateway proof")
    proof = reference.ROOT / "artifacts" / proof_name
    if proof.resolve() != proof or (proof / "result.json").is_symlink():
        raise ValueError("outside-effect proof cannot be redirected")
    saved = json.loads((proof / "result.json").read_text())
    if (saved.get("effect_only") is not True or saved.get("worker_image_id") != worker_image or
            any(saved.get(key) is not True for key in ["passed", "cleanup", "inputs_unchanged", "foreign_birth_refused", "foreign_mode_refused", "global_sysctls_unchanged"]) or
            upstream_unit.context_manifest(proof / "source") != saved["source_context"] or
            upstream_unit.context_manifest(proof / "source/tests/provider-gateway") != upstream_unit.context_manifest(reference.ROOT / "tests/provider-gateway") or
            reference.sha(proof / "provider-gateway") != saved["binary_sha256"]):
        raise ValueError("outside-effect gateway lacks its current complete native qualification")
    shutil.copyfile(proof / "provider-gateway", artifact / "provider-gateway")
    (artifact / "provider-gateway").chmod(0o555)
    for file in ["effect_lifecycle.py", "effect_ledger.py", "provider_peer.py"]:
        shutil.copyfile(reference.ROOT / "tools" / file, artifact / file)
    model = artifact / "opencode-flow/model.cjs"
    before = model.read_text()
    command = "/opt/factorybox/opencode-flow/held-tool.cjs "
    reply = "        delta = {role: 'assistant', content: 'FACTORYBOX_AGENT_ACK ' + content};"
    if before.count(command) != 1 or before.count(reply) != 1:
        raise ValueError("ordinary model boundary changed")
    after = before.replace(command, "/opt/factorybox/opencode-flow/effect-held-tool.cjs ").replace(reply,
        reply + "\n        delta.content += require('./effect-model.cjs')(root, content, results);")
    (artifact / "effect-model.before.cjs").write_text(before)
    patch = artifact / "effect-model.patch"
    patch.write_text(''.join(difflib.unified_diff(before.splitlines(True), after.splitlines(True), fromfile="a/model.cjs", tofile="b/model.cjs")))
    model.write_text(after)
    client = artifact / "opencode-flow/client.cjs"
    client_before = client.read_text()
    original = artifact / "opencode-flow/client.original.cjs"
    original.write_text(client_before)
    client_after = "require('./effect-client.cjs');\n"
    client_patch = artifact / "effect-client.patch"
    client_patch.write_text(''.join(difflib.unified_diff(client_before.splitlines(True), client_after.splitlines(True), fromfile="a/client.cjs", tofile="b/client.cjs")))
    client.write_text(client_after)
    result["effect_oracle"] = {"gateway_proof": proof_name, "gateway_proof_sha256": reference.sha(proof / "result.json"),
        "gateway_sha256": saved["binary_sha256"], "effect_only": True, "selected": False,
        "model": {"before_sha256": reference.sha(artifact / "effect-model.before.cjs"), "after_sha256": reference.sha(model), "patch_sha256": reference.sha(patch)},
        "client": {"ordinary_delegate_sha256": reference.sha(original), "after_sha256": reference.sha(client), "patch_sha256": reference.sha(client_patch)},
        "assertions_preserved": True, "scope": "Local deterministic model and host-owned external-effect oracle. Every original client and native lifecycle assertion remains. No real external inference or general exactly-once guarantee."}


def require_completion(result, status, completions):
    phases = result["phases"]
    if [row.get("phase") for row in phases] != ["prepare", "recover"] or any(row.get("passed") is not True or row.get("worker_removed") is not True for row in phases):
        raise ValueError("outside-effect proof requires two complete closed worker phases")
    epochs = [row["startup"]["status"]["epoch"] for row in phases]
    if not all(epochs) or epochs[0] == epochs[1]:
        raise ValueError("outside-effect proof requires distinct worker epochs")
    if status.get("applications") != 1 or set(completions) != {"recovered", "branch"}:
        raise ValueError("outside effect repeated or both independent agent completions are missing")
    hashes = []
    for receipt in completions.values():
        if (receipt.get("operation") != status["operation"] or receipt.get("tool_launches") != 1 or
                any(receipt.get(key) is not True for key in ["result_unknown_before_reconciliation", "agent_received_uncertainty", "agent_received_reconciliation"]) or
                receipt.get("reconciliation") != {**status, "operator_action": "do-not-retry"}):
            raise ValueError("agent lacks its actual outside reconciliation without replay")
        digest = receipt.get("unknown_sha256", "")
        if not re.fullmatch(r"[0-9a-f]{64}", digest):
            raise ValueError("uncertain state lacks its exact identity")
        hashes.append(digest)
    if hashes[0] != hashes[1]:
        raise ValueError("independent descendants changed the original uncertain result")
    return {"passed": True, "worker_epochs": epochs, "operation": status["operation"],
            "outside_applications": 1, "independent_reconciliations": ["recovered", "branch"], "unknown_sha256": hashes[0]}


class Service:
    def __init__(self, artifact, result):
        self.artifact, self.result = artifact, result
        self.directory = tempfile.TemporaryDirectory(prefix="fbe-")
        self.relay = effect_ledger.Ledger(Path(self.directory.name) / "provider.sock", artifact / "outside-effects")
        os.chmod(Path(self.directory.name) / "provider.sock", 0o666)
        self.thread = threading.Thread(target=self.relay.serve_forever, daemon=True)
        self.thread.start()
        self.snapshots = []

    def snapshot(self, phase):
        with self.relay.lock:
            events = self.relay._events()
        if len(events) != 1:
            raise ValueError("outside counter missing or repeated before worker phase closure")
        status = self.relay.status(events[0]["operation"])
        self.snapshots.append({"phase": phase, "status": status})
        self.result["effect_oracle"]["phase_snapshots"] = list(self.snapshots)

    def require_requests(self):
        self.snapshot("complete")
        records = self.relay.records
        applied = [row for row in records if row["path"] == effect_ledger.APPLY]
        if (any("error" in row for row in records) or len(applied) != 1 or not applied[0]["committed"] or
                not applied[0].get("injected_lost_reply") or applied[0]["delivered"]):
            raise ValueError("outside oracle did not commit its one effect before losing the acknowledgment")
        if [row["phase"] for row in self.snapshots] != ["prepare", "recover", "complete"] or any(row["status"] != self.snapshots[0]["status"] for row in self.snapshots):
            raise ValueError("outside state was rolled back or its controller changed across worker loss")
        completions = {}
        for target in ["recovered", "branch"]:
            value = json.loads((self.artifact / "recover-output" / ("opencode-result-" + target + ".json")).read_text())
            if value.get("agent_ack") is not True or value.get("model_received_exit") != 37:
                raise ValueError("ordinary actual-tool result assertions are missing")
            completions[target] = value["external_effect"]
        self.result["effect_completion"] = require_completion(self.result, self.snapshots[-1]["status"], completions)

    def close(self):
        self.relay.shutdown()
        self.thread.join(timeout=2)
        self.relay.server_close()
        self.result["effect_oracle"].update(requests=list(self.relay.records), ledger_sha256=reference.sha(self.relay.path),
                                           controller_pid=os.getpid(), cleanup=not self.thread.is_alive())
        self.directory.cleanup()
