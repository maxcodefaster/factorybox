"""Digest identity checks at the actual Docker image-inspection boundary."""
import json
import pathlib
import tempfile
import unittest
from unittest.mock import patch

import devx_inputs
import devx_project_images


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


class ConsumerArchiveTests(unittest.TestCase):
    def prepared(self, root):
        name = "factorybox-devx-inputs-" + "a" * 32
        archive = root / "artifacts" / name
        archive.mkdir(parents=True)
        payload = archive / "runtime-rootfs.tar"
        payload.write_bytes(b"complete exported rootfs")
        selected = {role: role + "@sha256:" + "b" * 64 for role in devx_inputs.ROLES}
        result = {
            "passed": True, "cleanup": True, "selected_images": selected,
            "images": {role: {
                "image": image, "id": "sha256:" + "c" * 64, "architecture": "arm64", "os": "linux",
            } for role, image in selected.items()},
            "rootfs_tar_bytes": payload.stat().st_size,
            "rootfs_tar_sha256": devx_inputs.file_sha(payload),
        }
        (archive / "result.json").write_text(json.dumps(result))
        return name, archive, result

    def test_frozen_rootfs_survives_consumer_image_cache_eviction(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = pathlib.Path(temporary)
            name, archive, _ = self.prepared(root)
            with patch.object(devx_inputs, "ROOT", root), \
                    patch.object(devx_inputs, "inspect_image", side_effect=AssertionError("cache evicted")) as inspect:
                directory, _ = devx_inputs.load(name, rootfs=True)
                self.assertEqual(directory, archive)
                inspect.assert_not_called()
                payload = archive / "runtime-rootfs.tar"
                original = payload.read_bytes()
                payload.write_bytes(b"X" + original[1:])
                with self.assertRaises(ValueError):
                    devx_inputs.load(name, rootfs=True)

    def test_direct_image_consumers_still_require_current_image_identity(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = pathlib.Path(temporary)
            name, _, _ = self.prepared(root)
            with patch.object(devx_inputs, "ROOT", root), \
                    patch.object(devx_inputs, "inspect_image", side_effect=ValueError("cache evicted")) as inspect:
                with self.assertRaisesRegex(ValueError, "cache evicted"):
                    devx_inputs.load(name)
                self.assertEqual(inspect.call_count, 1)


class ProjectArchiveTests(unittest.TestCase):
    def test_archive_payload_recipe_and_repository_identity_are_bound(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = pathlib.Path(temporary)
            name = "factorybox-devx-project-images-" + "a" * 32
            archive = root / "artifacts" / name
            archive.mkdir(parents=True)
            compose = root / "examples/factory/.devcontainer/compose.yaml"
            compose.parent.mkdir(parents=True)
            image = "docker.io/library/node:24@sha256:" + "b" * 64
            compose.write_text("services:\n  first:\n    image: " + image +
                               "\n  second:\n    image: " + image + "\n")
            payload = archive / "images.tar"
            payload.write_bytes(b"original verified archive")
            result = {
                "passed": True, "compose_sha256": devx_inputs.file_sha(compose),
                "archive_sha256": devx_inputs.file_sha(payload),
                "images": [{"image": image, "canonical_image": devx_inputs.canonical_digest(image)}],
            }
            receipt = archive / "result.json"
            receipt.write_text(json.dumps(result))
            with patch.object(devx_project_images, "ROOT", root):
                directory, _ = devx_project_images.load(name)
                self.assertEqual(directory, archive)
                payload.write_bytes(b"changed archive")
                with self.assertRaises(ValueError):
                    devx_project_images.load(name)
                payload.write_bytes(b"original verified archive")
                compose.write_text(compose.read_text() + "# changed recipe\n")
                with self.assertRaises(ValueError):
                    devx_project_images.load(name)
                result["compose_sha256"] = devx_inputs.file_sha(compose)
                result["images"][0]["canonical_image"] = "foreign@sha256:" + "b" * 64
                receipt.write_text(json.dumps(result))
                with self.assertRaises(ValueError):
                    devx_project_images.load(name)
                result["images"][0]["canonical_image"] = devx_inputs.canonical_digest(image)
                result["passed"] = False
                receipt.write_text(json.dumps(result))
                with self.assertRaises(ValueError):
                    devx_project_images.load(name)

    def test_archive_path_cannot_escape_artifact_directory(self):
        for name in ["../foreign", "factorybox-devx-project-images-../foreign", "/tmp/archive"]:
            with self.subTest(name=name), self.assertRaises(ValueError):
                devx_project_images.load(name)


if __name__ == "__main__":
    unittest.main()
