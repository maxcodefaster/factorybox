from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

import build_criu_shared_fds as build
import reference


class PidfdTargetDeltaTests(unittest.TestCase):
    def base(self):
        return {
            "candidate_patch_sha256": {
                "shared-fds.patch": reference.sha(build.PATCH),
                "namespace-access.patch": reference.sha(build.NAMESPACE_PATCH),
                "shared-fds-packets.patch": reference.sha(build.PACKETS_PATCH),
                "shared-filemap-access.patch": reference.sha(build.MAPPING_PATCH),
            },
            "features": {"schema": 1, "shared_file_transfer_version": 2,
                         "shared_file_descriptions": True, "shared_source_directories": True},
        }

    def test_exact_task_binding_is_an_ordered_delta_with_explicit_image_boundary(self):
        self.assertEqual(build.delta_recipe(False, self.base(), pidfd_targets=True),
                         (build.PIDFD_PATCH, "pidfd-target-binding",
                          ["images/pidfd.proto", "criu/include/pidfd.h",
                           "criu/files.c", "criu/pidfd.c"]))

    def test_every_prior_delta_is_required(self):
        previous = self.base()
        for missing in previous["candidate_patch_sha256"]:
            changed = {**previous, "candidate_patch_sha256": {
                name: value for name, value in previous["candidate_patch_sha256"].items()
                if name != missing}}
            with self.subTest(missing=missing), self.assertRaises(ValueError):
                build.delta_recipe(False, changed, pidfd_targets=True)

    def test_no_combined_mode_or_additional_source_can_enter_the_build(self):
        for mode in ["namespace_access", "descriptor_packets", "mapping_access"]:
            arguments = {"namespace_access": False, "pidfd_targets": True, mode: True}
            with self.subTest(mode=mode), self.assertRaisesRegex(ValueError, "cannot be combined"):
                build.delta_recipe(previous=self.base(), **arguments)
        with tempfile.TemporaryDirectory() as directory:
            supplied = Path(directory) / "pidfd.patch"
            for targets in [
                ["images/pidfd.proto", "criu/include/pidfd.h", "criu/files.c", "../outside"],
                ["images/pidfd.proto", "criu/include/pidfd.h", "criu/files.c", "criu/pie/restorer.c"],
                ["images/pidfd.proto", "criu/include/pidfd.h", "criu/files.c", "criu/pidfd.c",
                 "criu/crtools.c"],
            ]:
                supplied.write_text("\n".join("+++ b/" + target for target in targets))
                with patch.object(build, "PIDFD_PATCH", supplied), \
                        self.assertRaisesRegex(ValueError, "undeclared source boundary"):
                    build.delta_recipe(False, self.base(), pidfd_targets=True)
