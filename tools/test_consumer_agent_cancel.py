import copy
from pathlib import Path
import shutil
import subprocess
import unittest

import consumer_worker_loss


@unittest.skipUnless(shutil.which("node"), "exact cancellation client requires Node")
class CancellationProcessIdentityTests(unittest.TestCase):
    def test_disappearing_proc_stat_accepts_esrch_but_never_hides_access_errors(self):
        # The actual native cancellation failed at this read with ESRCH after
        # its tool reached error. Exercise the unchanged client functions,
        # substituting only the proc read to pin that otherwise racing errno.
        program = r"""
const fs = require('node:fs'), vm = require('node:vm'), assert = require('node:assert/strict');
const source = fs.readFileSync(process.argv[1], 'utf8');
const start = source.indexOf('function identity('), end = source.indexOf('async function call(');
assert(start >= 0 && end > start);
let read;
const context = vm.createContext({fs: {readFileSync: () => read()}});
vm.runInContext(source.slice(start, end), context);
for (const code of ['ENOENT', 'ESRCH']) {
  read = () => {throw Object.assign(new Error(code), {code});};
  assert.equal(context.gone({pid: 42, birth: '100'}), true, code);
}
for (const code of ['EACCES', 'EPERM', 'EIO']) {
  read = () => {throw Object.assign(new Error(code), {code});};
  assert.throws(() => context.gone({pid: 42, birth: '100'}), error => error.code === code);
}
const stat = (state, birth) => '42 (held tool) ' + [state, ...Array(18).fill('0'), birth].join(' ');
read = () => stat('S', '100');
assert.equal(context.gone({pid: 42, birth: '100'}), false);
read = () => stat('S', '101');
assert.equal(context.gone({pid: 42, birth: '100'}), true);
for (const state of ['Z', 'X']) {
  read = () => stat(state, '100');
  assert.equal(context.gone({pid: 42, birth: '100'}), true);
}
"""
        client = Path(__file__).resolve().parents[1] / "tests/opencode-flow/cancel-client.cjs"
        completed = subprocess.run(["node", "-e", program, str(client)], capture_output=True, text=True, timeout=5)
        self.assertEqual(completed.returncode, 0, completed.stderr)


class AgentCancellationPhaseTests(unittest.TestCase):
    def test_cancellation_requires_actual_worker_replacement_phase(self):
        self.assertEqual(consumer_worker_loss.planned_phases("agent-cancel"), ("prepare", "recover"))

    def test_original_probe_phase_plans_remain(self):
        for probe in ["worker-loss", "incomplete-pause"]:
            with self.subTest(probe=probe):
                self.assertEqual(consumer_worker_loss.planned_phases(probe), ("prepare", "recover"))
        for probe in ["attached-exec", "topology", "lifecycle", "warm-preparation"]:
            with self.subTest(probe=probe):
                self.assertEqual(consumer_worker_loss.planned_phases(probe), ("prepare",))

    def test_unknown_probe_cannot_silently_become_one_phase(self):
        with self.assertRaises(ValueError):
            consumer_worker_loss.planned_phases("unplanned-new-case")

    def test_prepare_only_native_result_cannot_qualify_agent_recovery(self):
        # The first real cancellation run passed and closed only preparation.
        # Its historical result stays unchanged, but must fail this completion
        # boundary before any result can claim worker replacement.
        partial = {"phases": [{"phase": "prepare", "passed": True, "worker_removed": True}]}
        with self.assertRaisesRegex(ValueError, "both prepare and recover"):
            consumer_worker_loss.require_agent_cancel_recovery(partial)

    def test_recovery_requires_same_cancellation_new_epoch_and_independent_descendant(self):
        agent = {"session_id": "synthetic-session", "call_sha256": "a" * 64}
        def continuation(token):
            return {**agent, "completed": True, "tool_error_consumed": True, "launches": 1,
                    "tool_call_id": "call_factorybox_held", "prompt_token": token,
                    "ack": "FACTORYBOX_CANCEL_ACK_" + token, "content_sha256": "c" * 64,
                    "user_message_id": "msg_user", "assistant_message_id": "msg_assistant", "idle_message_id": "msg_idle"}
        result = {"phases": [
            {"phase": "prepare", "passed": True, "worker_removed": True,
             "startup": {"status": {"epoch": "first"}}, "agent_cancellation": {"agent": agent}},
            {"phase": "recover", "passed": True, "worker_removed": True,
             "startup": {"status": {"epoch": "second"}}, "agent_cancellation": {
                 "agent": copy.deepcopy(agent), "independent_branch": True, "source_deleted": True, "recovery_replay": True,
                 "continuations": {"recovered": continuation("d" * 32), "branch": continuation("e" * 32)}}},
        ]}
        self.assertTrue(consumer_worker_loss.require_agent_cancel_recovery(result)["passed"])
        for changed in ["passed", "worker_removed", "agent", "epoch", "independent_branch", "source_deleted", "recovery_replay"]:
            with self.subTest(changed=changed):
                candidate = copy.deepcopy(result)
                recovery = candidate["phases"][1]
                if changed in {"passed", "worker_removed"}:
                    recovery[changed] = False
                elif changed == "agent":
                    recovery["agent_cancellation"]["agent"]["call_sha256"] = "b" * 64
                elif changed == "epoch":
                    recovery["startup"]["status"]["epoch"] = "first"
                else:
                    recovery["agent_cancellation"][changed] = False
                with self.assertRaises(ValueError):
                    consumer_worker_loss.require_agent_cancel_recovery(candidate)
        for missing in ["continuations", "recovered", "branch", "tool_error_consumed", "idle_message_id", "prompt_token"]:
            with self.subTest(missing=missing):
                candidate = copy.deepcopy(result)
                recovery = candidate["phases"][1]["agent_cancellation"]
                if missing == "continuations":
                    del recovery[missing]
                elif missing in {"recovered", "branch"}:
                    del recovery["continuations"][missing]
                else:
                    del recovery["continuations"]["branch"][missing]
                with self.assertRaises(ValueError):
                    consumer_worker_loss.require_agent_cancel_recovery(candidate)
        for changed, value in [("launches", 2), ("tool_call_id", "other-call"), ("completed", False),
                               ("tool_error_consumed", False), ("session_id", "other-session"),
                               ("prompt_token", "d" * 32), ("content_sha256", "unverified")]:
            with self.subTest(changed_continuation=changed):
                candidate = copy.deepcopy(result)
                candidate["phases"][1]["agent_cancellation"]["continuations"]["branch"][changed] = value
                with self.assertRaises(ValueError):
                    consumer_worker_loss.require_agent_cancel_recovery(candidate)
