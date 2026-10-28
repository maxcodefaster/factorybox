import argparse
import json
import unittest

import build_worker_pipeline
import build_criu_shared_fds
import criu_candidate
import reference


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
            self.assertEqual(commands["shared-fds"][1:],
                             ["tools/build_criu_shared_fds.py", "--base-candidate", "exact-criu"])
            self.assertEqual(commands["namespace-access"][1:],
                             ["tools/build_criu_shared_fds.py", "--base-candidate", "exact-criu",
                              "--namespace-access"])
            self.assertIn("--bounded-build", commands["sysbox"])
            self.assertIn("owned-builder", commands["sysbox"])
            self.assertIn("--discard-compiler-scratch", commands["worker"])
            self.assertEqual(commands["recovery"][1:],
                             ["tools/build_criu_recovery.py", "--base-candidate", "exact-criu"])
            for command in commands.values():
                self.assertNotIn("--push", command)
                self.assertNotIn("publish", command)

    def test_reuse_requires_every_exact_recovery_correction_and_the_base_failure_fix(self):
        patches = {name: reference.sha(path)
                   for name, path in criu_candidate.RECOVERY_CORRECTIONS.items()}
        key = str(criu_candidate.PSTREE_CORRECTION.relative_to(reference.ROOT))
        component = {"local_inputs_sha256": {
            key: reference.sha(criu_candidate.PSTREE_CORRECTION)}}
        complete = {"candidate_patch_sha256": patches}
        criu_candidate.require_recovery_corrections(complete, component)
        for name in patches:
            for value in [None, "0" * 64]:
                changed = dict(patches)
                if value is None:
                    del changed[name]
                else:
                    changed[name] = value
                with self.subTest(patch=name, value=value), self.assertRaisesRegex(ValueError, "recovery corrections"):
                    criu_candidate.require_recovery_corrections(
                        {"candidate_patch_sha256": changed, "recovery_corrections": True}, component)
        for changed in [{}, {"local_inputs_sha256": {key: "0" * 64}}]:
            with self.assertRaisesRegex(ValueError, "process-tree failure correction"):
                criu_candidate.require_recovery_corrections(complete, changed)
        with self.assertRaisesRegex(ValueError, "recovery corrections"):
            criu_candidate.require_recovery_corrections({"recovery_corrections": True}, component)

    def test_only_the_exact_full_shared_source_component_can_skip_the_delta_build(self):
        digest = reference.sha(build_criu_shared_fds.PATCH)
        final = {"candidate_patch_sha256": {"shared-fds.patch": digest},
                 "features": {"schema": 1, "shared_file_descriptions": True,
                              "shared_source_directories": True}}
        self.assertEqual(build_worker_pipeline.shared_source_input("exact-complete", final),
                         "exact-complete")
        self.assertIsNone(build_worker_pipeline.shared_source_input("exact-base", {}))
        for changed in [
            {**final, "features": {"schema": 1, "shared_file_descriptions": True}},
            {**final, "features": {"schema": 1, "shared_file_descriptions": True,
                                  "shared_source_directories": 1}},
            {**final, "candidate_patch_sha256": {"shared-fds.patch": "0" * 64}},
        ]:
            with self.assertRaisesRegex(ValueError, "shared-source"):
                build_worker_pipeline.shared_source_input("not-qualified", changed)

    def test_only_a_successful_exact_receipt_selects_the_next_input(self):
        receipt = {"passed": True, "evidence": "/owned/artifacts/exact"}
        self.assertEqual(build_worker_pipeline.completed_output(
            "compiler output\n" + json.dumps(receipt) + "\n"), receipt["evidence"])
        for output in ["", "{}", '{"passed":false,"evidence":"/owned/failed"}',
                       '{"passed":true}', '[]']:
            with self.subTest(output=output), self.assertRaisesRegex(ValueError, "receipt"):
                build_worker_pipeline.completed_output(output)

    def test_only_the_exact_namespace_patch_can_reuse_a_completed_input(self):
        digest = reference.sha(build_criu_shared_fds.NAMESPACE_PATCH)
        self.assertEqual(build_worker_pipeline.namespace_access_input(
            "exact", {"candidate_patch_sha256": {"namespace-access.patch": digest}}), "exact")
        self.assertIsNone(build_worker_pipeline.namespace_access_input("old", {}))
        with self.assertRaisesRegex(ValueError, "namespace-validation"):
            build_worker_pipeline.namespace_access_input(
                "changed", {"candidate_patch_sha256": {"namespace-access.patch": "0" * 64}})
