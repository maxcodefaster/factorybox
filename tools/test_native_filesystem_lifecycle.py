import copy
import unittest

import native_filesystem_lifecycle


class NativeFilesystemLifecycleProof(unittest.TestCase):
    def test_native_backing_retirement_requires_exact_cleanup_and_independent_work(self):
        fields = ["passed", "exact_replay", "native_mutable_absent", "manager_roots_absent",
                  "common_base_preserved", "independent_child_useful"]
        retired = dict.fromkeys(fields, True)
        retired["child"] = dict.fromkeys(fields[:-1], True)
        native_filesystem_lifecycle.require_backing_retirement({"native_backing_retirement": retired})
        for field in fields:
            with self.subTest(field=field), self.assertRaises(ValueError):
                native_filesystem_lifecycle.require_backing_retirement(
                    {"native_backing_retirement": {**retired, field: False}})
        with self.assertRaises(ValueError):
            native_filesystem_lifecycle.require_backing_retirement({})
        for field in fields[:-1]:
            with self.subTest(child_field=field), self.assertRaises(ValueError):
                native_filesystem_lifecycle.require_backing_retirement(
                    {"native_backing_retirement": {**retired, "child": {**retired["child"], field: False}}})

    def test_native_checkpoint_retirement_requires_every_safety_and_useful_work_check(self):
        fields = ["passed", "selection_protected", "exact_replay", "native_roots_absent",
                  "checkpoint_metadata_preserved", "new_fork_refused", "independent_child_useful"]
        retired = dict.fromkeys(fields, True)
        native_filesystem_lifecycle.require_checkpoint_retirement({"native_checkpoint_retirement": retired})
        for field in fields:
            with self.subTest(field=field), self.assertRaises(ValueError):
                native_filesystem_lifecycle.require_checkpoint_retirement(
                    {"native_checkpoint_retirement": {**retired, field: False}})
        with self.assertRaises(ValueError):
            native_filesystem_lifecycle.require_checkpoint_retirement({})
        with self.assertRaises(ValueError):
            native_filesystem_lifecycle.require_checkpoint_retirement({"native_checkpoint_retirement": retired}, True)
        native_filesystem_lifecycle.require_checkpoint_retirement(
            {"native_checkpoint_retirement": {**retired, "actual_controller_sigkill": True,
                                               "partial_retry_preserved_foreign": True}}, True)

    def test_retained_recovery_binds_both_epochs_store_request_and_new_runtime(self):
        prepared = {"request": {"source_id": "original"}, "store_id": "same-store", "epoch": "old"}
        startup = {"status": {"store_id": "same-store", "epoch": "new"}}
        proof = {"passed": True, "original_ram_and_files_preserved": True, "new_useful_work": True,
                 "original_request": prepared["request"], "store_id": "same-store", "old_epoch": "old",
                 "new_epoch": "new", "target_id": "native-recovered", "owner": "new-owner"}
        deleted = {"record": {"id": "native-recovered", "owner": "new-owner", "phase": "deleted",
                              "delete": {"completed_at": "completed"}}, "runtime": None}
        native_filesystem_lifecycle.require_retained_complete(proof, deleted, prepared, startup)
        for key, value in [("passed", False), ("original_ram_and_files_preserved", False),
                           ("new_useful_work", False), ("store_id", "foreign"), ("old_epoch", "new"),
                           ("new_epoch", "old"), ("owner", "foreign"), ("original_request", {}),
                           ("target_id", "original")]:
            with self.subTest(key=key), self.assertRaises(ValueError):
                native_filesystem_lifecycle.require_retained_complete({**proof, key: value}, deleted, prepared, startup)
        with self.assertRaises(ValueError):
            native_filesystem_lifecycle.require_retained_complete(
                proof, {**deleted, "runtime": {"status": "stopped"}}, prepared, startup)

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
