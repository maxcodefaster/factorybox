import http.client
from contextlib import closing
import io
import json
from pathlib import Path
import socket
import tempfile
import threading
import time
import unittest
from unittest.mock import Mock, patch
import uuid

import model_relay


class UnixHTTP(http.client.HTTPConnection):
    def __init__(self, address):
        super().__init__("synthetic.invalid", timeout=3)
        self.address = address

    def connect(self):
        self.sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        self.sock.settimeout(self.timeout)
        self.sock.connect(self.address)


class ExternalRelayDisconnect(unittest.TestCase):
    @patch("model_relay.urllib.request.build_opener")
    def test_one_actual_http_response_is_lost_after_upstream_completion_without_relay_retry(self, build_opener):
        def response(*args, **kwargs):
            value = io.BytesIO(b'{"synthetic":"remote-reply"}')
            value.code = 200
            value.headers = {"Content-Type": "application/json"}
            return value

        opener = Mock()
        opener.open.side_effect = response
        build_opener.return_value = opener
        address = "\0factorybox-relay-disconnect-" + uuid.uuid4().hex
        headers = {"Authorization": "Bearer public", "User-Agent": "opencode/synthetic-test",
                   "x-opencode-session": "synthetic-session", "x-opencode-client": "synthetic-client",
                   "Content-Type": "application/json"}
        body = json.dumps({"model": "synthetic-free", "messages": [{"role": "tool", "content": "synthetic completed result"}]}).encode()
        with tempfile.TemporaryDirectory() as directory, model_relay.Relay(
                address, Path(directory), "synthetic-free", disconnect_response=2) as relay:
            thread = threading.Thread(target=relay.serve_forever, daemon=True)
            thread.start()
            try:
                for number in range(1, 4):
                    with closing(UnixHTTP(address)) as client:
                        client.request("POST", "/v1/chat/completions", body, headers)
                        if number == 2:
                            with self.assertRaises(http.client.RemoteDisconnected):
                                client.getresponse()
                        else:
                            actual = client.getresponse()
                            self.assertEqual(actual.status, 200)
                            self.assertEqual(actual.read(), b'{"synthetic":"remote-reply"}')
                deadline = time.monotonic() + 1
                while len(relay.records) != 3 and time.monotonic() < deadline:
                    time.sleep(.01)
                records = sorted(relay.records, key=lambda row: row["number"])
                self.assertEqual(opener.open.call_count, 3)
                self.assertEqual([row["delivered"] for row in records], [True, False, True])
                self.assertEqual([row.get("injected_disconnect", False) for row in records], [False, True, False])
                self.assertTrue(all(row["passed"] and row["status"] == 200 and not row["inference_retry"] for row in records))
                self.assertEqual(len({row["request_sha256"] for row in records}), 1)
                self.assertEqual(len(list(Path(directory).glob("*.response"))), 3)
            finally:
                relay.shutdown()
                thread.join(timeout=2)


if __name__ == "__main__":
    unittest.main()
