#!/usr/bin/env python3
"""Bounded synthetic tool-protocol probe against an explicitly free OpenCode model.

This does not qualify the pinned OpenCode consumer or any lifecycle operation.
No credentials, paid fallback, redirects or inference retries are used.
"""
import argparse
import datetime
import hashlib
from html.parser import HTMLParser
import json
import shutil
import time
import urllib.error
import urllib.request
import uuid

import reference
import reference_lifecycle


CATALOG = "https://opencode.ai/zen/v1/models"
PRICES = "https://opencode.ai/v2/docs/console/models/"
COMPLETIONS = "https://opencode.ai/zen/v1/chat/completions"
DOCUMENTED_COMPLETIONS = "https://opencode.ai/inference/openai/v1/chat/completions"
LIMIT = 2 << 20


class Tables(HTMLParser):
    def __init__(self):
        super().__init__(convert_charrefs=True)
        self.rows = []
        self.row = None
        self.cell = None

    def handle_starttag(self, tag, attrs):
        if tag == "tr":
            self.row = []
        if tag in ("td", "th") and self.row is not None:
            self.cell = []

    def handle_data(self, data):
        if self.cell is not None:
            self.cell.append(data)

    def handle_endtag(self, tag):
        if tag in ("td", "th") and self.cell is not None:
            self.row.append(" ".join("".join(self.cell).split()))
            self.cell = None
        if tag == "tr" and self.row is not None:
            self.rows.append(self.row)
            self.row = None


def approve_free_model(model, catalog, prices):
    """Require matching current identity and three explicit Free price columns."""
    if not model or not model.endswith("-free"):
        raise ValueError("an explicit free model ID is required")
    if sum(row.get("id") == model for row in catalog["data"]) != 1:
        raise ValueError("free model must occur exactly once in the live catalog")
    tables = Tables()
    tables.feed(prices)
    identities = [row for row in tables.rows if len(row) == 4 and row[1] == model]
    if len(identities) != 1 or identities[0][2:] != [DOCUMENTED_COMPLETIONS, "@ai-sdk/openai-compatible"]:
        raise ValueError("model identity or documented protocol is ambiguous")
    label = identities[0][0]
    costs = [row for row in tables.rows if len(row) == 5 and row[0] == label]
    if len(costs) != 1 or costs[0][1:4] != ["Free", "Free", "Free"] or costs[0][4] != "-":
        raise ValueError("model has no unambiguous explicit free pricing")
    return {"model": model, "identity_row": identities[0], "pricing_row": costs[0]}


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        raise ValueError("redirect refused; review the provider endpoint explicitly")


def exchange(url, payload=None):
    if url not in (CATALOG, PRICES, COMPLETIONS):
        raise ValueError("unapproved provider URL")
    if (url == COMPLETIONS) != (payload is not None):
        raise ValueError("inference requires a request body; discovery must be read-only")
    request = urllib.request.Request(url, data=None if payload is None else json.dumps(payload).encode(),
                                     headers={"Content-Type": "application/json", "User-Agent": "factorybox-free-model-probe/1"})
    started = time.monotonic()
    try:
        response = urllib.request.build_opener(NoRedirect).open(request, timeout=30)
    except urllib.error.HTTPError as error:
        response = error
    with response:
        body = response.read(LIMIT + 1)
        if len(body) > LIMIT:
            raise ValueError("provider response exceeds the evidence bound")
        return body, {"url": url, "method": request.get_method(), "status": response.code,
                      "elapsed_seconds": time.monotonic() - started, "bytes": len(body),
                      "sha256": hashlib.sha256(body).hexdigest()}


