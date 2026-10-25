import json
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch

import consumer_worker_loss
import reference


class DockerSysctlObservationTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name).resolve()
        (self.root / "tests").mkdir()
        (self.root / "tests/global-sysctl-observation.py").write_text(
            'NAMES = ["kernel/panic", "vm/overcommit_memory"]\n')
        self.artifact = self.root / "artifacts/factorybox-owned"
        self.artifact.mkdir(parents=True)
        self.owner = "a" * 32
        self.image = "owned-base@sha256:" + "b" * 64
        self.addCleanup(patch.stopall)
        patch.object(reference, "ROOT", self.root).start()
        self.remove = patch.object(reference, "owned_delete").start()
        self.run = patch.object(consumer_worker_loss.subprocess, "run", return_value=
                                subprocess.CompletedProcess([], 0, json.dumps({
                                    "kernel/panic": "0", "vm/overcommit_memory": "1"}), "")).start()

    def observe(self, **overrides):
        return consumer_worker_loss.sysctls(**{
            "docker_image": self.image, "owner": self.owner,
            "artifact": self.artifact, "phase": "before", **overrides})

    def test_exact_read_only_docker_host_observation_and_owned_cleanup(self):
        self.assertEqual(self.observe(), {"kernel/panic": "0", "vm/overcommit_memory": "1"})
        command = self.run.call_args.args[0]
        self.assertNotIn("--mount", command)
        self.assertIn("pathlib.Path('/proc/sys')", command[-1])
        for flag in ["--read-only", "--network=none", "--cap-drop=ALL", "--pull=never", "--userns=host"]:
            self.assertIn(flag, command)
        self.assertNotIn("--privileged", command)
        self.assertEqual(command[-3:-1], [self.image, "-c"])
        self.remove.assert_called_once_with(
            "container", "factorybox-sysctl-observation-" + self.owner + "-before", self.owner)
        saved = json.loads((self.artifact / "global-sysctls-before.json").read_text())
        self.assertTrue(saved["passed"] and saved["cleanup"])

    def test_missing_or_invalid_values_fail_without_weakening_the_sentinel_set(self):
        for values in [{"kernel/panic": "0"}, {"kernel/panic": 0, "vm/overcommit_memory": "1"}]:
            self.run.return_value.stdout = json.dumps(values)
            with self.assertRaisesRegex(ValueError, "sentinel"):
                self.observe()
            saved = json.loads((self.artifact / "global-sysctls-before.json").read_text())
            self.assertFalse(saved["passed"])
            self.assertTrue(saved["cleanup"])

    def test_absent_kernel_interface_is_retained_as_in_the_original_observer(self):
        values = {"kernel/panic": "0", "vm/overcommit_memory": None}
        self.run.return_value.stdout = json.dumps(values)
        self.assertEqual(self.observe(), values)
        saved = json.loads((self.artifact / "global-sysctls-before.json").read_text())
        self.assertEqual(saved["unavailable_sentinels"], ["vm/overcommit_memory"])
        self.assertTrue(saved["passed"])
        script = self.run.call_args.args[0][-1]
        self.assertIn("except FileNotFoundError:", script)
        self.assertNotIn("except PermissionError", script)

    def test_failed_observer_still_removes_only_its_owned_container(self):
        self.run.return_value = subprocess.CompletedProcess([], 1, "", "failure")
        with self.assertRaises(subprocess.CalledProcessError):
            self.observe()
        self.remove.assert_called_once()
        saved = json.loads((self.artifact / "global-sysctls-before.json").read_text())
        self.assertFalse(saved["passed"])
        self.assertTrue(saved["cleanup"])

    def test_unpinned_or_redirected_observation_is_refused_before_docker(self):
        redirected = self.root / "redirected"
        redirected.symlink_to(self.artifact)
        for overrides in [{"docker_image": "floating:latest"}, {"owner": "foreign"},
                          {"phase": "write"}, {"artifact": redirected}, {"artifact": self.root}]:
            with self.subTest(overrides=overrides), self.assertRaises(ValueError):
                self.observe(**overrides)
        self.run.assert_not_called()
        self.remove.assert_not_called()
