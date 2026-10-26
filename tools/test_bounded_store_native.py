import hashlib
from pathlib import Path
import tempfile
import unittest

import bounded_store_native
import build_worker


class BoundedWorkerPlatformTests(unittest.TestCase):
    def test_detach_fixture_uses_the_exact_image_runtime_without_host_ownership(self):
        with tempfile.TemporaryDirectory() as directory:
            worker = Path(directory).resolve()
            runtime = worker / "image-context/bundle/components/sysbox-runc"
            runtime.parent.mkdir(parents=True)
            runtime.write_bytes(b"exact fixture runtime")
            expected = hashlib.sha256(runtime.read_bytes()).hexdigest()
            receipt = {"bundle_sha256": {"components/sysbox-runc": expected}}
            self.assertEqual(bounded_store_native.detach_fixture_arguments(worker, receipt), [
                "--env", "FACTORYBOX_RUNTIME_SHA256=" + expected])
            runtime.write_bytes(b"changed fixture runtime")
            with self.assertRaisesRegex(ValueError, "exact delivered"):
                bounded_store_native.detach_fixture_arguments(worker, receipt)

    def test_detach_gate_requires_all_original_cycles_and_negative_controls(self):
        prefix = "--- PASS: TestNativeTimerDetachCompletion"
        rows = [prefix + " (1.0s)",
                "256 actual detach completions without a diagnostic pre-read",
                *[prefix + "/" + name + " (0.0s)" for name in
                  ["foreign-thaw-refused", "changed-source-refused", "live-tracer-refused"]]]
        proof = bounded_store_native.require_detach_coverage("\n".join(rows))
        self.assertEqual(proof["cycles"], 256)
        self.assertEqual(len(proof["negative_controls"]), 3)
        for index in range(len(rows)):
            with self.subTest(removed=index), self.assertRaisesRegex(ValueError, "omitted"):
                bounded_store_native.require_detach_coverage("\n".join(rows[:index] + rows[index+1:]))
        with self.assertRaisesRegex(ValueError, "omitted"):
            bounded_store_native.require_detach_coverage("\n".join([*rows, "--- SKIP: required"]))

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
