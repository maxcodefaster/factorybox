from contextlib import closing
import http.client
import json
from pathlib import Path
import socket
import tempfile
import threading
import time
import unittest

import effect_ledger


class UnixHTTP(http.client.HTTPConnection):
    def __init__(self, address):
        super().__init__("synthetic-effect.invalid", timeout=3)
        self.address = address

    def connect(self):
        self.sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        self.sock.settimeout(self.timeout)
        self.sock.connect(self.address)


class OutsideEffectLedgerTests(unittest.TestCase):
    def test_lost_reply_preserves_effect_and_repeated_apply_is_detectable(self):
        socket_directory = tempfile.TemporaryDirectory(prefix="fbx-", dir="/tmp")
        self.addCleanup(socket_directory.cleanup)
        address = str(Path(socket_directory.name) / "effect.sock")
        operation = "a" * 64
        with tempfile.TemporaryDirectory() as directory, effect_ledger.Ledger(address, Path(directory) / "outside") as ledger:
            thread = threading.Thread(target=ledger.serve_forever, daemon=True)
            thread.start()
            try:
                for expected in [0, 1, 2]:
                    with closing(UnixHTTP(address)) as client:
                        client.request("POST", effect_ledger.STATUS, json.dumps({"operation": operation}))
                        response = client.getresponse()
                        self.assertEqual(response.status, 200)
                        actual = json.loads(response.read())
                        self.assertEqual(actual["applications"], expected)
                    if expected == 2:
                        break
                    with closing(UnixHTTP(address)) as client:
                        client.request("POST", effect_ledger.APPLY, json.dumps({"operation": operation}))
                        with self.assertRaises(http.client.RemoteDisconnected):
                            client.getresponse()
                    # The request lost its reply, but this independent disk
                    # reader already sees its committed effect. No dedup/retry.
                    events = [json.loads(line) for line in ledger.path.read_text().splitlines()]
                    self.assertEqual(len(events), expected + 1)
                    self.assertEqual(events[-1]["operation"], operation)
                deadline = time.monotonic() + 1
                while len(ledger.records) != 5 and time.monotonic() < deadline:
                    time.sleep(.01)
                applied = [row for row in ledger.records if row["path"] == effect_ledger.APPLY]
                self.assertEqual(len(applied), 2)
                self.assertTrue(all(row["committed"] and row["injected_lost_reply"] and not row["delivered"] for row in applied))
                ledger.path.write_text(ledger.path.read_text().replace('"number": 1', '"number": 9'))
                with self.assertRaisesRegex(ValueError, "ledger changed"):
                    ledger.status(operation)
            finally:
                ledger.shutdown()
                thread.join(timeout=2)

    def test_invalid_route_or_identity_cannot_commit_an_effect(self):
        socket_directory = tempfile.TemporaryDirectory(prefix="fbx-", dir="/tmp")
        self.addCleanup(socket_directory.cleanup)
        address = str(Path(socket_directory.name) / "effect.sock")
        with tempfile.TemporaryDirectory() as directory, effect_ledger.Ledger(address, Path(directory) / "outside") as ledger:
            thread = threading.Thread(target=ledger.serve_forever, daemon=True)
            thread.start()
            try:
                for path, body in [("/v1/chat/completions", {"operation": "a" * 64}),
                                   (effect_ledger.APPLY, {"operation": "invalid"}),
                                   (effect_ledger.APPLY, {"operation": "a" * 64, "other": True})]:
                    with closing(UnixHTTP(address)) as client:
                        client.request("POST", path, json.dumps(body))
                        response = client.getresponse()
                        self.assertEqual(response.status, 400)
                        response.read()
                self.assertEqual(ledger.status("a" * 64)["applications"], 0)
                self.assertEqual(ledger.path.read_bytes(), b"")
            finally:
                ledger.shutdown()
                thread.join(timeout=2)
