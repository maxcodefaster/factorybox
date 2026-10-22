"""Optional consumer-test egress through an owned Unix socket, never runtime state.

Forward only a currently approved free model and the real OpenCode public-client
headers. Do not invent client identity, accept credentials or retry inference.
"""
import hashlib
import http.server
import json
from pathlib import Path
import socketserver
import threading
import time
import urllib.error
import urllib.request

import free_model_probe


class Relay(socketserver.ThreadingMixIn, socketserver.UnixStreamServer):
    daemon_threads = True

    def __init__(self, socket, output, model, max_requests=8):
        self.output = Path(output)
        self.model = model
        self.remaining = max_requests
        self.records = []
        self.lock = threading.Lock()
        super().__init__(str(socket), Handler)

    def record(self, value):
        with self.lock:
            self.records.append(value)
            (self.output / "relay.json").write_text(json.dumps(self.records, indent=2) + "\n")


def admit(path, headers, body, model):
    if path != "/v1/chat/completions":
        raise ValueError("only the selected inference path is admitted")
    if headers.get("authorization", "") not in ("", "Bearer public"):
        raise ValueError("this synthetic probe does not accept provider credentials")
    if not headers.get("user-agent", "").lower().startswith("opencode/") or not headers.get("x-opencode-session") or not headers.get("x-opencode-client"):
        raise ValueError("actual OpenCode client/session provenance is required")
    payload = json.loads(body)
    if payload.get("model") != model:
        raise ValueError("inference model differs from the explicitly free input")
    if not payload.get("messages") or not isinstance(payload["messages"], list):
        raise ValueError("missing synthetic messages")
    return payload


class Handler(http.server.BaseHTTPRequestHandler):
    def setup(self):
        super().setup()
        self.connection.settimeout(5)

    def log_message(self, *args):
        pass

    def reject(self, message, code=400):
        body = json.dumps({"error": {"message": message}}).encode()
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_POST(self):
        record = {"started": time.time(), "passed": False, "inference_retry": False}
        try:
            size = int(self.headers.get("Content-Length", "0"))
            if self.headers.get("Transfer-Encoding") or not 0 < size <= free_model_probe.LIMIT:
                raise ValueError("an explicitly bounded request is required")
            body = self.rfile.read(size)
            if len(body) != size:
                raise ValueError("incomplete inference request")
            headers = {key.lower(): value for key, value in self.headers.items()}
            payload = admit(self.path, headers, body, self.server.model)
            with self.server.lock:
                if self.server.remaining <= 0:
                    raise ValueError("bounded inference request allowance exhausted")
                self.server.remaining -= 1
                number = len(list(self.server.output.glob("*.request.json"))) + 1
                (self.server.output / f"{number:02}.request.json").write_bytes(body)
            record.update(model=payload["model"], number=number, request_sha256=hashlib.sha256(body).hexdigest(),
                          client=headers["user-agent"], session=headers["x-opencode-session"],
                          stream=payload.get("stream", False))
            forwarded = {key: value for key, value in headers.items() if key in {
                "authorization", "content-type", "user-agent", "x-opencode-session-id", "x-opencode-parent-session-id",
                "x-session-affinity", "x-session-id", "x-parent-session-id", "x-opencode-project", "x-opencode-session", "x-opencode-client"}}
            request = urllib.request.Request(free_model_probe.COMPLETIONS, data=body, headers=forwarded)
            started = time.monotonic()
            try:
                upstream = urllib.request.build_opener(free_model_probe.NoRedirect).open(request, timeout=45)
            except urllib.error.HTTPError as error:
                upstream = error
            with upstream:
                response = upstream.read(4 * free_model_probe.LIMIT + 1)
                if len(response) > 4 * free_model_probe.LIMIT:
                    raise ValueError("bounded provider response exceeded")
                (self.server.output / f"{number:02}.response").write_bytes(response)
                record.update(status=upstream.code, seconds=time.monotonic() - started, response_bytes=len(response),
                              response_sha256=hashlib.sha256(response).hexdigest(), passed=upstream.code == 200)
                self.send_response(upstream.code)
                self.send_header("Content-Type", upstream.headers.get("Content-Type", "application/json"))
                self.send_header("Content-Length", str(len(response)))
                self.end_headers()
                self.wfile.write(response)
        except Exception as error:
            record["passed"] = False
            record["error_type"] = type(error).__name__
            record["error"] = str(error)
            self.reject("bounded inference relay refused or failed", 502)
        finally:
            self.server.record(record)

    def do_GET(self):
        self.reject("discovery is performed and frozen before starting OpenCode")
