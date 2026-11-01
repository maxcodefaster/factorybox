import copy
import unittest

import descriptor_transfer_proof


class DescriptorTransferProofTests(unittest.TestCase):
    def test_only_exact_native_interoperability_and_feature_admission_can_gate_a_worker(self):
        context = {"go.mod": {"sha256": "a" * 64}}
        expected = {
            "nativePlatform": "linux/arm64",
            "criuCandidate": "exact-criu",
            "criuResultSHA256": "b" * 64,
            "criuBinarySHA256": "c" * 64,
            "nativeBaseBuild": "exact-base",
            "goSourceContext": context,
        }
        result = {
            "schema": 1, "passed": True, "cleanup": True, "exitCode": 0,
            "oomKilled": False, "testsSkipped": 0,
            "featureExitCode": 0, "featureOOMKilled": False, "featureTestsSkipped": 0,
            "passedTests": sorted(descriptor_transfer_proof.REQUIRED_TESTS),
            **expected,
        }
        descriptor_transfer_proof.validate(result, expected)
        replacements = {
            "schema": [None, True, 2],
            "passed": [False, 1],
            "cleanup": [False, 1],
            "exitCode": [1, False],
            "testsSkipped": [1, False],
            "oomKilled": [True, 0],
            "featureExitCode": [1, False],
            "featureOOMKilled": [True, 0],
            "featureTestsSkipped": [1, False],
            "nativePlatform": ["linux/amd64"],
            "criuCandidate": ["other-criu"],
            "criuResultSHA256": ["d" * 64],
            "criuBinarySHA256": ["d" * 64],
            "nativeBaseBuild": ["other-base"],
            "goSourceContext": [{}, {"go.mod": {"sha256": "d" * 64}}],
            "passedTests": [[], sorted(descriptor_transfer_proof.REQUIRED_TESTS - {"TestCriuFeatures"})],
        }
        for field, values in replacements.items():
            for value in values:
                changed = copy.deepcopy(result)
                changed[field] = value
                with self.subTest(field=field, value=value), \
                        self.assertRaisesRegex(ValueError, "descriptor interoperability"):
                    descriptor_transfer_proof.validate(changed, expected)
