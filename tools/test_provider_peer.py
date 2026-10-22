import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import Mock, patch

import provider_peer


class OwnedConsumerProviderPeer(unittest.TestCase):
    def fixture(self, directory):
        root = Path(directory)
        (root / "archive").mkdir()
        (root / "output").mkdir()
        peer = provider_peer.Peer("owned-worker", "container-id", "a" * 32, root / "archive", root / "output", "c" * 64)
        request = {"schema": 1, "nonce": "d" * 32, "phase": "attach", "operation": "arm",
                   "target": {"run_owner": "a" * 32, "sandbox": "source", "owner": "b" * 32,
                              "runtime_id": "owned-runtime", "pid": 123, "runtime_created": "synthetic"}}
        return peer, request

    def publish(self, peer, request, mode=0o600):
        path = peer.output / ("provider-request-" + request["nonce"] + "-" + request["phase"] + ".json")
        path.write_text(json.dumps(request))
        path.chmod(mode)
        return path

    @patch("provider_peer.reference.docker")
    def test_foreign_run_cannot_start_or_inspect_a_gateway(self, docker):
        with tempfile.TemporaryDirectory() as directory:
            peer, request = self.fixture(directory)
            request["target"]["run_owner"] = "e" * 32
            with self.assertRaises(ValueError):
                peer.target(request)
            docker.assert_not_called()

    def test_completed_intent_is_not_reissued_or_silently_rewritten(self):
        with tempfile.TemporaryDirectory() as directory:
            peer, request = self.fixture(directory)
            path = self.publish(peer, request)
            peer.attach = Mock(return_value={"gateway_pid": 456, "gateway_birth": "original", "gateway_sha256": "c" * 64, "gateway_network": "net:[unit]"})
            peer.pump()
            reply = peer.output / ("provider-reply-" + request["nonce"] + "-attach.json")
            self.assertTrue(json.loads(reply.read_text())["accepted"])
            self.assertEqual(reply.stat().st_mode & 0o777, 0o600)
            peer.pump()
            self.assertEqual(peer.attach.call_count, 1)
            request["target"]["pid"] = 789
            path.write_text(json.dumps(request))
            with self.assertRaises(ValueError):
                peer.pump()
            self.assertEqual(peer.attach.call_count, 1)

    def test_nonprivate_or_symlinked_request_cannot_act(self):
        with tempfile.TemporaryDirectory() as directory:
            peer, request = self.fixture(directory)
            path = self.publish(peer, request, 0o644)
            peer.attach = Mock()
            with self.assertRaises(ValueError):
                peer.pump()
            peer.attach.assert_not_called()
            original = path.with_name("original.json")
            path.rename(original)
            path.symlink_to(original)
            with self.assertRaises(OSError):
                peer.pump()
            peer.attach.assert_not_called()

    def test_stop_command_binds_the_already_attached_birth_before_signalling(self):
        with tempfile.TemporaryDirectory() as directory:
            peer, request = self.fixture(directory)
            peer.entries[request["nonce"]] = {"record": {"identity": {"pid": 456, "birth": "original"}}}
            command = peer.command("stop", request["nonce"], 123)
            self.assertEqual(command[-4:], ["--expected-pid", "456", "--expected-birth", "original"])


if __name__ == "__main__":
    unittest.main()
