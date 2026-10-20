import sys
import unittest
from unittest import mock

import devx_lifecycle


class DeveloperStorageTests(unittest.TestCase):
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
