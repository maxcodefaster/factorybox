import json
from pathlib import Path
import shutil
import tempfile
import unittest
from unittest import mock

import consumer_test_inputs
import reference
import upstream_unit


class FrozenConsumerInputs(unittest.TestCase):
    def fixture(self, root):
        proof = root / "artifacts" / ("factorybox-consumer-worker-loss-" + "a" * 32)
        build = root / "artifacts" / ("factorybox-build-consumer-tests-" + "b" * 32)
        proof.mkdir(parents=True); build.mkdir()
        source = proof / "source"
        (source / "tests").mkdir(parents=True)
        (source / "tests/assertions_test.go").write_text("original independent RAM and filesystem assertions\n")
        shutil.copytree(source, build / "source")
        context = upstream_unit.context_manifest(source)
        binaries = {}
        for name in consumer_test_inputs.FILES:
            file = build / name
            file.write_bytes(("native-tested bytes for " + name).encode())
            file.chmod(0o555)
            binaries[name] = reference.sha(file)
        worker = "factorybox-build-worker-" + "c" * 32
        native = {"passed": True, "cleanup": True, "global_sysctls_unchanged": True,
                  "baseline_input": None, "probe": "lifecycle", "phases": [{"passed": True}],
                  "source_context": context, "inputs_sha256": binaries, "worker_build": worker}
        (proof / "result.json").write_text(json.dumps(native))
        result = {"passed": True, "platform": "linux/amd64", "worker_build": worker,
                  "native_proof": proof.name, "native_result_sha256": reference.sha(proof / "result.json"),
                  "source_context": context, "binary_sha256": binaries}
        (build / "result.json").write_text(json.dumps(result))
        return proof, build, worker, context

    def test_valid_inputs_require_the_complete_current_assertion_source(self):
        with tempfile.TemporaryDirectory() as directory, mock.patch.object(reference, "ROOT", Path(directory)):
            proof, build, worker, context = self.fixture(Path(directory))
            self.assertEqual(consumer_test_inputs.load(build.name, worker, context)[0], build)
            # Unchanged runtime code is insufficient: a new test assertion must
            # compile into new helper bytes before another native qualification.
            changed = {**context, "tests/another_assertion_test.go": {"sha256": "d" * 64}}
            with self.assertRaisesRegex(ValueError, "complete current test source"):
                consumer_test_inputs.load(build.name, worker, changed)
            with self.assertRaisesRegex(ValueError, "worker"):
                consumer_test_inputs.load(build.name, "factorybox-build-worker-" + "d" * 32, context)
            (build / "source/tests/assertions_test.go").write_text("changed assertions\n")
            with self.assertRaises(ValueError):
                consumer_test_inputs.load(build.name, worker, context)

    def test_changed_or_redirected_helper_cannot_authorize_reuse(self):
        with tempfile.TemporaryDirectory() as directory, mock.patch.object(reference, "ROOT", Path(directory)):
            _, build, worker, context = self.fixture(Path(directory))
            file = build / "consumer-worker.test"
            original = file.read_bytes()
            file.chmod(0o755)
            with self.assertRaises(ValueError):
                consumer_test_inputs.load(build.name, worker, context)
            file.write_bytes(b"different helper bytes")
            file.chmod(0o555)
            with self.assertRaises(ValueError):
                consumer_test_inputs.load(build.name, worker, context)
            file.unlink()
            outside = Path(directory) / "same-bytes-outside-the-build"
            outside.write_bytes(original); outside.chmod(0o555)
            file.symlink_to(outside)
            with self.assertRaisesRegex(ValueError, "redirected"):
                consumer_test_inputs.load(build.name, worker, context)

    def test_incomplete_or_changed_original_native_proof_refuses_reuse(self):
        with tempfile.TemporaryDirectory() as directory, mock.patch.object(reference, "ROOT", Path(directory)):
            proof, build, worker, context = self.fixture(Path(directory))
            native_path = proof / "result.json"
            original = native_path.read_text()
            native = json.loads(original); native["cleanup"] = False
            native_path.write_text(json.dumps(native))
            with self.assertRaisesRegex(ValueError, "incomplete"):
                consumer_test_inputs.load(build.name, worker, context)
            native_path.write_text(original + "\n")
            with self.assertRaises(ValueError):
                consumer_test_inputs.load(build.name, worker, context)
