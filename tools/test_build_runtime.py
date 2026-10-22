import copy
import unittest

import build_runtime


class RuntimeProfileTests(unittest.TestCase):
    def test_profile_refuses_mixed_binaries_platforms_patches_and_overlay_candidate(self):
        binaries = {name: character * 64 for name, character in
                    [("sysbox-runc", "a"), ("sysbox-fs", "b"), ("sysbox-mgr", "c"), ("criu", "d")]}
        profile = {"schema": 1, "status": "candidate", "binary_sha256": binaries,
                   "additional_criu_patches": [{"sha256": "e" * 64}],
                   "additional_criu_overlay": {"sha256": "f" * 64}}
        sysbox = {"platform": "linux/amd64", "binary_sha256": {k: v for k, v in binaries.items() if k != "criu"}}
        criu = {"platform": "linux/amd64", "binary_sha256": binaries["criu"],
                "exit_hold_socket_candidate": True, "candidate_patch_sha256": {"retained.patch": "e" * 64},
                "candidate_overlay_sha256": {"exit-observer.h": "f" * 64}}
        build_runtime.profile_inputs(profile, sysbox, criu)
        for change in [{"platform": "linux/arm64"}, {"binary_sha256": "0" * 64},
                       {"exit_hold_socket_candidate": False}, {"nested_overlay_candidate": True},
                       {"candidate_patch_sha256": {"retained.patch": "0" * 64}},
                       {"candidate_overlay_sha256": {"exit-observer.h": "0" * 64}}]:
            with self.subTest(change=change), self.assertRaises(ValueError):
                altered = copy.deepcopy(criu)
                altered.update(change)
                build_runtime.profile_inputs(profile, sysbox, altered)
        altered = copy.deepcopy(sysbox)
        altered["binary_sha256"]["sysbox-runc"] = "0" * 64
        with self.assertRaises(ValueError):
            build_runtime.profile_inputs(profile, altered, criu)


if __name__ == "__main__":
    unittest.main()
