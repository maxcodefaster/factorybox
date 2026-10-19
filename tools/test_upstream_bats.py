import json
import pathlib
import functools
import resource
import subprocess
import sys
import tempfile
import unittest
from unittest import mock

import upstream_bats


class BatsEvidenceTests(unittest.TestCase):
    def test_archive_refuses_live_or_foreign_attachments_before_starting_a_helper(self):
        owner, name, volume = "a" * 32, "exact-fixture", "exact-volume"
        for container in [
            {"Name": "/" + name, "Config": {"Labels": {"com.factorybox.run": owner}},
             "State": {"Running": True, "Pid": 123}},
            {"Name": "/foreign", "Config": {"Labels": {"com.factorybox.run": owner}},
             "State": {"Running": False, "Pid": 0}},
            {"Name": "/" + name, "Config": {"Labels": {"com.factorybox.run": "foreign"}},
             "State": {"Running": False, "Pid": 0}},
            {"Name": "/" + name, "Config": {"Labels": {"com.factorybox.run": owner}},
             "State": {"Running": False, "Pid": 123}},
        ]:
            def docker(args):
                if args[:2] == ["volume", "inspect"]:
                    return json.dumps([{"Labels": {"com.factorybox.run": owner}}])
                if args[:2] == ["container", "ls"]:
                    return "attached"
                if args[:2] == ["container", "inspect"]:
                    return json.dumps([container])
                raise AssertionError("unexpected Docker call")
            with self.subTest(container=container), tempfile.TemporaryDirectory() as directory:
                with mock.patch.object(upstream_bats.reference, "docker", side_effect=docker), \
                        mock.patch.object(upstream_bats.subprocess, "run") as run:
                    with self.assertRaises(ValueError):
                        upstream_bats.archive_stopped_volumes(
                            pathlib.Path(directory), name, owner, [volume], "image", {})
                    run.assert_not_called()

    def test_streamed_archive_is_hashed_before_exact_cleanup_can_follow(self):
        owner, name, volume = "a" * 32, "exact-fixture", "exact-volume"
        container = {
            "Name": "/" + name, "Config": {"Labels": {"com.factorybox.run": owner}},
            "State": {"Running": False, "Pid": 0},
        }
        def docker(args):
            if args[:2] == ["volume", "inspect"]:
                return json.dumps([{"Labels": {"com.factorybox.run": owner}}])
            if args[:2] == ["container", "ls"]:
                return "attached"
            if args[:2] == ["container", "inspect"]:
                return json.dumps([container])
            raise AssertionError("unexpected Docker call")
        with tempfile.TemporaryDirectory() as directory:
            archive = pathlib.Path(directory)
            def save(command, **kwargs):
                self.assertIn("--log-driver=none", command)
                self.assertEqual(command[-2:], ["-", "."])
                self.assertEqual(kwargs["stdout"].name, str(archive / "failed-volume-0.tar"))
                self.assertEqual(kwargs["timeout"], 240)
                self.assertIs(kwargs["preexec_fn"].func, upstream_bats.limit_archive_process)
                self.assertEqual(kwargs["preexec_fn"].args, (1 << 30,))
                (archive / "failed-volume-0.tar").write_bytes(b"complete retained payload")
            result = {}
            with mock.patch.object(upstream_bats.reference, "docker", side_effect=docker), \
                    mock.patch.object(upstream_bats.reference, "owned_delete") as remove, \
                    mock.patch.object(upstream_bats, "archive_file_limit", return_value=1 << 30), \
                    mock.patch.object(upstream_bats.subprocess, "run", side_effect=save):
                upstream_bats.archive_stopped_volumes(
                    archive, name, owner, [volume], "image", result, timeout=240)
            self.assertEqual(result["failed_volume_archives"][volume], {
                "file": "failed-volume-0.tar",
                "sha256": upstream_bats.file_sha(archive / "failed-volume-0.tar"),
                "bytes": len(b"complete retained payload")})
            remove.assert_called_once_with("container", name + "-archive-0", owner)

    def test_archive_budget_preserves_host_free_space_and_hard_process_limit(self):
        with tempfile.TemporaryDirectory() as directory:
            with mock.patch.object(upstream_bats.shutil, "disk_usage") as usage, \
                    mock.patch.object(resource, "getrlimit",
                                      return_value=(resource.RLIM_INFINITY, resource.RLIM_INFINITY)):
                usage.return_value.free = (12 << 30) + 8192
                self.assertEqual(upstream_bats.archive_file_limit(directory), 8192)
                usage.return_value.free = 12 << 30
                with self.assertRaisesRegex(ValueError, "reserved free disk"):
                    upstream_bats.archive_file_limit(directory)
            with mock.patch.object(upstream_bats.shutil, "disk_usage") as usage, \
                    mock.patch.object(resource, "getrlimit", return_value=(1024, 2048)):
                usage.return_value.free = 16 << 30
                self.assertEqual(upstream_bats.archive_file_limit(directory), 2048)

    def test_real_child_cannot_write_beyond_archive_cap_or_change_parent_limits(self):
        before = resource.getrlimit(resource.RLIMIT_FSIZE)
        with tempfile.TemporaryDirectory() as directory:
            payload = pathlib.Path(directory) / "bounded-output"
            with payload.open("xb") as output:
                child = subprocess.run(
                    [sys.executable, "-B", "-S", "-c",
                     "import os; os.write(1,b'x'*8192); os.write(1,b'x'*8192)"],
                    stdout=output, stderr=subprocess.DEVNULL,
                    preexec_fn=functools.partial(upstream_bats.limit_archive_process, 1024),
                    timeout=10)
            self.assertNotEqual(child.returncode, 0)
            self.assertEqual(payload.stat().st_size, 1024)
        self.assertEqual(resource.getrlimit(resource.RLIMIT_FSIZE), before)

    def test_archive_stream_cannot_exceed_remaining_repository_budget(self):
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            archive = root / "artifacts" / "closed-run"
            archive.mkdir(parents=True)
            with mock.patch.object(upstream_bats, "ROOT", root), \
                    mock.patch.object(upstream_bats.shutil, "disk_usage") as usage, \
                    mock.patch.object(upstream_bats.reference.artifact_storage,
                                      "allocated_bytes") as allocated, \
                    mock.patch.object(resource, "getrlimit",
                                      return_value=(resource.RLIM_INFINITY, resource.RLIM_INFINITY)):
                usage.return_value.free = 100 << 30
                allocated.return_value = upstream_bats.reference.artifact_storage.ARTIFACT_LIMIT - 8192
                self.assertEqual(upstream_bats.archive_file_limit(archive), 8192)
                allocated.return_value += 8192
                with self.assertRaises(ValueError):
                    upstream_bats.archive_file_limit(archive)

    def test_zero_exit_at_archive_cap_cannot_publish_a_cleanup_receipt(self):
        owner, name, volume = "a" * 32, "exact-fixture", "exact-volume"
        def docker(args):
            if args[:2] == ["volume", "inspect"]:
                return json.dumps([{"Labels": {"com.factorybox.run": owner}}])
            if args[:2] == ["container", "ls"]:
                return ""
            raise AssertionError("unexpected Docker call")
        with tempfile.TemporaryDirectory() as directory:
            archive = pathlib.Path(directory)
            def truncated(command, **kwargs):
                kwargs["stdout"].write(b"x" * 1024)
                kwargs["stdout"].flush()
            result = {}
            with mock.patch.object(upstream_bats.reference, "docker", side_effect=docker), \
                    mock.patch.object(upstream_bats.reference, "owned_delete") as remove, \
                    mock.patch.object(upstream_bats, "archive_file_limit", return_value=1024), \
                    mock.patch.object(upstream_bats.subprocess, "run", side_effect=truncated):
                with self.assertRaisesRegex(ValueError, "source must be retained"):
                    upstream_bats.archive_stopped_volumes(
                        archive, name, owner, [volume], "image", result)
            self.assertEqual(result["failed_volume_archives"], {})
            remove.assert_called_once_with("container", name + "-archive-0", owner)

    def test_zero_exit_cannot_hide_a_missing_or_failed_assertion(self):
        expected = ["first", "second"]
        observed = upstream_bats.observations("1..2\nok 1 first\nnot ok 2 second\n", expected)
        self.assertTrue(observed["complete"])
        self.assertEqual(observed["failed"], ["second"])
        for text in ["1..2\nok 1 first\n", "1..2\nok 1 first\nok 1 second\n",
                     "1..2\nok 1 first\nok 2 changed\n", "1..2\n1..2\nok 1 first\nok 2 second\n"]:
            with self.subTest(text=text):
                self.assertFalse(upstream_bats.observations(text, expected)["complete"])

    def test_skipped_upstream_case_is_not_an_executed_pass(self):
        observed = upstream_bats.observations(
            "1..2\nok 1 first # skip cgroup v1 required\nok 2 second\n", ["first", "second"])
        self.assertTrue(observed["complete"])
        self.assertEqual(observed["skipped"], ["first"])
        self.assertEqual(observed["executed_passes"], 1)

    def test_image_archive_rejects_changed_payload_and_failed_acquisition(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = pathlib.Path(temporary)
            name = "factorybox-bats-images-" + "a" * 32
            archive = root / "artifacts" / name
            (archive / "output").mkdir(parents=True)
            (root / "upstream").mkdir()
            (root / "build").mkdir()
            pins = root / "upstream/bats-images.json"
            pins.write_text(json.dumps({"images": [{"alias": "original:latest", "image": "original@sha256:" + "b" * 64}]}))
            dockerfile = root / "build/upstream-bats.Dockerfile"
            dockerfile.write_text("FROM pinned\n")
            rows = [{"alias": "original:latest", "image": "original@sha256:" + "b" * 64,
                     "id": "sha256:" + "c" * 64}]
            image_json = archive / "output/images.json"
            image_json.write_text(json.dumps(rows))
            payload = archive / "output/images.tar"
            payload.write_bytes(b"original archive payload")
            result = {
                "passed": True, "cleanup": True, "native_base_build": "exact-base",
                "inputs_sha256": {"pins.json": upstream_bats.file_sha(pins),
                                 "context/Dockerfile": upstream_bats.file_sha(dockerfile)},
                "images_json_sha256": upstream_bats.file_sha(image_json),
                "images_tar_sha256": upstream_bats.file_sha(payload), "images": rows,
            }
            receipt = archive / "result.json"
            receipt.write_text(json.dumps(result))
            with mock.patch.object(upstream_bats, "ROOT", root):
                directory, _ = upstream_bats.image_archive(name, "exact-base")
                self.assertEqual(directory, archive / "output")
                payload.write_bytes(b"substituted image archive")
                with self.assertRaises(ValueError):
                    upstream_bats.image_archive(name, "exact-base")
                payload.write_bytes(b"original archive payload")
                result["passed"] = False
                receipt.write_text(json.dumps(result))
                with self.assertRaises(ValueError):
                    upstream_bats.image_archive(name, "exact-base")


if __name__ == "__main__":
    unittest.main()
