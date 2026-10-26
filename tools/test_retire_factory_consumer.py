import copy
import unittest

import retire_superseded_failure as retention


class NamespaceAccessSupersessionTests(unittest.TestCase):
    def fixture(self):
        identity = {"platform": "linux/arm64", "native_candidate": None,
                    "consumer_build": "consumer", "consumer_image": "image", "hub_image": "hub",
                    "hub_bun": "/native/bun", "source_sha256": {"prepare.py": "same"},
                    "hub_sha256": "same", "registry_binary_sha256": "same", "registry_source_sha256": {},
                    "reserve": True, "cleanup": True, "inputs_unchanged": True,
                    "host_sysctls_unchanged": True, "standby_docker_exec_passed": True,
                    "terminal_proof": False, "diagnostic_only": False}
        old = {**identity, "passed": False, "error":
               "Can't open 123/ns/net on procfs: Permission denied; "
               "One or more namespaces doesn't belong to the target user namespace"}
        pause = {"record": {"phase": "paused", "pause": {"completed_at": "complete",
                 "after": {"anon": 0}, "snapshot": {"resident_bytes_after": 0}}}}
        new = {**identity, "passed": True, "worker_build": "fixed-worker",
               "before": {"nonce": "same-memory", "pid": 117, "count": 1},
               "after": {"nonce": "same-memory", "pid": 117, "count": 2},
               "child_after": {"State": {"Running": True}, "RestartCount": 0},
               "standby_pause": copy.deepcopy(pause), "pause": copy.deepcopy(pause)}
        core = {"internal/core.go": {"sha256": "same"}}
        old_worker = {"platform": "linux/arm64", "native_candidate": None,
                      "source_context": core, "criu_build": "old-criu",
                      "bundle_sha256": {key: key for key in
                          ["factorybox", "components/sysbox-runc", "components/sysbox-fs", "components/sysbox-mgr", "components/criu"]}}
        new_worker = {**copy.deepcopy(old_worker), "image_id": "fixed-image", "criu_build": "new-criu"}
        new_worker["bundle_sha256"]["components/criu"] = "fixed-criu"
        native = {"passed": True, "cleanup": True, "inputs_unchanged": True,
                  "suite": "standby-repeat", "worker_build": "fixed-worker",
                  "worker_image_id": "fixed-image", "platform": "linux/arm64", "source_context": core}
        proof = {"passed": True, "foreign_namespace_refused": True,
                 "cases": [{"protected": False}, {"protected": True, "passed": True,
                     "observations": [{"actor": {"dumpable": 0}}, {"actor": {"dumpable": 0}}]}]}
        old_criu = {"candidate_patch_sha256": {"shared-fds.patch": "same"}}
        new_criu = {"base_candidate": "old-criu", "candidate_patch_sha256": {
            **old_criu["candidate_patch_sha256"], "namespace-access.patch":
            retention.reference.sha(retention.reference.ROOT / "upstream/patches/criu-userns-validation-handles.patch")}}
        return old, new, old_worker, new_worker, native, proof, old_criu, new_criu

    def test_only_the_completed_namespace_correction_supersedes_its_original_failure(self):
        values = self.fixture()
        before = copy.deepcopy(values)
        retention.verify_namespace_access_replacement(*values)
        self.assertEqual(values, before)

    def test_current_cgroup_failure_or_incomplete_native_proof_never_qualifies(self):
        for index, change in [
            (0, lambda r: r.update(error="restored original cgroup is not populated and frozen")),
            (1, lambda r: r.update(passed=False)),
            (1, lambda r: r["after"].update(nonce="cold-replacement")),
            (1, lambda r: r["pause"]["record"]["pause"]["after"].update(anon=4096)),
            (1, lambda r: r.update(source_sha256={"prepare.py": "another"})),
            (3, lambda r: r["bundle_sha256"].update({"components/sysbox-runc": "changed"})),
            (4, lambda r: r.update(worker_image_id="different")),
            (5, lambda r: r.update(foreign_namespace_refused=False)),
            (5, lambda r: r["cases"][1]["observations"][1]["actor"].update(dumpable=1)),
            (7, lambda r: r["candidate_patch_sha256"].update({"another.patch": "unproved"})),
        ]:
            values = copy.deepcopy(self.fixture())
            change(values[index])
            with self.subTest(index=index), self.assertRaisesRegex(ValueError, "namespace supersession"):
                retention.verify_namespace_access_replacement(*values)


