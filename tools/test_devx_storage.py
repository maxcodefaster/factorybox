import io
import sys
import unittest
from unittest import mock

import devx_lifecycle


class DeveloperStorageTests(unittest.TestCase):
    def test_failed_worker_stops_before_peer_cleanup_and_failure_archival(self):
        events = []
        peer = mock.Mock()
        peer.result = {"passed": False, "cleanup": True}
        peer.close.side_effect = lambda _: events.append("peer")
        result = {"passed": False, "cleanup": False}
        failure = ValueError("native deadline expired")
        with mock.patch.object(devx_lifecycle.upstream_bats, "stop_failed_fixture",
                               side_effect=lambda *_: events.append("stop")), \
                mock.patch.object(devx_lifecycle.upstream_bats, "archive_failure",
                                  side_effect=lambda *_, **__: events.append("archive")), \
                mock.patch.object(devx_lifecycle.reference, "owned_delete"):
            returned = devx_lifecycle.cleanup_resources(
                "archive", "worker", "owner", "image", result, peer, True, True, failure)
        self.assertIs(returned, failure)
        self.assertEqual(events, ["stop", "peer", "archive"])
        self.assertTrue(result["cleanup"])
        self.assertFalse(result["passed"])

    def test_failed_ide_cleanup_cannot_be_reported_as_complete_worker_cleanup(self):
        peer = mock.Mock()
        peer.result = {"passed": False, "cleanup": False,
                       "cleanup_error": "owned IDE container removal timed out"}
        result = {"passed": False, "cleanup": False}
        failure = ValueError("native deadline expired")
        with mock.patch.object(devx_lifecycle.upstream_bats, "stop_failed_fixture"), \
                mock.patch.object(devx_lifecycle.upstream_bats, "archive_failure"), \
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

    def test_docker_child_fault_requires_its_dedicated_suite_before_resource_admission(self):
        command = ["devx_lifecycle.py", "--docker-fault-process", "dockerd"]
        for flag in ["consumer-inputs", "project-images", "native-base-build", "sysbox-build", "retained-candidate"]:
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

    def test_pause_fault_requires_its_dedicated_suite_before_resource_admission(self):
        command = ["devx_lifecycle.py", "--docker-fault-operation", "pause"]
        for flag in ["consumer-inputs", "project-images", "native-base-build", "sysbox-build", "retained-candidate"]:
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

    def test_helper_fault_rejects_unsupported_modes_before_inputs_or_resources(self):
        modes = [
            (["--docker-fault-component", "helper"],
             "--docker-fault-component requires --suite docker-interruption"),
            (["--docker-fault-component", "controller"],
             "--docker-fault-component requires --suite docker-interruption"),
            (["--docker-fault-component", "guard"],
             "--docker-fault-component requires --suite docker-interruption"),
            (["--suite", "docker-interruption", "--docker-fault-component", "helper",
              "--docker-fault-operation", "pause"],
             "the helper-loss proof currently requires checkpoint"),
        ]
        for flags, message in modes:
            with self.subTest(flags=flags):
                command = ["devx_lifecycle.py", *flags]
                for flag in ["consumer-inputs", "project-images", "native-base-build",
                             "sysbox-build", "retained-candidate"]:
                    command += ["--" + flag, "unused"]
                with mock.patch.object(sys, "argv", command), \
                        mock.patch.object(sys, "stderr", new_callable=io.StringIO) as stderr, \
                        mock.patch.object(devx_lifecycle, "require_no_retained_worker") as admission, \
                        mock.patch.object(devx_lifecycle.devx_inputs, "load") as consumers, \
                        mock.patch.object(devx_lifecycle.devx_project_images, "load") as projects, \
                        mock.patch.object(devx_lifecycle.native_base, "load") as native, \
                        mock.patch.object(devx_lifecycle.reference, "new_artifact") as artifact:
                    with self.assertRaises(SystemExit) as error:
                        devx_lifecycle.main()
                    self.assertEqual(error.exception.code, 2)
                    self.assertIn(message, stderr.getvalue())
                    for stage in [admission, consumers, projects, native, artifact]:
                        stage.assert_not_called()

    def test_guard_fault_rejects_missing_exit_observer_before_artifact_creation(self):
        command = ["devx_lifecycle.py", "--suite", "docker-interruption",
                   "--docker-fault-component", "guard"]
        for flag in ["consumer-inputs", "project-images", "native-base-build",
                     "sysbox-build", "retained-candidate"]:
            command += ["--" + flag, "unused"]
        with mock.patch.object(sys, "argv", command), \
                mock.patch.object(sys, "stderr", new_callable=io.StringIO) as stderr, \
                mock.patch.object(devx_lifecycle, "require_no_retained_worker"), \
                mock.patch.object(devx_lifecycle.devx_inputs, "load", return_value=("consumer", {})), \
                mock.patch.object(devx_lifecycle.devx_project_images, "load", return_value=("project", {})), \
                mock.patch.object(devx_lifecycle.native_base, "load", return_value=("image", {})), \
                mock.patch.object(devx_lifecycle.reference_lifecycle, "component_build",
                                  return_value=("sysbox", {})), \
                mock.patch.object(devx_lifecycle.criu_candidate, "load",
                                  return_value=("criu", {"exit_hold_candidate": False}, None, None)), \
                mock.patch.object(devx_lifecycle.reference, "new_artifact") as artifact:
            with self.assertRaises(SystemExit) as error:
                devx_lifecycle.main()
            self.assertEqual(error.exception.code, 2)
            self.assertIn("requires the pinned exit-observer candidate", stderr.getvalue())
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

    def test_failed_ide_admission_blocks_all_large_input_staging(self):
        command = ["devx_lifecycle.py", "--theia-image", "unused-pinned-image"]
        for flag in ["consumer-inputs", "project-images", "native-base-build", "sysbox-build", "criu-build"]:
            command += ["--" + flag, "unused"]
        image = {"id": "sha256:" + "a" * 64}
        with mock.patch.object(sys, "argv", command), \
                mock.patch.object(devx_lifecycle, "require_no_retained_worker"), \
                mock.patch.object(devx_lifecycle.devx_inputs, "inspect_image", return_value=image), \
                mock.patch.object(devx_lifecycle.theia_admission, "qualify",
                                  side_effect=ValueError("admission deadline")) as qualify, \
                mock.patch.object(devx_lifecycle.devx_inputs, "load") as consumers, \
                mock.patch.object(devx_lifecycle.devx_project_images, "load") as projects, \
                mock.patch.object(devx_lifecycle.native_base, "load") as native, \
                mock.patch.object(devx_lifecycle.reference, "new_artifact") as artifact:
            with self.assertRaisesRegex(ValueError, "admission deadline"):
                devx_lifecycle.main()
            qualify.assert_called_once_with(image)
            for stage in [consumers, projects, native, artifact]:
                stage.assert_not_called()


if __name__ == "__main__":
    unittest.main()
