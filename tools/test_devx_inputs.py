"""Digest identity checks at the actual Docker image-inspection boundary."""
import json
import unittest
from unittest.mock import patch

import devx_inputs


class ImageIdentityTests(unittest.TestCase):
    def inspect(self, request, digests, *, architecture="arm64"):
        data = [{
            "Architecture": architecture, "Os": "linux", "Id": "sha256:" + "a" * 64,
            "RepoDigests": digests, "Config": {}, "Size": 1,
        }]
        with patch.object(devx_inputs.reference, "docker", return_value=json.dumps(data)):
            return devx_inputs.inspect_image(request)

    def test_docker_familiar_name_retains_exact_digest(self):
        digest = "@sha256:" + "a" * 64
        actual = self.inspect("docker.io/library/node:24-alpine" + digest, ["node" + digest])
        self.assertEqual(actual["canonical_image"], "node" + digest)

    def test_other_repository_digest_and_platform_are_rejected(self):
        digest = "@sha256:" + "a" * 64
        for refs, arch in [
            (["node@sha256:" + "b" * 64], "arm64"),
            (["other" + digest], "arm64"),
            (["node" + digest], "amd64"),
            ([], "arm64"),
        ]:
            with self.subTest(refs=refs, arch=arch), self.assertRaises(ValueError):
                self.inspect("node" + digest, refs, architecture=arch)

    def test_private_registry_port_and_repository_stay_distinct(self):
        digest = "@sha256:" + "a" * 64
        image = "registry.example:5000/team/node:24" + digest
        actual = self.inspect(image, ["registry.example:5000/team/node" + digest])
        self.assertEqual(actual["canonical_image"], "registry.example:5000/team/node" + digest)
        with self.assertRaises(ValueError):
            self.inspect(image, ["node" + digest])

    def test_unpinned_or_credentialed_reference_is_rejected_before_docker(self):
        with patch.object(devx_inputs.reference, "docker") as docker:
            for image in ["node:latest", "user:password@registry/node@sha256:" + "a" * 64, None]:
                with self.subTest(image=image), self.assertRaises(ValueError):
                    devx_inputs.inspect_image(image)
            docker.assert_not_called()


if __name__ == "__main__":
    unittest.main()
