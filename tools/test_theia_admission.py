import contextlib
import json
from pathlib import Path
import subprocess
import tempfile
from types import SimpleNamespace
import unittest
from unittest import mock

import theia_admission
import theia_peer


class TheiaAdmissionTests(unittest.TestCase):
    @contextlib.contextmanager
    def fixture(self, run_error=None, cleanup_error=None):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            owner = "a" * 32
            name = "factorybox-theia-admission-" + owner
            archive = root / name
            image = {"image": "fixture@sha256:" + "b" * 64, "id": "sha256:" + "b" * 64}
            observed = {
                "Name": "/" + name, "Image": image["id"],
                "Config": {"User": "0:0", "Labels": {"com.factorybox.run": owner}},
                "State": {"Running": False, "OOMKilled": False, "ExitCode": 0},
                "HostConfig": {
                    "NanoCpus": 1_000_000_000, "Memory": 256 << 20, "PidsLimit": 32,
                    "ReadonlyRootfs": True, "Privileged": False, "NetworkMode": "none",
                    "CapDrop": ["ALL"], "CapAdd": ["CAP_CHOWN", "CAP_DAC_OVERRIDE"],
                    "SecurityOpt": ["no-new-privileges"],
                },
            }
            def docker(args):
                if args == ["container", "inspect", name]:
                    return json.dumps([observed])
                if args[:2] == ["volume", "create"]:
                    return name
                if args[:2] == ["ps", "-a"] or args[:2] == ["volume", "ls"]:
                    return ""
                raise AssertionError("unexpected external operation " + str(args))
            with mock.patch.object(theia_admission.uuid, "uuid4",
                                   return_value=SimpleNamespace(hex=owner)), \
                    mock.patch.object(theia_admission.reference, "new_artifact",
                                      return_value=archive), \
                    mock.patch.object(theia_admission.reference, "docker", side_effect=docker), \
                    mock.patch.object(theia_peer.subprocess, "run", side_effect=run_error,
                                      return_value=subprocess.CompletedProcess([], 0)) as run, \
                    mock.patch.object(theia_admission.reference, "owned_delete",
                                      side_effect=cleanup_error) as remove:
                yield image, archive, observed, run, remove

    def test_original_recipe_deadline_and_limits_precede_a_positive_receipt(self):
        with self.fixture() as (image, archive, observed, run, remove):
            receipt = theia_admission.qualify(image)
            saved = json.loads((archive / "result.json").read_text())
            self.assertTrue(saved["passed"] and saved["cleanup"] and saved["inputs_unchanged"])
            self.assertFalse(saved["production_qualified"])
            command = saved["setup_command"]
            for flag in ["--cpus=1", "--memory=256m", "--pids-limit=32", "--network=none",
                         "--read-only", "--user=0:0", "--cap-drop=ALL", "--cap-add=CHOWN",
                         "--cap-add=DAC_OVERRIDE", "--security-opt=no-new-privileges"]:
                self.assertIn(flag, command)
            self.assertEqual(run.call_args.kwargs["timeout"], 30)
            self.assertTrue(run.call_args.kwargs["check"])
            self.assertEqual(receipt["image_id"], image["id"])
            self.assertEqual(receipt["result_sha256"],
                             theia_admission.reference.sha(archive / "result.json"))
            self.assertEqual([call.args[0] for call in remove.call_args_list], ["container", "volume"])

    def test_timeout_keeps_original_error_output_and_cleans_only_exact_owned_resources(self):
        failure = subprocess.TimeoutExpired(["docker"], 30, output=b"before start", stderr=b"pending")
        with self.fixture(run_error=failure) as (image, archive, observed, run, remove):
            with self.assertRaisesRegex(ValueError, "before developer rootfs staging") as raised:
                theia_admission.qualify(image)
            self.assertIs(raised.exception.__cause__, failure)
            saved = json.loads((archive / "result.json").read_text())
            self.assertFalse(saved["passed"])
            self.assertTrue(saved["cleanup"])
            self.assertEqual(saved["setup_failure"]["type"], "TimeoutExpired")
            self.assertEqual((archive / "setup-stdout.log").read_bytes(), b"before start")
            self.assertEqual((archive / "setup-stderr.log").read_bytes(), b"pending")
            for call in remove.call_args_list:
                self.assertEqual(call.args[1], archive.name)
                self.assertEqual(call.args[2], "a" * 32)

    def test_foreign_cleanup_failure_is_not_admission_or_permission_to_delete_the_volume(self):
        with self.fixture(cleanup_error=ValueError("foreign owner")) as (image, archive, observed, run, remove):
            with self.assertRaisesRegex(ValueError, "admission failed"):
                theia_admission.qualify(image)
            saved = json.loads((archive / "result.json").read_text())
            self.assertFalse(saved["passed"] or saved["cleanup"])
            self.assertIn("foreign owner", saved["cleanup_error"])
            self.assertEqual(remove.call_count, 1)
            self.assertEqual(remove.call_args.args[0], "container")

    def test_changed_limits_or_frozen_inputs_cannot_publish_a_positive_receipt(self):
        with self.fixture() as (image, archive, observed, run, remove):
            observed["HostConfig"]["Privileged"] = True
            with self.assertRaisesRegex(ValueError, "admission failed"):
                theia_admission.qualify(image)
            self.assertFalse(json.loads((archive / "result.json").read_text())["passed"])
        with self.fixture() as (image, archive, observed, run, remove):
            def change_frozen_input(*args, **kwargs):
                (archive / "theia_peer.py").write_text("changed")
                return subprocess.CompletedProcess([], 0)
            run.side_effect = change_frozen_input
            with self.assertRaisesRegex(ValueError, "admission failed"):
                theia_admission.qualify(image)
            saved = json.loads((archive / "result.json").read_text())
            self.assertFalse(saved["passed"] or saved["inputs_unchanged"])

    def test_unreconciled_admission_container_blocks_another_probe_before_creates(self):
        name = "factorybox-theia-admission-" + "a" * 32
        with mock.patch.object(theia_admission.reference, "docker", return_value=name), \
                mock.patch.object(theia_admission.reference, "new_artifact",
                                  side_effect=AssertionError("probe created before retained-resource check")) as create:
            with self.assertRaisesRegex(ValueError, "unreconciled IDE admission"):
                theia_admission.qualify({})
            create.assert_not_called()

    def test_reference_failure_blocks_resource_checks_and_creates(self):
        with mock.patch.object(theia_admission.reference, "verify",
                               side_effect=ValueError("reference differs")), \
                mock.patch.object(theia_admission.reference, "docker") as docker, \
                mock.patch.object(theia_admission.reference, "new_artifact") as create:
            with self.assertRaisesRegex(ValueError, "reference differs"):
                theia_admission.qualify({})
            docker.assert_not_called()
            create.assert_not_called()
