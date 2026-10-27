import hashlib
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

import bounded_store_native
import build_worker


class BoundedWorkerPlatformTests(unittest.TestCase):
    def test_restore_hold_preserves_original_guards_and_requires_each_public_resume(self):
        cases = {}
        for mode in ['fork', 'resumed', 'fork-disconnect', 'resumed-disconnect', 'wrong-ack']:
            cases[mode] = {'held_before_ack': True, 'application_counter_unchanged': True}
            if mode in ['fork', 'resumed']:
                cases[mode].update({'held_after_criu_detach': True, 'explicit_thaw_runs_application': True})
            else:
                cases[mode]['failed_closed'] = True
        public = {
            'passed': True, 'no_consumer_or_peer': True, 'cycles': 20,
            'observations': [{'cycle': n, 'passed': True} for n in range(1, 21)],
        }
        proof = {'passed': True, 'cases': cases}
        bounded_store_native.require_restore_hold_coverage(proof, public)
        for mode, case in cases.items():
            missing = json.loads(json.dumps(proof))
            del missing['cases'][mode]
            with self.subTest(missing=mode), self.assertRaises(ValueError):
                bounded_store_native.require_restore_hold_coverage(missing, public)
            for key, value in case.items():
                if value is True:
                    changed = json.loads(json.dumps(proof))
                    changed['cases'][mode][key] = False
                    with self.subTest(mode=mode, guard=key), self.assertRaises(ValueError):
                        bounded_store_native.require_restore_hold_coverage(changed, public)
        for key in ['passed', 'no_consumer_or_peer']:
            with self.subTest(public=key), self.assertRaises(ValueError):
                bounded_store_native.require_restore_hold_coverage(proof, {**public, key: False})
        for index in range(20):
            changed = json.loads(json.dumps(public))
            changed['observations'][index]['passed'] = False
            with self.subTest(cycle=index), self.assertRaises(ValueError):
                bounded_store_native.require_restore_hold_coverage(proof, changed)
        changed = json.loads(json.dumps(public))
        changed['observations'].pop()
        with self.assertRaises(ValueError):
            bounded_store_native.require_restore_hold_coverage(proof, changed)

    def test_exec_trace_requires_the_exact_worker_base_and_unchanged_files(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory).resolve()
            name = "factorybox-exec-trace-inputs-" + "a" * 32
            source = root / "artifacts" / name
            binary = source / "rootfs/usr/bin/strace"
            binary.parent.mkdir(parents=True)
            binary.write_bytes(b"frozen diagnostic executable")
            binary.chmod(0o755)
            worker = {"platform": "linux/arm64", "native_base_build": "exact-base",
                      "base_packages_sha256": "exact-packages", "native_base_image": "exact-image"}
            receipt = {key: worker[key] for key in
                       ["platform", "native_base_build", "base_packages_sha256"]}
            receipt.update({"passed": True, "cleanup": True, "image": "exact-image",
                "files": {"usr/bin/strace": {"sha256": hashlib.sha256(binary.read_bytes()).hexdigest(),
                                            "mode": 0o755}}})
            (source / "result.json").write_text(json.dumps(receipt))
            target = root / "run"
            target.mkdir()
            with patch.object(bounded_store_native.reference, "ROOT", root):
                for key in worker:
                    with self.subTest(changed=key), self.assertRaisesRegex(ValueError, "exact worker base"):
                        bounded_store_native.freeze_exec_trace(name, {**worker, key: "changed"}, target)
                frozen = bounded_store_native.freeze_exec_trace(name, worker, target)
                self.assertEqual(frozen["files"], receipt["files"])
                self.assertEqual((target / "exec-trace/usr/bin/strace").read_bytes(), binary.read_bytes())
                binary.write_bytes(b"changed")
                with self.assertRaisesRegex(ValueError, "diagnostic file identity differs"):
                    bounded_store_native.freeze_exec_trace(name, worker, target)

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
