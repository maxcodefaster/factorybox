import hashlib
import sys
import time
import unittest
from unittest.mock import patch

import bounded_store_identity


class ImageStreamTests(unittest.TestCase):
    def test_hashes_only_the_exact_complete_image_member(self):
        writer = ('import io,sys,tarfile; b=io.BytesIO(); t=tarfile.open(fileobj=b,mode="w");'
                  'i=tarfile.TarInfo("store.img"); i.size=256;t.addfile(i,io.BytesIO(b"x"*256));'
                  't.close();sys.stdout.buffer.write(b.getvalue())')
        with patch.object(bounded_store_identity.reference, 'DOCKER', [sys.executable, '-c', writer]):
            actual = bounded_store_identity.image_digest('owned-id', '/private/store.img', 256)
        self.assertEqual(actual, {'bytes': 256, 'sha256': hashlib.sha256(b'x'*256).hexdigest()})

    def test_nonterminating_archive_producer_is_killed_at_deadline(self):
        # A real child never produces a tar header. The deadline must interrupt
        # the blocked read, not merely a subsequent process.wait call.
        start = time.monotonic()
        with patch.object(bounded_store_identity.reference, 'DOCKER', [sys.executable, '-c', 'import time;time.sleep(60)']):
            with self.assertRaises(Exception):
                bounded_store_identity.image_digest('owned-id', '/private/store.img', 256, timeout=.05)
        self.assertLess(time.monotonic()-start, 2)

    def test_changed_member_identity_is_refused(self):
        writer = ('import io,sys,tarfile; b=io.BytesIO();t=tarfile.open(fileobj=b,mode="w");'
                  'i=tarfile.TarInfo("other.img");i.size=256;t.addfile(i,io.BytesIO(b"x"*256));'
                  't.close();sys.stdout.buffer.write(b.getvalue())')
        with patch.object(bounded_store_identity.reference, 'DOCKER', [sys.executable, '-c', writer]):
            with self.assertRaisesRegex(ValueError, 'unexpected image archive member'):
                bounded_store_identity.image_digest('owned-id', '/private/store.img', 256)


if __name__ == '__main__':
    unittest.main()
