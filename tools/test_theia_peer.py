import json
from pathlib import Path
import signal
import subprocess
import sys
import tempfile
import time
import unittest
from unittest import mock

import devx_lifecycle
import theia_peer


class TheiaPeerTests(unittest.TestCase):
    def test_outer_deadline_escapes_component_cleanup_that_handles_ordinary_command_errors(self):
        class CatchingPeer:
            started = True
            closed = False

            def close(self, failed):
                try:
                    time.sleep(5)
                except Exception:
                    time.sleep(5)
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory)
            (output / "theia-lifecycle-complete").write_text("0\n")
            started = time.monotonic()
            with self.assertRaises(subprocess.TimeoutExpired):
                devx_lifecycle.run_with_theia(
                    [sys.executable, "-B", "-S", "-c", "import time;time.sleep(30)"],
                    subprocess.DEVNULL, subprocess.DEVNULL, CatchingPeer(), output, timeout=0.2)
            self.assertLess(time.monotonic() - started, 2)

    def test_ide_is_released_before_the_worker_finishes_its_cleanup(self):
        class FinishedPeer:
            started = True
            closed = False
            result = {"passed": True, "cleanup": True}

            def close(self, failed):
                if failed:
                    raise AssertionError("completed IDE operations were treated as a failure")
                self.closed = True
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory)
            (output / "theia-operations-complete").write_text("done\n")
            script = """
from pathlib import Path
import sys,time
root=Path(sys.argv[1])
while not (root/'theia-released').exists():time.sleep(.01)
if (root/'theia-released').read_text().strip()!='ok':raise SystemExit(3)
(root/'theia-lifecycle-complete').write_text('0\\n')
while not (root/'theia-host-finished').exists():time.sleep(.01)
"""
            peer = FinishedPeer()
            completed = devx_lifecycle.run_with_theia(
                [sys.executable, "-B", "-S", "-c", script, str(output)],
                subprocess.DEVNULL, subprocess.DEVNULL, peer, output, timeout=2)
            self.assertEqual(completed.returncode, 0)
            self.assertTrue(peer.closed)

    def test_outer_deadline_interrupts_a_blocked_peer_start_and_reaps_its_client(self):
        class BlockedPeer:
            started = closed = False

            def start(self, request):
                time.sleep(5)
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory)
            (output / "theia-request.json").write_text("{}")
            children = []
            original = subprocess.Popen
            def launch(*args, **kwargs):
                child = original(*args, **kwargs)
                children.append(child)
                return child
            handler = signal.getsignal(signal.SIGALRM)
            started = time.monotonic()
            with mock.patch.object(devx_lifecycle.subprocess, "Popen", side_effect=launch):
                with self.assertRaises(subprocess.TimeoutExpired):
                    devx_lifecycle.run_with_theia(
                        [sys.executable, "-c", "import time;time.sleep(30)"],
                        subprocess.DEVNULL, subprocess.DEVNULL, BlockedPeer(), output, timeout=0.2)
            self.assertLess(time.monotonic() - started, 2)
            self.assertEqual(len(children), 1)
            self.assertIsNotNone(children[0].poll())
            self.assertEqual(signal.getsignal(signal.SIGALRM), handler)
            self.assertEqual(signal.getitimer(signal.ITIMER_REAL), (0.0, 0.0))

    def test_foreign_worker_is_rejected_before_any_volume_or_peer_create(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            owner = "a" * 32
            worker = "factorybox-devx-lifecycle-" + owner
            peer = theia_peer.Peer(worker, owner, {}, root, root, "unused")
            observed = {"Name": "/" + worker, "State": {"Running": True},
                        "Config": {"Labels": {"com.factorybox.run": "foreign"}}}
            with mock.patch.object(theia_peer.reference, "docker",
                                   return_value=json.dumps([observed])) as docker:
                with self.assertRaisesRegex(ValueError, "original running owned worker"):
                    peer.start({"version": 1, "run_owner": owner, "uid": 1000, "gid": 1000})
            self.assertEqual(docker.call_args_list, [
                mock.call(["container", "inspect", worker])])
            self.assertFalse(peer.volume_created)
            self.assertFalse(peer.attempted)
