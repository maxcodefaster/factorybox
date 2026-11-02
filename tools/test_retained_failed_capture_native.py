import unittest

import retained_failed_capture_native


class RetainedFailedCaptureNativeTests(unittest.TestCase):
    def test_requires_exact_legacy_receipt_and_actual_cleanup_after_replacement(self):
        complete = dict.fromkeys(retained_failed_capture_native.PROOFS, True)
        retained_failed_capture_native.require_complete(complete)
        for key in complete:
            with self.subTest(key=key):
                with self.assertRaisesRegex(ValueError, "omitted"):
                    retained_failed_capture_native.require_complete({**complete, key: False})


if __name__ == "__main__":
    unittest.main()
