import argparse
from pathlib import Path
import subprocess
import tempfile
import unittest

import build_factory_consumer as build


class FactoryConsumerBuildTests(unittest.TestCase):
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
