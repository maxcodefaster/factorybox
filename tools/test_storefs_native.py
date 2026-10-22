"""Exercise the real runner's passed-result path through exact resource cleanup."""
import contextlib
import io
import json
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest import mock

import storefs_native


class StorefsRunnerTests(unittest.TestCase):
    def exercise_passed_fixture(self, runner, native_count=4):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / "internal/storefs").mkdir(parents=True)
            (root / "internal/storefs/source.go").write_text("package storefs\n")
            for name in ["go.mod", "go.sum"]:
                (root / name).write_text("synthetic input\n")
            artifact = root / "artifacts/run"
            artifact.parent.mkdir()
            state = {"identity": "f" * 64, "deleted": []}
            proofs = {
                "capacity": ["real_enospc", "boundary_canary_enospc", "rollback_after_reserve_release", "retained_unchanged",
                             "remount_persistence", "existing_initialization_refused", "exact_autoclear_cleanup"],
                "identity": ["second_owner_refused", "shared_inode_refused", "symlink_substitution_refused",
                             "existing_kernel_association_refused", "changed_mount_preserved", "original_data_preserved", "exact_autoclear_cleanup"],
                "autoclear": ["unconfirmed_release_refused", "lease_retained_until_release", "cleanup_retry_completed", "exact_reopen"],
                "initialization": ["wrong_formatter_refused_before_creation", "failed_initialization_retained", "uncertain_image_not_reformatted",
                                   "uninitialized_attachment_refused", "retained_image_unchanged"],
            }

            def docker(arguments):
                if arguments[0] == "info":
                    return "{}"
                if arguments[0] == "ps":
                    return ""
                if arguments[:2] == ["volume", "create"]:
                    return arguments[-1]
                if arguments[0] == "create":
                    state["name"] = arguments[arguments.index("--name") + 1]
                    state["owner"] = arguments[arguments.index("--label") + 1].split("=", 1)[1]
                    return state["identity"]
                self.fail("unexpected external action: " + repr(arguments))

            def exact(name, owner, identity):
                self.assertEqual((name, owner, identity), (state["name"], state["owner"], state["identity"]))
                return {"State": {"Running": False, "OOMKilled": False, "ExitCode": 0}}

            def delete(kind, name, owner):
                self.assertEqual((name, owner), (state["name"], state["owner"]))
                state["deleted"].append(kind)

            def execute(command, **options):
                if command[0] == "go":
                    Path(command[command.index("-o") + 1]).write_bytes(b"synthetic test executable")
                elif command[:3] == ["docker", "start", "--attach"]:
                    self.assertEqual(command[-1], state["identity"])
                    options["stdout"].write("\n".join("--- PASS: TestNativeStoreFS" + suffix for suffix in [
                        "CapacityAndRemount", "IdentityAndLeaseRefusals", "RetainsLeaseUntilAutoclear", "UncertainInitializationRefused"][:native_count]) + "\n")
                    for name, keys in proofs.items():
                        (artifact / "output" / (name + ".json")).write_text(json.dumps({"passed": True, **dict.fromkeys(keys, True)}))
                else:
                    self.fail("unexpected subprocess: " + repr(command))
                return subprocess.CompletedProcess(command, 0)

            with contextlib.ExitStack() as patches:
                patches.enter_context(mock.patch.object(runner.reference, "ROOT", root))
                patches.enter_context(mock.patch.object(runner.reference, "DOCKER", ["docker"]))
                patches.enter_context(mock.patch.object(runner.reference, "new_artifact", return_value=artifact))
                patches.enter_context(mock.patch.object(runner.reference, "docker", side_effect=docker))
                patches.enter_context(mock.patch.object(runner.reference, "owned_delete", side_effect=delete))
                patches.enter_context(mock.patch.object(runner.build_worker, "load", return_value=(root, {"image_id": "exact-image", "image": "exact-image"})))
                patches.enter_context(mock.patch.object(runner.native_base, "require_native_host"))
                patches.enter_context(mock.patch.object(runner.consumer_worker_loss, "sysctls", return_value={"fixture": "unchanged"}))
                patches.enter_context(mock.patch.object(runner.consumer_worker_loss, "exact_worker", side_effect=exact))
                patches.enter_context(mock.patch.object(runner.subprocess, "run", side_effect=execute))
                patches.enter_context(mock.patch("sys.argv", ["storefs-native", "--worker-build", "synthetic-image"]))
                patches.enter_context(contextlib.redirect_stdout(io.StringIO()))
                runner.main()
            self.assertEqual(state["deleted"], ["container", "volume"])
            saved = json.loads((artifact / "result.json").read_text())
            self.assertTrue(saved["passed"])
            self.assertTrue(saved["cleanup"])
            self.assertFalse(saved["production_qualified"])
            self.assertEqual(saved["passed_tests"], native_count)

    def test_passed_proofs_keep_exact_container_identity_through_cleanup(self):
        self.exercise_passed_fixture(storefs_native)


if __name__ == "__main__":
    unittest.main()
