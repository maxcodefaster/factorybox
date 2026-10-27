"""Exercise the actual ordered Bats patch, including its original failure."""
import collections
import os
from pathlib import Path
import subprocess
import unittest


ROOT = Path(__file__).resolve().parents[1]
PATCH = ROOT / "upstream/patches/sysbox-tests-systemd-proc-profile.patch"


def proc_case(prepared):
    lines = PATCH.read_text().splitlines(keepends=True)
    prefixes = {" ", "+" if prepared else "-"}
    body = "".join(line[1:] for line in lines[3:] if line[:1] in prefixes)
    body = body.replace('@test "systemd /proc exposure" {', "proc_exposure() {", 1)
    # Bats aborts on a failed assertion. macOS Bash 3.2 does not reliably apply
    # errexit to [[ ... ]] in functions, so reproduce that behavior explicitly.
    return "\n".join(line + " || return 1" if line.strip().startswith(("[ ", "[[ ")) else line
                     for line in body.splitlines()) + "\n"


def run_case(*, prepared=True, host="present", node=False, status=1,
             output="Permission denied", kmsg="Permission denied", symbols="0000000000000000"):
    # These are deterministic Docker outcomes at the upstream Bats helper seam.
    # Native Bats must separately prove them against the real kernel and Sysbox.
    helpers = r'''
set -eu
docker_run() { printf 'owned-sandbox\n'; }
wait_for_systemd_init() { :; }
docker_stop() { status=0; }
docker() {
  case "$*" in
    *"systemctl status"*) status=0; output="State: running" ;;
    *"test ! -e /proc/kcore"*) status="$NODE_EXISTS"; output="" ;;
    *"cat /proc/kcore"*) status="$READ_STATUS"; output="$READ_OUTPUT" ;;
    *"cat /proc/kmsg"*) status=1; output="$KMSG_OUTPUT" ;;
    *"cat /proc/kallsyms"*) status=0; output="$SYMBOLS"; lines=("$SYMBOLS") ;;
    *) return 99 ;;
  esac
}
'''
    return subprocess.run(
        ["bash", "-c", helpers + proc_case(prepared) + "\nproc_exposure\n"],
        env=dict(os.environ, FACTORYBOX_HOST_KCORE=host, NODE_EXISTS=str(int(node)),
                 READ_STATUS=str(status), READ_OUTPUT=output, KMSG_OUTPUT=kmsg,
                 SYMBOLS=symbols, CTR_IMG_REPO="pinned-upstream"),
        capture_output=True, text=True, timeout=5)


class SystemdProcProfileTests(unittest.TestCase):
    def test_original_failure_is_reproduced_and_absent_host_case_is_not_skipped(self):
        error = "cat: /proc/kcore: No such file or directory"
        self.assertNotEqual(run_case(prepared=False, host="absent", output=error).returncode, 0)
        self.assertEqual(run_case(host="absent", output=error).returncode, 0)
        self.assertNotRegex(proc_case(True), r"(?m)^\s*skip(?:\s|$)")

    def test_present_host_keeps_the_original_permission_denied_assertions(self):
        for prepared in [False, True]:
            self.assertEqual(run_case(prepared=prepared, node=True).returncode, 0)
            for status, output in [(0, "kernel memory"), (1, "No such file or directory"),
                                   (1, "unexpected failure")]:
                with self.subTest(prepared=prepared, status=status, output=output):
                    self.assertNotEqual(
                        run_case(prepared=prepared, node=True, status=status, output=output).returncode, 0)

    def test_absence_branch_rejects_an_exposed_node_and_every_successful_read(self):
        for node, status, output in [
            (True, 1, "No such file or directory"),
            (True, 1, "Permission denied"),
            (False, 0, "No such file or directory"),
            (False, 0, "kernel memory"),
            (False, 1, "Permission denied"),
            (False, 1, "unexpected failure"),
        ]:
            with self.subTest(node=node, status=status, output=output):
                self.assertNotEqual(run_case(host="absent", node=node, status=status,
                                             output=output).returncode, 0)

    def test_other_proc_security_assertions_remain_required_on_both_hosts(self):
        for host, node, output in [
            ("present", True, "Permission denied"),
            ("absent", False, "No such file or directory"),
        ]:
            for changed in [{"kmsg": "kernel messages"}, {"symbols": "ffffffffdeadbeef"}]:
                with self.subTest(host=host, changed=changed):
                    self.assertNotEqual(
                        run_case(host=host, node=node, output=output, **changed).returncode, 0)
        self.assertNotEqual(run_case(host="unknown").returncode, 0)

    def test_every_original_assertion_is_preserved_in_the_declared_patch(self):
        def assertions(prepared):
            return collections.Counter(line.strip() for line in proc_case(prepared).splitlines()
                                       if line.strip().startswith(("[ ", "[[ ")))
        self.assertFalse(assertions(False) - assertions(True))

    def test_host_profile_is_observed_before_sysbox_not_accepted_from_the_caller(self):
        wrapper = (ROOT / "tests/upstream-bats.sh").read_text()
        start = wrapper.index("export FACTORYBOX_HOST_KCORE=")
        self.assertLess(start, wrapper.index("sysbox-mgr --log"))
        self.assertIn('test ! -e /proc/kcore && test ! -L /proc/kcore', wrapper[:start])
