import io
import json
import subprocess
import tarfile
import unittest
from unittest.mock import patch

import worker_readiness


class PublishedReadiness(unittest.TestCase):
    def record(self, value, *, kind=tarfile.REGTYPE, count=1):
        output = io.BytesIO()
        with tarfile.open(fileobj=output, mode="w") as archive:
            for index in range(count):
                raw = json.dumps(value).encode()
                member = tarfile.TarInfo("worker.json"+str(index))
                member.type, member.size = kind, len(raw)
                archive.addfile(member, io.BytesIO(raw))
        return subprocess.CompletedProcess([], 0, output.getvalue(), b"")

    @patch("worker_readiness.subprocess.run")
    def test_archive_hint_never_executes_in_starting_worker(self, run):
        run.return_value = self.record({"schema": 1, "store_id": "owned", "ready_at": "now"})
        self.assertTrue(worker_readiness.published(["docker"], "exact", "owned"))
        self.assertEqual(run.call_args.args[0], ["docker", "cp", "exact:/run/factorybox/worker.json", "-"])

    @patch("worker_readiness.subprocess.run")
    def test_missing_record_has_no_readiness(self, run):
        run.return_value = subprocess.CompletedProcess([], 1, b"", b"not found")
        self.assertFalse(worker_readiness.published(["docker"], "exact", "owned"))

    @patch("worker_readiness.subprocess.run")
    def test_foreign_or_ambiguous_record_cannot_authorize_exec(self, run):
        for reply in [self.record({"schema": 1, "store_id": "foreign", "ready_at": "now"}),
                      self.record({}, kind=tarfile.SYMTYPE), self.record({}, count=2)]:
            run.return_value = reply
            with self.assertRaises(ValueError):
                worker_readiness.published(["docker"], "exact", "owned")

    @patch("worker_readiness.subprocess.run")
    def test_truncated_publication_has_no_readiness(self, run):
        run.return_value = subprocess.CompletedProcess([], 0, b"partial", b"")
        self.assertFalse(worker_readiness.published(["docker"], "exact", "owned"))
