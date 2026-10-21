import json
from pathlib import Path
import tempfile
import unittest
from unittest import mock

import lifecycle_native


class LifecycleBuilderSelectionTests(unittest.TestCase):
    def test_supplied_components_do_not_require_or_rebuild_a_homogeneous_builder(self):
        composition = {"candidate_only": True, "base_sysbox_build": "pinned-base",
                       "per_component_build": {"sysbox-runc": "pinned-base", "sysbox-fs": "pinned-fs"}}
        with mock.patch.object(lifecycle_native.reference, "sha") as sha, \
                mock.patch.object(lifecycle_native.upstream_unit, "context_manifest") as context:
            command = lifecycle_native.builder_command(
                Path("explicit-component-input"), composition, Path("new-run"), "worker",
                component_inputs=True)
        self.assertIsNone(command)
        sha.assert_not_called()
        context.assert_not_called()

    def test_actual_builder_still_checks_its_recipe_and_complete_context(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            recipe = root / "build/sysbox.Dockerfile"
            recipe.parent.mkdir()
            recipe.write_text("FROM pinned-test-input\n")
            build = root / "frozen-build"
            context = build / "context"
            context.mkdir(parents=True)
            source = context / "original-source"
            source.write_text("original\n")
            expected = lifecycle_native.upstream_unit.context_manifest(context)
            (build / "context-manifest.json").write_text(json.dumps(expected))
            components = {"dockerfile_sha256": lifecycle_native.reference.sha(recipe)}
            with mock.patch.object(lifecycle_native, "ROOT", root):
                command = lifecycle_native.builder_command(
                    build, components, root / "new-run", "owned-worker", component_inputs=False)
                self.assertEqual(command[-1], str(context))
                self.assertIn(str(recipe), command)
                self.assertIn("owned-worker", command)
                recipe.write_text("FROM changed\n")
                with self.assertRaisesRegex(ValueError, "recipe changed"):
                    lifecycle_native.builder_command(
                        build, components, root / "new-run", "owned-worker", component_inputs=False)
                recipe.write_text("FROM pinned-test-input\n")
                source.write_text("changed\n")
                with self.assertRaisesRegex(ValueError, "context changed"):
                    lifecycle_native.builder_command(
                        build, components, root / "new-run", "owned-worker", component_inputs=False)
