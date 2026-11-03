import copy
import unittest
from unittest.mock import patch

import consumer_loop_holder


class ConsumerLoopHolderIdentityTests(unittest.TestCase):
    def active(self, owner, uuid, loop, device, schema=2):
        return {"loop_number": loop, "block_device": device,
                "image": {"owner": owner, "filesystem_uuid": uuid, "schema": schema}}

    def start(self, previous, active):
        row = consumer_loop_holder.new_case("owned-proof")
        active = copy.deepcopy(active)
        active["image"]["owner"] = row["owner"]
        with patch.object(consumer_loop_holder.reference, "docker", return_value="owned-container"):
            consumer_loop_holder.start(
                row, "exact-image", {"lifecycle": {"bounded_store": previous}}, 123,
                lambda *args: {"status": {"lifecycle": {"bounded_store": active}}})
        return row

    def test_btrfs_holder_proves_physical_slot_without_equating_virtual_devices(self):
        old = self.active("original", "original-uuid", 1, 172)
        row = self.start(old, self.active("holder", "holder-uuid", 1, 159))
        replacement = self.active("original", "original-uuid", 2, 172)
        consumer_loop_holder.verify_replacement(row, {"lifecycle": {"bounded_store": replacement}})
        self.assertTrue(row["old_slot_held"])
        self.assertTrue(row["replacement_verified"])

    def test_physical_slot_and_exact_image_requirements_remain(self):
        old = self.active("original", "original-uuid", 1, 172)
        with self.assertRaises(ValueError):
            self.start(old, self.active("holder", "holder-uuid", 2, 172))
        row = self.start(old, self.active("holder", "holder-uuid", 1, 172))
        for active in [self.active("original", "original-uuid", 1, 999),
                       self.active("foreign", "foreign-uuid", 2, 999)]:
            with self.subTest(active=active), self.assertRaises(ValueError):
                consumer_loop_holder.verify_replacement(row, {"lifecycle": {"bounded_store": active}})

    def test_ext4_keeps_original_physical_device_assertions(self):
        old = self.active("original", "original-uuid", 1, 1793, schema=1)
        with self.assertRaises(ValueError):
            self.start(old, self.active("holder", "holder-uuid", 1, 1794, schema=1))
        row = self.start(old, self.active("holder", "holder-uuid", 1, 1793, schema=1))
        with self.assertRaises(ValueError):
            consumer_loop_holder.verify_replacement(
                row, {"lifecycle": {"bounded_store": self.active("original", "original-uuid", 2, 1793, schema=1)}})
        consumer_loop_holder.verify_replacement(
            row, {"lifecycle": {"bounded_store": self.active("original", "original-uuid", 2, 1794, schema=1)}})


if __name__ == "__main__":
    unittest.main()
