import unittest
from pathlib import Path

import bounded_store_loss
import build_worker


class BoundedWorkerLossPlatformTests(unittest.TestCase):
    def test_real_recovery_probe_matches_exact_native_worker_architecture_and_kernel(self):
        for platform, machine, architecture in [
            ("linux/amd64", "x86_64", "amd64"), ("linux/arm64", "aarch64", "arm64")
        ]:
            host = {"OSType": "linux", "Architecture": machine, "KernelVersion": "exact-native"}
            worker = {"platform": platform}
            command, environment = bounded_store_loss.probe_build(worker, host, False, Path("/proof/probe.test"))
            self.assertEqual(environment["GOOS"], "linux")
            self.assertEqual(environment["GOARCH"], architecture)
            self.assertEqual(environment["CGO_ENABLED"], "0")
            self.assertIn("-tags=factorybox_worker_probe", command)
            worker["native_candidate"] = build_worker.native_candidate_profile(platform, host)
            command, _ = bounded_store_loss.probe_build(worker, host, True, Path("/proof/probe.test"))
            self.assertIn("-tags=factorybox_candidate_" + architecture + ",factorybox_worker_probe", command)
            self.assertIn(worker["native_candidate"]["compile_options"][1], command)
            with self.assertRaisesRegex(ValueError, "explicit"):
                bounded_store_loss.probe_build(worker, host, False, Path("/proof/probe.test"))
            with self.assertRaisesRegex(ValueError, "kernel"):
                bounded_store_loss.probe_build(worker, {**host, "KernelVersion": "foreign"}, True,
                                               Path("/proof/probe.test"))
            with self.assertRaisesRegex(ValueError, "matching Linux"):
                bounded_store_loss.probe_build(worker, {**host, "Architecture": "foreign"}, True,
                                               Path("/proof/probe.test"))
