import copy
import json
from pathlib import Path
import tempfile
import tarfile
import unittest
from unittest.mock import patch

import reference
import release_reuse


class NativeReceiptReuseTests(unittest.TestCase):
    def test_retained_bats_transport_must_contain_every_current_test_byte(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            tests = root / "tests"
            tests.mkdir()
            case = tests / "case.bats"
            case.write_text('original assertion\n')
            archive = root / "source.tar"
            with tarfile.open(archive, "w") as transport:
                transport.add(tests, arcname="tests")
            release_reuse.require_bats_source(archive, root)
            case.write_text("changed assertion\n")
            with self.assertRaisesRegex(ValueError, "current pinned assertions"):
                release_reuse.require_bats_source(archive, root)
            case.write_text("original assertion\n")
            (tests / "another.bats").write_text("new required assertion\n")
            with self.assertRaisesRegex(ValueError, "omits current pinned assertions"):
                release_reuse.require_bats_source(archive, root)

    def test_continuation_rejects_changed_commands_receipts_and_foreign_workers(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary).resolve()
            parent = root / "artifacts" / ("factorybox-release-native-" + "a" * 32)
            leaf = root / "artifacts" / ("factorybox-capture-budget-native-" + "b" * 32)
            parent.mkdir(parents=True)
            leaf.mkdir()
            result = {"passed": True, "cleanup": True, "worker_build": "worker"}
            (leaf / "result.json").write_text(json.dumps(result))
            row = {"role": "capture-budget", "passed": True, "command": ["exact"],
                   "evidence": leaf.name, "result_sha256": reference.sha(leaf / "result.json")}
            original = {"passed": False, "cleanup": True, "worker_build": "worker",
                        "image_id": "sha256:exact", "stages": [row]}
            (parent / "result.json").write_text(json.dumps(original))
            with patch.object(reference, "ROOT", root):
                good = release_reuse.continuation(
                    parent.name, "worker", {"image_id": "sha256:exact"}, {"capture-budget": ["exact"]})
                self.assertEqual(set(good), {"capture-budget"})
                for change in [
                        {"worker_build": "other"}, {"image_id": "sha256:other"}, {"cleanup": False},
                        {"stages": [{**row, "command": ["changed"]}]},
                        {"stages": [{**row, "result_sha256": "wrong"}]}]:
                    (parent / "result.json").write_text(json.dumps({**original, **change}))
                    with self.subTest(change=change), self.assertRaises(ValueError):
                        release_reuse.continuation(
                            parent.name, "worker", {"image_id": "sha256:exact"}, {"capture-budget": ["exact"]})
                (leaf / "result.json").write_text(json.dumps({**result, "worker_build": "foreign"}))
                (parent / "result.json").write_text(json.dumps({
                    **original, "stages": [{**row, "result_sha256": reference.sha(leaf / "result.json")}]}))
                with self.assertRaisesRegex(ValueError, "worker"):
                    release_reuse.continuation(
                        parent.name, "worker", {"image_id": "sha256:exact"}, {"capture-budget": ["exact"]})

    def test_continuation_leaves_upstream_roles_for_separate_component_verification(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary).resolve()
            parent = root / "artifacts" / ("factorybox-release-native-" + "a" * 32)
            parent.mkdir(parents=True)
            commands = {role: ["exact", role] for role in release_reuse.UPSTREAM_ROLES.values()}
            rows = [
                {"role": role, "passed": True, "reused": True, "command": command,
                 "evidence": "factorybox-upstream-" + "b" * 32, "result_sha256": "unverified"}
                for role, command in commands.items()
            ]
            original = {"passed": False, "cleanup": True, "worker_build": "worker",
                        "image_id": "sha256:exact", "stages": rows}
            receipt = parent / "result.json"
            receipt.write_text(json.dumps(original))
            with patch.object(reference, "ROOT", root):
                # Continuation must neither reject nor accept these component
                # proofs. Explicit upstream reuse verifies their full inputs.
                self.assertEqual(release_reuse.continuation(
                    parent.name, "worker", {"image_id": "sha256:exact"}, commands), {})
                receipt.write_text(json.dumps({
                    **original, "stages": [{**rows[0], "command": ["changed"]}]}))
                with self.assertRaisesRegex(ValueError, "command"):
                    release_reuse.continuation(
                        parent.name, "worker", {"image_id": "sha256:exact"}, commands)

    def test_upstream_reuse_rejects_failed_partial_changed_host_or_harness(self):
        expected = {"component_build": "exact", "host": {"KernelVersion": "native"},
                    "test_script_sha256": "unchanged"}
        suites = {"runc": {"exit_code": 0, "failed": [], "passed_tests": 47}}
        record = {**expected, "passed": True, "cleanup": True, "candidate_platform": False,
                  "test": None, "inputs_unchanged": True, "suites": suites}
        release_reuse.require_unit_identity(record, expected, suites)
        for change in [
                {"passed": False}, {"cleanup": False}, {"candidate_platform": True},
                {"test": "TestOneCase"}, {"inputs_unchanged": False},
                {"component_build": "another"}, {"host": {"KernelVersion": "emulated"}},
                {"test_script_sha256": "changed"}, {"suites": {}},
                {"suites": {"runc": {"exit_code": 1, "failed": ["TestBroken"]}}}]:
            with self.subTest(change=change), self.assertRaises(ValueError):
                release_reuse.require_unit_identity({**record, **change}, expected, suites)

    def test_bats_reuse_preserves_original_assertions_and_skip_conditions(self):
        expected = {"sysbox_build": "exact", "component_sha256": {"sysbox-runc": "pinned"},
                    "expected_titles": ["first", "second"], "systemd_network_profile": True}
        observed = {"complete": True, "failed": [], "skipped": [], "executed_passes": 2}
        record = {**expected, "passed": True, "cleanup": True, "inputs_unchanged": True,
                  "components_unchanged": True, "test": None, "observed": observed}
        release_reuse.require_bats_identity(record, expected, observed)
        for change in [
                {"passed": False}, {"cleanup": False}, {"test": "first"},
                {"components_unchanged": False}, {"expected_titles": ["first"]},
                {"component_sha256": {"sysbox-runc": "other"}}, {"systemd_network_profile": False}]:
            with self.subTest(change=change), self.assertRaises(ValueError):
                release_reuse.require_bats_identity({**record, **change}, expected, observed)
        for mutation in [
                {"complete": False}, {"failed": ["second"]}, {"skipped": ["second"]}]:
            changed = copy.deepcopy(observed)
            changed.update(mutation)
            with self.subTest(mutation=mutation), self.assertRaises(ValueError):
                release_reuse.require_bats_identity({**record, "observed": changed}, expected, changed)
