import json
from pathlib import Path
import tempfile
import shutil
import unittest
from unittest import mock

import lifecycle_native


class LifecycleBuilderSelectionTests(unittest.TestCase):
    def test_high_exit_actor_patches_only_the_new_context_and_rejects_reapplication(self):
        root = lifecycle_native.ROOT
        name = "internal/lifecycle/capture_native_linux_test.go"
        original = (root / name).read_bytes()
        with tempfile.TemporaryDirectory() as directory:
            archive = Path(directory)
            target = archive / "context" / name
            target.parent.mkdir(parents=True)
            target.write_bytes(original)
            declaration = lifecycle_native.prepare_zombie_actor(archive / "context", archive)
            self.assertEqual((root / name).read_bytes(), original)
            self.assertEqual((archive / "zombie-high-exit-actor.before.go").read_bytes(), original)
            self.assertEqual(lifecycle_native.reference.sha(target), declaration["after_sha256"])
            self.assertIn(b'zombieExit := 47', target.read_bytes())
            self.assertIn(b'zombieExit = 201', target.read_bytes())
            with self.assertRaisesRegex(ValueError, "original or ordered patch changed"):
                lifecycle_native.prepare_zombie_actor(archive / "context", archive)

    def test_high_exit_suite_adds_coverage_and_keeps_original_script_and_adapter_assertions(self):
        source = lifecycle_native.ROOT / "tests/lifecycle-native.sh"
        before = source.read_bytes()
        with tempfile.TemporaryDirectory() as directory:
            archive = Path(directory)
            shutil.copyfile(source, archive / "lifecycle-native.sh")
            declaration = lifecycle_native.select_adapter_fixture(archive, include_high_exit=True)
            after = (archive / "lifecycle-native.sh").read_bytes()
            self.assertEqual(source.read_bytes(), before)
            self.assertEqual(declaration["original_adapter_tests"], list(lifecycle_native.ADAPTER_TESTS))
            self.assertFalse(declaration["assertions_modified"])
            self.assertTrue(declaration["zero_skip_requirement_unchanged"])
            self.assertEqual(declaration["additional_namespace_tests"], ["TestCaptureNestedZombieHighExitNamespaceNative"])
            self.assertIn(b'NestedZombie(HighExit)?Namespace', after)
            self.assertIn(b'test_timeout=360s', after)

    def test_candidates_link_only_the_explicit_architecture_and_exact_kernel(self):
        for platform in ["linux/arm64", "linux/amd64"]:
            self.assertEqual(lifecycle_native.candidate_compile_options(platform, "7.0.14-test+native"), [
                "-tags=factorybox_candidate_" + platform.split("/")[1],
                "-ldflags=-X=factorybox/internal/platform.candidateKernel=7.0.14-test+native",
            ])
        for platform, kernel in [("darwin/arm64", "7.0"), ("linux/arm64", ""),
                                 ("linux/amd64", "7.0 -X=foreign.symbol=enabled"),
                                 ("linux/arm64", None)]:
            with self.subTest(platform=platform, kernel=kernel), self.assertRaises(ValueError):
                lifecycle_native.candidate_compile_options(platform, kernel)

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
