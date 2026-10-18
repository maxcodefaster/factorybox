import copy
import json
import unittest

import reference_lifecycle as runner


class LifecycleOwnershipTests(unittest.TestCase):
    def test_replacement_and_changed_owner_are_never_adopted(self):
        expected = {"name": "fixture", "uid": "original", "labels": {runner.LABEL: "owner"}}
        runner.owned(expected, "fixture", "owner", "original")
        for changed in [
                dict(expected, uid="replacement"), dict(expected, name="different"),
                dict(expected, labels={runner.LABEL: "other"}),
                dict(expected, uid="")]:
            with self.subTest(metadata=changed), self.assertRaises(ValueError):
                runner.owned(changed, "fixture", "owner", "original")

    def test_rehoming_keeps_the_full_workload_and_resource_contract(self):
        template = json.loads((runner.FIXTURE / "pod.json").read_text())
        original = copy.deepcopy(template)
        pod = runner.make_pod(template, "owned-ns", "owner", "rebuilt@sha256:digest")
        self.assertEqual(template, original)
        self.assertEqual(pod["metadata"]["namespace"], "owned-ns")
        self.assertEqual(pod["spec"]["containers"][0]["env"],
                         original["spec"]["containers"][0]["env"])
        self.assertEqual(pod["spec"]["containers"][1], original["spec"]["containers"][1])
        self.assertEqual(pod["spec"]["volumes"], original["spec"]["volumes"])
        self.assertEqual(pod["spec"]["containers"][0]["image"], "rebuilt@sha256:digest")
        self.assertEqual(pod["spec"]["containers"][0]["resources"]["limits"],
                         original["spec"]["containers"][0]["resources"]["limits"])
        self.assertEqual(pod["spec"]["containers"][0]["resources"]["requests"]["cpu"], "2500m")
        self.assertEqual(pod["spec"]["containers"][0]["args"][0].count("FACTORYBOX_EXIT="), 1)
        self.assertIn("/lab criu-probe\nstatus=$?\n", pod["spec"]["containers"][0]["args"][0])
        template["spec"]["containers"][0]["resources"]["limits"]["memory"] = "1Gi"
        with self.assertRaises(ValueError):
            runner.make_pod(template, "owned-ns", "owner", "rebuilt")

    def test_passing_exit_without_all_assertions_is_rejected(self):
        expected = {"pause-memory-released": 1, "fork-independent": 2}
        lines = [json.dumps({"name": name, "status": "passed"}) for name in
                 ["pause-memory-released", "fork-independent", "fork-independent"]]
        self.assertEqual(len(runner.verify_records("\n".join(lines), expected)), 3)
        with self.assertRaisesRegex(ValueError, "missing"):
            runner.verify_records("\n".join(lines[:-1]), expected)
        lines.append(json.dumps({"name": "resume-ram", "status": "failed"}))
        with self.assertRaisesRegex(ValueError, "resume-ram"):
            runner.verify_records("\n".join(lines), expected)

    def test_new_branch_identity_keeps_required_assertions(self):
        text = json.dumps({"name": "sysbox-branch-" + "a" * 32 + "-resumed", "status": "passed"})
        rows = runner.verify_records(text, {"sysbox-branch-IDENTITY-resumed": 1})
        self.assertEqual(len(rows), 1)

    def test_only_one_complete_terminal_result_is_accepted(self):
        self.assertIsNone(runner.terminal_status('{"name":"native-started"}\n'))
        self.assertIsNone(runner.terminal_status("FACTORYBOX_EXIT="))
        self.assertEqual(runner.terminal_status("native log\nFACTORYBOX_EXIT=0\n"), 0)
        self.assertEqual(runner.terminal_status("FACTORYBOX_EXIT=37\n"), 37)
        for invalid in ["FACTORYBOX_EXIT=256", "FACTORYBOX_EXIT=-1", "FACTORYBOX_EXIT=x",
                        "FACTORYBOX_EXIT=0\nFACTORYBOX_EXIT=0"]:
            with self.subTest(invalid=invalid), self.assertRaises(ValueError):
                runner.terminal_status(invalid + "\n")


if __name__ == "__main__":
    unittest.main()
