import json
import subprocess
import unittest
from unittest.mock import patch

import reference


class OwnershipCleanupTests(unittest.TestCase):
    def test_foreign_label_cannot_remove_container(self):
        data = [{"Name": "/owned", "Config": {"Labels": {"com.factorybox.run": "other"}}}]
        with patch.object(reference, "docker", return_value=json.dumps(data)) as docker:
            with self.assertRaisesRegex(ValueError, "foreign"):
                reference.owned_delete("container", "owned", "expected")
            self.assertEqual(docker.call_count, 1)

    def test_foreign_name_cannot_remove_volume_even_with_matching_label(self):
        data = [{"Name": "other", "Labels": {"com.factorybox.run": "expected"}}]
        with patch.object(reference, "docker", return_value=json.dumps(data)) as docker:
            with self.assertRaisesRegex(ValueError, "foreign"):
                reference.owned_delete("volume", "owned", "expected")
            self.assertEqual(docker.call_count, 1)

    def test_exact_owned_resource_can_be_removed(self):
        data = [{"Name": "/owned", "Config": {"Labels": {"com.factorybox.run": "expected"}}}]
        with patch.object(reference, "docker",
                          side_effect=[json.dumps(data), "owned"]) as docker:
            reference.owned_delete("container", "owned", "expected")
            self.assertEqual(docker.call_args.args[0], ["container", "rm", "--force", "owned"])

    def test_daemon_failure_is_not_proof_of_absence(self):
        error = subprocess.CalledProcessError(
            1, ["docker", "inspect"], stderr="Cannot connect to the Docker daemon")
        with patch.object(reference, "docker", side_effect=error):
            with self.assertRaises(subprocess.CalledProcessError):
                reference.owned_delete("container", "owned", "expected")

    def test_never_created_container_does_not_block_volume_cleanup(self):
        error = subprocess.CalledProcessError(
            1, ["docker", "inspect"], stderr="Error: No such container: owned")
        with patch.object(reference, "docker", side_effect=error) as docker:
            reference.owned_delete("container", "owned", "expected")
            self.assertEqual(docker.call_count, 1)
