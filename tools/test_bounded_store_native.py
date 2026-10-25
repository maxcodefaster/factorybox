import unittest

import bounded_store_native
import build_worker


class BoundedWorkerPlatformTests(unittest.TestCase):
    def test_ordinary_worker_has_no_candidate_flags_and_requires_native_architecture(self):
        for platform, machine in [("linux/amd64", "x86_64"), ("linux/arm64", "aarch64")]:
            worker = {"platform": platform}
            host = {"OSType": "linux", "Architecture": machine, "KernelVersion": "7.0.14-test"}
            self.assertEqual(bounded_store_native.worker_compile_options(worker, host),
                             ["-tags=factorybox_worker_probe"])
            with self.assertRaisesRegex(ValueError, "matching Linux"):
                bounded_store_native.worker_compile_options(worker, {**host, "Architecture": "foreign"})
            with self.assertRaisesRegex(ValueError, "delivered worker"):
                bounded_store_native.worker_compile_options(worker, host, native_candidate=True)

    def test_candidate_is_explicit_and_bound_to_the_delivered_kernel(self):
        for platform, machine in [("linux/amd64", "x86_64"), ("linux/arm64", "aarch64")]:
            host = {"OSType": "linux", "Architecture": machine, "KernelVersion": "7.0.14-test"}
            candidate = build_worker.native_candidate_profile(platform, host)
            worker = {"platform": platform, "native_candidate": candidate}
            flags = bounded_store_native.worker_compile_options(worker, host, native_candidate=True)
            self.assertEqual(flags, [
                candidate["compile_options"][0] + ",factorybox_worker_probe",
                candidate["compile_options"][1]])
            self.assertEqual(sum(flag.startswith("-tags=") for flag in flags), 1)
            with self.assertRaisesRegex(ValueError, "explicit"):
                bounded_store_native.worker_compile_options(worker, host)
            with self.assertRaisesRegex(ValueError, "exact current kernel"):
                bounded_store_native.worker_compile_options(worker, {**host, "KernelVersion": "another"},
                                                            native_candidate=True)
            with self.assertRaisesRegex(ValueError, "matching Linux"):
                bounded_store_native.worker_compile_options(worker, {**host, "Architecture": "foreign"},
                                                            native_candidate=True)
