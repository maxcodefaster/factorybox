from pathlib import Path
import tempfile
import unittest

import build_criu_recovery
import criu_candidate
import reference


class RecoveryBuildTests(unittest.TestCase):
    def component(self):
        return {"local_inputs_sha256": {
            str(criu_candidate.PSTREE_CORRECTION.relative_to(reference.ROOT)):
                reference.sha(criu_candidate.PSTREE_CORRECTION)}}

    def test_exact_corrections_keep_the_declared_production_boundary(self):
        patches = build_criu_recovery.selected_patches({}, self.component())
        self.assertEqual(patches, list(criu_candidate.RECOVERY_CORRECTIONS.items()))
        self.assertEqual(build_criu_recovery.targets(patches), {
            "criu/Makefile.crtools", "criu/cr-restore.c", "criu/include/nested-ns.h",
            "criu/include/pstree.h", "criu/include/rst_info.h", "criu/nested-ns.c",
            "criu/nested-zombie.c", "criu/pstree.c", "images/pstree.proto"})

    def test_missing_base_and_partial_or_completed_delta_are_rejected(self):
        with self.assertRaisesRegex(ValueError, "process-tree failure correction"):
            build_criu_recovery.selected_patches({}, {})
        for name, patch in criu_candidate.RECOVERY_CORRECTIONS.items():
            with self.subTest(patch=name), self.assertRaisesRegex(ValueError, "plain retained input"):
                build_criu_recovery.selected_patches(
                    {"candidate_patch_sha256": {name: reference.sha(patch)}}, self.component())
        with self.assertRaisesRegex(ValueError, "plain retained input"):
            build_criu_recovery.selected_patches({"candidate_patch_sha256": {
                name: reference.sha(patch) for name, patch in criu_candidate.RECOVERY_CORRECTIONS.items()
            }}, self.component())

    def test_patch_targets_cannot_change_assertions_or_escape_the_context(self):
        for target in ["tests/assertions.c", "../criu/source.c", "/tmp/source.c"]:
            with self.subTest(target=target), tempfile.TemporaryDirectory() as directory:
                patch = Path(directory) / "fixture.patch"
                patch.write_text("--- a/old\n+++ b/" + target + "\n")
                with self.assertRaisesRegex(ValueError, "source boundary"):
                    build_criu_recovery.targets([("fixture.patch", patch)])
        with self.assertRaisesRegex(ValueError, "no production targets"):
            build_criu_recovery.targets([])
