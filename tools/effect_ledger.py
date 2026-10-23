"""Synthetic outside-world effect oracle, owned by the host test controller.

Every apply appends an effect, including repeated operation IDs. There is no
deduplication or retry. The durable commit precedes the deliberate lost reply.
Only the Unix socket is exposed to a disposable namespace gateway; the ledger
and its process must remain outside all captured environments.
"""
import hashlib
import http.server
import json
import os
from pathlib import Path
import re
import socket
import socketserver
import threading


TOKEN = re.compile(r"[0-9a-f]{64}")
APPLY = "/fixture/effects/apply"
STATUS = "/fixture/effects/status"


def digest(value):
    return hashlib.sha256(json.dumps(value, sort_keys=True, separators=(",", ":")).encode()).hexdigest()


class Ledger(socketserver.ThreadingMixIn, socketserver.UnixStreamServer):
    daemon_threads = True

    def __init__(self, address, output, *, lose_reply=True, max_effects=4, max_queries=16):
        self.output = Path(output)
        self.output.mkdir(mode=0o700)
        self.path = self.output / "effects.jsonl"
        descriptor = os.open(self.path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
        try:
            os.fsync(descriptor)
        finally:
            os.close(descriptor)
        directory = os.open(self.output, os.O_RDONLY | os.O_DIRECTORY)
        try:
            os.fsync(directory)
        finally:
            os.close(directory)
        self.lose_reply, self.max_effects, self.max_queries = lose_reply, max_effects, max_queries
        self.lock = threading.Lock()
        self.records, self.queries = [], 0
        super().__init__(str(address), Handler)

    def _events(self):
        if self.path.is_symlink() or self.path.stat().st_size > 65536:
            raise ValueError("outside effect ledger is not its bounded file")
        events, previous = [], "0" * 64
        for line in self.path.read_text().splitlines():
            event = json.loads(line)
            recorded = event.pop("sha256")
            if (set(event) != {"number", "operation", "previous_sha256"} or
                    event["number"] != len(events) + 1 or not TOKEN.fullmatch(event["operation"]) or
                    event["previous_sha256"] != previous or digest(event) != recorded):
                raise ValueError("outside effect ledger changed")
            previous = recorded
            events.append({**event, "sha256": recorded})
        if len(events) > self.max_effects:
            raise ValueError("outside effect bound exceeded")
        return events

    def apply(self, operation):
        with self.lock:
            events = self._events()
            if len(events) >= self.max_effects:
                raise ValueError("outside effect allowance exhausted")
            event = {"number": len(events) + 1, "operation": operation,
                     "previous_sha256": events[-1]["sha256"] if events else "0" * 64}
            event["sha256"] = digest(event)
            data = (json.dumps(event, sort_keys=True) + "\n").encode()
            descriptor = os.open(self.path, os.O_WRONLY | os.O_APPEND | os.O_NOFOLLOW)
            try:
                if os.write(descriptor, data) != len(data):
                    raise ValueError("incomplete outside effect commit")
                os.fsync(descriptor)
            finally:
                os.close(descriptor)
            return event

    def status(self, operation):
        with self.lock:
            events = [event for event in self._events() if event["operation"] == operation]
            return {"schema": 1, "operation": operation, "applications": len(events),
                    "last_effect_sha256": events[-1]["sha256"] if events else None,
                    "observer_pid": os.getpid()}

    def record(self, value):
        with self.lock:
            self.records.append(value)
            with (self.output / "requests.jsonl").open("a") as output:
                output.write(json.dumps(value, sort_keys=True) + "\n")


class Handler(http.server.BaseHTTPRequestHandler):
    def setup(self):
        super().setup()
        self.connection.settimeout(3)

    def log_message(self, *args):
        pass

    def reject(self):
        self.send_error(400, "bounded synthetic effect request refused")

    def do_GET(self):
        self.reject()

    def do_POST(self):
        record = {"path": self.path, "committed": False, "delivered": False}
        try:
            size = int(self.headers.get("Content-Length", "0"))
            if self.path not in {APPLY, STATUS} or self.headers.get("Transfer-Encoding") or not 0 < size <= 1024:
                raise ValueError("requires one bounded synthetic effect operation")
            body = self.rfile.read(size)
            request = json.loads(body)
            if len(body) != size or set(request) != {"operation"} or not isinstance(request["operation"], str) or not TOKEN.fullmatch(request["operation"]):
                raise ValueError("requires an exact synthetic operation ID")
            operation = record["operation"] = request["operation"]
            if self.path == APPLY:
                event = self.server.apply(operation)
                record.update(committed=True, effect_sha256=event["sha256"])
                if self.server.lose_reply:
                    record["injected_lost_reply"] = True
                    self.close_connection = True
                    self.connection.shutdown(socket.SHUT_RDWR)
                    return
            else:
                with self.server.lock:
                    self.server.queries += 1
                    if self.server.queries > self.server.max_queries:
                        raise ValueError("outside status allowance exhausted")
            result = self.server.status(operation)
            data = json.dumps(result).encode()
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(data)))
            self.send_header("Connection", "close")
            self.end_headers()
            self.wfile.write(data)
            record["delivered"] = True
        except Exception as error:
            record["error"] = type(error).__name__ + ": " + str(error)
            try:
                self.reject()
            except (BrokenPipeError, ConnectionResetError):
                pass
        finally:
            self.server.record(record)
