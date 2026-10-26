from pathlib import Path
import sys
import unittest

import consumer_namespace_observer as observer


class ConsumerNamespaceObserverTests(unittest.TestCase):
    def test_probe_enters_only_the_owned_target_user_namespace(self):
        self.assertEqual(observer.inside_command(1430), [
            "nsenter", "--user=/proc/1430/ns/user", "--setgid=0", "--setuid=0",
            "--", sys.executable, str(Path(observer.__file__).resolve()), "--inside", "1430"])

    def test_missing_and_nonoriginal_pid_never_start_an_observer(self):
        for pid in [None, 0, 1, -1, True, "1430", "../1430"]:
            for operation in [observer.require_pid, observer.namespace_access,
                              observer.inside_command, observer.observe]:
                with self.subTest(pid=pid, operation=operation), self.assertRaisesRegex(ValueError, "original owned"):
                    operation(pid)
