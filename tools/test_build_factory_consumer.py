import argparse
import json
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest import mock

import build_factory_consumer as build


class FactoryConsumerBuildTests(unittest.TestCase):
    def test_completed_inputs_are_held_before_inspection_without_running_or_allocating_volumes(self):
        with tempfile.TemporaryDirectory() as temporary:
            artifact = Path(temporary).resolve()
            state = {"owner": "a" * 32, "images": {}}
            for role in build.TARGETS:
                state["images"][role] = {
                    "image": role + "@sha256:" + "b" * 64, "image_id": "sha256:" + "c" * 64,
                }
                inspected = {"Id": "sha256:" + "c" * 64, "Config": {"Volumes": {"/data": {}}}}

                def docker(arguments):
                    self.assertEqual(arguments[0], "create")
                    self.assertEqual(arguments[-1], state["images"][role]["image"])
                    self.assertIn("--network=none", arguments)
                    self.assertIn("--read-only", arguments)
                    self.assertIn("/data:rw,size=16m", arguments)
                    self.assertIn("/bin/true", arguments)
                    pending = json.loads((artifact / "intent.json").read_text())["image_holds"][role]
                    self.assertEqual(pending["image_id"], inspected["Id"])
                    self.assertEqual(pending["name"], "factorybox-consumer-input-" + state["owner"] + "-" + role)
                    return "d" * 64

                with mock.patch.object(build.reference, "docker", side_effect=docker):
                    build.retain_built_image(artifact, state, role, inspected)
                self.assertEqual(state["image_holds"][role]["container_id"], "d" * 64)
                for changed in [
                    {**inspected, "Id": "changed"},
                    {**inspected, "Config": {"Volumes": {"/": {}}}},
                ]:
                    with mock.patch.object(build.reference, "docker") as external:
                        with self.assertRaises(ValueError):
                            build.retain_built_image(artifact, state, role, changed)
                        external.assert_not_called()

    def test_each_concrete_image_build_is_native_bounded_local_and_unpublished(self):
        for platform in ["linux/arm64", "linux/amd64"]:
            options = argparse.Namespace(platform=platform, network="host", bounded_build=True,
                                         builder="owned-consumer-builder")
            for role, target in build.TARGETS.items():
                command = build.build_command(options, role, "owned-image", Path("/frozen"), Path("/evidence/metadata.json"))
                self.assertIn("--platform=" + platform, command)
                self.assertIn("--builder=owned-consumer-builder", command)
                self.assertIn("--network=host", command)
                self.assertIn("--load", command)
                self.assertEqual(command[command.index("--target") + 1], target)
                self.assertNotIn("--push", command)
                self.assertNotIn("publish", command)
            with self.assertRaises(ValueError):
                build.build_command(options, "unrelated", "owned", Path("/frozen"), Path("/evidence/meta"))

    def test_source_freeze_keeps_changed_untracked_inputs_but_not_ignored_secrets(self):
        with tempfile.TemporaryDirectory() as temporary:
            repo = Path(temporary).resolve()
            subprocess.run(["git", "init", "-q", str(repo)], check=True)
            required = [path for path in build.INPUTS if path not in build.DIRECTORIES]
            required += ["services/runtime-connector/go.mod", "services/runtime-connector/go.sum",
                         "services/runtime-connector/main.go",
                         "deploy/opencode-server/package.json", "deploy/opencode-server/bun.lock",
                         "deploy/opencode-server/server.mjs", "deploy/opencode-server/retained-events.mjs",
                         "deploy/opencode-server/install-skills.mjs",
                         "deploy/opencode-server/config/AGENTS.md", "deploy/opencode-server/config/opencode.json"]
            for name in required:
                path = repo / name
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_text("exact owned input\n")
            (repo / ".gitignore").write_text(".env\n**/.env\n")
            (repo / "services/runtime-connector/.env").write_text("must-not-enter-image")
            (repo / "deploy/opencode-server/.env").write_text("must-not-enter-image")
            (repo / "unrelated.txt").write_text("not a consumer input")
            selected = build.source_files(repo)
            self.assertEqual(selected, sorted(required))
            (repo / "services/runtime-connector/main.go").unlink()
            (repo / "services/runtime-connector/main.go").symlink_to(repo / "unrelated.txt")
            with self.assertRaisesRegex(ValueError, "regular"):
                build.source_files(repo)
            (repo / "upstream.json").unlink()
            with self.assertRaisesRegex(ValueError, "missing"):
                build.source_files(repo)
