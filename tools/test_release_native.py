import argparse
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

import reference
import release_native


class NativeReleaseQualificationTests(unittest.TestCase):
    def test_workflow_only_publishes_releases_after_native_qualification_and_license_resolution(self):
        workflow = (reference.ROOT / ".github/workflows/images.yaml").read_text()
        self.assertEqual(workflow.split("on:", 1)[1].split("permissions:", 1)[0],
                         "\n  release:\n    types: [published]\n")
        for required in ["arch: [amd64]", 'runs-on: [self-hosted, linux, "factorybox-native-${{ matrix.arch }}"]',
                         '[[ ! -s LICENSE ]]', "git merge-base --is-ancestor HEAD origin/main",
                         "tools/release_native.py", "release_native.completed_receipt",
                         "QUALIFIED_IMAGE: ${{ steps.qualify.outputs.image }}",
                         "needs: [release, native]", "Registry absence was not confirmed",
                         "ci-builder-identity.json", "actual==expected",
                         'test "$version" = "$(cat internal/version/VERSION)"',
                         "tools/release_sources.py", 'gh release upload "v$RELEASE"',
                         "d11b1ec7b83bcd3ff2df0d7a03e0907de9d4eb78"]:
            self.assertIn(required, workflow)
        for forbidden in ["workflow_dispatch", "pull_request:", "push:", "setup-qemu", "--native-candidate",
                          ":latest", "cancel-in-progress: true", "docker system prune"]:
            self.assertNotIn(forbidden, workflow)
        self.assertLess(workflow.index("tools/release_sources.py"), workflow.index("docker push"))
        self.assertNotIn('"$image-arm64"', workflow)

    def test_both_targets_require_all_original_native_gates_without_candidate_admission_or_push(self):
        worker = {"native_base_build": "exact-base", "sysbox_build": "exact-sysbox",
                  "criu_build": "exact-criu"}
        for platform in ["linux/amd64", "linux/arm64"]:
            options = argparse.Namespace(platform=platform, worker_build="exact-worker",
                consumer_build="exact-consumer", project_images="exact-project-images",
                factory_repo=Path("/exact-factory"), builder="exact-builder")
            commands = release_native.native_commands(options, worker, "exact-bats-images")
            roles = [role for role, _ in commands]
            self.assertEqual(len(roles), len(set(roles)))
            self.assertEqual({role.removeprefix("runtime-") for role in roles if role.startswith("runtime-")},
                             set(release_native.RUNTIME_SUITES))
            self.assertEqual(set(release_native.RUNTIME_SUITES), {
                "capacity", "retirement", "restore-hold", "stopped-memory", "timer-detach",
                "timer-admission-reader", "timer-state-reader", "timer-exit-guard",
                "timer-signal", "timer-admission-signal", "timer-remainder",
                "timer-remainder-protection", "timer-orphan", "timer-guard-loss",
            })
            for required in ["criu-core", "sysbox-unit", "sysbox-integration", "bats-lifecycle", "bats-systemd",
                             "recovery-planned", "recovery-abrupt", "factory-source", "factory-reserve"]:
                self.assertIn(required, roles)
            for role, command in commands:
                self.assertNotIn("--native-candidate", command)
                self.assertNotIn("--push", command)
                if role in {"criu-core", "sysbox-unit", "sysbox-integration", "bats-lifecycle", "bats-systemd"}:
                    self.assertEqual("--candidate-platform" in command, platform == "linux/amd64")
                    if platform == "linux/amd64":
                        self.assertIn(platform, command)
                else:
                    self.assertIn("exact-worker", command)
            for role in ["sysbox-unit", "sysbox-integration", "bats-lifecycle", "bats-systemd"]:
                command = dict(commands)[role]
                self.assertIn("--bounded-build", command)
                self.assertIn("exact-builder", command)
            self.assertNotIn("--reserve", dict(commands)["factory-source"])
            self.assertIn("--reserve", dict(commands)["factory-reserve"])

    def test_only_the_final_exact_successful_closed_receipt_is_accepted(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary).resolve()
            artifact = root / "artifacts/exact"
            artifact.mkdir(parents=True)
            result = {"passed": True, "cleanup": True}
            path = artifact / "result.json"
            path.write_text(json.dumps(result))
            log = root / "stage.stdout"
            good = {"evidence": str(artifact), **result}
            log.write_text("native output\n" + json.dumps(good) + "\n")
            with patch.object(reference, "ROOT", root):
                self.assertEqual(release_native.completed_receipt(log), (artifact, result))
                for final in [{**good, "passed": False}, {**good, "cleanup": False}]:
                    log.write_text(json.dumps(good) + "\n" + json.dumps(final) + "\n")
                    with self.assertRaises(ValueError):
                        release_native.completed_receipt(log)
                log.write_text(json.dumps(good) + "\n")
                path.write_text(json.dumps({**result, "cleanup": False}))
                with self.assertRaises(ValueError):
                    release_native.completed_receipt(log)
                path.write_text(json.dumps(result))
                redirected = root / "artifacts/redirected"
                redirected.symlink_to(artifact, target_is_directory=True)
                log.write_text(json.dumps({**good, "evidence": str(redirected)}) + "\n")
                with self.assertRaises(ValueError):
                    release_native.completed_receipt(log)

    def test_release_identity_requires_an_ordinary_native_worker_and_exact_cli_version(self):
        doctor = {"matches_reference": True, "platform": {"os": "linux", "architecture": "arm64",
                                                        "page_size": 4096, "kernel": "exact"}}
        worker = {"platform": "linux/arm64", "original_license_sha256": "a" * 64}
        license_line = "a" * 64 + "  /opt/factorybox/licenses/factorybox/LICENSE\n"
        output = json.dumps(doctor) + "\nfactorybox v0.1.0\n" + license_line
        release_native.validate_doctor(worker, "0.1.0", output)
        for changed, version, body in [
            ({**worker, "native_candidate": {}}, "0.1.0", output),
            ({**worker, "platform": "linux/amd64"}, "0.1.0", output),
            (worker, "0.2.0", output),
            (worker, "0.1.0", json.dumps({**doctor, "matches_reference": False}) + "\nfactorybox v0.1.0\n" + license_line),
            (worker, "0.1.0", output.replace("a" * 64, "b" * 64)),
        ]:
            with self.assertRaises(ValueError):
                release_native.validate_doctor(changed, version, body)

    def test_publication_requires_the_approved_notice_to_be_inside_the_verified_image(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary).resolve()
            original = root / "LICENSE"
            with patch.object(reference, "ROOT", root):
                with self.assertRaises(ValueError):
                    release_native.require_original_license({})
                original.write_text("Explicit license fixture, not a selected project license.\n")
                digest = reference.sha(original)
                worker = {"original_license_sha256": digest,
                          "bundle_sha256": {"licenses/factorybox/LICENSE": digest}}
                release_native.require_original_license(worker)
                for changed in [{}, {**worker, "bundle_sha256": {}},
                                {**worker, "original_license_sha256": "0" * 64}]:
                    with self.assertRaises(ValueError):
                        release_native.require_original_license(changed)
                original.write_text("different approved text")
                with self.assertRaises(ValueError):
                    release_native.require_original_license(worker)
