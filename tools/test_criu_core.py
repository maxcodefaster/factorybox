import importlib.util
import json
import pathlib
import tempfile
import unittest

import criu_core

SPEC = importlib.util.spec_from_file_location(
    "criu_core_fixture", pathlib.Path(__file__).resolve().parents[1] / "tests/criu-core.py")
fixture = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(fixture)


class CRIUCoreAdapterTests(unittest.TestCase):
    def test_native_target_requires_matching_builds_and_docker_host(self):
        amd64 = {"platform": "linux/amd64"}
        host = {"OSType": "linux", "Architecture": "x86_64"}
        self.assertEqual(criu_core.native_platform(amd64, amd64, "linux/amd64", host), "linux/amd64")
        with self.assertRaisesRegex(ValueError, "explicit native target"):
            criu_core.native_platform(amd64, amd64, None, host)
        with self.assertRaisesRegex(ValueError, "explicit native target"):
            criu_core.native_platform(amd64, {}, "linux/amd64", host)
        for other in [{"OSType": "linux", "Architecture": "aarch64"},
                      {"OSType": "windows", "Architecture": "x86_64"}]:
            with self.subTest(host=other), self.assertRaisesRegex(ValueError, "emulation"):
                criu_core.native_platform(amd64, amd64, "linux/amd64", other)
        self.assertEqual(criu_core.native_platform({}, {}, None,
                         {"OSType": "linux", "Architecture": "aarch64"}), "linux/arm64")

    def test_container_must_match_the_explicit_architecture_and_page_size(self):
        fixture.require_platform("linux/arm64", "aarch64", 4096)
        fixture.require_platform("linux/amd64", "x86_64", 4096)
        for target, machine, page in [("linux/arm64", "x86_64", 4096),
                                      ("linux/amd64", "aarch64", 4096),
                                      ("linux/amd64", "x86_64", 65536),
                                      ("linux/riscv64", "riscv64", 4096)]:
            with self.subTest(target=target, machine=machine, page=page), self.assertRaises(ValueError):
                fixture.require_platform(target, machine, page)

    def test_upstream_failure_cannot_hide_behind_later_pass(self):
        fixture.require_pass("12:00:00.001: 12: PASS\n")
        for text in ["", "12:00:00.001: 12: FAIL: data lost\n",
                     "12:00:00.001: 12: FAIL: data lost\n12:00:00.002: 12: PASS\n",
                     "12:00:00.001: 12: PASS\n12:00:00.002: 12: incomplete\n"]:
            with self.subTest(text=text), self.assertRaises(ValueError):
                fixture.require_pass(text)

    def test_descriptor_cannot_silently_drop_upstream_requirements(self):
        with tempfile.TemporaryDirectory() as temporary:
            directory = pathlib.Path(temporary)
            case = {"name": "file_locks00", "criu_args": ["--file-locks"]}
            path = directory / "file_locks00.desc"
            path.write_text("{'flags': 'excl', 'opts': '--file-locks'}")
            fixture.verify_descriptor(directory, case)
            with self.assertRaises(ValueError):
                fixture.verify_descriptor(directory, {"name": "file_locks00"})
            for descriptor in [
                    {"flavor": "ns uns"}, {"arch": "x86_64"}, {"deps": ["external"]},
                    {"flags": "noauto"}, {"flags": "suid", "opts": "--file-locks --extra"}]:
                with self.subTest(descriptor=descriptor), self.assertRaises(ValueError):
                    path.write_text(repr(descriptor))
                    fixture.verify_descriptor(directory, case)
            path.write_text("{'flags': 'excl', 'opts': '--file-locks'}")
            (directory / "file_locks00.hook").write_text("exit 0\n")
            with self.assertRaises(ValueError):
                fixture.verify_descriptor(directory, case)

    def test_recipe_has_unique_cases_and_two_real_restore_cycles(self):
        recipe = json.loads((pathlib.Path(__file__).resolve().parents[1] /
                             "tests/criu-core-cases.json").read_text())
        names = [case["name"] for case in recipe["cases"]]
        self.assertEqual(len(names), 18)
        self.assertEqual(len(set(names)), len(names))
        self.assertEqual(recipe["cycles"], 2)
        self.assertEqual(recipe["flavor"], "h")


if __name__ == "__main__":
    unittest.main()
