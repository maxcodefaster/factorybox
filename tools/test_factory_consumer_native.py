import copy
import json
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch

import build_worker
import factory_consumer_native as consumer
import reference
import upstream_unit


class RepeatedConsumerTests(unittest.TestCase):
    def setUp(self):
        self.result = {'sandboxID': 'original', 'start': {'record': {'runtime_id': 'original-runtime'}}}
        self.previous = {'nonce': 'original-memory', 'pid': 17, 'count': 1, 'editor': 'retained-source'}
        self.calls = []
        self.work = dict(self.previous)

    def pause(self, entry):
        self.calls.append(('pause', entry['snapshot_id']))
        return {'record': {'id': 'original', 'runtime_id': 'original-runtime', 'phase': 'paused',
                           'pause': {'snapshot_id': entry['snapshot_id'], 'completed_at': 'committed',
                                     'after': {'anon': 0},
                                     'snapshot': {'file_backed': True, 'resident_bytes_after': 0}}}}

    def resume(self, entry):
        self.calls.append(('resume', entry['snapshot_id']))
        return {'record': {'id': 'original', 'runtime_id': 'original-runtime', 'phase': 'running',
                           'pause': {'snapshot_id': entry['snapshot_id'],
                                     'resume': {'completed_at': 'committed'}}}}

    def useful(self):
        self.calls.append(('useful', None))
        self.work['count'] += 1
        return dict(self.work)

    def inspect(self):
        self.calls.append(('inspect', None))
        return {'State': {'Running': True}, 'RestartCount': 0}

    def run_cycles(self, **callbacks):
        return consumer.repeated_consumer_cycles(
            4, self.result, self.previous, callbacks.get('pause', self.pause),
            callbacks.get('resume', self.resume), callbacks.get('useful', self.useful),
            callbacks.get('inspect', self.inspect), lambda stage:self.calls.append(('phase', stage)))

    def test_reuses_the_original_stack_and_confirms_each_independent_snapshot(self):
        after = self.run_cycles()
        self.assertEqual(after, {**self.previous, 'count': 4})
        entries = self.result['repeat_cycles']
        self.assertEqual([entry['cycle'] for entry in entries], [2, 3, 4])
        self.assertEqual(len({entry['snapshot_id'] for entry in entries}), 3)
        self.assertTrue(all(entry['pause_attempted'] and entry['resume_attempted'] and entry['passed']
                            for entry in entries))
        self.assertEqual([call[0] for call in self.calls],
                         ['pause', 'resume', 'useful', 'inspect', 'phase'] * 3)

    def test_failed_resume_is_retained_without_retry_or_another_pause(self):
        def refused(entry):
            self.calls.append(('resume', entry['snapshot_id']))
            raise ValueError('restored original cgroup is not populated and frozen')
        with self.assertRaisesRegex(ValueError, 'original cgroup'):
            self.run_cycles(resume=refused)
        self.assertEqual([call[0] for call in self.calls], ['pause', 'resume'])
        self.assertEqual(len(self.result['repeat_cycles']), 1)
        self.assertTrue(self.result['repeat_cycles'][0]['resume_attempted'])
        self.assertNotIn('passed', self.result['repeat_cycles'][0])

    def test_changed_identity_or_unreleased_ram_refuses_before_resume(self):
        entry = {'snapshot_id': 'a' * 32}
        view = self.pause(entry)
        changes = [
            ('id', 'foreign'), ('runtime_id', 'foreign-runtime'), ('phase', 'running'),
        ]
        for key, value in changes:
            broken = copy.deepcopy(view)
            broken['record'][key] = value
            with self.assertRaisesRegex(ValueError, 'original Sandbox'):
                consumer.require_paused_consumer(broken, 'original', 'original-runtime', entry['snapshot_id'])
        for path, value in [
                (['snapshot_id'], 'b' * 32), (['completed_at'], ''),
                (['after', 'anon'], 4096), (['snapshot', 'file_backed'], False),
                (['snapshot', 'resident_bytes_after'], 4096)]:
            broken = copy.deepcopy(view)
            target = broken['record']['pause']
            for name in path[:-1]:
                target = target[name]
            target[path[-1]] = value
            with self.assertRaisesRegex(ValueError, 'release its RAM'):
                consumer.require_paused_consumer(broken, 'original', 'original-runtime', entry['snapshot_id'])
        def retained_ram(actual):
            broken = self.pause(actual)
            broken['record']['pause']['after']['anon'] = 4096
            return broken
        with self.assertRaises(ValueError):
            self.run_cycles(pause=retained_ram)
        self.assertEqual([call[0] for call in self.calls], ['pause', 'pause'])

    def test_restart_or_changed_source_stops_without_another_cycle(self):
        for key, value in [('nonce', 'replacement'), ('pid', 18), ('count', 1), ('editor', 'foreign')]:
            self.setUp()
            def changed():
                return {**self.useful(), key:value}
            with self.assertRaisesRegex(ValueError, 'app memory'):
                self.run_cycles(useful=changed)
            self.assertEqual([call[0] for call in self.calls], ['pause', 'resume', 'useful'])
        self.setUp()
        with self.assertRaisesRegex(ValueError, 'Docker child'):
            self.run_cycles(inspect=lambda:{'State': {'Running': True}, 'RestartCount': 1})
        self.assertEqual(len(self.result['repeat_cycles']), 1)

    def test_single_cycle_adds_no_work_and_invalid_counts_refuse(self):
        self.assertIs(consumer.repeated_consumer_cycles(
            1, self.result, self.previous, self.pause, self.resume, self.useful, self.inspect, None), self.previous)
        self.assertEqual(self.calls, [])
        for count in [0, -1, True]:
            with self.assertRaises(ValueError):
                consumer.repeated_consumer_cycles(
                    count, self.result, self.previous, self.pause, self.resume, self.useful, self.inspect, None)


