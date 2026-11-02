import argparse
from pathlib import Path
import unittest

import capture_budget_native
import release_native


class CaptureBudgetNativeTests(unittest.TestCase):
    def complete(self):
        return {
            "copy_delay_seconds": 65, "pause_seconds": 65.2,
            "copy_delay_observed": True, "committed_pause": True,
            "anonymous_memory_released": True, "same_ram_after_resume": True,
            "useful_after_resume": True, "sandbox_deleted": True,
            "worker_stopped_cleanly": True, "kernel_storage_released": True,
            "host_sysctls_unchanged": True, "inputs_unchanged": True,
        }

    def test_requires_the_real_delayed_capture_and_every_preservation_gate(self):
        complete = self.complete()
        capture_budget_native.require_complete(complete)
        for key, value in complete.items():
            changed = dict(complete)
            changed[key] = False if isinstance(value, bool) else 60
            with self.subTest(key=key):
                with self.assertRaisesRegex(ValueError, "capture budget omitted"):
                    capture_budget_native.require_complete(changed)

    def test_both_release_architectures_require_the_delivered_cli_budget_case(self):
        for platform in ["linux/arm64", "linux/amd64"]:
            options = argparse.Namespace(
                platform=platform, worker_build="exact-worker", consumer_build="exact-consumer",
                project_images="exact-project-images", factory_repo=Path("/exact-factory"),
                builder="exact-builder")
            worker = {"native_base_build": "exact-base", "sysbox_build": "exact-sysbox",
                      "criu_build": "exact-criu"}
            commands = dict(release_native.native_commands(options, worker, "exact-bats"))
            self.assertIn("capture-budget", commands)
            self.assertEqual(commands["capture-budget"][1:], [
                "tools/capture_budget_native.py", "--worker-build", "exact-worker",
            ])
            self.assertEqual(commands["failed-capture-exit"][1:], [
                "tools/capture_budget_native.py", "--worker-build", "exact-worker", "--failed-copy",
            ])
            self.assertEqual(commands["recovered-capture-retirement"][1:], [
                "tools/capture_budget_native.py", "--worker-build", "exact-worker", "--failed-copy", "--retire-recovered",
            ])

    def test_failed_capture_exit_requires_every_preservation_and_release_gate(self):
        complete = dict.fromkeys([
            "failed_copy_observed", "recovered_failed_pause", "same_ram_after_failed_capture",
            "useful_after_failed_capture", "incomplete_alias_mounted", "failed_payload_preserved",
            "sandbox_deleted", "worker_stopped_cleanly", "kernel_storage_released",
            "same_store_new_epoch", "successor_storage_released", "host_sysctls_unchanged", "inputs_unchanged",
        ], True)
        capture_budget_native.require_failed_capture_complete(complete)
        for key in complete:
            with self.subTest(key=key):
                with self.assertRaisesRegex(ValueError, "failed capture exit omitted"):
                    capture_budget_native.require_failed_capture_complete({**complete, key: False})

    def test_only_a_definite_original_kernel_owner_refusal_can_be_reobserved(self):
        diagnostic = "fixed filesystem still has an image owner: resource temporarily unavailable\n"
        receipt = {"passed": False, "cleanup": True, "helper_exit": {
            "command_exit": 1, "running": False, "exit_code": 1, "oom_killed": False,
        }}
        self.assertTrue(capture_budget_native.pending_kernel_owner(receipt, diagnostic))
        for field, value in [("command_exit", None), ("command_exit", 0), ("running", True),
                             ("exit_code", 0), ("oom_killed", True)]:
            changed = {**receipt, "helper_exit": {**receipt["helper_exit"], field: value}}
            self.assertFalse(capture_budget_native.pending_kernel_owner(changed, diagnostic))
        self.assertFalse(capture_budget_native.pending_kernel_owner({**receipt, "passed": True}, diagnostic))
        self.assertFalse(capture_budget_native.pending_kernel_owner(receipt, "unconfirmed other identity"))

    def test_recovered_retirement_cannot_substitute_preservation_for_actual_cleanup(self):
        complete = dict.fromkeys([
            "failed_copy_observed", "recovered_failed_pause", "same_ram_after_failed_capture",
            "useful_after_failed_capture", "incomplete_alias_mounted", "sandbox_deleted",
            "worker_stopped_cleanly", "kernel_storage_released", "same_store_new_epoch",
            "successor_storage_released", "host_sysctls_unchanged", "inputs_unchanged",
            "failed_payload_retired", "cleanup_evidence_preserved", "retirement_replay_exact",
            "registered_base_preserved", "foreign_successor_preserved", "invalid_abort_refused",
            "published_generation_refused", "foreign_hardlink_refused", "foreign_mount_refused",
        ], True)
        complete["retire_recovered"] = True
        capture_budget_native.require_failed_capture_complete(complete)
        for key in complete:
            with self.subTest(key=key):
                with self.assertRaisesRegex(ValueError, "failed capture exit omitted"):
                    capture_budget_native.require_failed_capture_complete({**complete, key: False})

    def test_retained_retirement_requires_old_identity_and_unchanged_record(self):
        complete = dict.fromkeys([
            "failed_copy_observed", "recovered_failed_pause", "same_ram_after_failed_capture",
            "useful_after_failed_capture", "incomplete_alias_mounted", "sandbox_deleted",
            "worker_stopped_cleanly", "kernel_storage_released", "same_store_new_epoch",
            "successor_storage_released", "host_sysctls_unchanged", "inputs_unchanged",
            "failed_payload_retired", "retained_cleanup_replay_exact", "retained_original_receipt_preserved",
            "retained_wrong_owner_refused", "retained_current_epoch_refused", "retained_record_unchanged",
            "registered_base_preserved",
        ], True)
        complete["retire_retained_recovered"] = True
        capture_budget_native.require_failed_capture_complete(complete)
        for key in complete:
            with self.subTest(key=key):
                with self.assertRaisesRegex(ValueError, "failed capture exit omitted"):
                    capture_budget_native.require_failed_capture_complete({**complete, key: False})


if __name__ == "__main__":
    unittest.main()
