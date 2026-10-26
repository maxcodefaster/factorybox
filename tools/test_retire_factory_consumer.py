import copy
import unittest

import retire_superseded_failure as retention


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