class FactoryConsumerAdmissionSupersessionTests(unittest.TestCase):
    def fixture(self):
        identity = {"platform": "linux/arm64", "native_candidate": {"kernel": "exact"},
                    "consumer_build": "exact-consumer", "consumer_image": "exact-image",
                    "hub_image": "exact-hub", "hub_bun": "/native/bun",
                    "source_sha256": {"bootstrap.sh": "exact-bootstrap"},
                    "hub_sha256": "exact-hub-script", "registry_binary_sha256": "exact-registry",
                    "reserve": False, "inputs_unchanged": True, "host_sysctls_unchanged": True,
                    "cleanup": True}
        old = {**identity, "passed": False,
               "error": "Native Factory consumer command failed: " + retention.FACTORY_CAPABILITY_ERROR + "\n",
               "commands": [{"arguments": ["exec", "worker", "factorybox", "pause"],
                             "exit": 1, "stderr": retention.FACTORY_CAPABILITY_ERROR + "\n"}]}
        new = {**identity, "passed": True,
               "before": {"nonce": "same-memory", "pid": 118, "count": 2},
               "after": {"nonce": "same-memory", "pid": 118, "count": 3, "editor": "during-pause"},
               "pause": {"record": {"phase": "paused", "pause": {"completed_at": "exact",
                   "after": {"anon": 0}, "snapshot": {"resident_bytes_after": 0}}}},
               "child_after": {"State": {"Running": True}, "RestartCount": 0}}
        worker = {"platform": "linux/arm64", "native_candidate": {"kernel": "exact"},
                  "source_context": {"internal/runtime.go": {"sha256": "exact"},
                                     "internal/runtime_test.go": {"sha256": "old-test"}},
                  "bundle_sha256": {"factorybox": "exact-cli", "components/criu": "old-criu",
                                    "components/sysbox-runc": "exact-runc",
                                    "components/sysbox-fs": "exact-fs", "components/sysbox-mgr": "exact-manager"}}
        next_worker = copy.deepcopy(worker)
        next_worker["bundle_sha256"]["components/criu"] = "new-criu"
        next_worker["source_context"]["internal/runtime_test.go"]["sha256"] = "new-test"
        return old, new, worker, next_worker

    def test_only_complete_same_worker_consumer_proof_supersedes_the_exact_precapture_refusal(self):
        old, new, worker, next_worker = self.fixture()
        retention.verify_factory_consumer_replacement(old, new, worker, next_worker)
        for key, value in [
            ("passed", False), ("cleanup", False), ("reserve", True),
            ("consumer_image", "different-image"), ("inputs_unchanged", False)
        ]:
            with self.subTest(key=key), self.assertRaisesRegex(ValueError, "Factory consumer"):
                retention.verify_factory_consumer_replacement(old, {**new, key: value}, worker, next_worker)
        for mutate in [
            lambda r: r["after"].update(nonce="restarted"),
            lambda r: r["after"].update(pid=119),
            lambda r: r["after"].update(editor="before"),
            lambda r: r["pause"]["record"]["pause"]["after"].update(anon=4096),
            lambda r: r["pause"]["record"]["pause"]["snapshot"].update(resident_bytes_after=4096),
            lambda r: r["child_after"].update(RestartCount=1),
        ]:
            changed = copy.deepcopy(new)
            mutate(changed)
            with self.assertRaisesRegex(ValueError, "Factory consumer"):
                retention.verify_factory_consumer_replacement(old, changed, worker, next_worker)

    def test_changed_runtime_or_sysbox_and_an_actual_capture_failure_are_not_superseded(self):
        old, new, worker, next_worker = self.fixture()
        for mutate in [
            lambda r: r["bundle_sha256"].update(factorybox="changed-cli"),
            lambda r: r["bundle_sha256"].update({"components/sysbox-runc": "changed-runc"}),
            lambda r: r["source_context"]["internal/runtime.go"].update(sha256="changed-runtime"),
        ]:
            changed = copy.deepcopy(next_worker)
            mutate(changed)
            with self.assertRaisesRegex(ValueError, "Factory consumer"):
                retention.verify_factory_consumer_replacement(old, new, worker, changed)
        with self.assertRaisesRegex(ValueError, "Factory consumer"):
            retention.verify_factory_consumer_replacement({**old, "error": "actual capture failed"},
                                                          new, worker, next_worker)


