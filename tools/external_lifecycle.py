"""Optional actual free-model inputs and relay for native consumer proofs.

The gateway is admitted only from a matching complete native namespace/stop
proof. Nothing here belongs to the standalone lifecycle runtime or snapshot.
"""
import json
import os
from pathlib import Path
import re
import shutil
import tempfile
import threading

import free_model_probe
import model_relay
import reference
import reference_lifecycle
import upstream_unit


def prepare(artifact, proof_name, worker_image, result):
    if not re.fullmatch(r"factorybox-provider-gateway-[0-9a-f]{32}", proof_name):
        raise ValueError("requires one exact native gateway proof")
    proof = reference.ROOT / "artifacts" / proof_name
    if proof.resolve() != proof or (proof / "result.json").is_symlink():
        raise ValueError("gateway proof cannot be redirected")
    record = json.loads((proof / "result.json").read_text())
    if (any(record.get(key) is not True for key in ["passed", "cleanup", "inputs_unchanged", "foreign_birth_refused", "global_sysctls_unchanged"]) or
            record["worker_image_id"] != worker_image or
            upstream_unit.context_manifest(proof / "source") != record["source_context"] or
            reference.sha(proof / "provider-gateway") != record["binary_sha256"]):
        raise ValueError("gateway proof does not qualify its frozen helper on this worker")
    current = upstream_unit.context_manifest(reference.ROOT / "tests/provider-gateway")
    frozen = upstream_unit.context_manifest(proof / "source/tests/provider-gateway")
    if current != frozen:
        raise ValueError("gateway source changed since its native proof")
    shutil.copyfile(proof / "provider-gateway", artifact / "provider-gateway")
    (artifact / "provider-gateway").chmod(0o555)
    shutil.copyfile(reference.ROOT / "tests/opencode-external/lifecycle-client.cjs", artifact / "opencode-flow/client.cjs")
    shutil.copyfile(reference.ROOT / "tests/opencode-external/lifecycle-client.cjs", artifact / "external-client.cjs")
    for file in ["external_lifecycle.py", "provider_peer.py", "model_relay.py", "free_model_probe.py"]:
        shutil.copyfile(reference.ROOT / "tools" / file, artifact / file)
    discovery = {}
    for label, url in [("catalog", free_model_probe.CATALOG), ("pricing", free_model_probe.PRICES)]:
        body, observation = free_model_probe.exchange(url)
        (artifact / (label + ".body")).write_bytes(body)
        discovery[label] = observation
        if observation["status"] != 200:
            raise ValueError("current external free-model discovery failed")
    model = "glm-5.3-flash-free"
    evidence = free_model_probe.approve_free_model(model,
        json.loads((artifact / "catalog.body").read_text()), (artifact / "pricing.body").read_text())
    descriptor = {"id": model, "label": evidence["identity_row"][0]}
    reference_lifecycle.write_json(artifact / "opencode-flow/model.json", descriptor)
    model_config = {"name": descriptor["label"],
                    "capabilities": {"tools": True, "input": ["text"], "output": ["text"]},
                    "limit": {"context": 32768, "output": 2048}, "cost": {"input": 0, "output": 0}}
    config = {"model": "opencode/" + model, "update": "disable", "share": "disabled", "snapshots": False, "warming": False,
              "permissions": [{"action": "*", "resource": "*", "effect": "allow"}], "providers": {"opencode": {
                  "package": "@opencode/ai/providers/openai-compatible",
                  "settings": {"baseURL": "http://127.0.0.1:4097/v1", "apiKey": "public"},
                  "models": {model: model_config}}}}
    reference_lifecycle.write_json(artifact / "opencode-flow/config.json", config)
    result["external_provider"] = {"model": model, "discovery": discovery, "free_model_evidence": evidence,
                                   "gateway_proof": proof_name, "gateway_proof_sha256": reference.sha(proof / "result.json"),
                                   "gateway_sha256": record["binary_sha256"], "selected": False,
                                   "scope": "Quiescent external inference only. Exact gateway pidfd exit before each capture; a fresh gateway for later inference. No in-flight external TCP restore."}


class Service:
    def __init__(self, artifact, result, disconnect=False):
        self.artifact, self.disconnect = artifact, disconnect
        self.directory = tempfile.TemporaryDirectory(prefix="fbl-")
        self.output = artifact / "external-provider"
        self.output.mkdir(mode=0o700)
        self.relay = model_relay.Relay(Path(self.directory.name) / "provider.sock", self.output,
                                       result["external_provider"]["model"], max_requests=8,
                                       disconnect_response=2 if disconnect else None)
        result["external_provider"]["disconnect_completed_response"] = 2 if disconnect else None
        os.chmod(Path(self.directory.name) / "provider.sock", 0o666)
        self.thread = threading.Thread(target=self.relay.serve_forever, daemon=True)
        self.thread.start()
        self.result = result

    def close(self):
        self.relay.shutdown()
        self.thread.join(timeout=2)
        self.relay.server_close()
        self.result["external_provider"]["requests"] = list(self.relay.records)
        self.directory.cleanup()

    def require_requests(self):
        if len(self.relay.records) < 2 or not all(row["passed"] for row in self.relay.records):
            raise ValueError("actual external inference is missing or failed; retain every attempt")
        records = sorted(self.relay.records, key=lambda row: row["number"])
        dropped = [row for row in records if row.get("injected_disconnect")]
        if not self.disconnect:
            if dropped or not all(row["delivered"] for row in records):
                raise ValueError("a provider response was lost outside the declared fault case")
            return
        if (len(records) < 3 or len(dropped) != 1 or dropped[0]["number"] != 2 or dropped[0]["delivered"] or
                any(not row["delivered"] for row in records if row["number"] != 2) or
                records[1]["request_sha256"] != records[2]["request_sha256"] or
                records[1]["session"] != records[2]["session"]):
            raise ValueError("the completed-response fault lacks its exact client inference replay")
        completed = json.loads((self.artifact / "prepare-output/opencode-result-source.json").read_text())["result"]
        request = json.loads((self.output / "02.request.json").read_text())
        actual_tool_results = json.dumps([item.get("content") for item in request["messages"] if item["role"] == "tool"])
        if completed["nonce"] not in actual_tool_results or completed["ram_sha256"] not in actual_tool_results:
            raise ValueError("the lost response was not to this actual completed shell result")
        self.result["external_provider"]["exact_client_inference_replay_verified"] = True
        self.result["external_provider"]["actual_completed_tool_result_in_lost_exchange"] = True