class NativeFactoryConsumerInputTests(unittest.TestCase):
    def test_reserve_retains_its_slot_sandbox_when_an_attempt_is_assigned(self):
        slot = 'a' * 32
        attempt = '11111111-2222-4333-8444-555555555555'
        self.assertEqual(consumer.consumer_sandbox_id(slot, attempt, True), 'runtime-'+slot)
        self.assertEqual(consumer.consumer_sandbox_id(slot, attempt, False),
                         'runtime-'+attempt.replace('-', ''))
        later_attempt = '66666666-7777-4888-8999-aaaaaaaaaaaa'
        self.assertEqual(consumer.consumer_sandbox_id(slot, later_attempt, True), 'runtime-'+slot)

    def test_diagnostic_success_never_becomes_native_acceptance(self):
        for diagnostic in [False, True]:
            result = {'passed': True, 'cleanup': True, 'host_sysctls_unchanged': True,
                      'inputs_unchanged': True, 'diagnostic_only': diagnostic}
            self.assertTrue(consumer.settle_lane_result(result))
            self.assertEqual(result['passed'], not diagnostic)
            if diagnostic:
                self.assertTrue(result['diagnostic_control_passed'])
            for failed in ['passed', 'cleanup', 'host_sysctls_unchanged', 'inputs_unchanged']:
                broken = {**result, 'passed': True, failed: False}
                self.assertFalse(consumer.settle_lane_result(broken))
                self.assertFalse(broken['passed'])
                if diagnostic:
                    self.assertFalse(broken['diagnostic_control_passed'])

    def test_preparation_delivers_only_the_selected_sandbox_files_and_release_has_no_credentials(self):
        files = {"slot.json": '{"id":"slot"}', "assignment.json": '{"runtimeAttemptId":"assigned"}',
                 "parent-token": "private-parent", "child-token": "private-child"}
        self.assertEqual(consumer.preparation_assignment(files, True),
                         {"slot.json": files["slot.json"]})
        normal = consumer.preparation_assignment(files, False)
        self.assertEqual(set(normal), {"assignment.json", "parent-token", "child-token"})
        self.assertNotIn("slot.json", normal)
        request = {"version": 3, "sandboxID": "owned", "assignment": normal}
        self.assertEqual(consumer.preparation_identity(request), {"version": 3, "sandboxID": "owned"})
        self.assertEqual(request["assignment"], normal)

    def test_readiness_uses_http_from_a_separate_owned_cgroup_before_any_worker_exec(self):
        calls = []
        def verify():
            calls.append('verified')
        def run(arguments, **options):
            self.assertEqual(calls, ['verified'])
            self.assertEqual(arguments[:6],
                             ['exec', 'owned-helper', 'nsenter', '--net=/proc/123/ns/net', '--', 'python3'])
            self.assertNotIn('owned-worker', arguments)
            self.assertIn('/readyz', arguments[-1])
            self.assertEqual(options, {'check': False, 'timeout': 5})
            calls.append('http')
            return '204\n'
        self.assertTrue(consumer.worker_http_ready(run, 'owned-worker', 'owned-helper', 123, verify))
        self.assertEqual(calls, ['verified', 'http'])

    def test_readiness_rejects_nonready_responses_and_identity_changes_without_worker_exec(self):
        for response in ['200\n', '503\n', '', '204 extra\n']:
            self.assertFalse(consumer.worker_http_ready(
                lambda *args, **kwargs: response, 'worker', 'helper', 123, lambda: None))
        def refused():
            raise RuntimeError('original worker exited')
        with patch('factory_consumer_native.reference.docker') as command:
            with self.assertRaisesRegex(RuntimeError, 'exited'):
                consumer.worker_http_ready(command, 'worker', 'helper', 123, refused)
            command.assert_not_called()
            for pid, helper in [(0, 'helper'), (123, 'worker')]:
                with self.assertRaisesRegex(ValueError, 'separate owned'):
                    consumer.worker_http_ready(command, 'worker', helper, pid, lambda: None)
            command.assert_not_called()

    def test_timed_out_container_create_retains_exact_cleanup_intent(self):
        containers = []
        command = ['run', '--name', 'owned-directory', '--label',
                   'com.factorybox.run=owned', '--entrypoint', 'mkdir', 'exact-image']
        def timeout(arguments, **options):
            self.assertEqual(arguments, command)
            self.assertEqual(containers, ['owned-directory'])
            self.assertEqual(options, {'timeout': 30})
            raise subprocess.TimeoutExpired(arguments, 30)
        with self.assertRaises(subprocess.TimeoutExpired):
            consumer.run_owned_container(timeout, containers, 'owned-directory',
                                         command[3:], timeout=30)
        self.assertEqual(containers, ['owned-directory'])

    def test_successful_container_create_registers_once_and_returns_identity(self):
        containers = []
        def run(arguments, **options):
            self.assertEqual(arguments, ['run', '--name', 'owned-hub', '--detach', 'exact-image'])
            self.assertEqual(containers, ['owned-hub'])
            return 'exact-container-id\n'
        self.assertEqual(consumer.run_owned_container(
            run, containers, 'owned-hub', ['--detach', 'exact-image']), 'exact-container-id\n')
        self.assertEqual(containers, ['owned-hub'])

    def test_native_candidate_admission_is_explicit_architecture_and_kernel_bound(self):
        for platform, machine in [('linux/amd64', 'x86_64'), ('linux/arm64', 'aarch64')]:
            host = {'OSType': 'linux', 'Architecture': machine, 'KernelVersion': 'owned-kernel'}
            ordinary = {'platform': platform}
            consumer.require_native_worker(ordinary, host, False)
            candidate = {**ordinary, 'native_candidate': build_worker.native_candidate_profile(platform, host)}
            consumer.require_native_worker(candidate, host, True)
            with self.assertRaisesRegex(ValueError, 'explicit'):
                consumer.require_native_worker(candidate, host, False)
            with self.assertRaisesRegex(ValueError, 'kernel'):
                consumer.require_native_worker(candidate, {**host, 'KernelVersion': 'different'}, True)
            with self.assertRaisesRegex(ValueError, 'matching Linux'):
                consumer.require_native_worker(ordinary, {**host, 'Architecture': 'foreign'}, False)

    def test_consumer_build_requires_frozen_current_sources_and_both_native_linux_image_roles(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary).resolve()
            name = 'factorybox-build-consumer-' + 'a' * 32
            build = root / 'artifacts' / name
            context = build / 'context'
            context.mkdir(parents=True)
            (context / 'exact.go').write_text('exact consumer source\n')
            runtime = 'owned-runtime@sha256:' + '1' * 64
            hub = 'owned-hub@sha256:' + '2' * 64
            result = {'passed': True, 'cleanup': True, 'platform': 'linux/arm64',
                      'image': runtime, 'hub_image': hub, 'hub_bun': '/opt/factory/opencode/musl/bun',
                      'source_context': upstream_unit.context_manifest(context),
                      'images': {'runtime': {'platform': 'linux/arm64', 'image': runtime},
                                 'hub': {'platform': 'linux/arm64', 'image': hub}}}
            path = build / 'result.json'
            path.write_text(json.dumps(result))
            with patch.object(reference, 'ROOT', root), \
                 patch.object(reference, 'docker', return_value='[{}]'), \
                 patch.object(consumer.native_base, 'verify_image') as verify:
                self.assertEqual(consumer.load_consumer(name, 'linux/arm64')[1], result)
                self.assertEqual(verify.call_count, 2)
                with self.assertRaisesRegex(ValueError, 'current native'):
                    consumer.load_consumer(name, 'linux/amd64')
                (context / 'exact.go').write_text('changed after image build')
                with self.assertRaisesRegex(ValueError, 'current native'):
                    consumer.load_consumer(name, 'linux/arm64')
                with self.assertRaisesRegex(ValueError, 'exact current'):
                    consumer.load_consumer('../redirected', 'linux/arm64')
