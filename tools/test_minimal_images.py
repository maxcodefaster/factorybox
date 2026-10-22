import json
import pathlib
import tempfile
import unittest
from unittest.mock import patch

import devx_inputs
import devx_project_images


class MinimalImageTests(unittest.TestCase):
    def test_minimal_archive_cannot_substitute_for_factory_or_changed_example(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = pathlib.Path(temporary)
            recipe = root / "examples/minimal/.devcontainer/devcontainer.json"
            recipe.parent.mkdir(parents=True)
            image = "docker.io/library/alpine@sha256:" + "a" * 64
            recipe.write_text(json.dumps({"image": image}))
            factory = root / "examples/factory/.devcontainer/compose.yaml"
            factory.parent.mkdir(parents=True)
            factory.write_text("services:\n  workspace:\n    image: " + image + "\n")
            name = "factorybox-devx-project-images-" + "b" * 32
            archive = root / "artifacts" / name
            archive.mkdir(parents=True)
            payload = archive / "images.tar"
            payload.write_bytes(b"exact minimal archive")
            result = {"passed": True, "example": "minimal", "platform": "linux/amd64",
                      "compose_sha256": devx_inputs.file_sha(recipe),
                      "archive_sha256": devx_inputs.file_sha(payload),
                      "images": [{"image": image, "canonical_image": devx_inputs.canonical_digest(image)}]}
            (archive / "result.json").write_text(json.dumps(result))
            with patch.object(devx_project_images, "ROOT", root):
                selected, _ = devx_project_images.load(name, platform="linux/amd64", example="minimal")
                self.assertEqual(selected, archive)
                with self.assertRaises(ValueError):
                    devx_project_images.load(name, platform="linux/amd64", example="factory")
                recipe.write_text(json.dumps({"image": image, "remoteUser": "root"}))
                with self.assertRaises(ValueError):
                    devx_project_images.load(name, platform="linux/amd64", example="minimal")


if __name__ == "__main__":
    unittest.main()
