import unittest
from pathlib import Path
import subprocess
from unittest.mock import patch

import bounded_store_loss
import build_worker


class BoundedWorkerLossPlatformTests(unittest.TestCase):
    def test_pending_kernel_observation_keeps_the_workers_stderr(self):
        completed = subprocess.CompletedProcess([], 0, stdout='ordinary output\n',
            stderr='Waiting for original bounded kernel association\n')
        with patch.object(bounded_store_loss.subprocess, 'run', return_value=completed) as execute:
            output = bounded_store_loss.worker_logs('exact-owned-worker')
        self.assertIn('ordinary output', output)
        self.assertIn('Waiting for original bounded kernel association', output)
        self.assertEqual(execute.call_args.args[0][-2:], ['logs', 'exact-owned-worker'])
        self.assertTrue(execute.call_args.kwargs['check'])

    def test_abrupt_recovery_requires_observed_withdrawal_before_new_admission(self):
        names = ['successor_unready_without_mount', 'only_original_descriptor_released',
                 'successor_admitted_after_release', 'removed']
        proof = dict.fromkeys(names, True)
        bounded_store_loss.require_abrupt_attachment_observation({'held_reference': proof})
        for key in names:
            for value in [False, None]:
                with self.subTest(key=key, value=value), self.assertRaisesRegex(ValueError, 'held-association'):
                    bounded_store_loss.require_abrupt_attachment_observation({
                        'held_reference': {**proof, key: value}})
        with self.assertRaisesRegex(ValueError, 'held-association'):
            bounded_store_loss.require_abrupt_attachment_observation({})

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
