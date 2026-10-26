import argparse
from pathlib import Path
import tempfile
import unittest

import reference
import upstream_bats
import upstream_unit


class BoundedFixtureRecipeTransportTests(unittest.TestCase):
    def test_bats_rebuild_keeps_exact_native_recipe_and_checks_its_full_package_inventory(self):
        prefix = (reference.ROOT / "build/native-base.Dockerfile").read_bytes()
        template = (reference.ROOT / "build/upstream-bats.Dockerfile").read_bytes()
        for changed in [False, True]:
            with self.subTest(changed=changed), tempfile.TemporaryDirectory() as temporary:
                artifact = Path(temporary).resolve()
                (artifact / "context").mkdir()
                (artifact / "context/Dockerfile").write_bytes(template)
                record = {"image": "exact-base", "dockerfile_sha256":
                          "0" * 64 if changed else reference.sha(reference.ROOT / "build/native-base.Dockerfile"),
                          "packages_sha256": "b" * 64}
                if changed:
                    with self.assertRaises(ValueError):
                        upstream_bats.bounded_fixture_recipe(artifact, "exact-base", record)
                else:
                    recipe = upstream_bats.bounded_fixture_recipe(artifact, "exact-base", record).read_bytes()
                    self.assertTrue(recipe.startswith(prefix))
                    self.assertIn(b"b" * 64 + b"  /opt/factorybox-native-packages.tsv", recipe)
                    header = b"ARG NATIVE_BASE_IMAGE\nFROM ${NATIVE_BASE_IMAGE}\n"
                    self.assertTrue(recipe.endswith(template[len(header):]))
                    self.assertNotIn(b"FROM ${NATIVE_BASE_IMAGE}", recipe)
                    self.assertEqual((artifact / "context/Dockerfile").read_bytes(), template)

    def test_unit_recipe_preserves_every_original_byte_and_uses_the_in_file_build_stage(self):
        prefix = (reference.ROOT / "build/sysbox.Dockerfile").read_bytes()
        template = (reference.ROOT / "build/upstream-unit.Dockerfile").read_bytes()
        recipe = upstream_unit.compose_offline_unit_dockerfile(prefix, template)
        self.assertTrue(recipe.startswith(prefix))
        self.assertNotIn(b"${BUILDER_IMAGE}", recipe)
        self.assertIn(b"FROM build AS offline\n", recipe)
        tail = template.split(b"FROM ${BUILDER_IMAGE}\n", 1)[1]
        self.assertTrue(recipe.endswith(tail))
        for changed in [template.replace(b"FROM ${BUILDER_IMAGE}", b"FROM unexpected"),
                        template + b"\nFROM ${BUILDER_IMAGE}\n"]:
            with self.assertRaises(ValueError):
                upstream_unit.compose_offline_unit_dockerfile(prefix, changed)

    def test_unit_build_explicitly_loads_both_platforms_without_a_daemon_only_parent(self):
        for platform in ["linux/arm64", "linux/amd64"]:
            options = argparse.Namespace(platform=platform, network="host",
                                         bounded_build=True, builder="owned-builder")
            command = upstream_unit.offline_builder_command(options, Path("/frozen/Dockerfile"),
                Path("/exact-context"), "exact-image", Path("/evidence"))
            self.assertIn("--platform=" + platform, command)
            self.assertIn("--builder=owned-builder", command)
            self.assertIn("--load", command)
            self.assertEqual(command[command.index("--target") + 1], "offline")
            self.assertNotIn("--push", command)
            self.assertNotIn("BUILDER_IMAGE", " ".join(command))