def tool_request(model, nonce):
    return {"model": model, "max_tokens": 512, "stream": False,
            "messages": [{"role": "user", "content": "Call record_probe exactly once with nonce " + nonce +
                          ". After its result, reply with the returned receipt exactly. This is a synthetic protocol test."}],
            "tools": [{"type": "function", "function": {"name": "record_probe", "description": "Record a synthetic nonce and return a receipt.",
                       "parameters": {"type": "object", "properties": {"nonce": {"type": "string"}},
                                      "required": ["nonce"], "additionalProperties": False}}}],
            "tool_choice": {"type": "function", "function": {"name": "record_probe"}}}


def tool_call(reply, nonce):
    choices = reply.get("choices", [])
    if len(choices) != 1 or choices[0].get("finish_reason") != "tool_calls":
        raise ValueError("provider did not complete one tool-call choice")
    message = choices[0]["message"]
    calls = message.get("tool_calls", [])
    if len(calls) != 1 or not calls[0].get("id") or calls[0].get("type") != "function":
        raise ValueError("provider did not request exactly one identifiable function")
    function = calls[0]["function"]
    if function.get("name") != "record_probe" or json.loads(function["arguments"]) != {"nonce": nonce}:
        raise ValueError("provider tool arguments differ from the synthetic nonce")
    return message, calls[0]["id"]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--model", default="glm-5.3-flash-free")
    options = parser.parse_args()
    artifact = reference.new_artifact("factorybox-free-model-probe-" + uuid.uuid4().hex)
    artifact.mkdir()
    shutil.copyfile(__file__, artifact / "runner.py")
    result = {"schema": 1, "started_at": datetime.datetime.now(datetime.timezone.utc).isoformat(),
              "model": options.model, "passed": False, "inference_requests": 0,
              "credentials_used": False, "inference_retries": 0, "production_qualified": False,
              "opencode_qualified": False, "lifecycle_qualified": False, "exchanges": []}
    def recorded(name, url, payload=None):
        if payload is not None:
            reference_lifecycle.write_json(artifact / (name + ".request.json"), payload)
            result["inference_requests"] += 1
        body, metadata = exchange(url, payload)
        (artifact / (name + ".body")).write_bytes(body)
        result["exchanges"].append(metadata)
        if metadata["status"] != 200:
            if metadata["status"] in (401, 403):
                result["access_required"] = True
                try:
                    result["opencode_client_required"] = json.loads(body).get("error", {}).get("type") == "FreeTierError"
                except (ValueError, AttributeError):
                    pass
            raise ValueError("provider returned HTTP " + str(metadata["status"]) + "; no retry or fallback")
        return body
    try:
        catalog = json.loads(recorded("catalog", CATALOG))
        prices = recorded("pricing", PRICES).decode()
        result["free_model_evidence"] = approve_free_model(options.model, catalog, prices)
        nonce = uuid.uuid4().hex
        request = tool_request(options.model, nonce)
        first = json.loads(recorded("tool", COMPLETIONS, request))
        message, call_id = tool_call(first, nonce)
        receipt = "FACTORYBOX_RECEIPT_" + uuid.uuid4().hex
        followup = dict(request, tool_choice="none", messages=[*request["messages"], message,
                        {"role": "tool", "tool_call_id": call_id, "content": json.dumps({"nonce": nonce, "receipt": receipt, "exit": 37})}])
        second = json.loads(recorded("receipt", COMPLETIONS, followup))
        choices = second.get("choices", [])
        if len(choices) != 1 or choices[0].get("finish_reason") != "stop" or choices[0]["message"].get("tool_calls") or choices[0]["message"].get("content", "").strip() != receipt:
            raise ValueError("provider did not acknowledge the actual synthetic tool receipt")
        result["passed"] = True
    except Exception as error:
        result["error_type"] = type(error).__name__
        result["error"] = str(error)
    finally:
        reference_lifecycle.write_json(artifact / "result.json", result)
        print(json.dumps({"evidence": str(artifact), "passed": result["passed"], "access_required": result.get("access_required", False),
                          "inference_requests": result["inference_requests"], "error": result.get("error")}), flush=True)
    return 0 if result["passed"] else 1


if __name__ == "__main__":
    raise SystemExit(main())
