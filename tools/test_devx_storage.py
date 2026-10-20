import sys
import unittest
from unittest import mock

import devx_lifecycle


class DeveloperStorageTests(unittest.TestCase):
    def test_failed_ide_cleanup_cannot_be_reported_as_complete_worker_cleanup(self):
        peer = mock.Mock()
        peer.result = {"passed": False, "cleanup": False,
                       "cleanup_error": "owned IDE container removal timed out"}
        result = {"passed": False, "cleanup": False}
        failure = ValueError("native deadline expired")
        with mock.patch.object(devx_lifecycle.upstream_bats, "archive_failure"), \
                mock.patch.object(devx_lifecycle.reference, "owned_delete") as remove:
            returned = devx_lifecycle.cleanup_resources(
                "archive", "worker", "owner", "image", result, peer, True, True, failure)
        self.assertIs(returned, failure)
        peer.close.assert_called_once_with(True)
        self.assertEqual(remove.call_args_list, [
            mock.call("container", "worker", "owner"), mock.call("volume", "worker", "owner")])
        self.assertFalse(result["cleanup"])
        self.assertTrue(result["worker_cleanup"])
        self.assertFalse(result["passed"])

    def test_successful_worker_and_peer_cleanup_preserve_the_complete_verdict(self):
        peer = mock.Mock()
        peer.result = {"passed": True, "cleanup": True}
        result = {"passed": True, "cleanup": False}
        with mock.patch.object(devx_lifecycle.upstream_bats, "archive_failure") as archive, \
                mock.patch.object(devx_lifecycle.reference, "owned_delete"):
            returned = devx_lifecycle.cleanup_resources(
                "archive", "worker", "owner", "image", result, peer, True, True, None)
        self.assertIsNone(returned)
        archive.assert_not_called()
        self.assertTrue(result["cleanup"])
        self.assertTrue(result["passed"])

    def test_peer_receipt_error_does_not_skip_worker_cleanup_or_publish_a_pass(self):
        peer = mock.Mock()
        peer.result = {"passed": True, "cleanup": True}
        peer.close.side_effect = OSError("peer receipt write failed")
        result = {"passed": True, "cleanup": False}
        with mock.patch.object(devx_lifecycle.upstream_bats, "archive_failure"), \
                mock.patch.object(devx_lifecycle.reference, "owned_delete") as remove:
            failure = devx_lifecycle.cleanup_resources(
                "archive", "worker", "owner", "image", result, peer, True, True, None)
        self.assertIsInstance(failure, OSError)
        self.assertEqual(remove.call_count, 2)
        self.assertTrue(result["cleanup"])
        self.assertFalse(result["passed"])
        self.assertIn("peer receipt", result["peer_cleanup_error"])

    def test_retained_ide_volume_blocks_new_developer_runs(self):
        volume = "factorybox-devx-lifecycle-" + "a" * 32 + "-ide-work"
        with mock.patch.object(devx_lifecycle.reference, "docker", return_value=volume), \
                mock.patch.object(devx_lifecycle.reference, "owned_delete") as remove:
            with self.assertRaisesRegex(ValueError, "existing developer volume"):
                devx_lifecycle.require_no_retained_worker()
            remove.assert_not_called()

    def test_docker_fault_refuses_selected_criu_before_resource_admission(self):
        command = ["devx_lifecycle.py", "--suite", "docker-interruption"]
        for flag in ["consumer-inputs", "project-images", "native-base-build", "sysbox-build", "criu-build"]:
            command += ["--" + flag, "unused"]
        with mock.patch.object(sys, "argv", command), \
                mock.patch.object(devx_lifecycle, "require_no_retained_worker") as admission, \
                mock.patch.object(devx_lifecycle.reference, "new_artifact") as artifact, \
                mock.patch.object(sys, "stderr"):
            with self.assertRaises(SystemExit) as error:
                devx_lifecycle.main()
            self.assertEqual(error.exception.code, 2)
            admission.assert_not_called()
            artifact.assert_not_called()

    def test_retained_volume_blocks_the_next_run_before_inputs_or_artifacts_are_created(self):
        volume = "factorybox-devx-lifecycle-" + "a" * 32
        command = ["devx_lifecycle.py"]
        for flag in ["consumer-inputs", "project-images", "native-base-build", "sysbox-build", "criu-build"]:
            command += ["--" + flag, "unused"]
        with mock.patch.object(sys, "argv", command), \
                mock.patch.object(devx_lifecycle.reference, "docker", return_value=volume), \
                mock.patch.object(devx_lifecycle.devx_inputs, "load") as load, \
                mock.patch.object(devx_lifecycle.reference, "new_artifact") as artifact:
            with self.assertRaisesRegex(ValueError, "reconcile the existing developer volume"):
                devx_lifecycle.main()
            load.assert_not_called()
            artifact.assert_not_called()

    def test_retained_input_and_unrelated_volumes_do_not_block_or_get_removed(self):
        names = "factorybox-docker-profile-input-" + "a" * 32 + "\nforeign-project-data\n"
        with mock.patch.object(devx_lifecycle.reference, "docker", return_value=names) as docker, \
                mock.patch.object(devx_lifecycle.reference, "owned_delete") as remove:
            devx_lifecycle.require_no_retained_worker()
            self.assertEqual(docker.call_count, 1)
            self.assertEqual(docker.call_args.args[0][:2], ["volume", "ls"])
            remove.assert_not_called()


if __name__ == "__main__":
    unittest.main()