class ResolvedStandbyExecSupersessionTests(unittest.TestCase):
    def test_separate_passing_input_isolation_proof_does_not_qualify_the_failed_consumer(self):
        values = list(self.fixture())
        old, new = values[:2]
        old["source_sha256"] = {"prepare.py": "old", "prepare_test.py": "old-test"}
        new["source_sha256"] = {"prepare.py": "same", "prepare_test.py": "new-test"}
        first = {"nonce": "original-memory", "pid": 117, "count": 1}
        input_proof = {
            **copy.deepcopy(new), "passed": True, "sibling_isolation": True,
            "preparation_ownership_controls_passed": True,
            "before": first, "after": {**first, "count": 2},
            "pause": {"record": {"pause": copy.deepcopy(new["standby_pause"]["record"]["pause"])}},
        }
        with self.assertRaisesRegex(ValueError, "separate complete"):
            retention.verify_standby_exec_replacement(*values)
        retention.verify_standby_exec_replacement(*values, input_proof)
        self.assertFalse(new["passed"])
        for change in [
            lambda r: r.update(passed=False),
            lambda r: r.update(sibling_isolation=False),
            lambda r: r.update(source_sha256={"prepare.py": "different"}),
            lambda r: r["after"].update(nonce="cold-replacement"),
            lambda r: r["pause"]["record"]["pause"]["after"].update(anon=4096),
        ]:
            refused = copy.deepcopy(input_proof)
            change(refused)
            with self.assertRaisesRegex(ValueError, "separate complete"):
                retention.verify_standby_exec_replacement(*values, refused)
        old["source_sha256"]["bootstrap.sh"] = "unqualified-change"
        with self.assertRaisesRegex(ValueError, "separate complete"):
            retention.verify_standby_exec_replacement(*values, input_proof)

    def fixture(self):
        identity = {"platform": "linux/arm64", "native_candidate": None,
                    "consumer_build": "consumer", "consumer_image": "image",
                    "hub_image": "hub", "hub_bun": "/native/bun",
                    "source_sha256": {"prepare.py": "same"},
                    "hub_sha256": "same", "registry_binary_sha256": "same",
                    "registry_source_sha256": {"main.go": "same"},
                    "reserve": True, "terminal_proof": False, "cleanup": True,
                    "inputs_unchanged": True, "host_sysctls_unchanged": True}
        old = {**identity, "passed": False, "error": retention.STANDBY_EXEC_ERROR,
               "standby_docker_exec": {"exit": 128, "stdout":
                   "error copying bootstrap data to pipe: write init-p: broken pipe"}}
        memory = {"completed_at": "complete", "snapshot_id": "snapshot",
                  "checkpoint_sha256": "same-checkpoint", "after": {"anon": 0},
                  "snapshot": {"resident_bytes_after": 0}}
        record = {"id": "same-sandbox", "runtime_id": "same-runtime", "phase": "running"}
        new = {**identity, "passed": False, "diagnostic_only": False,
               "worker_build": "new-worker", "standby_docker_exec_passed": True,
               "standby_docker_exec": {"exit": 0, "stdout": "factorybox-standby-exec"},
               "hub_before": {"connected": True}, "before": {"nonce": "app-memory", "pid": 117},
               "start": {"record": record},
               "standby_pause": {"record": {**record, "phase": "paused", "pause": memory}},
               "standby_resume": {"record": {**record, "pause": memory}},
               "pause_attempted": True,
               "error": "Can't open 123/ns/net on procfs: Permission denied; "
                        "One or more namespaces doesn't belong to the target user namespace"}
        worker = {"platform": "linux/arm64", "native_candidate": None,
                  "source_context": {"internal/lifecycle/capture_worker_linux.go": {"sha256": "old"}},
                  "bundle_sha256": {key: key for key in
                      ["components/criu", "components/sysbox-runc", "components/sysbox-fs", "components/sysbox-mgr"]}}
        next_worker = copy.deepcopy(worker)
        next_worker["source_context"] = {
            "internal/lifecycle/capture_worker_linux.go": {"sha256": "new"},
            "internal/lifecycle/capture_stop_linux.go": {"sha256": "new"}}
        next_worker["image_id"] = "exact-next-image"
        native = {"passed": True, "cleanup": True, "inputs_unchanged": True,
                  "suite": "standby-exec", "worker_build": "new-worker",
                  "platform": "linux/arm64", "worker_image_id": "exact-next-image",
                  "nested_exec_input": {"consumer_build": "consumer", "image": "image"},
                  "source_context": next_worker["source_context"]}
        proof = {"passed": True, "before": "nested-exec-ready", "after": "nested-exec-ready\n"}
        return old, new, worker, next_worker, native, proof

    def test_only_the_resolved_exec_is_superseded_without_turning_the_later_failure_green(self):
        values = self.fixture()
        original = copy.deepcopy(values)
        retention.verify_standby_exec_replacement(*values)
        self.assertEqual(values, original)
        self.assertFalse(values[1]["passed"])
        with self.assertRaisesRegex(ValueError, "Factory consumer"):
            retention.verify_factory_consumer_replacement(*values[:4])

    def test_missing_real_first_work_memory_or_regression_never_retires_the_old_failure(self):
        for index, mutate in [
            (0, lambda r: r.update(error="actual capture failed")),
            (1, lambda r: r.update(diagnostic_only=True)),
            (1, lambda r: r.update(standby_docker_exec_passed=False)),
            (1, lambda r: r.update(consumer_image="different")),
            (1, lambda r: r["hub_before"].update(connected=False)),
            (1, lambda r: r["standby_docker_exec"].update(exit=128)),
            (1, lambda r: r["standby_pause"]["record"]["pause"]["after"].update(anon=4096)),
            (1, lambda r: r["standby_resume"]["record"].update(runtime_id="cold-replacement")),
            (1, lambda r: r.update(error="another unresolved failure")),
            (3, lambda r: r["bundle_sha256"].update({"components/criu": "different"})),
            (3, lambda r: r["source_context"].update({"internal/other.go": {"sha256": "changed"}})),
            (4, lambda r: r.update(passed=False)),
            (4, lambda r: r.update(worker_build="different")),
            (4, lambda r: r.update(platform="linux/amd64")),
            (5, lambda r: r.update(after="cold-start")),
        ]:
            values = copy.deepcopy(self.fixture())
            mutate(values[index])
            with self.subTest(index=index, change=mutate), self.assertRaisesRegex(ValueError, "standby exec"):
                retention.verify_standby_exec_replacement(*values)
