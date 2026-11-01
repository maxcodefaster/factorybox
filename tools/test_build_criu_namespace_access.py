from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

import build_criu_shared_fds as build
import reference


class NamespaceValidationDeltaTests(unittest.TestCase):
    def test_exact_delta_follows_the_complete_shared_description_patch(self):
        previous = {"candidate_patch_sha256": {"shared-fds.patch": reference.sha(build.PATCH)}}
        self.assertEqual(build.delta_recipe(True, previous),
                         (build.NAMESPACE_PATCH, "namespace-access", ["criu/namespaces.c"]))
        selected, name, changed = build.delta_recipe(False, {})
        self.assertEqual((selected, name), (build.PATCH, "shared-fds"))
        self.assertEqual(len(changed), 8)

    def test_incomplete_base_or_additional_source_changes_are_not_selected(self):
        for previous in [{}, {"candidate_patch_sha256": {"shared-fds.patch": "changed"}}]:
            with self.assertRaisesRegex(ValueError, "exact existing"):
                build.delta_recipe(True, previous)
        previous = {"candidate_patch_sha256": {"shared-fds.patch": reference.sha(build.PATCH)}}
        with tempfile.TemporaryDirectory() as directory:
            supplied = Path(directory) / "namespace.patch"
            for targets in [["criu/pie/restorer.c"], ["../outside"],
                            ["criu/namespaces.c", "criu/pie/restorer.c"]]:
                supplied.write_text("\n".join("+++ b/" + target for target in targets))
                with patch.object(build, "NAMESPACE_PATCH", supplied), \
                     self.assertRaisesRegex(ValueError, "undeclared source boundary"):
                    build.delta_recipe(True, previous)

    def test_packet_delta_follows_both_prior_patches_without_rewriting_them(self):
        previous = {"candidate_patch_sha256": {
            "shared-fds.patch": reference.sha(build.PATCH),
            "namespace-access.patch": reference.sha(build.NAMESPACE_PATCH),
        }}
        self.assertEqual(build.delta_recipe(False, previous, descriptor_packets=True),
                         (build.PACKETS_PATCH, "shared-fds-packets", ["criu/files.c", "criu/crtools.c"]))
        for missing in previous["candidate_patch_sha256"]:
            changed = {"candidate_patch_sha256": {name: digest for name, digest in
                       previous["candidate_patch_sha256"].items() if name != missing}}
            with self.subTest(missing=missing), self.assertRaisesRegex(ValueError, "exact existing"):
                build.delta_recipe(False, changed, descriptor_packets=True)
        with self.assertRaisesRegex(ValueError, "cannot be combined"):
            build.delta_recipe(True, previous, descriptor_packets=True)
        with tempfile.TemporaryDirectory() as directory:
            supplied = Path(directory) / "packets.patch"
            supplied.write_text("+++ b/criu/files.c\n+++ b/criu/pie/restorer.c\n")
            with patch.object(build, "PACKETS_PATCH", supplied), \
                    self.assertRaisesRegex(ValueError, "undeclared source boundary"):
                build.delta_recipe(False, previous, descriptor_packets=True)
