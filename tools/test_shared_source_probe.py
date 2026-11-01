import unittest

import shared_source_probe


class SharedSourceDescriptionTests(unittest.TestCase):
    def test_large_sets_keep_every_independent_position_and_original_file_content(self):
        original = {"count": 257, "sha256": "a" * 64}
        shared_source_probe.require_large_descriptions(original, 257)
        shared_source_probe.require_large_descriptions(dict(original), 257, original)
        for changed in [
                {**original, "count": 128}, {**original, "sha256": "b" * 64},
                {**original, "sha256": "unconfirmed"}, {"count": 257}]:
            with self.assertRaisesRegex(ValueError, "independent positions"):
                shared_source_probe.require_large_descriptions(changed, 257, original)
