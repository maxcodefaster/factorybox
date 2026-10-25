import importlib.util
import hashlib
import json
from pathlib import Path
import errno
import struct
import unittest
from unittest.mock import patch


spec = importlib.util.spec_from_file_location(
    "independent_timer_probe", Path(__file__).resolve().parents[1] / "tests/timer-probe/probe.py")
probe = importlib.util.module_from_spec(spec)
spec.loader.exec_module(probe)


def duplicate_layout():
    btf = object.__new__(probe.BTF)
    def record(name, kind, value=0, members=(), **extra):
        return {"name": name, "kind": kind, "value": value, "members": list(members), **extra}
    btf.types = [
        None,
        record("upid", 4, 16, [("nr", 0, 0), ("ns", 0, 8)]),
        record("upid", 4, 8),
        record("", 3, element_type=1),
        record("pid", 4, 32, [("level", 0, 0), ("numbers", 3, 16)]),
        record("pid", 4, 128),
        record("", 2, 4),
        record("restart_block", 4, 64),
        record("task_struct", 4, 256, [("thread_pid", 6, 0), ("restart_block", 7, 16),
                                       ("start_boottime", 0, 80)]),
        record("task_struct", 4, 512),
        record("", 2, 8),
        record("task_alias", 8, 10),
        record("bpf_iter_meta", 4, 8, [("seq", 0, 0)]),
        record("bpf_iter_meta", 4, 32),
        record("", 2, 12),
        record("bpf_iter__task", 4, 16, [("task", 11, 0), ("meta", 14, 8)]),
    ]
    return btf


class IndependentTimerProbeLayoutTests(unittest.TestCase):
    def test_declared_port_preserves_the_original_instruction_builder_and_ownership_checks(self):
        root = Path(__file__).resolve().parents[1]
        declared = json.loads((root / "tests/compatibility/arm64-timer-probe.json").read_text())
        self.assertTrue(declared["assertions_preserved"])
        self.assertFalse(declared["upstream_assertions_modified"])
        sha = lambda value: hashlib.sha256(value.encode()).hexdigest()
        text = (root / "tests/timer-probe/probe.py").read_text()
        self.assertEqual(sha(text), declared["files"]["tests/timer-probe/probe.py"]["after_sha256"])
        program = text[text.index("class Program:"):text.index("\n\ndef task_", text.index("class Program:"))]
        ownership = text[text.index("    source_pid = int(sys.argv[-1])"):text.index("\n\nif __name__")]
        self.assertEqual(sha(program), declared["unchanged_sections"]["program_class_sha256"])
        self.assertEqual(sha(ownership), declared["unchanged_sections"]["ownership_and_result_tail_sha256"])

    def test_parser_preserves_the_array_element_reference(self):
        strings = b"\0upid\0"
        types = (struct.pack("<III", 1, 4 << 24, 16) +
                 struct.pack("<III", 0, 3 << 24, 0) +
                 struct.pack("<III", 1, 0, 1))
        raw = struct.pack("<HBB5I", 0xeb9f, 1, 0, 24, 0, len(types), len(types), len(strings))
        btf = probe.BTF(raw + types + strings)
        self.assertEqual(btf.types[2]["element_type"], 1)
        self.assertEqual(btf.referenced_aggregate(2, "upid"), 1)

    def test_iterator_references_choose_the_real_layout_among_duplicate_names(self):
        btf = duplicate_layout()
        with self.assertRaisesRegex(ValueError, "ambiguous"):
            btf.named("task_struct", 4)
        self.assertEqual(probe.task_layout(btf), (15, 8, 4, 1, 12, 7, 16, 64))

    def test_referenced_type_must_have_the_exact_name_and_bounded_valid_graph(self):
        btf = duplicate_layout()
        for identifier, name in [(8, "pid"), (0, "pid"), (-1, "pid"), (16, "pid")]:
            with self.subTest(identifier=identifier, name=name), self.assertRaises(ValueError):
                btf.referenced_aggregate(identifier, name)
        btf.types[6]["value"] = 6
        with self.assertRaisesRegex(ValueError, "recursive"):
            btf.referenced_aggregate(6, "pid")

    def test_native_syscall_numbers_are_explicit_and_other_architectures_refuse(self):
        self.assertEqual(probe.bpf_syscall_number("x86_64"), 321)
        self.assertEqual(probe.bpf_syscall_number("aarch64"), 280)
        for machine in ["arm64", "i686", "aarch64_be", "foreign"]:
            with self.subTest(machine=machine), self.assertRaises(ValueError):
                probe.bpf_syscall_number(machine)

    def test_release_enumerates_the_exact_id_without_reopening_it(self):
        for next_id in [43, 44]:
            calls = []
            def bpf(command, attr):
                calls.append(command)
                self.assertEqual(command, 11)
                self.assertEqual(len(attr), 12)
                self.assertEqual(struct.unpack_from("<I", attr)[0], 41)
                struct.pack_into("<I", attr, 4, next_id)
            probe.wait_program_released(42, bpf)
            self.assertEqual(calls, [11])
        def absent(command, attr):
            self.assertEqual(command, 11)
            raise OSError(errno.ENOENT, "absent")
        probe.wait_program_released(42, absent)

    def test_retained_id_and_failed_observation_are_not_release(self):
        calls = []
        def retained(command, attr):
            calls.append(command)
            struct.pack_into("<I", attr, 4, 42)
        with patch.object(probe.time, "sleep"), self.assertRaisesRegex(ValueError, "remains"):
            probe.wait_program_released(42, retained)
        self.assertEqual(calls, [11] * 50)
        def denied(command, attr):
            raise OSError(errno.EPERM, "denied")
        with self.assertRaises(OSError):
            probe.wait_program_released(42, denied)
        for value in [0, -1, 1 << 32]:
            with self.subTest(value=value), self.assertRaisesRegex(ValueError, "exact release ID"):
                probe.wait_program_released(value, lambda *args: self.fail("invalid ID reached kernel"))
        def wrong_id(command, attr):
            struct.pack_into("<I", attr, 4, 41)
        with self.assertRaisesRegex(ValueError, "invalid ID"):
            probe.wait_program_released(42, wrong_id)
