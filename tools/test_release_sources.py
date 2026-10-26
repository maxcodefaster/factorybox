import io
from pathlib import Path
import tarfile
import tempfile
import unittest

import build_worker
import release_sources


class ReleaseSourceTransportTests(unittest.TestCase):
    def test_archive_preserves_source_bytes_executable_mode_and_relative_links(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary).resolve()
            source = root / "build.sh"
            source.write_bytes(b"#!/bin/sh\necho source\n")
            source.chmod(0o755)
            link = root / "alias"
            link.symlink_to("build.sh")
            entries = {}
            release_sources.add_entry(entries, "component/build.sh", source)
            release_sources.add_entry(entries, "component/alias", link)
            output = io.BytesIO()
            release_sources.emit(entries, output)
            with tarfile.open(fileobj=io.BytesIO(output.getvalue())) as archive:
                self.assertEqual(archive.extractfile("component/build.sh").read(), source.read_bytes())
                self.assertEqual(archive.getmember("component/build.sh").mode, 0o755)
                self.assertEqual(archive.getmember("component/alias").linkname, "build.sh")
                self.assertEqual(archive.getmember("component/build.sh").mtime, 0)

    def test_changed_source_and_redirected_input_cannot_be_distributed_under_old_hash(self):
        for redirect in [False, True]:
            with self.subTest(redirect=redirect), tempfile.TemporaryDirectory() as temporary:
                root = Path(temporary).resolve()
                source = root / "source"
                source.write_bytes(b"original")
                entries = {}
                release_sources.add_entry(entries, "component/source", source)
                if redirect:
                    source.rename(root / "other")
                    source.symlink_to("other")
                else:
                    source.write_bytes(b"modified")
                with self.assertRaisesRegex(ValueError, "input changed"):
                    release_sources.emit(entries, io.BytesIO())

    def test_escaping_or_absolute_archive_links_are_rejected(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary).resolve()
            link = root / "link"
            for target in ["../../outside", "/etc/passwd"]:
                link.unlink(missing_ok=True)
                link.symlink_to(target)
                with self.assertRaises(ValueError):
                    release_sources.add_entry({}, "component/link", link)

    def test_distribution_notices_are_exact_and_redirects_are_rejected(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary).resolve()
            bundle = root / "bundle"
            bundle.mkdir()
            for name in ["NOTICE", "LICENSING.md"]:
                (root / name).write_text(name + "\n")
            hashes = build_worker.copy_distribution_notices(root, bundle)
            for name, digest in hashes.items():
                self.assertEqual(release_sources.sha(bundle / "licenses/factorybox" / name), digest)
            (root / "NOTICE").unlink()
            (root / "NOTICE").symlink_to("LICENSING.md")
            with self.assertRaises(ValueError):
                build_worker.copy_distribution_notices(root, bundle)
