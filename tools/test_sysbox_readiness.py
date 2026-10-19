import json
import os
from pathlib import Path
import socket
import subprocess
import tempfile
import threading
import time
import unittest

HELPER = Path(__file__).resolve().parents[1] / "tests/wait-sysbox.sh"


class SysboxReadinessTests(unittest.TestCase):
    def test_waits_for_both_real_unix_sockets_and_records_elapsed_time(self):
        with tempfile.TemporaryDirectory(prefix="fbx-ready-", dir="/tmp") as directory:
            root = Path(directory)
            sockets = []
            def start():
                time.sleep(0.2)
                for name in ["sysmgr.sock", "sysfs-seccomp.sock"]:
                    server = socket.socket(socket.AF_UNIX)
                    server.bind(str(root / name))
                    server.listen(1)
                    sockets.append(server)
            thread = threading.Thread(target=start)
            thread.start()
            try:
                result = subprocess.run(
                    ["bash", str(HELPER), str(os.getpid()), str(os.getpid()),
                     str(root), str(root), "3"],
                    capture_output=True, text=True, timeout=5)
                self.assertEqual(result.returncode, 0, result.stderr)
                record = json.loads((root / "sysbox-startup.json").read_text())
                self.assertTrue(record["passed"])
                self.assertTrue(record["manager_socket"])
                self.assertTrue(record["filesystem_socket"])
            finally:
                thread.join(timeout=5)
                for server in sockets:
                    server.close()

    def test_missing_filesystem_socket_fails_with_bounded_actionable_evidence(self):
        with tempfile.TemporaryDirectory(prefix="fbx-ready-", dir="/tmp") as directory:
            root = Path(directory)
            with socket.socket(socket.AF_UNIX) as server:
                server.bind(str(root / "sysmgr.sock"))
                server.listen(1)
                result = subprocess.run(
                    ["bash", str(HELPER), str(os.getpid()), str(os.getpid()),
                     str(root), str(root), "1"],
                    capture_output=True, text=True, timeout=4)
            self.assertEqual(result.returncode, 1)
            self.assertIn("not ready within 1 seconds", result.stderr)
            record = json.loads((root / "sysbox-startup.json").read_text())
            self.assertFalse(record["passed"])
            self.assertTrue(record["manager_socket"])
            self.assertFalse(record["filesystem_socket"])
            self.assertTrue((root / "sysbox-startup-processes.txt").is_file())
