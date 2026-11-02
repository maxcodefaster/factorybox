import copy
import json
import unittest
from unittest.mock import Mock

import failed_resume_delete


class FailedResumeStopTests(unittest.TestCase):
    def test_native_proof_rejects_unknown_or_live_restore_before_any_write(self):
        original = {"record": {"phase": "resuming", "pause": {"resume": {
            "attempted": True, "error": "original failed restore"}}},
            "runtime": {"status": "stopped", "pid": 0}}
        changes = [
            ("phase", "running"), ("attempted", False), ("error", ""),
            ("completed_at", "completed"), ("status", "running"), ("pid", 42),
        ]
        for field, value in changes:
            changed = copy.deepcopy(original)
            target = (changed["runtime"] if field in {"status", "pid"} else
                      changed["record"] if field == "phase" else changed["record"]["pause"]["resume"])
            target[field] = value
            run = Mock(return_value=json.dumps(changed))
            with self.subTest(field=field), self.assertRaisesRegex(ValueError, "genuinely attempted"):
                failed_resume_delete.settle(run, ["owned", "cli"], "source", "owned-worker")
            run.assert_called_once_with(["owned", "cli", "state", "source"])
