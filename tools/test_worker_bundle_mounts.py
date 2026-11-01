import importlib.util
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

import reference


spec = importlib.util.spec_from_file_location(
    "worker_bundle_mounts", reference.ROOT / "examples/worker/bundle_mounts.py")
mounts = importlib.util.module_from_spec(spec)
spec.loader.exec_module(mounts)


class WorkerBundleMountTests(unittest.TestCase):
    def test_effective_overlay_index_is_off_even_when_the_kernel_omits_its_default(self):
        for option, default, accepted in [
                (",index=off", "Y\n", True), ("", "N\n", True),
                (",index=on", "N\n", False), ("", "Y\n", False),
                ("", "unknown", False), (",index=on,index=off", "N\n", False)]:
            with self.subTest(option=option, default=default), tempfile.TemporaryDirectory() as temporary:
                root = Path(temporary).resolve() / "bundle"
                root.mkdir()
                paths = [str(root / name) for name in ("rootfs", "lower")]
                rows = {
                    paths[0]: {"mountinfo": f"1 0 0:1 / {paths[0]} rw - overlay overlay "
                               f"rw,lowerdir={paths[1]},upperdir={root}/upper,workdir={root}/work{option}"},
                    paths[1]: {"mountinfo": f"2 0 0:2 / {paths[1]} ro - ext4 /dev/loop0 ro"},
                }
                with patch.object(mounts, "mount_rows", return_value=rows), \
                        patch.object(mounts.Path, "read_text", return_value=default):
                    if accepted:
                        self.assertEqual(mounts.inventory(str(root))["mounts"], rows)
                    else:
                        with self.assertRaisesRegex(ValueError, "overlay"):
                            mounts.inventory(str(root))
