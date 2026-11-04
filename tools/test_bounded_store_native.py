import hashlib
import json
import os
from pathlib import Path
import struct
import subprocess
import tempfile
import unittest
from unittest.mock import patch

import bounded_store_native
import build_worker
from verify_fixture_extensions import pidfd_fixture_before


class BoundedWorkerPlatformTests(unittest.TestCase):
    def test_backing_gate_requires_automatic_cleanup_and_every_preservation_guard(self):
        proof = {name: True for name in bounded_store_native.BACKING_RETIREMENT_PROOFS}
        rows = ['--- PASS: ' + name + ' (0.0s)' for name in bounded_store_native.BACKING_RETIREMENT_UNIT_TESTS]
        log = '\n'.join(rows)
        bounded_store_native.require_backing_retirement_coverage(proof, log)
        for key in proof:
            missing = {name: value for name, value in proof.items() if name != key}
            with self.subTest(missing=key), self.assertRaisesRegex(ValueError, 'omitted'):
                bounded_store_native.require_backing_retirement_coverage(missing, log)
        for index in range(len(rows)):
            with self.subTest(missing_test=index), self.assertRaisesRegex(ValueError, 'omitted'):
                bounded_store_native.require_backing_retirement_coverage(proof, '\n'.join(rows[:index] + rows[index+1:]))
        with self.assertRaisesRegex(ValueError, 'omitted'):
            bounded_store_native.require_backing_retirement_coverage(proof, log + '\n--- SKIP: required')

    def test_capacity_fixture_declaration_preserves_original_assertion_bytes(self):
        root = bounded_store_native.reference.ROOT
        before = pidfd_fixture_before(root)
        def contents(name):
            return before[name] if name in before else (root / name).read_bytes()
        declaration = json.loads((root / 'tests/compatibility/bounded-capacity-debug-info.json').read_text())
        self.assertEqual(declaration['schema'], 1)
        self.assertTrue(declaration['assertions_preserved'])
        self.assertFalse(declaration['upstream_assertions_modified'])
        extension = json.loads((root / 'tests/compatibility/deleted-manager-retirement.json').read_text())
        self.assertEqual(extension['schema'], 1)
        self.assertTrue(extension['assertions_preserved'])
        self.assertFalse(extension['upstream_assertions_modified'])
        self.assertEqual(extension['previous_manifest'], 'tests/compatibility/bounded-capacity-debug-info.json')
        self.assertEqual(hashlib.sha256((root / extension['previous_manifest']).read_bytes()).hexdigest(),
                         extension['previous_manifest_sha256'])
        successor = json.loads((root / 'tests/compatibility/failed-capture-retirement.json').read_text())
        self.assertEqual(successor['schema'], 1)
        self.assertTrue(successor['assertions_preserved'])
        self.assertFalse(successor['upstream_assertions_modified'])
        self.assertEqual(successor['previous_manifest'], 'tests/compatibility/deleted-manager-retirement.json')
        self.assertEqual(hashlib.sha256((root / successor['previous_manifest']).read_bytes()).hexdigest(),
                         successor['previous_manifest_sha256'])
        mixed = json.loads((root / 'tests/compatibility/mixed-criu-backing-retirement.json').read_text())
        self.assertEqual(mixed['schema'], 1)
        self.assertTrue(mixed['assertions_preserved'])
        self.assertFalse(mixed['upstream_assertions_modified'])
        self.assertEqual(mixed['previous_manifest'], 'tests/compatibility/failed-capture-retirement.json')
        self.assertEqual(hashlib.sha256((root / mixed['previous_manifest']).read_bytes()).hexdigest(),
                         mixed['previous_manifest_sha256'])
        incomplete = json.loads((root / 'tests/compatibility/deleted-incomplete-start-retirement.json').read_text())
        self.assertEqual(incomplete['schema'], 1)
        self.assertTrue(incomplete['assertions_preserved'])
        self.assertFalse(incomplete['upstream_assertions_modified'])
        self.assertEqual(incomplete['previous_manifest'], 'tests/compatibility/mixed-criu-backing-retirement.json')
        self.assertEqual(hashlib.sha256((root / incomplete['previous_manifest']).read_bytes()).hexdigest(),
                         incomplete['previous_manifest_sha256'])
        cow = json.loads((root / 'tests/compatibility/cow-retirement-capacity.json').read_text())
        self.assertEqual(cow['schema'], 1)
        self.assertTrue(cow['assertions_preserved'])
        self.assertFalse(cow['upstream_assertions_modified'])
        self.assertEqual(cow['previous_manifest'], 'tests/compatibility/deleted-incomplete-start-retirement.json')
        self.assertEqual(hashlib.sha256((root / cow['previous_manifest']).read_bytes()).hexdigest(),
                         cow['previous_manifest_sha256'])
        native = json.loads((root / 'tests/compatibility/native-epoch-retirement.json').read_text())
        self.assertEqual(native['schema'], 1)
        self.assertTrue(native['assertions_preserved'])
        self.assertFalse(native['upstream_assertions_modified'])
        self.assertEqual(native['previous_manifest'], 'tests/compatibility/cow-retirement-capacity.json')
        self.assertEqual(hashlib.sha256((root / native['previous_manifest']).read_bytes()).hexdigest(),
                         native['previous_manifest_sha256'])
        retained = json.loads((root / 'tests/compatibility/retained-pending-backing.json').read_text())
        self.assertEqual(retained['schema'], 1)
        self.assertTrue(retained['assertions_preserved'])
        self.assertFalse(retained['upstream_assertions_modified'])
        self.assertEqual(retained['previous_manifest'], 'tests/compatibility/native-epoch-retirement.json')
        self.assertEqual(hashlib.sha256((root / retained['previous_manifest']).read_bytes()).hexdigest(),
                         retained['previous_manifest_sha256'])
        withdrawal = json.loads((root / 'tests/compatibility/native-manager-withdrawal.json').read_text())
        self.assertEqual(withdrawal['schema'], 1)
        self.assertTrue(withdrawal['assertions_preserved'])
        self.assertFalse(withdrawal['upstream_assertions_modified'])
        self.assertEqual(withdrawal['previous_manifest'], 'tests/compatibility/retained-pending-backing.json')
        self.assertEqual(hashlib.sha256((root / withdrawal['previous_manifest']).read_bytes()).hexdigest(),
                         withdrawal['previous_manifest_sha256'])
        manager_root = json.loads((root / 'tests/compatibility/manager-root-withdrawal.json').read_text())
        self.assertEqual(manager_root['schema'], 1)
        self.assertTrue(manager_root['assertions_preserved'])
        self.assertFalse(manager_root['upstream_assertions_modified'])
        self.assertEqual(manager_root['previous_manifest'], 'tests/compatibility/native-manager-withdrawal.json')
        self.assertEqual(hashlib.sha256((root / manager_root['previous_manifest']).read_bytes()).hexdigest(),
                         manager_root['previous_manifest_sha256'])
        def after_manager_root(name, row):
            final = manager_root['files'].get(name, row)
            if name in manager_root['files']:
                self.assertEqual(final['before_sha256'], row['after_sha256'])
            return final
        def after_withdrawal(name, row):
            final = withdrawal['files'].get(name, row)
            if name in withdrawal['files']:
                self.assertEqual(final['before_sha256'], row['after_sha256'])
            return after_manager_root(name, final)
        def after_retained(name, row):
            final = retained['files'].get(name, row)
            if name in retained['files']:
                self.assertEqual(final['before_sha256'], row['after_sha256'])
            return after_withdrawal(name, final)
        def after_native(name, row):
            final = native['files'].get(name, row)
            if name in native['files']:
                self.assertEqual(final['before_sha256'], row['after_sha256'])
            return after_retained(name, final)
        def after_cow(name, row):
            final = cow['files'].get(name, row)
            if name in cow['files']:
                self.assertEqual(final['before_sha256'], row['after_sha256'])
            return after_native(name, final)
        def after_incomplete_start(name, row):
            current = incomplete['files'].get(name, row)
            if name in incomplete['files']:
                self.assertEqual(current['before_sha256'], row['after_sha256'])
            return after_cow(name, current)
        def latest(name, row):
            current = mixed['files'].get(name, row)
            if name in mixed['files']:
                self.assertEqual(current['before_sha256'], row['after_sha256'])
            return after_incomplete_start(name, current)
        def current(name, row):
            if name in successor['files']:
                final = successor['files'][name]
                self.assertEqual(final['before_sha256'], row['after_sha256'])
                return latest(name, final)
            return latest(name, row)
        for name, row in declaration['files'].items():
            if name in extension['files']:
                final = extension['files'][name]
                self.assertEqual(final['before_sha256'], row['after_sha256'])
                row = final
            self.assertEqual(hashlib.sha256(contents(name)).hexdigest(), current(name, row)['after_sha256'])
        for name, row in extension['files'].items():
            self.assertEqual(hashlib.sha256(contents(name)).hexdigest(), current(name, row)['after_sha256'])
        for name, row in successor['files'].items():
            self.assertEqual(hashlib.sha256(contents(name)).hexdigest(), latest(name, row)['after_sha256'])
        for name, row in mixed['files'].items():
            self.assertEqual(hashlib.sha256(contents(name)).hexdigest(), after_incomplete_start(name, row)['after_sha256'])
        for name, row in incomplete['files'].items():
            if name in mixed['files']:
                self.assertEqual(row['before_sha256'], mixed['files'][name]['after_sha256'])
            else:
                self.assertIsNone(row['before_sha256'])
            self.assertEqual(hashlib.sha256(contents(name)).hexdigest(), after_cow(name, row)['after_sha256'])
        for name, row in cow['files'].items():
            self.assertRegex(row['before_sha256'], r'^[0-9a-f]{64}$')
            self.assertEqual(hashlib.sha256(contents(name)).hexdigest(), after_native(name, row)['after_sha256'])
        for name, row in native['files'].items():
            self.assertRegex(row['before_sha256'], r'^[0-9a-f]{64}$')
            self.assertEqual(hashlib.sha256(contents(name)).hexdigest(), after_retained(name, row)['after_sha256'])
        for name, row in retained['files'].items():
            self.assertRegex(row['before_sha256'], r'^[0-9a-f]{64}$')
            self.assertEqual(hashlib.sha256(contents(name)).hexdigest(), after_withdrawal(name, row)['after_sha256'])
        for name, row in withdrawal['files'].items():
            self.assertRegex(row['before_sha256'], r'^[0-9a-f]{64}$')
            self.assertEqual(hashlib.sha256(contents(name)).hexdigest(), after_manager_root(name, row)['after_sha256'])
        for name, row in manager_root['files'].items():
            if name == 'tools/bounded_store_native.py':
                self.assertEqual(row['before_sha256'], retained['files'][name]['after_sha256'])
                original_runner = contents(name).decode()
                for added in ['TestNativeRetainedManagerRootWithdrawal',
                              'TestManagerWithdrawalRequiresRetainedOriginalAttachment']:
                    original_runner = original_runner.replace("    '" + added + "',\n", '', 1)
                self.assertEqual(hashlib.sha256(original_runner.encode()).hexdigest(), row['before_sha256'])
            else:
                self.assertIsNone(row['before_sha256'])
                self.assertIn(name, ['internal/lifecycle/retained_manager_root_native_linux_test.go',
                                    'internal/lifecycle/manager_withdrawal_linux_test.go'])
            self.assertEqual(hashlib.sha256(contents(name)).hexdigest(), row['after_sha256'])
        original = declaration['files']['internal/lifecycle/bounded_store_native_linux_test.go']
        self.assertEqual(original['before_sha256'], original['after_sha256'])

    def test_capacity_actor_omits_dwarf_without_changing_executable_code(self):
        def sections(path):
            data = path.read_bytes()
            self.assertEqual(data[:6], b'\x7fELF\x02\x01')
            table = struct.unpack_from('<Q', data, 40)[0]
            stride, count, names = struct.unpack_from('<HHH', data, 58)
            headers = [struct.unpack_from('<IIQQQQIIQQ', data, table + n * stride)
                       for n in range(count)]
            strings = data[headers[names][4]:headers[names][4] + headers[names][5]]
            return {strings[h[0]:].split(b'\0', 1)[0].decode(): data[h[4]:h[4] + h[5]]
                    for h in headers}

        with tempfile.TemporaryDirectory() as directory:
            outputs = [Path(directory) / name for name in ['diagnostic.test', 'capacity.test']]
            for output, capacity in zip(outputs, [False, True]):
                subprocess.run(bounded_store_native.probe_build_command(
                    output, ['-tags=factorybox_worker_probe'], capacity=capacity),
                    cwd=bounded_store_native.reference.ROOT,
                    env=dict(os.environ, GOOS='linux', CGO_ENABLED='0'),
                    check=True, capture_output=True, timeout=90)
            diagnostic, capacity = [sections(output) for output in outputs]
            self.assertIn('.debug_info', set(diagnostic))
            self.assertNotIn('.debug_info', set(capacity))
            self.assertIn('.symtab', set(capacity))
            self.assertEqual(hashlib.sha256(diagnostic['.text']).hexdigest(),
                             hashlib.sha256(capacity['.text']).hexdigest())
            self.assertLess(outputs[1].stat().st_size, outputs[0].stat().st_size)

    def test_capacity_actor_keeps_candidate_kernel_and_probe_tags(self):
        host = {'OSType': 'linux', 'Architecture': 'x86_64', 'KernelVersion': '7.2.8-test'}
        candidate = build_worker.native_candidate_profile('linux/amd64', host)
        flags = bounded_store_native.worker_compile_options(
            {'platform': 'linux/amd64', 'native_candidate': candidate}, host, native_candidate=True)
        original = list(flags)
        command = bounded_store_native.probe_build_command('probe.test', flags, capacity=True)
        linked = [flag for flag in command if flag.startswith('-ldflags=')]
        self.assertEqual(linked, [candidate['compile_options'][1] + ' -w'])
        self.assertIn(candidate['compile_options'][0] + ',factorybox_worker_probe', command)
        self.assertEqual(flags, original)

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
