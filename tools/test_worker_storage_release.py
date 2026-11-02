import json
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest import mock

import worker_storage_release


class WorkerStorageReleaseTests(unittest.TestCase):
    def test_observation_preserves_known_refusals_until_actual_release(self):
        epoch = "b" * 32
        pending = {"passed": False, "cleanup": True, "helper_exit": {
            "running": False, "oom_killed": False, "command_exit": 1, "exit_code": 1}}
        calls = []
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            def verify(worker, volume, owner, status, artifact):
                calls.append(artifact)
                if len(calls) == 1:
                    path = artifact / ("storage-release-" + epoch)
                    path.with_suffix(".json").write_text(json.dumps(pending))
                    path.with_suffix(".stderr").write_text(
                        "fixed filesystem still has an image owner: resource temporarily unavailable\n")
                    raise ValueError("unconfirmed")
                return {"passed": True, "cleanup": True, "kernel_state_changed": False}
            with mock.patch.object(worker_storage_release, "verify", side_effect=verify), \
                 mock.patch.object(worker_storage_release.time, "sleep"), \
                 mock.patch.object(worker_storage_release.time, "monotonic", return_value=1):
                result = worker_storage_release.observe({}, "volume", "owner",
                    {"epoch": epoch}, root, deadline=10)
            self.assertEqual(result["observations"], 2)
            self.assertTrue(result["passed"])
            self.assertFalse(result["kernel_state_changed"])
            self.assertEqual([path.name for path in calls], ["0", "1"])
            original = calls[0] / ("storage-release-" + epoch + ".json")
            self.assertEqual(json.loads(original.read_text()), pending)

    def test_observation_does_not_repeat_unknown_errors_or_expired_admission(self):
        epoch = "b" * 32
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            with mock.patch.object(worker_storage_release, "verify", side_effect=ValueError("changed image")) as verify, \
                 mock.patch.object(worker_storage_release.time, "monotonic", return_value=1):
                with self.assertRaisesRegex(ValueError, "changed image"):
                    worker_storage_release.observe({}, "volume", "owner", {"epoch": epoch}, root, deadline=10)
                self.assertEqual(verify.call_count, 1)
            with mock.patch.object(worker_storage_release, "verify") as verify, \
                 mock.patch.object(worker_storage_release.time, "monotonic", return_value=10):
                with self.assertRaises(TimeoutError):
                    worker_storage_release.observe({}, "volume", "owner", {"epoch": epoch}, root, deadline=10)
                verify.assert_not_called()
            with self.assertRaises(ValueError):
                worker_storage_release.observe({}, "volume", "owner", {"epoch": "../foreign"}, root, deadline=10)

    def scenario(self, changed=None, foreign_owner=False, command_failed=False):
        owner, epoch = "a" * 32, "b" * 32
        image = {"owner": owner, "path": "/var/lib/sysbox/fixed-store/store.img",
                 "device": 41, "inode": 123, "bytes": 512 << 20}
        status = {"store_id": owner, "epoch": epoch, "lifecycle": {"bounded_store": {"image": image}}}
        observed = {"store_id": owner, "image": image.copy(), "kernel_release_verified": True,
                    "completed_epochs": [epoch]}
        if changed:
            changed(observed)
        calls, deleted = [], []
        with tempfile.TemporaryDirectory() as temporary:
            artifact = Path(temporary)

            def docker(arguments):
                calls.append(arguments)
                if arguments[:2] == ["volume", "inspect"]:
                    return json.dumps([{"Name": "owned-volume", "Labels": {
                        "com.factorybox.run": "foreign" if foreign_owner else owner}}])
                if arguments[0] == "create":
                    self.assertIn("type=volume,source=owned-volume,target=/var/lib/sysbox", arguments)
                    self.assertIn("worker-reconcile-storage", arguments)
                    self.assertNotIn("--force", arguments)
                    return "c" * 64
                self.fail("unexpected action: " + repr(arguments))

            def exact(name, actual_owner, identity):
                self.assertEqual((actual_owner, identity), (owner, "c" * 64))
                self.assertTrue(name.startswith("factorybox-storage-release-"))
                return {"State": {"Running": False, "ExitCode": 1 if command_failed else 0, "OOMKilled": False}}

            with mock.patch.object(worker_storage_release.reference, "docker", side_effect=docker), \
                 mock.patch.object(worker_storage_release.reference, "DOCKER", ["docker"]), \
                 mock.patch.object(worker_storage_release.reference, "owned_delete", side_effect=lambda *args: deleted.append(args)), \
                 mock.patch.object(worker_storage_release.consumer_worker_loss, "exact_worker", side_effect=exact), \
                 mock.patch.object(worker_storage_release.subprocess, "run", return_value=subprocess.CompletedProcess(
                     ["owned-reconcile"], 1 if command_failed else 0, json.dumps(observed), "")):
                if changed or foreign_owner or command_failed:
                    with self.assertRaises(ValueError):
                        worker_storage_release.verify({"image": "exact-worker"}, "owned-volume", owner, status, artifact)
                    self.assertFalse(any(json.loads(path.read_text()).get("passed") for path in artifact.glob("*.json")))
                else:
                    result = worker_storage_release.verify({"image": "exact-worker"}, "owned-volume", owner, status, artifact)
                    self.assertTrue(result["passed"])
                    self.assertTrue(result["cleanup"])
                    self.assertFalse(result["kernel_state_changed"])
            self.assertEqual(len(deleted), 0 if foreign_owner else 1)

    def test_exact_owned_post_exit_release_is_required(self):
        self.scenario()

    def test_other_image_or_epoch_cannot_be_promoted(self):
        for change in [
            lambda value: value.update(store_id="d" * 32),
            lambda value: value.update(kernel_release_verified=False),
            lambda value: value.update(completed_epochs=["d" * 32]),
            lambda value: value["image"].update(inode=124),
        ]:
            with self.subTest(change=change):
                self.scenario(changed=change)

    def test_failed_or_foreign_observation_is_not_release(self):
        self.scenario(foreign_owner=True)
        self.scenario(command_failed=True)
