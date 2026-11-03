import copy
import unittest

import native_filesystem_lifecycle


class NativeFilesystemLifecycleProof(unittest.TestCase):
    def test_native_ram_useful_work_and_exact_delete_are_required(self):
        proof = {
            "passed": True, "same_private_ram": True, "useful_after_resume": True, "native_roots": 8,
            "owner": "original-owner",
            "child_owner": "child-owner", "independent_fork_ram": True,
            "child_resume_preserves_work": True, "child_useful_after_source_delete": True,
            "pause": {"after": {"anon": 0}, "snapshot": {"resident_bytes_after": 0}},
            "child_pause": {"after": {"anon": 0}, "snapshot": {"resident_bytes_after": 0}},
        }
        deleted = {"record": {"phase": "deleted", "owner": "original-owner",
                              "delete": {"completed_at": "completed"}}, "runtime": None}
        child_deleted = copy.deepcopy(deleted)
        child_deleted["record"]["owner"] = "child-owner"
        native_filesystem_lifecycle.require_complete(proof, deleted, child_deleted)
        for field in ["passed", "same_private_ram", "useful_after_resume", "independent_fork_ram",
                      "child_resume_preserves_work", "child_useful_after_source_delete"]:
            changed = copy.deepcopy(proof)
            changed[field] = False
            with self.subTest(field=field), self.assertRaises(ValueError):
                native_filesystem_lifecycle.require_complete(changed, deleted, child_deleted)
        for changed in [
                {**proof, "native_roots": 7},
                {**proof, "pause": {"after": {"anon": 1}, "snapshot": {"resident_bytes_after": 0}}},
                {**proof, "pause": {"after": {"anon": 0}, "snapshot": {"resident_bytes_after": 1}}},
        ]:
            with self.subTest(proof=changed), self.assertRaises(ValueError):
                native_filesystem_lifecycle.require_complete(changed, deleted, child_deleted)
        for changed in [
                {**deleted, "runtime": {"status": "stopped"}},
                {"record": {**deleted["record"], "owner": "replacement"}, "runtime": None},
                {"record": {**deleted["record"], "phase": "paused"}, "runtime": None},
                {"record": {**deleted["record"], "delete": {}}, "runtime": None},
        ]:
            with self.subTest(deleted=changed), self.assertRaises(ValueError):
                native_filesystem_lifecycle.require_complete(proof, changed, child_deleted)
        with self.assertRaises(ValueError):
            native_filesystem_lifecycle.require_complete(proof, deleted, deleted)


if __name__ == "__main__":
    unittest.main()
