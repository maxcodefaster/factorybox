import json
import pathlib
import tempfile
import unittest
from unittest.mock import patch

import native_base


class NativeBaseInputsTests(unittest.TestCase):
    def test_missing_buildx_config_digest_still_requires_the_exact_manifest_and_architecture(self):
        result = {"image": "owned@sha256:" + "a" * 64, "platform": "linux/amd64"}
        inspected = {"Id": "sha256:" + "b" * 64, "RepoDigests": [result["image"]],
                     "Architecture": "amd64", "Os": "linux"}
        native_base.bind_image_identity(result, {}, inspected)
        self.assertEqual(result["image_config"], inspected["Id"])
        self.assertEqual(result["image_identity_source"], "manifest_inspection")
        with self.assertRaisesRegex(ValueError, "identity/platform differs"):
            native_base.bind_image_identity(result, {"containerimage.config.digest": "sha256:" + "c" * 64}, inspected)
        inspected["RepoDigests"] = ["other@sha256:" + "a" * 64]
        with self.assertRaisesRegex(ValueError, "identity/platform differs"):
            native_base.bind_image_identity(result, {}, inspected)
        inspected["RepoDigests"] = [result["image"]]
        inspected["Architecture"] = "arm64"
        with self.assertRaisesRegex(ValueError, "identity/platform differs"):
            native_base.bind_image_identity(result, {}, inspected)

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

    def test_amd64_base_is_an_explicit_build_input_and_cannot_enable_native_suites(self):
        name = "factorybox-build-native-base-" + "d" * 32
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            archive = root / "artifacts" / name
            archive.mkdir(parents=True)
            (root / "build").mkdir()
            recipe = root / "build/native-base.Dockerfile"
            recipe.write_text("FROM pinned-test-input\n")
            packages = archive / "packages.tsv"
            packages.write_text("fixture\t1.0\tamd64\n")
            result = {
                "passed": True, "cleanup": True, "platform": "linux/amd64",
                "dockerfile_sha256": native_base.reference.sha(recipe),
                "packages_sha256": native_base.reference.sha(packages),
                "image": name + "@sha256:" + "e" * 64,
                "image_config": "sha256:" + "f" * 64,
            }
            (archive / "result.json").write_text(json.dumps(result))
            inspected = {"Id": result["image_config"], "RepoDigests": [result["image"]],
                         "Architecture": "amd64", "Os": "linux"}
            with patch.object(native_base.reference, "ROOT", root), \
                    patch.object(native_base.reference, "docker", side_effect=lambda _: json.dumps([inspected])):
                with self.assertRaisesRegex(ValueError, "available only to build"):
                    native_base.load(name)
                self.assertEqual(native_base.load(name, for_build=True)[0], result["image"])
                self.assertEqual(native_base.load(name, candidate_platform="linux/amd64")[0], result["image"])
                with self.assertRaisesRegex(ValueError, "explicit candidate platform"):
                    native_base.load(name, candidate_platform="linux/arm64")
                inspected["Architecture"] = "arm64"
                with self.assertRaisesRegex(ValueError, "identity/platform differs"):
                    native_base.load(name, for_build=True)


if __name__ == "__main__":
    unittest.main()
