import unittest

import shared_source_probe


class SharedSourceDescriptionTests(unittest.TestCase):
    def test_closed_writable_mapping_requires_original_inode_bytes_and_access(self):
        original = {"inode": 261, "counter": 1, "mapped_hex": "01" * 16, "writable_shared": True}
        shared_source_probe.require_writable_mapping(original)
        shared_source_probe.require_writable_mapping(dict(original), original)
        for changed in [
                {**original, "inode": 0}, {**original, "inode": True},
                {**original, "counter": -1}, {**original, "writable_shared": 1},
                {**original, "mapped_hex": "unconfirmed"},
                {**original, "inode": 262}, {**original, "mapped_hex": "02" * 16},
                {**original, "counter": 2}]:
            with self.subTest(changed=changed), self.assertRaisesRegex(ValueError, "closed writable"):
                shared_source_probe.require_writable_mapping(changed, original)

    def test_large_sets_keep_every_independent_position_and_original_file_content(self):
        original = {"count": 257, "sha256": "a" * 64}
        shared_source_probe.require_large_descriptions(original, 257)
        shared_source_probe.require_large_descriptions(dict(original), 257, original)
        for changed in [
                {**original, "count": 128}, {**original, "sha256": "b" * 64},
                {**original, "sha256": "unconfirmed"}, {"count": 257}]:
            with self.assertRaisesRegex(ValueError, "independent positions"):
                shared_source_probe.require_large_descriptions(changed, 257, original)
