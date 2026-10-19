import json
import pathlib
import tempfile
import unittest
from unittest.mock import patch

import native_base


class NativeBaseInputsTests(unittest.TestCase):
    def test_manifest_reference_is_checked_for_config_and_manifest_image_stores(self):
        result = {"image": "owned@sha256:" + "a" * 64, "image_config": "sha256:" + "b" * 64}
        inspected = {
            "Id": result["image"].split("@")[1], "RepoDigests": [result["image"]],
            "Architecture": "arm64", "Os": "linux",
        }
        native_base.verify_image(result, inspected)
        inspected["Id"] = result["image_config"]
        native_base.verify_image(result, inspected)
        inspected["RepoDigests"] = ["unrelated@sha256:" + "c" * 64]
        with self.assertRaisesRegex(ValueError, "identity/platform differs"):
            native_base.verify_image(result, inspected)

    def test_explicit_build_rejects_changed_recipe_packages_and_image_identity(self):
        name = "factorybox-build-native-base-" + "a" * 32
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            archive = root / "artifacts" / name
            archive.mkdir(parents=True)
            (root / "build").mkdir()
            recipe = root / "build/native-base.Dockerfile"
            recipe.write_text("FROM pinned-test-input\n")
            packages = archive / "packages.tsv"
            packages.write_text("fixture\t1.0\tarm64\n")
            result = {
                "passed": True, "cleanup": True,
                "dockerfile_sha256": native_base.reference.sha(recipe),
                "packages_sha256": native_base.reference.sha(packages),
                "image": name + "@sha256:" + "b" * 64,
                "image_config": "sha256:" + "c" * 64,
            }
            record = archive / "result.json"
            record.write_text(json.dumps(result))
            inspected = {
                "Id": result["image_config"], "RepoDigests": [result["image"]],
                "Architecture": "arm64", "Os": "linux",
            }
            with patch.object(native_base.reference, "ROOT", root), \
                    patch.object(native_base.reference, "docker", side_effect=lambda _: json.dumps([inspected])):
                self.assertEqual(native_base.load(name)[0], result["image"])
                inspected["Architecture"] = "amd64"
                with self.assertRaisesRegex(ValueError, "identity/platform differs"):
                    native_base.load(name)
                inspected["Architecture"] = "arm64"
                recipe.write_text("FROM changed-test-input\n")
                with self.assertRaisesRegex(ValueError, "recipe changed"):
                    native_base.load(name)
                recipe.write_text("FROM pinned-test-input\n")
                packages.write_text("fixture\t2.0\tarm64\n")
                with self.assertRaisesRegex(ValueError, "package manifest changed"):
                    native_base.load(name)
                packages.write_text("fixture\t1.0\tarm64\n")
                result["image"] = "unrelated@sha256:" + "b" * 64
                record.write_text(json.dumps(result))
                with self.assertRaisesRegex(ValueError, "pinned build output"):
                    native_base.load(name)

    def test_incomplete_build_and_unscoped_names_are_rejected(self):
        with self.assertRaisesRegex(ValueError, "exact local build"):
            native_base.load("../foreign")
        name = "factorybox-build-native-base-" + "c" * 32
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            archive = root / "artifacts" / name
            archive.mkdir(parents=True)
            (archive / "result.json").write_text(json.dumps({"passed": True, "cleanup": False}))
            with patch.object(native_base.reference, "ROOT", root):
                with self.assertRaisesRegex(ValueError, "cleanup did not pass"):
                    native_base.load(name)


if __name__ == "__main__":
    unittest.main()
