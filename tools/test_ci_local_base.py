import hashlib
import io
import json
from pathlib import Path
import tarfile
import tempfile
import unittest

import ci_local_base


class LocalBaseContextTests(unittest.TestCase):
    def archive(self, entries):
        output = io.BytesIO()
        with tarfile.open(fileobj=output, mode="w") as archive:
            for name, data, kind in entries:
                info = tarfile.TarInfo(name)
                info.size = len(data)
                info.type = kind
                archive.addfile(info, io.BytesIO(data))
        output.seek(0)
        return output

    def entries(self):
        manifest = b'{"schemaVersion":2}'
        digest = "sha256:" + hashlib.sha256(manifest).hexdigest()
        return digest, [
            ("oci-layout", b'{"imageLayoutVersion":"1.0.0"}', tarfile.REGTYPE),
            ("index.json", json.dumps({"schemaVersion": 2, "manifests": [{"digest": digest}]}).encode(), tarfile.REGTYPE),
            ("blobs/sha256/" + digest[7:], manifest, tarfile.REGTYPE),
        ]

    def test_exact_digest_export_preserves_its_bytes(self):
        digest, entries = self.entries()
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary) / "layout"
            ci_local_base.extract_layout(self.archive(entries), root, digest)
            self.assertEqual((root / entries[-1][0]).read_bytes(), entries[-1][1])

    def test_corrupt_missing_and_substituted_manifests_fail_closed(self):
        digest, entries = self.entries()
        cases = [entries[:-1], [*entries[:-1], (entries[-1][0], b"corrupt", tarfile.REGTYPE)]]
        for value in cases:
            with tempfile.TemporaryDirectory() as temporary, self.assertRaises(ValueError):
                ci_local_base.extract_layout(self.archive(value), Path(temporary) / "layout", digest)
        with tempfile.TemporaryDirectory() as temporary, self.assertRaises(ValueError):
            ci_local_base.extract_layout(self.archive(entries), Path(temporary) / "layout", "sha256:" + "0" * 64)

    def test_duplicate_paths_links_and_path_traversal_are_refused(self):
        digest, entries = self.entries()
        for name, kind in [("../foreign", tarfile.REGTYPE), ("blobs/sha256/" + "0" * 64, tarfile.SYMTYPE),
                           ("index.json", tarfile.REGTYPE), ("blobs/sha256/" + "0" * 64, tarfile.LNKTYPE)]:
            with tempfile.TemporaryDirectory() as temporary, self.assertRaises(ValueError):
                ci_local_base.extract_layout(self.archive([*entries, (name, b"", kind)]), Path(temporary) / "layout", digest)

    def test_only_the_exact_base_build_gets_an_immutable_context(self):
        digest, entries = self.entries()
        image = "factorybox-build-native-base-" + "a" * 32 + "@" + digest
        original = ["--context", "default", "build", "--network=none", "--no-cache", "--build-arg", "NATIVE_BASE=" + image, "-"]
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary) / "layout"
            ci_local_base.extract_layout(self.archive(entries), root, digest)
            receipt = {"image": image, "layout": str(root)}
            actual = ci_local_base.build_arguments(original, receipt)
            self.assertEqual(actual, [*original[:3], "--build-context", image + "=oci-layout://" + str(root) + "@" + digest, *original[3:]])
            for arguments in [["--context", "default", "run", image], ["--context", "default", "build", "--build-arg", "OTHER=unchanged"]]:
                self.assertEqual(ci_local_base.build_arguments(arguments, receipt), arguments)
            for base in [image.split("@")[0], image.replace("a" * 32, "b" * 32)]:
                with self.assertRaises(ValueError):
                    ci_local_base.build_arguments(["--context", "default", "build", "--build-arg", "NATIVE_BASE=" + base], receipt)


if __name__ == "__main__":
    unittest.main()
