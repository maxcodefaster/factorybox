import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

import external_lifecycle


class ExternalGatewayAdmissionTests(unittest.TestCase):
    def test_effect_only_native_proof_cannot_qualify_external_inference(self):
        # Both modes use the same executable and worker. A successful effect
        # proof must still be refused before any provider discovery or copy.
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory).resolve()
            name = "factorybox-provider-gateway-" + "a" * 32
            proof = root / "artifacts" / name
            proof.mkdir(parents=True)
            record = {key: True for key in ["passed", "cleanup", "inputs_unchanged", "foreign_birth_refused", "global_sysctls_unchanged"]}
            record.update(effect_only=True, worker_image_id="synthetic-worker")
            (proof / "result.json").write_text(json.dumps(record))
            with patch.object(external_lifecycle.reference, "ROOT", root), \
                    patch.object(external_lifecycle.free_model_probe, "exchange") as discovery, \
                    patch.object(external_lifecycle.shutil, "copyfile") as copy:
                with self.assertRaisesRegex(ValueError, "does not qualify"):
                    external_lifecycle.prepare(root / "new-artifact", name, "synthetic-worker", {})
                discovery.assert_not_called()
                copy.assert_not_called()
