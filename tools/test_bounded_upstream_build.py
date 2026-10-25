import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

import upstream_bats


class BoundedUpstreamBuildTests(unittest.TestCase):
    def test_fixture_checks_and_passes_the_exact_builder(self):
        with tempfile.TemporaryDirectory() as directory:
            archive = Path(directory)
            (archive / "context").mkdir()
            (archive / "builder.json").write_text(json.dumps({"containerimage.digest": "sha256:" + "a" * 64}))
            with patch.object(upstream_bats.build_config, "verify_builder") as verify, \
                    patch.object(upstream_bats.subprocess, "run") as run:
                actual = upstream_bats.build_fixture(
                    archive, "owned-fixture", "exact-base", platform="linux/amd64",
                    bounded_build=True, builder="owned-builder")
            self.assertEqual(actual, "owned-fixture@sha256:" + "a" * 64)
            options, docker = verify.call_args.args
            self.assertEqual(options.builder, "owned-builder")
            self.assertTrue(options.bounded_build)
            self.assertEqual(docker, upstream_bats.reference.DOCKER)
            command = run.call_args.args[0]
            self.assertIn("--builder=owned-builder", command)
            self.assertIn("--platform=linux/amd64", command)

    def test_bounded_fixture_cannot_build_before_builder_verification(self):
        with tempfile.TemporaryDirectory() as directory, \
                patch.object(upstream_bats.subprocess, "run") as run:
            with self.assertRaisesRegex(ValueError, "requires --builder"):
                upstream_bats.build_fixture(Path(directory), "owned-fixture", "exact-base",
                                             bounded_build=True)
            run.assert_not_called()
