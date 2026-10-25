import argparse
import json
import unittest

import build_worker_pipeline


class WorkerPipelineTests(unittest.TestCase):
    def test_each_target_uses_exact_completed_inputs_and_never_publishes(self):
        for platform in ["linux/arm64", "linux/amd64"]:
            options = argparse.Namespace(platform=platform, network="host", builder="owned-builder",
                                         bounded_build=True, native_base_build="exact-base")
            commands = build_worker_pipeline.commands(options, "exact-sysbox", "exact-criu")
            self.assertIn(platform, commands["sysbox"])
            self.assertIn(platform, commands["worker"])
            self.assertIn("exact-base", commands["criu"])
            self.assertIn("exact-base", commands["worker"])
            self.assertIn("exact-sysbox", commands["worker"])
            self.assertIn("exact-criu", commands["worker"])
            self.assertIn("--bounded-build", commands["sysbox"])
            self.assertIn("owned-builder", commands["sysbox"])
            self.assertIn("--discard-compiler-scratch", commands["worker"])
            for command in commands.values():
                self.assertNotIn("--push", command)
                self.assertNotIn("publish", command)

    def test_only_a_successful_exact_receipt_selects_the_next_input(self):
        receipt = {"passed": True, "evidence": "/owned/artifacts/exact"}
        self.assertEqual(build_worker_pipeline.completed_output(
            "compiler output\n" + json.dumps(receipt) + "\n"), receipt["evidence"])
        for output in ["", "{}", '{"passed":false,"evidence":"/owned/failed"}',
                       '{"passed":true}', '[]']:
            with self.subTest(output=output), self.assertRaisesRegex(ValueError, "receipt"):
                build_worker_pipeline.completed_output(output)
